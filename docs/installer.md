# The NexVE installer

`installer/install.sh` turns a **stock Debian 10–13 (and newer)** box into a NexVE node.
It is idempotent – re-running it upgrades files and keeps your config and secrets.

## Usage

```bash
sudo ./installer/install.sh                # interactive (whiptail TUI when available)
sudo ./installer/install.sh --yes          # non-interactive, accepts every default
sudo ./installer/install.sh --help         # all flags
```

| Flag | Effect |
|---|---|
| `-y, --yes` | non-interactive: accept every default (for preseed/automation) |
| `-f, --force` | continue on non-Debian releases / i386 |
| `-p, --port PORT` | web UI/API port (default 8006) |
| `-i, --iface IFACE` | physical NIC to enslave into the bridge |
| `--bridge NAME` | bridge name (default vmbr0) |
| `--no-bridge` | skip bridge setup |
| `--no-start` | install but do not enable/start the service |
| `--uninstall [--purge]` | remove the installation (purge also deletes config/data) |

## What it does (in order)

1. **Preflight** – `/etc/os-release` check (Debian ≥ 10), architecture check
   (x86_64 primary, i386 best-effort), host firmware detection (UEFI vs BIOS –
   reported only, the host bootloader is never touched), `/dev/kvm` presence.
2. **Packages** – `qemu-kvm qemu-utils libvirt-daemon-system libvirt-clients
   libvirt-daemon-driver-lxc lxc debootstrap bridge-utils iproute2 dnsmasq-base ovmf
   curl ca-certificates openssl whiptail gawk`. Installed batch-first, then
   package-by-package fallback so one renamed package cannot abort the install.
3. **Service account** – system user `nexve` (shell `nologin`, home `/var/lib/nexve`),
   added to groups `libvirt` and `kvm`.
4. **Binary** – finds `nexved` next to the installer (release tarball) or in `<repo>/dist/`
   and installs it to `/usr/local/lib/nexve/nexved`. The web UI is embedded in the binary.
5. **Bridge `vmbr0`** – see below.
6. **TLS** – self-signed certificate, 10 years, SANs for hostname + primary IP +
   `nexve.local` + `127.0.0.1` → `/etc/nexve/tls/{cert,key}.pem`.
7. **Config** – `/etc/nexve/nexve.env` (systemd EnvironmentFile) with generated
   `NEXVE_AUTH_TOKEN` and `NEXVE_AUTH_PASSWORD`. Re-runs keep existing secrets.
8. **systemd** – `nexved.service` installed, enabled, started (runs as `nexve`).
9. **libvirt baseline** – default NAT network and default storage pool set to autostart.
10. **Verification** – polls `https://127.0.0.1:<port>/api/v1/health` for 15 s before
    printing the success banner.

## The network bridge

The Proxmox-style layout: `vmbr0` carries the host's IP and has the default-route NIC
as a port; VMs attach to `vmbr0`.

- **ifupdown** (Debian default): the `eth0` stanza in `/etc/network/interfaces` is
  *commented out* (never deleted; backup at `/etc/network/interfaces.nexve-backup`) and
  a bridge stanza is written to `/etc/network/interfaces.d/vmbr0` with the same
  addressing method (static keeps address/netmask/gateway; dhcp stays dhcp).
- **systemd-networkd** fallback: netdev + network files in `/etc/systemd/network`.
- **Runtime**: unless you answer "no" in the TUI (or run `--yes`, which auto-accepts),
  the bridge is also applied live – the IP moves to `vmbr0` with a ~1 s blip, and the
  script **rolls back automatically** if the move leaves the host without a default route.

## BIOS vs UEFI

- **Host**: detected and reported; installer never modifies GRUB/firmware settings.
  Both boot modes work – nothing in NexVE depends on them.
- **Guests**: chosen per VM in the create wizard. UEFI guests use the distro OVMF
  firmware (`ovmf` package): `OVMF_CODE*.fd` + NVRAM seeded from `OVMF_VARS*.fd`,
  auto-detected across Debian/Ubuntu paths.

## Troubleshooting

```bash
systemctl status nexved              # service state
journalctl -u nexved -e             # daemon log
cat /etc/nexve/nexve.env             # generated credentials
virsh --connect qemu:///system list --all   # libvirt sanity check
ip -br a; bridge link               # bridge state
```

- *API does not answer*: check the log; port conflict (`--port`), or TLS files missing.
- *VMs have no network*: `virsh net-list --all` – the `default` network must be active;
  `virsh net-autostart default`.
- *Bridge broke connectivity*: boot a console, restore
  `/etc/network/interfaces.nexve-backup` → `/etc/network/interfaces`, reboot.
- *Nested virtualization warning*: `/dev/kvm` missing usually means VT-x/SVM is disabled
  in the firmware setup.
