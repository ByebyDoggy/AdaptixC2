#!/bin/sh
# cleanup_persistence.sh — remove the persistence mechanisms installed by
# persist_linux.sh.
#
# This script ONLY touches artifacts carrying the "adaptix-persist" marker, so
# pre-existing SSH keys, cron entries and systemd units on the host are left
# alone. It never removes execution traces (see cleanup_traces.sh for that).
#
# Usage:
#   cleanup_persistence.sh [--dry-run] [--user root]

set -u

MARKER="adaptix-persist"
CRON_DIR="/etc/cron.d"
SYSTEMD_DIR="/etc/systemd/system"

DRY_RUN=0
USER_NAME=""

log()  { printf '[cleanup-persistence] %s\n' "$*"; }
fail() { printf '[cleanup-persistence] ERROR: %s\n' "$*" >&2; exit 1; }

while [ $# -gt 0 ]; do
    case "$1" in
        --dry-run) DRY_RUN=1; shift ;;
        --user)    USER_NAME="${2:-}"; shift 2 ;;
        --help|-h) sed -n '2,11p' "$0"; exit 0 ;;
        *)         fail "unknown argument: $1" ;;
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
        printf '[cleanup-persistence] DRY-RUN: %s\n' "$*"
    else
        "$@"
    fi
}

# --- authorized_keys -------------------------------------------------------
KEYS_FILE="$HOME_DIR/.ssh/authorized_keys"
if [ -f "$KEYS_FILE" ]; then
    # Remove only the key lines that carry the marker; unrelated keys stay.
    TMP_KEYS="$(mktemp 2>/dev/null || echo "")"
    if [ -n "$TMP_KEYS" ]; then
        kept=0
        removed=0
        while IFS= read -r line || [ -n "$line" ]; do
            case "$line" in
                *"$MARKER"*)
                    removed=$((removed + 1))
                    ;;
                *)
                    printf '%s\n' "$line" >> "$TMP_KEYS"
                    kept=$((kept + 1))
                    ;;
            esac
        done < "$KEYS_FILE"

        if [ "$removed" -gt 0 ]; then
            run cp "$TMP_KEYS" "$KEYS_FILE"
            run chmod 600 "$KEYS_FILE"
            log "removed $removed marked line(s) from $KEYS_FILE (kept $kept)"
        else
            log "no marked key lines in $KEYS_FILE"
        fi
        rm -f "$TMP_KEYS"
    fi
else
    log "no authorized_keys at $KEYS_FILE"
fi

# --- cron ------------------------------------------------------------------
if [ -d "$CRON_DIR" ]; then
    for f in "$CRON_DIR"/*; do
        [ -f "$f" ] || continue
        if grep -q -- "$MARKER" "$f" 2>/dev/null; then
            log "removing cron file $f"
            run rm -f "$f"
        fi
    done
else
    log "no $CRON_DIR directory"
fi

# --- systemd ---------------------------------------------------------------
if [ -d "$SYSTEMD_DIR" ]; then
    for f in "$SYSTEMD_DIR"/*.service "$SYSTEMD_DIR"/*.timer; do
        [ -f "$f" ] || continue
        if grep -q -- "$MARKER" "$f" 2>/dev/null; then
            unit="$(basename "$f")"
            log "removing systemd unit $unit"
            if command -v systemctl >/dev/null 2>&1; then
                run systemctl disable --now "$unit"
            fi
            run rm -f "$f"
        fi
    done
    if command -v systemctl >/dev/null 2>&1 && [ "$DRY_RUN" -eq 0 ]; then
        systemctl daemon-reload >/dev/null 2>&1 || true
    fi
else
    log "no $SYSTEMD_DIR directory"
fi

# --- launchers -------------------------------------------------------------
for f in /usr/local/bin/*.run; do
    [ -f "$f" ] || continue
    if grep -q -- "$MARKER" "$f" 2>/dev/null; then
        log "removing launcher $f"
        run rm -f "$f"
    fi
done

# --- uploaded script payloads ---------------------------------------------
for f in /tmp/.adaptix-*.sh; do
    [ -f "$f" ] || continue
    log "removing uploaded script $f"
    run rm -f "$f"
done

log "cleanup finished"
exit 0
