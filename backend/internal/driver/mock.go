package driver

// mock.go – in-memory driver for developing the web UI on any machine
// (laptops, CI) where libvirt/KVM is not available:  NEXVE_DRIVER=mock

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"
)

type Mock struct {
	mu        sync.Mutex
	instances map[string]*Instance
	isos      map[string]ISO
}

func NewMock() *Mock {
	m := &Mock{
		instances: map[string]*Instance{},
		isos: map[string]ISO{
			"debian-12-netinst.iso": {Name: "debian-12-netinst.iso", Path: "/mock/iso/debian-12-netinst.iso", SizeB: 630 * 1024 * 1024},
			"ubuntu-24.04-live.iso": {Name: "ubuntu-24.04-live.iso", Path: "/mock/iso/ubuntu-24.04-live.iso", SizeB: 2600 * 1024 * 1024},
		},
	}
	seed := []Instance{
		{ID: MakeID(TypeQEMU, "web-01"), Name: "web-01", Type: TypeQEMU, State: "running", Running: true,
			VCPUs: 2, MemoryMB: 4096, DiskGB: 40, Firmware: "uefi", Autostart: true},
		{ID: MakeID(TypeQEMU, "gitlab-runner"), Name: "gitlab-runner", Type: TypeQEMU, State: "shut off", Running: false,
			VCPUs: 4, MemoryMB: 8192, DiskGB: 100, Firmware: "bios"},
		{ID: MakeID(TypeLXC, "dns"), Name: "dns", Type: TypeLXC, State: "running", Running: true,
			VCPUs: 1, MemoryMB: 256, DiskGB: 2},
	}
	for _, i := range seed {
		cp := i
		m.instances[i.ID] = &cp
	}
	return m
}

func (m *Mock) Name() string { return "mock" }

func (m *Mock) Ping(ctx context.Context) error { return nil }

func (m *Mock) List(ctx context.Context) ([]Instance, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Instance, 0, len(m.instances))
	for _, i := range m.instances {
		out = append(out, *i)
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Name < out[b].Name })
	return out, nil
}

func (m *Mock) Get(ctx context.Context, id string) (*Instance, error) {
	if _, _, err := ParseID(id); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	i, ok := m.instances[id]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	cp := *i
	return &cp, nil
}

func (m *Mock) Create(ctx context.Context, spec CreateSpec) (*Instance, error) {
	if err := spec.ApplyDefaults(); err != nil {
		return nil, err
	}
	id := MakeID(spec.Type, spec.Name)
	m.mu.Lock()
	if _, exists := m.instances[id]; exists {
		m.mu.Unlock()
		return nil, fmt.Errorf("%w: %s", ErrExists, spec.Name)
	}
	state, running := "shut off", false
	if spec.Start {
		state, running = "running", true
	}
	m.instances[id] = &Instance{
		ID: id, Name: spec.Name, Type: spec.Type, State: state, Running: running,
		VCPUs: spec.VCPUs, MemoryMB: spec.MemoryMB, DiskGB: spec.DiskGB, Firmware: spec.Firmware,
	}
	cp := *m.instances[id]
	m.mu.Unlock()
	// Pretend provisioning takes a moment so the UI can show its spinner.
	time.Sleep(400 * time.Millisecond)
	return &cp, nil
}

func (m *Mock) Start(ctx context.Context, id string) error {
	return m.transition(id, true)
}

func (m *Mock) Stop(ctx context.Context, id string, force bool) error {
	return m.transition(id, false)
}

func (m *Mock) transition(id string, up bool) error {
	if _, _, err := ParseID(id); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	i, ok := m.instances[id]
	if !ok {
		return fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	if up {
		i.State, i.Running = "running", true
	} else {
		i.State, i.Running = "shut off", false
	}
	return nil
}

func (m *Mock) Delete(ctx context.Context, id string, purge bool) error {
	if _, _, err := ParseID(id); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.instances[id]; !ok {
		return fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	delete(m.instances, id)
	return nil
}

func (m *Mock) Console(ctx context.Context, id string) (*ConsoleInfo, error) {
	i, err := m.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if !i.Running {
		return nil, fmt.Errorf("%w: instance is not running", ErrInvalid)
	}
	if i.Type == TypeLXC {
		return &ConsoleInfo{Mode: "serial", Endpoint: "mock",
			Hint: "virsh --connect lxc:/// console " + i.Name}, nil
	}
	return &ConsoleInfo{Mode: "vnc", Endpoint: "127.0.0.1:5901",
		Hint: "mock driver – no real VNC endpoint"}, nil
}

func (m *Mock) ListISOs(ctx context.Context) ([]ISO, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]ISO, 0, len(m.isos))
	for _, i := range m.isos {
		out = append(out, i)
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Name < out[b].Name })
	return out, nil
}

func (m *Mock) SaveISO(ctx context.Context, filename string, r io.Reader) (*ISO, error) {
	name := strings.TrimSpace(filename)
	if name == "" {
		return nil, fmt.Errorf("%w: filename is required", ErrInvalid)
	}
	n, _ := io.Copy(io.Discard, r) // consume the stream; content is not stored
	iso := ISO{Name: name, Path: "/mock/iso/" + name, SizeB: n}
	m.mu.Lock()
	m.isos[name] = iso
	m.mu.Unlock()
	return &iso, nil
}
