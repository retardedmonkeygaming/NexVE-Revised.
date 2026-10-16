#!/usr/bin/env bash
# Development mode: backend with the mock driver (no KVM needed) + Vite dev
# server with hot reload on http://localhost:5173.
#
#   ./scripts/dev.sh              # mock driver
#   NEXVE_DRIVER=libvirt ./scripts/dev.sh   # talk to local libvirt
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."

export NEXVE_DRIVER="${NEXVE_DRIVER:-mock}"
export NEXVE_LISTEN="${NEXVE_LISTEN:-:8006}"

echo "==> backend  http://127.0.0.1${NEXVE_LISTEN}  (driver: ${NEXVE_DRIVER})"
(cd backend && go run ./cmd/nexved) &
BACKEND_PID=$!
trap 'kill $BACKEND_PID 2>/dev/null || true' EXIT

echo "==> frontend http://localhost:5173  (hot reload, /api proxied to backend)"
(cd frontend && npm run dev)
