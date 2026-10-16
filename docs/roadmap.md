# NexVE roadmap

NexVE is community-only: **everything here is and stays free & open source (AGPLv3)**.
There is no enterprise edition and no paid feature, now or ever.

## ✅ Baseline (v0.1.0) – implemented in this repository

- Monorepo: installer / Go backend / React frontend / docs / scripts
- Installer for Debian 10+, interactive + `--yes`, vmbr0 bridge, TLS, systemd
- `nexved`: node status, VM/container list, create, start, stop, delete, ISO library,
  console info; bearer-token/basic auth; embedded web UI
- Web UI: dashboard (CPU/RAM/storage/uptime), instance table with actions,
  4-step create wizard (BIOS + UEFI guests), ISO upload, dark-first design

## 🔜 Next (v0.2 – the "daily driver" milestone)

- **In-browser console**: websocket proxy to VNC (noVNC) and serial consoles
- **Task/job system**: async operations with progress (ISO upload, container build,
  backups) surfaced in a global activity panel
- **Cloud-init** support for VMs (user-data/metadata ISO)
- **LXC hardening**: templates (Debian/Alpine download), start/stop via pct-style UX,
  cgroup v2 verification on Debian 12/13
- **Storage management**: pools (dir/LVM-thin/ZFS), create/resize disks, disk attach
- **Config backups** of the NexVE node itself

## 🧭 Later (v0.3+)

- Users, roles, API tokens per user (RBAC) – still no paid tier
- VLANs / SDN zones, bridges with VLAN tagging, network management UI
- Replication and live migration
- Clustering with quorum, HA resource manager
- Ceph integration, ZFS management
- Snapshots & backup/restore jobs ( vzdump-style, retention )
- Metrics history (RRD/time-series) and alerting rules
- ARM64 support; i386 kept best-effort as long as upstream packages allow
- Mobile-friendly PWA; native mobile app (exploratory)

## ❌ Explicit non-goals

- Proprietary/enterprise editions, feature gating, paid support tiers in-repo
- Forking or vendoring Proxmox/pve code – NexVE is clean-room on top of
  upstream KVM/QEMU, libvirt and LXC
- Telemetry/phoning home

Contributions welcome: pick an item from 🔜 or 🧭, open an issue to claim it.
