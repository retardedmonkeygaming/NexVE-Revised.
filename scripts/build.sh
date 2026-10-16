#!/usr/bin/env bash
# Build a complete NexVE release (backend + embedded frontend) into ./dist.
# Thin wrapper around the Makefile, for CI and people who prefer scripts.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
make build
make package
echo
echo "Artifacts:"
ls -la dist/nexved dist/nexve_*.tar.gz
