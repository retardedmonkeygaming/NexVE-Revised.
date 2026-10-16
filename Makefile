# NexVE – top-level build orchestration.
#
#   make build      build everything into ./dist (frontend embedded in nexved)
#   make backend    Go backend only
#   make frontend   web UI only
#   make dev        backend + Vite dev server with hot reload
#   make package    release tarball (installer + binary) in ./dist
#   make clean      remove build output
#
# Requires: Go >= 1.22, Node.js >= 18, npm.

VERSION      ?= 0.1.0
GO           ?= go
NPM          ?= npm
DIST         := dist
BACKEND      := backend
FRONTEND     := frontend
EMBED_DIR    := $(BACKEND)/internal/web/dist

# Injected into the binary so /api/v1/health and --version report the release.
LDFLAGS      := -s -w -X main.version=$(VERSION)

.PHONY: all build backend frontend dev package clean verify help

all: build

help:
	@echo "NexVE $(VERSION) – targets:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

build: frontend backend ## full production build into ./dist

frontend: ## build the web UI and stage it for embedding
	@echo "==> frontend: npm install + vite build"
	cd $(FRONTEND) && $(NPM) install --no-audit --no-fund && $(NPM) run build
	@echo "==> staging frontend into $(EMBED_DIR)"
	rm -rf $(EMBED_DIR)
	mkdir -p $(EMBED_DIR)
	cp -R $(FRONTEND)/dist/. $(EMBED_DIR)/
	touch $(EMBED_DIR)/.gitkeep

backend: ## build the nexved binary (embeds the staged frontend)
	@echo "==> backend: go build (version $(VERSION))"
	cd $(BACKEND) && $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o ../$(DIST)/nexved ./cmd/nexved
	@echo "==> $(DIST)/nexved ready"
	@./$(DIST)/nexved --version

dev: ## run backend (mock driver if no libvirt) + Vite with hot reload
	@echo "==> dev: backend on :8006 (mock driver), vite on :5173"
	@NEXVE_DRIVER=$${NEXVE_DRIVER:-mock} NEXVE_LISTEN=:8006 \
		cd $(BACKEND) && $(GO) run ./cmd/nexved & \
	cd $(FRONTEND) && $(NPM) run dev
	@wait

package: build ## create a release tarball with installer + binary
	@echo "==> packaging dist/nexve_$(VERSION).tar.gz"
	@rm -rf $(DIST)/pkg
	@mkdir -p $(DIST)/pkg/nexve_$(VERSION)
	@cp $(DIST)/nexved $(DIST)/pkg/nexve_$(VERSION)/
	@cp installer/install.sh installer/uninstall.sh $(DIST)/pkg/nexve_$(VERSION)/
	@cp -R installer/lib installer/systemd $(DIST)/pkg/nexve_$(VERSION)/
	@tar -C $(DIST)/pkg -czf $(DIST)/nexve_$(VERSION).tar.gz nexve_$(VERSION)
	@echo "==> $(DIST)/nexve_$(VERSION).tar.gz"
	@echo "    unpack on the target host, then: sudo ./install.sh --yes"

verify: ## compile + typecheck everything without producing dist output
	@echo "==> go vet"
	cd $(BACKEND) && $(GO) vet ./...
	@echo "==> frontend typecheck"
	cd $(FRONTEND) && $(NPM) run typecheck

clean: ## remove build output
	rm -rf $(DIST) $(FRONTEND)/dist $(EMBED_DIR)/assets $(EMBED_DIR)/index.html
	touch $(EMBED_DIR)/.gitkeep
