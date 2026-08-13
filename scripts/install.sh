#!/usr/bin/env bash
set -euo pipefail

if [[ "${EUID}" -ne 0 ]]; then
  echo "Run this installer as root." >&2
  exit 1
fi

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
INSTALL_BIN=/usr/local/sbin/fancontrol-gui
INSTALL_SERVICE=/etc/systemd/system/fancontrol-gui.service

install -m 0755 "${SCRIPT_DIR}/fancontrol-gui.sh" "${INSTALL_BIN}"
install -m 0644 "${SCRIPT_DIR}/fancontrol-gui.service" "${INSTALL_SERVICE}"

systemctl disable --now fancontrol.service 2>/dev/null || true
systemctl daemon-reload

if [[ -f /etc/fancontrol-gui.conf ]] && grep -q '^ENABLED=1$' /etc/fancontrol-gui.conf; then
  systemctl enable --now fancontrol-gui.service
else
  systemctl disable --now fancontrol-gui.service 2>/dev/null || true
fi

echo "Installed ${INSTALL_BIN}"
echo "Installed ${INSTALL_SERVICE}"
echo "Use the GUI to create /etc/fancontrol-gui.conf and apply the selected curve."
