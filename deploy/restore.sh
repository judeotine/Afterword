#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
COMPOSE_FILE="$SCRIPT_DIR/docker-compose.yml"
ENV_FILE="$SCRIPT_DIR/.env"
APP_SERVICES=(caddy api web transcribe-worker bot-worker backup)
USAGE="usage: restore.sh --list | restore.sh --dump <postgres-STAMP.dump> [--objects] --yes"

DUMP_NAME=""
RESTORE_OBJECTS=false
CONFIRMED=false
LIST_ONLY=false

log() {
    printf '%s afterword-restore: %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$1"
}

fail() {
    log "$1"
    exit 1
}

compose() {
    docker compose --profile web --profile workers -f "$COMPOSE_FILE" "$@"
}

env_value() {
    sed -n "s/^$1=//p" "$ENV_FILE" | tail -n1
}

parse_arguments() {
    while [ $# -gt 0 ]; do
        case "$1" in
            --list)
                LIST_ONLY=true
                ;;
            --dump)
                [ $# -ge 2 ] || fail "$USAGE"
                DUMP_NAME="$2"
                shift
                ;;
            --objects)
                RESTORE_OBJECTS=true
                ;;
            --yes)
                CONFIRMED=true
                ;;
            -h|--help)
                printf '%s\n' "$USAGE"
                exit 0
                ;;
            *)
                fail "unknown argument $1, $USAGE"
                ;;
        esac
        shift
    done
}

check_preconditions() {
    [ -f "$COMPOSE_FILE" ] || fail "no compose file at $COMPOSE_FILE"
    [ -f "$ENV_FILE" ] || fail "no environment file at $ENV_FILE"
}

list_script() {
    cat <<'EOS'
set -eu
apk add --no-cache rclone >/dev/null
rclone lsf "offsite:$BACKUP_BUCKET/${BACKUP_PREFIX:-afterword}/postgres/"
EOS
}

database_script() {
    cat <<'EOS'
set -eu
apk add --no-cache postgresql16-client rclone >/dev/null
if [ ! -f "/backups/$DUMP_NAME" ]; then
    rclone copyto "offsite:$BACKUP_BUCKET/${BACKUP_PREFIX:-afterword}/postgres/$DUMP_NAME" "/backups/$DUMP_NAME"
fi
pg_restore --clean --if-exists --no-owner --no-acl --exit-on-error --dbname "$DATABASE_URL" "/backups/$DUMP_NAME"
EOS
}

objects_script() {
    cat <<'EOS'
set -eu
apk add --no-cache rclone >/dev/null
for bucket in $BUCKETS; do
    rclone sync "offsite:$BACKUP_BUCKET/${BACKUP_PREFIX:-afterword}/objects/$bucket" "minio:$bucket"
done
EOS
}

list_backups() {
    log "dumps available offsite"
    compose run --rm --no-deps --entrypoint /bin/sh backup -c "$(list_script)"
}

stop_application_services() {
    log "stopping ${APP_SERVICES[*]}"
    compose stop "${APP_SERVICES[@]}"
}

start_database() {
    log "starting postgres"
    compose up -d postgres
    local user database
    user=$(env_value POSTGRES_USER)
    database=$(env_value POSTGRES_DB)
    for _ in $(seq 1 60); do
        if compose exec -T postgres pg_isready -q -U "$user" -d "$database"; then
            return 0
        fi
        sleep 2
    done
    fail "postgres did not become ready"
}

restore_database() {
    log "restoring $DUMP_NAME into postgres"
    compose run --rm --no-deps -e DUMP_NAME="$DUMP_NAME" --entrypoint /bin/sh backup -c "$(database_script)"
}

restore_objects() {
    log "starting minio"
    compose up -d minio
    log "restoring object buckets"
    compose run --rm --no-deps --entrypoint /bin/sh backup -c "$(objects_script)"
}

main() {
    parse_arguments "$@"
    check_preconditions

    if [ "$LIST_ONLY" = true ]; then
        list_backups
        exit 0
    fi

    [ -n "$DUMP_NAME" ] || fail "$USAGE"
    if [ "$CONFIRMED" != true ]; then
        fail "this replaces the live database and refuses to run without --yes"
    fi

    stop_application_services
    start_database
    restore_database
    if [ "$RESTORE_OBJECTS" = true ]; then
        restore_objects
    fi

    log "restore complete, bring the stack back with: docker compose -f $COMPOSE_FILE up -d"
}

main "$@"
