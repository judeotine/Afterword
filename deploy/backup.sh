#!/bin/sh
set -eu

STAGING_DIR=${STAGING_DIR:-/backups}
BACKUP_PREFIX=${BACKUP_PREFIX:-afterword}
BACKUP_RETENTION_DAYS=${BACKUP_RETENTION_DAYS:-30}
BACKUP_HOUR_UTC=${BACKUP_HOUR_UTC:-3}
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

dump_database() {
    dump_file=$1
    log "dumping postgres to $dump_file"
    pg_dump --format=custom --compress=9 --no-owner --no-acl --file "$dump_file" "$DATABASE_URL"
    pg_restore --list "$dump_file" >/dev/null
    log "dump verified, $(wc -c <"$dump_file") bytes"
}

upload_dump() {
    dump_file=$1
    target="$(offsite_root)/postgres/$(basename "$dump_file")"
    log "uploading $target"
    rclone copyto "$dump_file" "$target"
}

mirror_objects() {
    for bucket in $BUCKETS; do
        log "mirroring bucket $bucket"
        rclone sync "minio:$bucket" "$(offsite_root)/objects/$bucket" --fast-list --transfers 4
    done
}

prune_old_backups() {
    log "pruning dumps older than $BACKUP_RETENTION_DAYS days"
    find "$STAGING_DIR" -type f -name 'postgres-*.dump' -mtime "+$BACKUP_RETENTION_DAYS" -delete
    rclone delete "$(offsite_root)/postgres" --min-age "${BACKUP_RETENTION_DAYS}d"
}

run_backup() {
    started=$(date -u +%s)
    stamp=$(date -u +%Y%m%dT%H%M%SZ)
    dump_file="$STAGING_DIR/postgres-$stamp.dump"
    dump_database "$dump_file"
    upload_dump "$dump_file"
    mirror_objects
    prune_old_backups
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
        if ! run_backup; then
            log "backup failed, retrying at the next scheduled hour"
        fi
    done
}

case "${1:---once}" in
    --once)
        require_environment
        run_backup
        ;;
    --loop)
        require_environment
        run_loop
        ;;
    *)
        fail "$USAGE"
        ;;
esac
