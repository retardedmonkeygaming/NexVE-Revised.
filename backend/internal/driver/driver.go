// Package driver defines the hypervisor abstraction and the data types the
// REST API exposes. Two implementations ship with the baseline:
//
//   - Libvirt (production): talks to qemu:///system and lxc:/// through the
//     `virsh` CLI – no cgo, single static binary, works on any Debian.
//   - Mock (development): an in-memory store so the UI can be developed on a
//     machine without KVM/libvirt (e.g. a laptop).
package driver

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
)

// Instance "kinds" – a VM and a container live on separate libvirt
// connections, so the REST API addresses them as "<type>:<name>".
const (
	TypeQEMU = "qemu" // KVM/QEMU virtual machine
	TypeLXC  = "lxc"  // Linux container (libvirt LXC driver, experimental)
)

// Common driver errors – the API layer maps them to HTTP status codes.
var (
	ErrNotFound    = errors.New("not found")
	ErrExists      = errors.New("already exists")
	ErrUnavailable = errors.New("hypervisor unavailable")
	ErrInvalid     = errors.New("invalid request")
	ErrUnsupported = errors.New("unsupported operation")
)

// Instance is one VM or container, as shown in the UI.
type Instance struct {
	ID        string `json:"id"`   // "qemu:my-vm" / "lxc:web"
	Name      string `json:"name"` // libvirt domain name
	Type      string `json:"type"` // TypeQEMU | TypeLXC
	State     string `json:"state"`
	Running   bool   `json:"running"`
	VCPUs     int    `json:"vcpus"`
	MemoryMB  int    `json:"memory_mb"`
	DiskGB    int    `json:"disk_gb"`  // 0 = unknown (e.g. container rootfs)
	Firmware  string `json:"firmware"` // "bios" | "uefi" | "" (containers)
	Autostart bool   `json:"autostart"`
	Detail    string `json:"detail,omitempty"` // creation in progress / last error

	// DiskPath is server-side detail used by Delete(purge); never serialised.
	DiskPath string `json:"-"`
}

// CreateSpec describes a new instance. DiskGB/ISO/Firmware only apply to
// QEMU VMs; containers get a debootstrap rootfs of unbounded size.
type CreateSpec struct {
	Type     string `json:"type"` // TypeQEMU (default) | TypeLXC
	Name     string `json:"name"`
	VCPUs    int    `json:"vcpus"`
	MemoryMB int    `json:"memory_mb"`
	DiskGB   int    `json:"disk_gb"`
	ISO      string `json:"iso"`      // file name inside the ISO dir, or absolute path
	Firmware string `json:"firmware"` // "bios" (default) | "uefi"
	Network  string `json:"network"`  // libvirt network name, default "default"
	Suite    string `json:"suite"`    // LXC only: debootstrap suite (default "stable")
	Start    bool   `json:"start"`    // boot immediately after creation
}

// ISO is an installation image available in the ISO library.
type ISO struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	SizeB int64  `json:"size_bytes"`
}

// ConsoleInfo tells the UI how to reach a guest console. A full browser
// console (noVNC / websocket) is future work; for now we hand out the
// connection details plus a copy-pasteable hint.
type ConsoleInfo struct {
	Mode     string `json:"mode"`     // "vnc" | "serial"
	Endpoint string `json:"endpoint"` // e.g. "127.0.0.1:5900"
	Hint     string `json:"hint"`     // human-readable access instructions
}

// Driver is the hypervisor backend contract.
type Driver interface {
	Name() string
	// Ping verifies the hypervisor is reachable (used by /health).
	Ping(ctx context.Context) error

	List(ctx context.Context) ([]Instance, error)
	Get(ctx context.Context, id string) (*Instance, error)
	Create(ctx context.Context, spec CreateSpec) (*Instance, error)
	Start(ctx context.Context, id string) error
	// Stop issues an ACPI/poweroff request; force=true pulls the plug.
	Stop(ctx context.Context, id string, force bool) error
	// Delete removes the instance; purge=true also removes its disk/rootfs.
	Delete(ctx context.Context, id string, purge bool) error
	Console(ctx context.Context, id string) (*ConsoleInfo, error)

	ListISOs(ctx context.Context) ([]ISO, error)
	SaveISO(ctx context.Context, filename string, r io.Reader) (*ISO, error)
}

// ParseID splits "qemu:my-vm" into its parts.
func ParseID(id string) (typ, name string, err error) {
	typ, name, ok := strings.Cut(id, ":")
	if !ok || name == "" {
		return "", "", fmt.Errorf("%w: malformed id %q (expected <type>:<name>)", ErrInvalid, id)
	}
	if typ != TypeQEMU && typ != TypeLXC {
		return "", "", fmt.Errorf("%w: unknown instance type %q", ErrInvalid, typ)
	}
	return typ, name, nil
}

// MakeID builds the public id from its parts.
func MakeID(typ, name string) string { return typ + ":" + name }

// SanitizeName reduces an arbitrary label to a libvirt-safe domain name:
// lowercase [a-z0-9-], starting with a letter or digit, max 63 chars.
func SanitizeName(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
		case r == '-' || r == '_' || r == '.' || r == ' ':
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) > 63 {
		out = out[:63]
		out = strings.Trim(out, "-")
	}
	return out
}

// ApplyDefaults fills in sane baseline defaults and validates required fields.
func (s *CreateSpec) ApplyDefaults() error {
	s.Name = SanitizeName(s.Name)
	if s.Name == "" {
		return fmt.Errorf("%w: name is required", ErrInvalid)
	}
	if s.Type == "" {
		s.Type = TypeQEMU
	}
	if s.Type != TypeQEMU && s.Type != TypeLXC {
		return fmt.Errorf("%w: type must be qemu or lxc", ErrInvalid)
	}
	if s.VCPUs <= 0 {
		s.VCPUs = 1
	}
	if s.VCPUs > 128 {
		return fmt.Errorf("%w: vcpus must be <= 128", ErrInvalid)
	}
	if s.MemoryMB <= 0 {
		s.MemoryMB = 1024
	}
	if s.MemoryMB < 64 || s.MemoryMB > 4*1024*1024 {
		return fmt.Errorf("%w: memory_mb must be between 64 and 4194304", ErrInvalid)
	}
	if s.Firmware == "" {
		s.Firmware = "bios"
	}
	if s.Firmware != "bios" && s.Firmware != "uefi" {
		return fmt.Errorf("%w: firmware must be bios or uefi", ErrInvalid)
	}
	if s.Network == "" {
		s.Network = "default"
	}
	if s.Suite == "" {
		s.Suite = "stable"
	}
	if s.Type == TypeQEMU {
		if s.DiskGB <= 0 {
			s.DiskGB = 16
		}
		if s.DiskGB > 65536 {
			return fmt.Errorf("%w: disk_gb must be <= 65536", ErrInvalid)
		}
	}
	return nil
}
