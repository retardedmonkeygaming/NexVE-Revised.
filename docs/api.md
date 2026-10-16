# NexVE REST API (v1)

Base URL: `https://<host>:8006` (self-signed TLS from the installer).

Authentication (all routes except `GET /api/v1/health`):

```
Authorization: Bearer <NEXVE_AUTH_TOKEN>          # or
Authorization: Basic  base64(NEXVE_AUTH_USER:NEXVE_AUTH_PASSWORD)
```

Errors are JSON: `{"error": "<message>"}` with a matching HTTP status
(400 invalid, 401 unauthorized, 404 not found, 409 exists, 503 hypervisor unavailable).

Instance ids are always `<type>:<name>` – e.g. `qemu:web-01`, `lxc:dns`.

## Endpoints

### `GET /api/v1/health` (public)

```json
{"status":"ok","version":"0.1.0","driver":"libvirt","time":"2026-01-01T00:00:00Z"}
```

### `GET /api/v1/node/status`

```json
{
  "time": "...", "hostname": "pve1", "os": "Debian GNU/Linux 12",
  "kernel": "6.1.0 ...", "arch": "amd64", "firmware": "uefi",
  "uptime_seconds": 123456,
  "cpu":     {"model":"...","logical":8,"physical":4,"percent":12.3,"load1":0.5,"load5":0.4,"load15":0.3},
  "memory":  {"total_bytes":0,"used_bytes":0,"available_bytes":0,"percent":0,"swap_total_bytes":0,"swap_used_bytes":0},
  "storage": [{"device":"/dev/sda2","mount":"/","fs":"ext4","total_bytes":0,"used_bytes":0,"percent":0}],
  "storage_totals": {"total_bytes":0,"used_bytes":0,"free_bytes":0,"percent":0},
  "kvm": true
}
```

### `GET /api/v1/vms`

`{"instances": [ <instance>, ... ]}` with:

```json
{
  "id": "qemu:web-01", "name": "web-01", "type": "qemu",
  "state": "running", "running": true, "vcpus": 2, "memory_mb": 2048,
  "disk_gb": 20, "firmware": "uefi", "autostart": false, "detail": ""
}
```

`state` is the raw libvirt domain state (`running`, `shut off`, `paused`, …) plus
`creating` / `failed` for containers being provisioned.

### `POST /api/v1/vms`

```json
{
  "type": "qemu",          // "qemu" (default) | "lxc"
  "name": "my-vm",         // required, sanitized to [a-z0-9-]
  "vcpus": 2,
  "memory_mb": 2048,
  "disk_gb": 16,           // qemu only, qcow2 in the default pool
  "iso": "debian-12-netinst.iso",   // optional: name in the ISO library or absolute path
  "firmware": "bios",      // "bios" (default) | "uefi" (OVMF)
  "network": "default",    // libvirt network
  "suite": "stable",       // lxc only: debootstrap suite
  "start": true            // boot right after creation
}
```

Returns `201` with the instance. For `type:"lxc"` the response has
`state:"creating"` and the rootfs is built in the background.

### Lifecycle

| Route | Effect |
|---|---|
| `GET /api/v1/vms/{id}` | one instance (includes `autostart`) |
| `POST /api/v1/vms/{id}/start` | `virsh start` |
| `POST /api/v1/vms/{id}/stop?force=1` | ACPI shutdown; `force=1` pulls the plug |
| `DELETE /api/v1/vms/{id}?purge=1` | undefine; `purge=1` also deletes the disk/rootfs |
| `GET /api/v1/vms/{id}/console` | `{"mode":"vnc"|"serial","endpoint":"127.0.0.1:5900","hint":"ssh -L …"}` |

### ISO library

| Route | Effect |
|---|---|
| `GET /api/v1/isos` | `{"isos":[{"name","path","size_bytes"}]}` from `/var/lib/libvirt/images/iso` |
| `POST /api/v1/isos` | multipart upload, file field `iso` (max 16 GiB), streamed to disk |

## curl examples

```bash
NX=https://192.168.1.10:8006
AUTH="Authorization: Bearer $(grep NEXVE_AUTH_TOKEN /etc/nexve/nexve.env | cut -d= -f2)"

curl -sk -H "$AUTH" $NX/api/v1/node/status
curl -sk -H "$AUTH" $NX/api/v1/vms
curl -sk -H "$AUTH" -H 'Content-Type: application/json' \
     -d '{"name":"debian-vm","vcpus":2,"memory_mb":2048,"disk_gb":32,"iso":"debian-12-netinst.iso","start":true}' \
     $NX/api/v1/vms
curl -sk -X POST -H "$AUTH" $NX/api/v1/vms/qemu:debian-vm/stop
curl -sk -X DELETE -H "$AUTH" "$NX/api/v1/vms/qemu:debian-vm?purge=1"

# upload an ISO
curl -sk -H "$AUTH" -F iso=@debian-12-netinst.iso $NX/api/v1/isos
```
