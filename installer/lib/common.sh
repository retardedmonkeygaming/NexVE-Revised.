# shellcheck shell=bash
###############################################################################
# NexVE installer – shared helper library
#
# Sourced by installer/install.sh and installer/uninstall.sh.
# Everything here is POSIX-ish bash (Debian 10+ ships bash 5.x).
###############################################################################

NEXVE_VERSION="0.1.0"

# ---------------------------------------------------------------------------
# Colourised logging. Disabled automatically when stdout is not a TTY or when
# NO_COLOR is set (https://no-color.org).
# ---------------------------------------------------------------------------
if [[ -t 1 && -z "${NO_COLOR:-}" ]]; then
  C_RESET=$'\033[0m'; C_BOLD=$'\033[1m'; C_RED=$'\033[31m'
  C_GREEN=$'\033[32m'; C_YELLOW=$'\033[33m'; C_BLUE=$'\033[34m'; C_CYAN=$'\033[36m'
else
  C_RESET=""; C_BOLD=""; C_RED=""; C_GREEN=""; C_YELLOW=""; C_BLUE=""; C_CYAN=""
fi

_log_file="${NEXVE_LOG_FILE:-/var/log/nexve-install.log}"

_log_to_file() {
  # Best effort: the log may not be writable yet (before root check).
  { printf '%s %s\n' "$(date '+%Y-%m-%d %H:%M:%S')" "$*" >>"$_log_file"; } 2>/dev/null || true
}

step() { printf '%s==>%s %s%s%s\n' "$C_BLUE" "$C_RESET" "$C_BOLD" "$*" "$C_RESET"; _log_to_file "STEP $*"; }
ok()    { printf '%s  [ok]%s %s\n' "$C_GREEN" "$C_RESET" "$*"; _log_to_file "OK   $*"; }
info()  { printf '%s  [..]%s %s\n' "$C_CYAN" "$C_RESET" "$*"; _log_to_file "INFO $*"; }
warn()  { printf '%s[warn]%s %s\n' "$C_YELLOW" "$C_RESET" "$*" >&2; _log_to_file "WARN $*"; }
err()   { printf '%s[err] %s %s\n' "$C_RED" "$C_RESET" "$*" >&2; _log_to_file "ERR  $*"; }
die()   { err "$*"; exit 1; }

# ---------------------------------------------------------------------------
# Root check – re-exec through sudo when we are a normal user.
# ---------------------------------------------------------------------------
require_root() {
  if [[ "${EUID}" -ne 0 ]]; then
    if command -v sudo >/dev/null 2>&1; then
      info "root privileges required – re-running with sudo"
      exec sudo -- "$0" "$@"
    fi
    die "please run as root (sudo $0 ...)"
  fi
}

# ---------------------------------------------------------------------------
# UI mode: whiptail TUI when available + interactive, plain text otherwise.
# NEXVE_YES=1 (via --yes) forces every confirmation to "yes".
# ---------------------------------------------------------------------------
UI_MODE="plain"

init_ui() {
  if [[ -n "${NEXVE_YES:-}" ]]; then
    UI_MODE="plain"          # fully non-interactive: everything auto-accepted
  elif [[ -t 0 ]] && command -v whiptail >/dev/null 2>&1; then
    UI_MODE="whiptail"
  else
    UI_MODE="plain"
  fi
}

# confirm "<question>" -> returns 0 (yes) / 1 (no)
confirm() {
  local q="$1"
  if [[ -n "${NEXVE_YES:-}" ]]; then return 0; fi
  if [[ "$UI_MODE" == "whiptail" ]]; then
    whiptail --title "NexVE installer" --yesno "$q" 11 72 >/dev/null 2>&1 && return 0 || return 1
  fi
  local a=""
  read -r -p "$q [y/N] " a
  case "$a" in y|Y|yes|YES) return 0 ;; *) return 1 ;; esac
}

# info_box "<title>" "<text>"  (purely cosmetic, best effort)
info_box() {
  [[ "$UI_MODE" == "whiptail" ]] || return 0
  whiptail --title "$1" --msgbox "$2" 14 74 >/dev/null 2>&1 || true
}

# prompt "<question>" "<default>" -> echoes the answer
prompt() {
  local q="$1" def="${2:-}" a=""
  if [[ -n "${NEXVE_YES:-}" ]]; then printf '%s' "$def"; return; fi
  if [[ "$UI_MODE" == "whiptail" ]]; then
    a=$(whiptail --title "NexVE installer" --inputbox "$q" 10 72 "$def" 3>&1 1>&2 2>&3) || a="$def"
    printf '%s' "${a:-$def}"
  else
    read -r -p "$q [$def] " a
    printf '%s' "${a:-$def}"
  fi
}

# ---------------------------------------------------------------------------
# OS detection (Debian 10 "buster" and newer are supported).
# ---------------------------------------------------------------------------
detect_os() {
  NEXVE_OS_ID="unknown"; NEXVE_OS_VERSION_ID=""; NEXVE_OS_MAJOR=0; NEXVE_OS_PRETTY="unknown OS"
  if [[ -r /etc/os-release ]]; then
    # shellcheck disable=SC1091
    . /etc/os-release
    NEXVE_OS_ID="${ID:-unknown}"
    NEXVE_OS_VERSION_ID="${VERSION_ID:-}"
    NEXVE_OS_PRETTY="${PRETTY_NAME:-$NEXVE_OS_ID}"
    NEXVE_OS_MAJOR="${VERSION_ID%%.*}"
    [[ "$NEXVE_OS_MAJOR" =~ ^[0-9]+$ ]] || NEXVE_OS_MAJOR=0
  fi
}

check_supported_os() {
  detect_os
  info "detected OS: $NEXVE_OS_PRETTY"
  if [[ "$NEXVE_OS_ID" == "debian" && "$NEXVE_OS_MAJOR" -ge 10 ]]; then
    return 0
  fi
  if [[ -n "${NEXVE_FORCE:-}" ]]; then
    warn "unsupported OS '$NEXVE_OS_PRETTY' – continuing because --force was given"
    return 0
  fi
  die "unsupported OS '$NEXVE_OS_PRETTY'. NexVE targets Debian 10+ (use --force to override)."
}

detect_arch() {
  NEXVE_ARCH="$(uname -m)"
  case "$NEXVE_ARCH" in
    x86_64)  info "architecture: x86_64 (primary target)" ;;
    i386|i486|i586|i686)
      warn "architecture: $NEXVE_ARCH – i386 is a best-effort target, expect rough edges"
      [[ -n "${NEXVE_FORCE:-}" ]] || die "i386 is best-effort; re-run with --force to continue"
      ;;
    *) die "unsupported architecture '$NEXVE_ARCH' (x86_64 primary, i386 best-effort)" ;;
  esac
}

# Firmware mode of the *host* we are installing on: UEFI or legacy BIOS.
# We only report/record it – we never touch the bootloader.
detect_firmware() {
  if [[ -d /sys/firmware/efi ]]; then
    NEXVE_FIRMWARE="uefi"
    info "boot firmware: UEFI (EFI variables present)"
  else
    NEXVE_FIRMWARE="bios"
    info "boot firmware: legacy BIOS"
  fi
}

# ---------------------------------------------------------------------------
# apt helpers – never prompt, never ask about restarting services.
# ---------------------------------------------------------------------------
export DEBIAN_FRONTEND=noninteractive

apt_update_once() {
  [[ -n "${NEXVE_APT_UPDATED:-}" ]] && return 0
  step "Updating apt package indexes"
  apt-get update -qq || warn "apt-get update reported problems (continuing)"
  NEXVE_APT_UPDATED=1
}

# install_packages pkg1 pkg2 ...
# Installs as a batch first; if that fails (e.g. one package renamed in a newer
# Debian release) it retries package-by-package so a single unknown package
# cannot break the whole install.
install_packages() {
  local pkgs=("$@") missing=() p
  apt_update_once
  for p in "${pkgs[@]}"; do
    dpkg-query -W -f='${Status}' "$p" 2>/dev/null | grep -q "install ok installed" || missing+=("$p")
  done
  if [[ ${#missing[@]} -eq 0 ]]; then
    ok "all required packages already installed"
    return 0
  fi
  step "Installing packages: ${missing[*]}"
  if ! apt-get install -y -o Dpkg::Options::="--force-confdef" -o Dpkg::Options::="--force-confold" "${missing[@]}"; then
    warn "batch install failed – retrying package-by-package"
    for p in "${missing[@]}"; do
      if ! apt-get install -y "$p"; then
        warn "could not install '$p' (skipped – it may not exist on this release)"
      fi
    done
  fi
  ok "packages installed"
}

# Run a command as the nexve service account (falls back to root).
run_as_nexve() {
  if id nexve >/dev/null 2>&1 && command -v sudo >/dev/null 2>&1; then
    sudo -u nexve -- "$@"
  else
    "$@"
  fi
}

# Primary IPv4 address used in the final "open https://..." banner.
primary_ip() {
  local ip
  ip=$(ip -4 route get 1.1.1.1 2>/dev/null | awk '{for(i=1;i<=NF;i++) if($i=="src"){print $(i+1); exit}}')
  [[ -n "$ip" ]] || ip=$(ip -4 addr show scope global 2>/dev/null | awk '/inet /{sub(/\/.*/,"",$2); print $2; exit}')
  printf '%s' "${ip:-<server-ip>}"
}
