# shellcheck shell=bash
###############################################################################
# NexVE installer – network bridge (vmbr0) configuration
#
# Proxmox-style layout: one Linux bridge named "vmbr0" that carries the host's
# existing IP configuration and has the primary physical NIC as a port. VMs
# attach to vmbr0.
#
# Two persistence paths are supported:
#   * ifupdown         – /etc/network/interfaces  (Debian 10/11/12/13 default)
#   * systemd-networkd – /etc/systemd/network     (fallback / newer setups)
#
# The change is ALSO applied at runtime so the user does not need to reboot.
# Moving an address is a sub-second network blip; if it fails we roll the
# runtime state back so the machine stays reachable.
###############################################################################

# Default-route interface, e.g. "eth0" / "enp1s0".
default_iface() {
  local ifc
  ifc=$(ip -4 route show default 2>/dev/null | awk '{for(i=1;i<=NF;i++) if($i=="dev"){print $(i+1); exit}}')
  printf '%s' "$ifc"
}

# Returns 0 when $1 is already a bridge.
is_bridge() {
  [[ -d "/sys/class/net/$1/bridge" ]]
}

# Configure vmbr0. Honors: NEXVE_IFACE, NEXVE_BRIDGE, NEXVE_NO_BRIDGE
configure_bridge() {
  local br="${NEXVE_BRIDGE:-vmbr0}"
  local ifc="${NEXVE_IFACE:-}"
  local ifc_file="/etc/network/interfaces"

  if [[ -n "${NEXVE_NO_BRIDGE:-}" ]]; then
    warn "skipping network bridge setup (--no-bridge)"
    return 0
  fi

  if ip link show "$br" >/dev/null 2>&1 || is_bridge "$br"; then
    ok "bridge $br already exists – nothing to do"
    return 0
  fi

  [[ -n "$ifc" ]] || ifc=$(default_iface)
  if [[ -z "$ifc" ]]; then
    warn "could not determine the default interface – skipping bridge setup"
    warn "create $br manually before starting any VMs"
    return 0
  fi
  if is_bridge "$ifc"; then
    info "$ifc is already a bridge – skipping (set NEXVE_IFACE=ethX to override)"
    return 0
  fi

  step "Configuring network bridge $br on port $ifc"

  # Current IPv4 configuration of the physical NIC – we move it onto the bridge.
  local addr gw method
  addr=$(ip -4 addr show dev "$ifc" 2>/dev/null | awk '/inet /{print $4; exit}')
  gw=$(ip -4 route show default dev "$ifc" 2>/dev/null | awk '/default/{print $3; exit}')

  # Inherit the addressing method (dhcp/static) from the existing stanza.
  method=$(awk -v IF="$ifc" '$1=="iface" && $2==IF && $3=="inet" {print $4; exit}' "$ifc_file" 2>/dev/null)
  if [[ -z "$method" ]]; then
    if [[ -n "$addr" ]]; then method="static"; else method="dhcp"; fi
  fi

  local persist_ok=0
  if [[ -f "$ifc_file" ]] && grep -qE '^[[:space:]]*source[[:space:]]+/etc/network/interfaces\.d' "$ifc_file"; then
    persist_ifupdown "$br" "$ifc" "$method" "$addr" "$gw" && persist_ok=1
  elif [[ -d /etc/systemd/network ]]; then
    persist_networkd "$br" "$ifc" "$method" "$addr" "$gw" && persist_ok=1
  fi

  if [[ "$persist_ok" -ne 1 ]]; then
    warn "could not persist bridge configuration – applying at runtime only"
  fi

  apply_bridge_runtime "$br" "$ifc" "$addr" "$gw"
}

# --- persistence: ifupdown --------------------------------------------------
# The physical NIC stanza is commented out (never deleted, so it can be
# restored) and replaced by a bridge stanza carrying the same addressing.
persist_ifupdown() {
  local br="$1" ifc="$2" method="$3" addr="$4" gw="$5"
  local ifc_file="/etc/network/interfaces"
  local d="/etc/network/interfaces.d"
  local backup="/etc/network/interfaces.nexve-backup"

  step "Writing bridge config to /etc/network/interfaces.d/$br"
  mkdir -p "$d"
  [[ -f "$backup" ]] || cp -p "$ifc_file" "$backup"

  # 1) comment out `auto eth0` / `allow-hotplug eth0` lines for the port NIC
  sed -E "s|^([[:space:]]*)(auto|allow-hotplug)([[:space:]]+)${ifc}([[:space:]]|$)|\1# nexve \2\3\4|" \
    "$ifc_file" >"$ifc_file.tmp" && mv "$ifc_file.tmp" "$ifc_file"

  # 2) comment out the whole `iface <ifc> inet ...` stanza (never delete it)
  awk -v IF="$ifc" '
    $1 == "iface" && $2 == IF { inblk = 1; print "# nexve: moved into '"$br"' -> " $0; next }
    inblk && $1 == "iface"     { inblk = 0 }
    inblk                      { print "# " $0; next }
    { print }
  ' "$ifc_file" >"$ifc_file.tmp" && mv "$ifc_file.tmp" "$ifc_file"

  # 3) append the bridge stanza (source-directory keeps it in its own file)
  {
    echo ""
    echo "# NexVE managed bridge – created by the NexVE installer"
    echo "auto $br"
    echo "iface $br inet $method"
    if [[ "$method" == "static" ]]; then
      echo "    address ${addr%%/*}"
      [[ "$addr" == */* ]] && echo "    netmask $(mask_from_cidr "${addr##*/}")"
      [[ -n "$gw" ]] && echo "    gateway $gw"
    fi
    echo "    bridge_ports $ifc"
    echo "    bridge_stp off"
    echo "    bridge_fd 0"
    echo "    bridge_maxwait 0"
  } >>"$d/$br"

  if command -v ifquery >/dev/null 2>&1; then
    if ! ifquery --list >/dev/null 2>&1; then
      warn "ifquery reports a problem – check /etc/network/interfaces before rebooting"
    fi
  fi
  ok "persistent ifupdown config written (backup: $backup)"
  BRIDGE_PERSISTED="ifupdown"
  return 0
}

# IPv4 prefix length -> dotted netmask (no external tools).
mask_from_cidr() {
  local p=$(( $1 )) mask="" i octet
  for i in 0 1 2 3; do
    if (( p >= 8 )); then octet=255; p=$(( p - 8 ));
    elif (( p >= 1 )); then octet=$(( 256 - 2 ** (8 - p) )); p=0;
    else octet=0; fi
    mask+="${mask:+.}$octet"
  done
  printf '%s' "$mask"
}

# --- persistence: systemd-networkd ----------------------------------------
persist_networkd() {
  local br="$1" ifc="$2" method="$3" addr="$4" gw="$5"
  step "Writing bridge config to /etc/systemd/network (systemd-networkd)"
  {
    echo "[NetDev]"
    echo "Name=$br"
    echo "Kind=bridge"
  } >"/etc/systemd/network/50-nexve-$br.netdev"

  {
    echo "[Match]"
    echo "Name=$br"
    echo ""
    echo "[Network]"
    if [[ "$method" == "dhcp" ]]; then
      echo "DHCP=yes"
    else
      [[ -n "$addr" ]] && echo "Address=$addr"
      [[ -n "$gw" ]] && echo "Gateway=$gw"
      echo "DNS=1.1.1.1"
    fi
  } >"/etc/systemd/network/51-nexve-$br.network"

  {
    echo "[Match]"
    echo "Name=$ifc"
    echo ""
    echo "[Network]"
    echo "Bridge=$br"
  } >"/etc/systemd/network/52-nexve-port-$ifc.network"

  if command -v networkctl >/dev/null 2>&1; then
    networkctl reload 2>/dev/null || true
  fi
  ok "networkd config written (active after reboot or 'systemctl restart systemd-networkd')"
  BRIDGE_PERSISTED="networkd"
  return 0
}

# --- runtime application ---------------------------------------------------
# Creates the bridge, enslaves the NIC and moves the address + default route.
# Rolls back automatically when the move leaves the host without a route.
apply_bridge_runtime() {
  local br="$1" ifc="$2" addr="$3" gw="$4"

  confirm "Apply the $br bridge now?\n\nThis moves the host IP from $ifc to $br.\nThere will be a brief (~1s) network interruption." || {
    warn "bridge config written but NOT applied – it takes effect on reboot"
    return 0
  }

  info "creating bridge $br and enslaving $ifc"
  ip link add name "$br" type bridge || { err "could not create bridge"; return 1; }
  ip link set "$ifc" master "$br"
  ip link set "$br" up
  ip link set "$ifc" up

  if [[ -n "$addr" ]]; then
    ip addr del "$addr" dev "$ifc" 2>/dev/null || true
    ip addr add "$addr" dev "$br" || {
      err "failed to move $addr to $br – rolling back"
      ip addr add "$addr" dev "$ifc" 2>/dev/null || true
      ip link set "$ifc" nomaster 2>/dev/null || true
      ip link del "$br" 2>/dev/null || true
      return 1
    }
  fi
  if [[ -n "$gw" ]]; then
    ip route replace default via "$gw" dev "$br" 2>/dev/null || true
  fi

  # sanity check: we still have a default route
  if ! ip -4 route show default 2>/dev/null | grep -q default; then
    warn "no default route after the move – rolling back"
    [[ -n "$addr" ]] && { ip addr del "$addr" dev "$br" 2>/dev/null || true; ip addr add "$addr" dev "$ifc" 2>/dev/null || true; }
    [[ -n "$gw" ]] && ip route replace default via "$gw" dev "$ifc" 2>/dev/null || true
    ip link set "$ifc" nomaster 2>/dev/null || true
    ip link del "$br" 2>/dev/null || true
    return 1
  fi

  ok "bridge $br is up ($addr, port $ifc)"
  BRIDGE_APPLIED=1
  return 0
}
