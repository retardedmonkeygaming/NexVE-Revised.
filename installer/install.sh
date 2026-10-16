#!/usr/bin/env bash
###############################################################################
# NexVE – modern installer for Debian 10 (buster) and newer
#
#   sudo ./installer/install.sh              # interactive (whiptail TUI)
#   sudo ./installer/install.sh --yes        # fully non-interactive
#   sudo ./installer/install.sh --help       # all options
#
# What it does:
#   1.  verifies Debian 10+ on x86_64 (i386 best-effort, --force to override)
#   2.  detects host firmware (UEFI / legacy BIOS) – reported, never modified
#   3.  installs qemu-kvm, libvirt, lxc, bridge-utils, ovmf, debootstrap, ...
#   4.  creates the dedicated 'nexve' system service account
#   5.  installs the single nexved binary (web UI + REST API are embedded)
#   6.  creates the vmbr0 network bridge (persistent + applied live)
#   7.  generates a self-signed TLS certificate for the web UI
#   8.  writes /etc/nexve/nexve.env with generated auth secrets
#   9.  installs + enables the nexved systemd service
#  10.  brings up the libvirt default network + storage pool
#  11.  prints:  "NexVE is ready – open https://<ip>:8006"
#
# The script is idempotent: re-running it upgrades files and keeps existing
# secrets/config.
###############################################################################
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
# shellcheck source=lib/common.sh
. "$SCRIPT_DIR/lib/common.sh"
# shellcheck source=lib/net.sh
. "$SCRIPT_DIR/lib/net.sh"

NEXVE_HOME="/usr/local/lib/nexve"        # where the binary lives
NEXVE_ETC="/etc/nexve"                   # config + TLS material
NEXVE_STATE="/var/lib/nexve"             # runtime state (binary data dir)
NEXVE_USER="nexve"
NEXVE_PORT_DEFAULT="8006"
BRIDGE_PERSISTED=""
BRIDGE_APPLIED=""

usage() {
  cat <<EOF
NexVE installer ${NEXVE_VERSION}  –  Debian 10+ / x86_64 (i386 best-effort)

Usage: sudo $0 [options]

Options:
  -y, --yes            non-interactive: accept every default (implies --force answers)
  -f, --force          continue on non-Debian/unsupported architectures
  -p, --port PORT      web UI/API port (default: ${NEXVE_PORT_DEFAULT})
  -i, --iface IFACE    physical NIC to enslave into vmbr0 (default: default-route NIC)
      --bridge NAME    bridge name (default: vmbr0)
      --no-bridge      skip network bridge setup entirely
      --no-start       install but do not start/enable the service
      --uninstall      remove the NexVE installation (keeps VM data unless --purge)
      --purge          with --uninstall: also remove config and VM data
  -h, --help           show this help
  -V, --version        print version
EOF
}

# ---------------------------------------------------------------------------
# Arguments
# ---------------------------------------------------------------------------
NEXVE_YES=""; NEXVE_FORCE=""; NEXVE_PORT="$NEXVE_PORT_DEFAULT"
NEXVE_NO_START=""; NEXVE_UNINSTALL=0; NEXVE_PURGE=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    -y|--yes)         NEXVE_YES=1; shift ;;
    -f|--force)       NEXVE_FORCE=1; shift ;;
    -p|--port)        NEXVE_PORT="${2:?--port needs a value}"; shift 2 ;;
    -i|--iface)       NEXVE_IFACE="${2:?--iface needs a value}"; shift 2 ;;
    --bridge)         NEXVE_BRIDGE="${2:?--bridge needs a value}"; shift 2 ;;
    --no-bridge)      NEXVE_NO_BRIDGE=1; shift ;;
    --no-start)       NEXVE_NO_START=1; shift ;;
    --uninstall)      NEXVE_UNINSTALL=1; shift ;;
    --purge)          NEXVE_PURGE=1; shift ;;
    -h|--help)        usage; exit 0 ;;
    -V|--version)     echo "NexVE installer ${NEXVE_VERSION}"; exit 0 ;;
    *)                err "unknown option: $1"; usage; exit 2 ;;
  esac
done
export NEXVE_YES NEXVE_FORCE NEXVE_IFACE NEXVE_BRIDGE NEXVE_NO_BRIDGE 2>/dev/null || true

# --uninstall delegates to the dedicated script
if [[ "$NEXVE_UNINSTALL" -eq 1 ]]; then
  exec bash "$SCRIPT_DIR/uninstall.sh" $([[ -n "$NEXVE_PURGE" ]] && echo --purge)
fi

[[ "$NEXVE_PORT" =~ ^[0-9]{1,5}$ ]] || die "--port must be numeric"

require_root "$@"
init_ui

cat <<EOF
${C_BOLD}${C_CYAN}
  _   _ _____ _____ ____   _____ __  __ ____  ____
 | \ | | ____|  ___/ ___| | ____|  \/  |  _ \|  _ \\
 |  \| |  _| | |_  \___ \ |  _| | |\/| | |_) | | | |
 | |\  | |___| |___ ___) || |___| |  | |  _ <| |_| |
 |_| \_|_____|_____|____/ |_____|_|  |_|_| \_\_| \\_\\
${C_RESET}${C_BOLD}  open-source Type-1 hypervisor – installer v${NEXVE_VERSION}${C_RESET}

EOF

# ---------------------------------------------------------------------------
# 1. Preflight checks
# ---------------------------------------------------------------------------
step "Preflight checks"
check_supported_os
detect_arch
detect_firmware
info "firmware mode recorded: $NEXVE_FIRMWARE (BIOS and UEFI both supported; host bootloader untouched)"

if [[ ! -e /dev/kvm ]]; then
  warn "/dev/kvm is not present – virtualization may be disabled in BIOS/UEFI firmware"
  warn "enable Intel VT-x / AMD-V (and 'Intel SVM'/'AMD IOMMU') in the firmware setup"
else
  ok "/dev/kvm available"
fi

# ---------------------------------------------------------------------------
# 2. Packages
# ---------------------------------------------------------------------------
PACKAGES=(
  # virtualization core
  qemu-kvm qemu-utils
  libvirt-daemon-system libvirt-clients
  libvirt-daemon-driver-lxc   # best-effort LXC driver (skipped if unavailable)
  lxc debootstrap
  # networking
  bridge-utils iproute2 dnsmasq-base
  # guest firmware (UEFI guests) + tooling
  ovmf
  # installer prerequisites
  curl ca-certificates openssl whiptail gawk
)
install_packages "${PACKAGES[@]}"

# ---------------------------------------------------------------------------
# 3. Service account + directories
# ---------------------------------------------------------------------------
step "Creating the '${NEXVE_USER}' system account"
if ! id "$NEXVE_USER" >/dev/null 2>&1; then
  useradd --system --home-dir "$NEXVE_STATE" --create-home \
          --shell /usr/sbin/nologin --comment "NexVE hypervisor daemon" "$NEXVE_USER"
  ok "created system user '$NEXVE_USER'"
else
  ok "system user '$NEXVE_USER' already exists"
fi
# libvirt group = talk to qemu:///system, kvm group = access /dev/kvm
usermod -aG libvirt,kvm "$NEXVE_USER" 2>/dev/null || warn "could not add $NEXVE_USER to libvirt/kvm groups"

mkdir -p "$NEXVE_HOME" "$NEXVE_ETC/tls" "$NEXVE_STATE" \
         /var/lib/libvirt/images/iso /var/lib/libvirt/filesystems
chown -R "$NEXVE_USER:$NEXVE_USER" "$NEXVE_STATE"
chmod 750 "$NEXVE_ETC/tls"

# ---------------------------------------------------------------------------
# 4. Install the backend binary (single binary: REST API + embedded web UI)
# ---------------------------------------------------------------------------
step "Installing the NexVE backend"
find_nexved() {
  local cand
  for cand in "$SCRIPT_DIR/nexved" "$REPO_ROOT/dist/nexved" "$SCRIPT_DIR/../dist/nexved"; do
    if [[ -x "$cand" ]]; then printf '%s' "$cand"; return 0; fi
  done
  return 1
}
NEXVED_SRC="$(find_nexved || true)"
if [[ -z "$NEXVED_SRC" ]]; then
  err "nexved binary not found next to the installer or in <repo>/dist/"
  die "build it first:  make build   (or unpack a release bundle containing dist/nexved)"
fi
install -m 0755 -o root -g root "$NEXVED_SRC" "$NEXVE_HOME/nexved"
ok "installed $NEXVE_HOME/nexved ($("$NEXVE_HOME/nexved" --version 2>/dev/null || echo 'version unknown'))"

# ---------------------------------------------------------------------------
# 5. Network bridge (vmbr0)
# ---------------------------------------------------------------------------
configure_bridge || warn "bridge setup did not complete cleanly – VMs may lack networking"

# ---------------------------------------------------------------------------
# 6. TLS certificate for the web UI (self-signed, 10 years, SAN = IP + hostname)
# ---------------------------------------------------------------------------
step "Generating self-signed TLS certificate"
CERT="$NEXVE_ETC/tls/cert.pem"; KEY="$NEXVE_ETC/tls/key.pem"
if [[ -f "$CERT" && -f "$KEY" && -z "${NEXVE_FORCE:-}" ]]; then
  ok "existing certificate kept ($CERT)"
else
  HOSTNAME_FQDN="$(hostname -f 2>/dev/null || hostname)"
  IP_SANS="IP:$(primary_ip)"
  [[ "$(primary_ip)" != "127.0.0.1" ]] && IP_SANS="$IP_SANS,IP:127.0.0.1"
  openssl req -x509 -newkey rsa:2048 -nodes -sha256 -days 3650 \
    -keyout "$KEY" -out "$CERT" \
    -subj "/O=NexVE/CN=$HOSTNAME_FQDN" \
    -addext "subjectAltName=DNS:$HOSTNAME_FQDN,DNS:nexve.local,DNS:localhost,$IP_SANS" \
    >/dev/null 2>&1 || die "certificate generation failed (openssl req)"
  chmod 640 "$KEY"; chown root:"$NEXVE_USER" "$KEY"; chmod 644 "$CERT"
  ok "certificate written to $CERT (valid 10 years, SAN: $HOSTNAME_FQDN / $(primary_ip))"
fi

# ---------------------------------------------------------------------------
# 7. Configuration (/etc/nexve/nexve.env) – secrets are generated once and
#    preserved on re-install.
# ---------------------------------------------------------------------------
step "Writing configuration to $NEXVE_ETC/nexve.env"
ENV_FILE="$NEXVE_ETC/nexve.env"
if [[ -f "$ENV_FILE" ]]; then
  ok "existing config kept"
else
  AUTH_TOKEN="$(openssl rand -hex 24)"
  AUTH_PASS="$(openssl rand -hex 12)"
  umask 027
  cat >"$ENV_FILE" <<EOF
# NexVE daemon configuration (systemd EnvironmentFile format).
# Generated by the NexVE installer – edit freely; restart with:
#   systemctl restart nexved

# Listener (plain HTTP is used only when TLS cert/key are empty)
NEXVE_LISTEN=:${NEXVE_PORT}
NEXVE_TLS_CERT=${CERT}
NEXVE_TLS_KEY=${KEY}

# Authentication: Basic auth (user below) *or* Bearer token (Authorization:
# Bearer <token>) are both accepted. Change these and restart nexved.
NEXVE_AUTH_USER=admin
NEXVE_AUTH_PASSWORD=${AUTH_PASS}
NEXVE_AUTH_TOKEN=${AUTH_TOKEN}

# Drivers
NEXVE_DRIVER=libvirt
NEXVE_QEMU_URI=qemu:///system
NEXVE_LXC_URI=lxc:///
NEXVE_STORAGE_DIR=/var/lib/libvirt/images
NEXVE_DATA_DIR=${NEXVE_STATE}

# Set to 1 to trust proxy-provided X-Forwarded-* headers (behind a reverse proxy)
NEXVE_TRUST_PROXY=0
EOF
  chown root:"$NEXVE_USER" "$ENV_FILE"; chmod 640 "$ENV_FILE"
  ok "config created (auth secrets generated)"
fi

# ---------------------------------------------------------------------------
# 8. systemd service
# ---------------------------------------------------------------------------
step "Installing the nexved systemd service"
install -m 0644 "$SCRIPT_DIR/systemd/nexved.service" /etc/systemd/system/nexved.service
systemctl daemon-reload
if [[ -z "$NEXVE_NO_START" ]]; then
  systemctl enable nexved >/dev/null 2>&1 || warn "could not enable nexved at boot"
  systemctl restart nexved
  ok "nexved enabled and started"
else
  info "service installed but not started (--no-start)"
fi

# ---------------------------------------------------------------------------
# 9. libvirt baseline: default NAT network + default storage pool
# ---------------------------------------------------------------------------
step "Preparing libvirt default network and storage pool"
# These must autostart so freshly created VMs always have a NIC + disk pool.
run_as_nexve virsh net-info default >/dev/null 2>&1 && {
  run_as_nexve virsh net-autostart default >/dev/null 2>&1 || true
  run_as_nexve virsh net-start  default >/dev/null 2>&1 || true
  ok "libvirt default network: autostart enabled"
} || warn "libvirt 'default' network not found – VMs will need manual networking"

if run_as_nexve virsh pool-info default >/dev/null 2>&1; then
  run_as_nexve virsh pool-autostart default >/dev/null 2>&1 || true
  run_as_nexve virsh pool-start default >/dev/null 2>&1 || true
  ok "storage pool 'default' (/var/lib/libvirt/images): autostart enabled"
else
  run_as_nexve virsh pool-define-as default dir --target /var/lib/libvirt/images >/dev/null 2>&1 || true
  run_as_nexve virsh pool-start default >/dev/null 2>&1 || true
  run_as_nexve virsh pool-autostart default >/dev/null 2>&1 || true
  ok "storage pool 'default' created"
fi

# ---------------------------------------------------------------------------
# 10. Health check – poll the API over TLS before claiming success
# ---------------------------------------------------------------------------
step "Verifying the NexVE API"
HEALTH_URL="https://127.0.0.1:${NEXVE_PORT}/api/v1/health"
for _ in $(seq 1 15); do
  if curl -ksf --max-time 2 "$HEALTH_URL" >/dev/null 2>&1; then
    ok "API answered on $HEALTH_URL"
    HEALTH_OK=1; break
  fi
  sleep 1
done
if [[ -z "${HEALTH_OK:-}" ]]; then
  warn "API did not answer within 15s – check: journalctl -u nexved -e"
fi

# ---------------------------------------------------------------------------
# Done
# ---------------------------------------------------------------------------
IP="$(primary_ip)"
cat <<EOF

${C_GREEN}${C_BOLD}
  ✔ NexVE is ready – open https://${IP}:${NEXVE_PORT}${C_RESET}

${C_BOLD}  Web UI / API${C_RESET}   https://${IP}:${NEXVE_PORT}
${C_BOLD}  Login${C_RESET}         admin / (see ${ENV_FILE} -> NEXVE_AUTH_PASSWORD)
${C_BOLD}  API token${C_RESET}     NEXVE_AUTH_TOKEN in ${ENV_FILE}
${C_BOLD}  Service${C_RESET}       systemctl status nexved
${C_BOLD}  Logs${C_RESET}          journalctl -u nexved -f
${C_BOLD}  Host firmware${C_RESET} ${NEXVE_FIRMWARE} (guests: BIOS or UEFI/OVMF, chosen per VM)
${C_BOLD}  Bridge${C_RESET}        ${NEXVE_BRIDGE:-vmbr0}${BRIDGE_APPLIED:+ (applied, persistent via ${BRIDGE_PERSISTED:-ifupdown})}${BRIDGE_APPLIED:- (config written; effective after reboot)}

  The certificate is self-signed – your browser will warn once. Accept it
  (or add your own certificate in ${NEXVE_ETC}/tls/).

  Firewall: open TCP ${NEXVE_PORT} if you manage NexVE from other hosts:
      ufw allow ${NEXVE_PORT}/tcp        # if ufw is active
${C_RESET}
EOF
