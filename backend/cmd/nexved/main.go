// nexved – the NexVE daemon.
//
// A single binary that serves:
//   - the REST API      (/api/v1/...)
//   - the embedded web UI (built frontend, served on the same port)
//
// Configuration comes from the environment (see internal/config); command
// line flags override it. In production it runs under systemd as the 'nexve'
// user (installer/systemd/nexved.service).
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/nexve/nexve/internal/api"
	"github.com/nexve/nexve/internal/config"
	"github.com/nexve/nexve/internal/driver"
	"github.com/nexve/nexve/internal/node"
)

// version is injected at build time: -ldflags "-X main.version=0.1.0"
var version = "0.1.0-dev"

func main() {
	var (
		showVersion = flag.Bool("version", false, "print version and exit")
		listen      = flag.String("listen", "", "override listen address, e.g. :8006")
		driverFlag  = flag.String("driver", "", "override driver: libvirt | mock")
		devMode     = flag.Bool("dev", false, "development mode: mock driver, loud logging")
	)
	flag.Parse()

	if *showVersion {
		println("nexved " + version)
		return
	}

	logger := log.New(os.Stderr, "nexved ", log.LstdFlags)

	cfg, err := config.FromEnv()
	if err != nil {
		logger.Fatalf("config: %v", err)
	}
	cfg.Version = version
	if *listen != "" {
		cfg.Listen = *listen
	}
	if *driverFlag != "" {
		cfg.Driver = *driverFlag
	}
	if *devMode {
		if cfg.Driver == "libvirt" {
			logger.Printf("dev mode: switching driver libvirt -> mock")
			cfg.Driver = "mock"
		}
	}

	// --- pick the hypervisor driver ---------------------------------------
	var drv driver.Driver
	switch cfg.Driver {
	case "mock":
		drv = driver.NewMock()
		logger.Printf("driver: mock (in-memory, no hypervisor)")
	default:
		l := driver.NewLibvirt(cfg.QemuURI, cfg.LxcURI, cfg.StorageDir)
		if err := l.Ping(context.Background()); err != nil {
			logger.Printf("WARNING: libvirt not reachable (%v) – falling back to the mock driver", err)
			logger.Printf("         start it with: systemctl enable --now libvirtd")
			drv = driver.NewMock()
		} else {
			drv = l
			logger.Printf("driver: libvirt (qemu: %s, lxc: %s)", cfg.QemuURI, cfg.LxcURI)
		}
	}

	// --- node metrics collector -------------------------------------------
	collector := node.NewCollector()
	defer collector.Close()

	// --- HTTP server --------------------------------------------------------
	srv := api.New(cfg, drv, collector, logger)

	// Graceful shutdown on Ctrl-C / systemd stop.
	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	select {
	case sig := <-sigCh:
		logger.Printf("received %s – shutting down", sig)
	case err := <-errCh:
		logger.Fatalf("server: %v", err)
	}
}
