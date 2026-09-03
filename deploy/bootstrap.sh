#!/usr/bin/env bash
set -euo pipefail

APP_USER=${APP_USER:-afterword}
APP_HOME=${APP_HOME:-/home/$APP_USER}
APP_DIR=${APP_DIR:-/opt/afterword}
DEPLOY_DIR="$APP_DIR/deploy"
COMPOSE_FILE="$DEPLOY_DIR/docker-compose.yml"
ENV_FILE="$DEPLOY_DIR/.env"
ENV_EXAMPLE="$DEPLOY_DIR/.env.example"
API_ENV_FILE="$DEPLOY_DIR/api.env"
API_ENV_EXAMPLE="$DEPLOY_DIR/api.env.example"
DOCKER_DAEMON_CHANGED=false
REPO_URL=${REPO_URL:-git@github.com:judeotine/Afterword.git}
REPO_BRANCH=${REPO_BRANCH:-main}
DEPLOY_KEY=${DEPLOY_KEY:-$APP_HOME/.ssh/id_ed25519}
HEALTH_ATTEMPTS=${HEALTH_ATTEMPTS:-60}
HEALTH_INTERVAL=${HEALTH_INTERVAL:-5}

log() {
    printf '%s afterword-bootstrap: %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$1"
}

fail() {
    log "$1"
    exit 1
}

require_root() {
    [ "$(id -u)" = "0" ] || fail "run this script as root"
}

run_as_app_user() {
    runuser -u "$APP_USER" -- "$@"
}

git_as_app_user() {
    runuser -u "$APP_USER" -- env \
        GIT_SSH_COMMAND="ssh -i $DEPLOY_KEY -o IdentitiesOnly=yes -o StrictHostKeyChecking=accept-new" \
        git "$@"
}

compose() {
    run_as_app_user docker compose -f "$COMPOSE_FILE" "$@"
}

env_value() {
    sed -n "s/^$1=//p" "$ENV_FILE" | tail -n1
}

install_base_packages() {
    log "installing base packages"
    export DEBIAN_FRONTEND=noninteractive
    apt-get update -qq
    apt-get install -y -qq \
        ca-certificates \
        curl \
        git \
        gnupg \
        fail2ban \
        ufw \
        unattended-upgrades
}

configure_docker_daemon() {
    log "configuring docker log caps"
    install -d -m 0755 /etc/docker
    staged=$(mktemp)
    cat >"$staged" <<'CONF'
{
  "log-driver": "json-file",
  "log-opts": {
    "max-size": "10m",
    "max-file": "3"
  }
}
CONF
    if [ -f /etc/docker/daemon.json ] && cmp -s "$staged" /etc/docker/daemon.json; then
        rm -f "$staged"
        return 0
    fi
    if [ -f /etc/docker/daemon.json ]; then
        cp /etc/docker/daemon.json "/etc/docker/daemon.json.$(date -u +%Y%m%dT%H%M%SZ).bak"
    fi
    install -m 0644 "$staged" /etc/docker/daemon.json
    rm -f "$staged"
    DOCKER_DAEMON_CHANGED=true
}

install_docker() {
    if command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1; then
        log "docker and the compose plugin are already installed"
    else
        log "installing docker engine and the compose plugin"
        install -m 0755 -d /etc/apt/keyrings
        curl -fsSL https://download.docker.com/linux/ubuntu/gpg -o /etc/apt/keyrings/docker.asc
        chmod a+r /etc/apt/keyrings/docker.asc
        printf 'deb [arch=%s signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/ubuntu %s stable\n' \
            "$(dpkg --print-architecture)" \
            "$(awk -F= '$1=="VERSION_CODENAME"{gsub(/"/,"",$2); print $2}' /etc/os-release)" \
            >/etc/apt/sources.list.d/docker.list
        apt-get update -qq
        apt-get install -y -qq \
            docker-ce \
            docker-ce-cli \
            containerd.io \
            docker-buildx-plugin \
            docker-compose-plugin
    fi
    systemctl enable --now docker
    if [ "$DOCKER_DAEMON_CHANGED" = true ]; then
        systemctl restart docker
    fi
}

create_app_user() {
    if id -u "$APP_USER" >/dev/null 2>&1; then
        log "user $APP_USER already exists"
    else
        log "creating user $APP_USER"
        useradd --create-home --home-dir "$APP_HOME" --shell /bin/bash "$APP_USER"
    fi
    usermod -aG docker "$APP_USER"
    install -d -m 0700 -o "$APP_USER" -g "$APP_USER" "$APP_HOME/.ssh"
}

configure_firewall() {
    log "configuring ufw"
    ufw --force default deny incoming
    ufw --force default allow outgoing
    ufw allow 22/tcp
    ufw allow 80/tcp
    ufw allow 443/tcp
    ufw allow 443/udp
    ufw --force enable
}

configure_fail2ban() {
    log "configuring fail2ban"
    cat >/etc/fail2ban/jail.d/afterword.local <<'CONF'
[sshd]
enabled = true
port = ssh
maxretry = 5
findtime = 10m
bantime = 1h
CONF
    systemctl enable --now fail2ban
    systemctl reload fail2ban || systemctl restart fail2ban
}

configure_unattended_upgrades() {
    log "configuring unattended upgrades"
    cat >/etc/apt/apt.conf.d/20auto-upgrades <<'CONF'
APT::Periodic::Update-Package-Lists "1";
APT::Periodic::Unattended-Upgrade "1";
APT::Periodic::AutocleanInterval "7";
CONF
    systemctl enable --now unattended-upgrades
}

check_deploy_key() {
    [ -f "$DEPLOY_KEY" ] || fail "no deploy key at $DEPLOY_KEY, install the repository read key there with mode 600"
    chown "$APP_USER:$APP_USER" "$DEPLOY_KEY"
    chmod 600 "$DEPLOY_KEY"
}

sync_repository() {
    install -d -o "$APP_USER" -g "$APP_USER" "$(dirname "$APP_DIR")"
    if [ -d "$APP_DIR/.git" ]; then
        log "updating $APP_DIR"
        chown -R "$APP_USER:$APP_USER" "$APP_DIR"
        git_as_app_user -C "$APP_DIR" fetch --prune origin
        git_as_app_user -C "$APP_DIR" checkout "$REPO_BRANCH"
        git_as_app_user -C "$APP_DIR" merge --ff-only "origin/$REPO_BRANCH"
    else
        log "cloning $REPO_URL into $APP_DIR"
        install -d -o "$APP_USER" -g "$APP_USER" "$APP_DIR"
        git_as_app_user clone --branch "$REPO_BRANCH" "$REPO_URL" "$APP_DIR"
    fi
}

prepare_environment() {
    created=""
    for pair in "$ENV_FILE:$ENV_EXAMPLE" "$API_ENV_FILE:$API_ENV_EXAMPLE"; do
        target=${pair%%:*}
        example=${pair#*:}
        if [ -f "$target" ]; then
            chown "$APP_USER:$APP_USER" "$target"
            chmod 600 "$target"
            continue
        fi
        install -m 600 -o "$APP_USER" -g "$APP_USER" "$example" "$target"
        log "wrote $target from $(basename "$example")"
        created="$created $target"
    done
    if [ -n "$created" ]; then
        log "fill in:$created"
        log "deploy/.env needs DOMAIN, ACME_EMAIL, TAG, the database and MinIO passwords, PRIVACY_URL and the BACKUP_ values"
        log "deploy/api.env needs JWT_SECRET and the SMTP credentials"
        log "then run this script again"
        exit 0
    fi
}

start_stack() {
    log "pulling images"
    compose pull
    log "starting the stack"
    compose up -d
}

run_migrations() {
    log "running database migrations"
    compose run --rm api /migrate up
}

wait_for_health() {
    local domain url attempt
    domain=$(env_value DOMAIN)
    [ -n "$domain" ] || fail "DOMAIN is not set in $ENV_FILE"
    url="https://api.$domain/healthz"
    log "waiting for $url"
    for attempt in $(seq 1 "$HEALTH_ATTEMPTS"); do
        if curl -fsS --max-time 10 "$url" >/dev/null; then
            log "api healthy after $attempt attempts"
            return 0
        fi
        sleep "$HEALTH_INTERVAL"
    done
    compose logs --tail 50 api caddy || true
    fail "api did not become healthy at $url"
}

main() {
    require_root
    install_base_packages
    configure_docker_daemon
    install_docker
    create_app_user
    configure_firewall
    configure_fail2ban
    configure_unattended_upgrades
    check_deploy_key
    sync_repository
    prepare_environment
    start_stack
    run_migrations
    wait_for_health
    log "bootstrap complete"
}

main "$@"
