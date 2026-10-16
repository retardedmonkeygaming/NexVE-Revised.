// Package config loads daemon configuration from environment variables (the
// systemd EnvironmentFile /etc/nexve/nexve.env written by the installer).
// Flags passed on the command line override the environment.
package config

import (
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	Listen  string // e.g. ":8006"
	TLSCert string // empty => plain HTTP (development only)
	TLSKey  string

	// Auth: both mechanisms are optional. If both are empty the API is open
	// (development only – the daemon logs a loud warning in that case).
	AuthToken    string
	AuthUser     string
	AuthPassword string

	Driver     string // "libvirt" (default) | "mock"
	QemuURI    string // libvirt URI for VMs
	LxcURI     string // libvirt URI for containers
	StorageDir string // /var/lib/libvirt/images – qcow2 disks + ISO library
	DataDir    string // /var/lib/nexve – daemon state

	TrustProxy bool // honour X-Forwarded-* headers

	Version string // set from -ldflags / --version
}

// FromEnv reads the configuration, applying defaults that make the daemon
// usable on a developer laptop (mock driver, plain HTTP) out of the box.
func FromEnv() (*Config, error) {
	c := &Config{
		Listen:       getEnv("NEXVE_LISTEN", ":8006"),
		TLSCert:      os.Getenv("NEXVE_TLS_CERT"),
		TLSKey:       os.Getenv("NEXVE_TLS_KEY"),
		AuthToken:    os.Getenv("NEXVE_AUTH_TOKEN"),
		AuthUser:     os.Getenv("NEXVE_AUTH_USER"),
		AuthPassword: os.Getenv("NEXVE_AUTH_PASSWORD"),
		Driver:       getEnv("NEXVE_DRIVER", "libvirt"),
		QemuURI:      getEnv("NEXVE_QEMU_URI", "qemu:///system"),
		LxcURI:       getEnv("NEXVE_LXC_URI", "lxc:///"),
		StorageDir:   getEnv("NEXVE_STORAGE_DIR", "/var/lib/libvirt/images"),
		DataDir:      getEnv("NEXVE_DATA_DIR", "/var/lib/nexve"),
		Version:      getEnv("NEXVE_VERSION", "0.1.0-dev"),
	}
	if v, err := strconv.ParseBool(getEnv("NEXVE_TRUST_PROXY", "0")); err == nil {
		c.TrustProxy = v
	}
	if (c.TLSCert == "") != (c.TLSKey == "") {
		return nil, fmt.Errorf("NEXVE_TLS_CERT and NEXVE_TLS_KEY must be set together")
	}
	switch c.Driver {
	case "libvirt", "mock":
	default:
		return nil, fmt.Errorf("unknown NEXVE_DRIVER %q (libvirt|mock)", c.Driver)
	}
	return c, nil
}

// AuthEnabled reports whether any credential check is configured.
func (c *Config) AuthEnabled() bool {
	return c.AuthToken != "" || (c.AuthUser != "" && c.AuthPassword != "")
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
