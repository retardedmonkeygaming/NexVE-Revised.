#!/usr/bin/env bash
###############################################################################
# NexVE – uninstaller
#
#   sudo ./installer/uninstall.sh            # remove program + service
#   sudo ./installer/uninstall.sh --purge    # also delete config + VM data
#
# VM images, ISOs and the libvirt packages are intentionally left alone
# unless --purge is given (and even then, only NexVE-owned paths are removed).
###############################################################################
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib/common.sh
. "$SCRIPT_DIR/lib/common.sh"

NEXVE_HOME="/usr/local/lib/nexve"
NEXVE_ETC="/etc/nexve"
NEXVE_STATE="/var/lib/nexve"

PURGE=0
for arg in "$@"; do
  case "$arg" in
    --purge) PURGE=1 ;;
    -h|--help) echo "usage: sudo $0 [--purge]"; exit 0 ;;
    *) die "unknown option: $arg" ;;
  esac
done

require_root "$@"
init_ui

step "Removing the NexVE service"
if systemctl list-unit-files nexved.service >/dev/null 2>&1; then
  systemctl disable --now nexved 2>/dev/null || true
fi
rm -f /etc/systemd/system/nexved.service
systemctl daemon-reload
ok "service removed"

step "Removing program files"
rm -rf "$NEXVE_HOME"
ok "removed $NEXVE_HOME"

if [[ "$PURGE" -eq 1 ]]; then
  step "Purging configuration and state"
  rm -rf "$NEXVE_ETC" "$NEXVE_STATE"
  if id nexve >/dev/null 2>&1; then
    userdel nexve 2>/dev/null || warn "could not delete the nexve user (still in use?)"
  fi
  ok "removed $NEXVE_ETC, $NEXVE_STATE and the 'nexve' user"
else
  info "kept $NEXVE_ETC and $NEXVE_STATE (use --purge to delete)"
fi

warn "hypervisor packages (qemu-kvm, libvirt, lxc, ...) were left installed"
warn "the vmbr0 bridge config was left in place – remove it manually if unwanted"

printf '%s\n' "${C_GREEN}NexVE has been uninstalled.${C_RESET}"
