#!/bin/sh
set -eu

STAGING_DIR=${STAGING_DIR:-/backups}
BACKUP_PREFIX=${BACKUP_PREFIX:-afterword}
BACKUP_RETENTION_DAYS=${BACKUP_RETENTION_DAYS:-30}
BACKUP_HOUR_UTC=${BACKUP_HOUR_UTC:-3}
BACKUP_MAX_DELETE=${BACKUP_MAX_DELETE:-1000}
BUCKETS=${BUCKETS:-audio transcripts clips exports}
USAGE="usage: backup.sh [--once|--loop]"

log() {
    printf '%s afterword-backup: %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$1"
}

fail() {
    log "$1"
    exit 1
}

require_variable() {
    if [ -z "$(printenv "$1" || true)" ]; then
        fail "$1 is required"
    fi
}

require_environment() {
    require_variable DATABASE_URL
    require_variable BACKUP_BUCKET
    require_variable RCLONE_CONFIG_OFFSITE_ENDPOINT
    require_variable RCLONE_CONFIG_OFFSITE_ACCESS_KEY_ID
    require_variable RCLONE_CONFIG_OFFSITE_SECRET_ACCESS_KEY
    mkdir -p "$STAGING_DIR"
}

offsite_root() {
    printf 'offsite:%s/%s' "$BACKUP_BUCKET" "$BACKUP_PREFIX"
}

object_count() {
    rclone size --json "$1" 2>/dev/null | sed -n 's/.*"count":[[:space:]]*\([0-9][0-9]*\).*/\1/p'
}

dump_database() {
    dump_file=$1
    log "dumping postgres to $dump_file"
    pg_dump --format=custom --compress=9 --no-owner --no-acl --file "$dump_file" "$DATABASE_URL" || return 1
    pg_restore --list "$dump_file" >/dev/null || return 1
    log "dump verified, $(wc -c <"$dump_file") bytes"
}

upload_dump() {
    dump_file=$1
    target="$(offsite_root)/postgres/$(basename "$dump_file")"
    log "uploading $target"
    rclone copyto "$dump_file" "$target" || return 1
    rclone lsf "$target" >/dev/null || return 1
    log "upload verified"
}

mirror_bucket() {
    bucket=$1
    stamp=$2
    source="minio:$bucket"
    destination="$(offsite_root)/objects/$bucket"

    source_objects=$(object_count "$source")
    source_objects=${source_objects:-}
    if [ -z "$source_objects" ]; then
        log "refusing to mirror $bucket: could not count objects in $source"
        return 1
    fi

    if [ "$source_objects" -eq 0 ]; then
        destination_objects=$(object_count "$destination")
        destination_objects=${destination_objects:-0}
        if [ "$destination_objects" -gt 0 ]; then
            log "refusing to mirror $bucket: source is empty but $destination_objects objects exist off site"
            return 1
        fi
        log "skipping $bucket: source and destination are both empty"
        return 0
    fi

    log "mirroring $bucket, $source_objects objects"
    rclone sync "$source" "$destination" \
        --max-delete "$BACKUP_MAX_DELETE" \
        --backup-dir "$(offsite_root)/deleted/$stamp/$bucket" \
        --fast-list \
        --transfers 4 || return 1
}

mirror_objects() {
    stamp=$1
    failures=0
    for bucket in $BUCKETS; do
        if mirror_bucket "$bucket" "$stamp"; then
            continue
        fi
        log "mirror of $bucket failed, continuing with the remaining buckets"
        failures=$(( failures + 1 ))
    done
    if [ "$failures" -gt 0 ]; then
        log "$failures of the configured buckets failed to mirror"
        return 1
    fi
}

prune_offsite() {
    log "pruning off-site copies older than $BACKUP_RETENTION_DAYS days"
    rclone delete "$(offsite_root)/postgres" --min-age "${BACKUP_RETENTION_DAYS}d" || return 1
    rclone delete "$(offsite_root)/deleted" --min-age "${BACKUP_RETENTION_DAYS}d" || return 1
    rclone rmdirs "$(offsite_root)/deleted" --leave-root || return 1
}

run_backup() {
    started=$(date -u +%s)
    stamp=$(date -u +%Y%m%dT%H%M%SZ)
    dump_file="$STAGING_DIR/postgres-$stamp.dump"

    dump_database "$dump_file" || return 1
    upload_dump "$dump_file" || return 1
    rm -f "$dump_file" || return 1

    mirrored=0
    mirror_objects "$stamp" || mirrored=1

    pruned=0
    prune_offsite || pruned=1

    if [ "$mirrored" -ne 0 ] || [ "$pruned" -ne 0 ]; then
        return 1
    fi

    log "backup complete in $(( $(date -u +%s) - started ))s"
}

seconds_until_backup_hour() {
    now=$(date -u +%s)
    target=$(( now - (now % 86400) + BACKUP_HOUR_UTC * 3600 ))
    while [ "$target" -le "$now" ]; do
        target=$(( target + 86400 ))
    done
    printf '%s' "$(( target - now ))"
}

run_loop() {
    log "nightly backups scheduled for ${BACKUP_HOUR_UTC}:00 UTC"
    while true; do
        delay=$(seconds_until_backup_hour)
        log "next backup in ${delay}s"
        sleep "$delay"
        if run_backup; then
            continue
        fi
        log "backup failed, retrying at the next scheduled hour"
    done
}

case "${1:---once}" in
    --once)
        require_environment
        if run_backup; then
            exit 0
        fi
        fail "backup failed"
        ;;
    --loop)
        require_environment
        run_loop
        ;;
    *)
        fail "$USAGE"
        ;;
esac
