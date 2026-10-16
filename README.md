# NexVE

**NexVE** is an independent, fully open-source **Type-1 hypervisor platform** – a clean-room
alternative to Proxmox VE built on the same proven foundation (**KVM/QEMU + LXC**) but with a
modern stack and a modern, dark-mode-first web UI.

> **Status: early baseline (v0.1.0).** Everything in this repository works today.
> Anything beyond the baseline is explicitly marked *future* in [docs/roadmap.md](docs/roadmap.md).

## Why NexVE?

| | Proxmox VE | NexVE |
|---|---|---|
| Hypervisor core | KVM/QEMU + LXC | KVM/QEMU + LXC (same solid foundation) |
| Backend | Perl + patched Debian | **Go** – one static binary, REST API |
| Web UI | legacy jQuery (2015 look) | **React + TypeScript + Vite + Tailwind** |
| Installer | Debian ISO remaster | **single script** – runs on stock Debian 10+ |
| License | GPL-2 with proprietary add-ons | **AGPLv3 only – free for everyone, no enterprise edition** |

NexVE is for homelabbers and small infra who want a clean, fast, self-hosted hypervisor
that installs on the Debian they already have.

## Features in this baseline

- **Installer** ([installer/install.sh](installer/install.sh)): detects Debian 10+, installs
  qemu-kvm/libvirt/lxc/bridge-utils/ovmf, creates the `nexve` service account, builds the
  `vmbr0` bridge, generates a self-signed TLS certificate, enables the systemd service.
  Interactive (whiptail) or fully non-interactive (`--yes`).
- **Backend** ([backend/](backend)): single Go binary (`nexved`) serving a REST API and the
  web UI on one port (8006). Node status (CPU/RAM/storage/uptime), VM & container
  list/create/start/stop/delete, ISO library with upload, VNC/serial console info.
  Auth via bearer token or basic auth.
- **Frontend** ([frontend/](frontend)): dark-first responsive SPA – dashboard with live
  gauges, instance table with lifecycle actions, 4-step create wizard (BIOS **and** UEFI
  guests), ISO upload, console dialog.
- **Packaging**: `make build` → one binary with the UI embedded; `make package` → tarball
  you can unpack on any Debian.

## Quick start – fresh Debian 10/11/12/13

```bash
# 1. get the code (or unpack the release tarball)
git clone https://github.com/nexve/nexve && cd nexve

# 2. build (needs go >= 1.22 and node >= 18; on the server: apt install golang-go nodejs npm
#    or use the official toolchains)
make build

# 3. install – interactive
sudo ./installer/install.sh

# …or fully non-interactive (great for preseed/automated installs)
sudo ./installer/install.sh --yes
```

When it finishes you'll see:

```
✔ NexVE is ready – open https://<your-ip>:8006
```

Open it in a browser (accept the self-signed certificate once) and sign in with
`admin` and the password from `/etc/nexve/nexve.env` (`NEXVE_AUTH_PASSWORD`),
or use the generated `NEXVE_AUTH_TOKEN` as a bearer token.

Useful installer flags: `--port 8006`, `--iface enp3s0` (NIC for `vmbr0`),
`--no-bridge`, `--no-start`, `--uninstall [--purge]`. See `./installer/install.sh --help`.

## Developing

```bash
git clone https://github.com/nexve/nexve && cd nexve
./scripts/dev.sh          # backend (mock driver, no KVM needed) + Vite hot reload
```

- Frontend: http://localhost:5173 (proxies `/api` to the backend on `:8006`)
- Backend API: http://127.0.0.1:8006/api/v1/health
- Sign in with any token in the UI – the mock driver accepts everything after a 200.

Individual commands:

```bash
cd backend  && go run ./cmd/nexved --dev        # mock driver, plain HTTP
cd frontend && npm install && npm run dev       # UI only
cd frontend && npm run build                    # production UI build
make backend                                    # embed staged UI into dist/nexved
make verify                                     # go vet + tsc typecheck
```

### Backend layout

```
backend/cmd/nexved/          main.go            entrypoint, flags, driver selection
backend/internal/config/     config.go          env-based configuration
backend/internal/api/        server.go          REST routes, auth middleware, static UI
backend/internal/driver/     driver.go          hypervisor abstraction + types
                             libvirt.go         production driver (virsh-based, no cgo)
                             mock.go            in-memory driver for UI development
backend/internal/node/       status.go          CPU/RAM/disk/uptime sampler
backend/internal/web/        embed.go           go:embed of the built frontend
```

### API in one breath

```
GET    /api/v1/health                 (public)
GET    /api/v1/node/status
GET    /api/v1/vms                    list VMs + containers
POST   /api/v1/vms                    create  {type:qemu|lxc, name, vcpus, memory_mb, …}
POST   /api/v1/vms/{id}/start         id = "qemu:my-vm" | "lxc:web"
POST   /api/v1/vms/{id}/stop[?force=1]
DELETE /api/v1/vms/{id}[?purge=1]
GET    /api/v1/vms/{id}/console       VNC/serial connection info
GET    /api/v1/isos   POST /api/v1/isos (multipart upload)
```

Full details: [docs/api.md](docs/api.md), design: [docs/architecture.md](docs/architecture.md),
installer internals: [docs/installer.md](docs/installer.md).

## Roadmap (future – NOT implemented)

Clustering/HA · Ceph/ZFS management · full storage UI · backup/restore · VLAN/SDN networking ·
user management & RBAC · live migration · cloud-init · browser console (noVNC) · ARM support ·
mobile app. See [docs/roadmap.md](docs/roadmap.md) for the ordered plan.

## License

[GNU AGPL v3](LICENSE) – forever free, no enterprise edition, no paywalled features.
