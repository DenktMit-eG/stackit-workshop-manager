#!/usr/bin/env bash
# Installs `stackit-ws-manager serve` as a systemd *user* service and restarts it automatically
# whenever the binary changes (e.g. after `go build`). Run as the user that owns the checkout.
#
#   scripts/install-service.sh            install/update + start
#   scripts/install-service.sh remove     stop + remove the units
#
# Units (in ~/.config/systemd/user):
#   stackit-ws-manager.service          the web UI (`serve`), restarted on failure
#   stackit-ws-manager-restart.service  oneshot: restarts the web UI
#   stackit-ws-manager.path             watches the binary, triggers the restart unit on change
set -euo pipefail
cd "$(dirname "$0")/.."
DIR=$(pwd)
BIN="$DIR/stackit-ws-manager"
UNIT_DIR="$HOME/.config/systemd/user"
export XDG_RUNTIME_DIR="${XDG_RUNTIME_DIR:-/run/user/$(id -u)}"

if [ "${1:-}" = "remove" ]; then
  systemctl --user disable --now stackit-ws-manager.path stackit-ws-manager.service 2>/dev/null || true
  rm -f "$UNIT_DIR"/stackit-ws-manager.service "$UNIT_DIR"/stackit-ws-manager-restart.service "$UNIT_DIR"/stackit-ws-manager.path
  systemctl --user daemon-reload
  echo "removed"
  exit 0
fi

[ -x "$BIN" ] || go build -o "$BIN" .
[ -f config.yaml ] || "$BIN" init

# keep the user manager (and thus the service) running without an open login session
if [ "$(loginctl show-user "$USER" -p Linger --value 2>/dev/null)" != "yes" ]; then
  sudo loginctl enable-linger "$USER"
fi

mkdir -p "$UNIT_DIR"
cat > "$UNIT_DIR/stackit-ws-manager.service" <<UNIT
[Unit]
Description=stackit-ws-manager web UI
After=network-online.target

[Service]
WorkingDirectory=$DIR
ExecStart=$BIN --config $DIR/config.yaml serve
Restart=on-failure
RestartSec=3

[Install]
WantedBy=default.target
UNIT

cat > "$UNIT_DIR/stackit-ws-manager-restart.service" <<UNIT
[Unit]
Description=restart stackit-ws-manager after the binary changed

[Service]
Type=oneshot
# go build writes the file in several steps; give it a moment to finish
ExecStartPre=/bin/sleep 2
ExecStart=/usr/bin/systemctl --user restart stackit-ws-manager.service
UNIT

cat > "$UNIT_DIR/stackit-ws-manager.path" <<UNIT
[Unit]
Description=watch the stackit-ws-manager binary and restart the service on change

[Path]
PathChanged=$BIN
Unit=stackit-ws-manager-restart.service

[Install]
WantedBy=default.target
UNIT

systemctl --user daemon-reload
systemctl --user enable --now stackit-ws-manager.service stackit-ws-manager.path
systemctl --user restart stackit-ws-manager.service
sleep 1
systemctl --user --no-pager --lines=3 status stackit-ws-manager.service stackit-ws-manager.path || true
echo
echo "logs:    journalctl --user -u stackit-ws-manager -f"
echo "rebuild: go build -o stackit-ws-manager .   (the service restarts by itself)"
