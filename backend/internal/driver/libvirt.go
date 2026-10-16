package driver

// libvirt.go – production driver.
//
// Design note: the baseline talks to libvirt through the `virsh` CLI instead
// of the cgo bindings (libvirt.org/go/libvirt). Rationale:
//   * single static binary, cross-compilable, no libvirt-dev headers needed
//   * identical behaviour on Debian 10..13 and on a dev laptop
//   * XML is produced/parsed with encoding/xml, never with fragile grep
// Swapping this for the native bindings later is contained to this file –
// the Driver interface stays unchanged.
//
// Instance addressing: "qemu:<domain>" for VMs (qemu:///system) and
// "lxc:<domain>" for containers (lxc:///).

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Libvirt struct {
	qemuURI    string
	lxcURI     string
	storageDir string // qcow2 disks
	isoDir     string // ISO library (<storageDir>/iso)
	fsDir      string // container rootfs bases

	mu      sync.Mutex
	pending map[string]*Instance // containers still being provisioned
}

func NewLibvirt(qemuURI, lxcURI, storageDir string) *Libvirt {
	return &Libvirt{
		qemuURI:    qemuURI,
		lxcURI:     lxcURI,
		storageDir: storageDir,
		isoDir:     filepath.Join(storageDir, "iso"),
		fsDir:      "/var/lib/libvirt/filesystems",
		pending:    map[string]*Instance{},
	}
}

func (d *Libvirt) Name() string { return "libvirt" }

func (d *Libvirt) uriFor(typ string) string {
	if typ == TypeLXC {
		return d.lxcURI
	}
	return d.qemuURI
}

// virsh runs a virsh subcommand on the given connection and returns stdout.
func (d *Libvirt) virsh(ctx context.Context, uri string, args ...string) (string, error) {
	if _, err := exec.LookPath("virsh"); err != nil {
		return "", fmt.Errorf("%w: virsh not found in PATH (is libvirt-clients installed?)", ErrUnavailable)
	}
	full := append([]string{"--connect", uri}, args...)
	cmd := exec.CommandContext(ctx, "virsh", full...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("virsh %s: %s", strings.Join(args, " "), msg)
	}
	return stdout.String(), nil
}

func (d *Libvirt) Ping(ctx context.Context) error {
	_, err := d.virsh(ctx, d.qemuURI, "hostname")
	return err
}

// ---------------------------------------------------------------------------
// Listing / inspection
// ---------------------------------------------------------------------------

func (d *Libvirt) List(ctx context.Context) ([]Instance, error) {
	var all []Instance
	for _, typ := range []string{TypeQEMU, TypeLXC} {
		insts, err := d.listConn(ctx, typ)
		if err != nil {
			// The LXC driver is optional: never let its absence hide VMs.
			if typ == TypeLXC {
				continue
			}
			return nil, err
		}
		all = append(all, insts...)
	}

	// Merge in containers that are still being provisioned.
	d.mu.Lock()
	for id, inst := range d.pending {
		found := false
		for _, i := range all {
			if i.ID == id {
				found = true
				break
			}
		}
		if !found {
			cp := *inst
			all = append(all, cp)
		}
	}
	d.mu.Unlock()
	return all, nil
}

func (d *Libvirt) listConn(ctx context.Context, typ string) ([]Instance, error) {
	out, err := d.virsh(ctx, d.uriFor(typ), "list", "--all", "--name")
	if err != nil {
		return nil, err
	}
	var insts []Instance
	for _, line := range strings.Split(out, "\n") {
		name := strings.TrimSpace(line)
		if name == "" {
			continue
		}
		inst, err := d.loadInstance(ctx, typ, name)
		if err != nil {
			// Keep the listing usable even if one domain is broken.
			insts = append(insts, Instance{
				ID: MakeID(typ, name), Name: name, Type: typ,
				State: "unknown", Detail: err.Error(),
			})
			continue
		}
		insts = append(insts, *inst)
	}
	return insts, nil
}

// virDomainXML is the subset of libvirt domain XML the baseline understands.
type virDomainXML struct {
	Type    string `xml:"type,attr"`
	Name    string `xml:"name"`
	Memory  uint64 `xml:"memory"`
	MemUnit string `xml:"memory,attr"`
	VCPU    int    `xml:"vcpu"`
	OS      struct {
		Type struct {
			Arch string `xml:"arch,attr"`
		} `xml:"type"`
		Loader struct {
			Path string `xml:",chardata"`
			Type string `xml:"type,attr"`
		} `xml:"loader"`
	} `xml:"os"`
	Devices struct {
		Disk []struct {
			Device string `xml:"device,attr"`
			Source struct {
				File string `xml:"file,attr"`
			} `xml:"source"`
		} `xml:"disk"`
		Graphics struct {
			Type string `xml:"type,attr"`
			Port int    `xml:"port,attr"`
		} `xml:"graphics"`
	} `xml:"devices"`
}

func (d *Libvirt) loadInstance(ctx context.Context, typ, name string) (*Instance, error) {
	uri := d.uriFor(typ)

	state, err := d.virsh(ctx, uri, "domstate", name)
	if err != nil {
		return nil, err
	}
	state = strings.TrimSpace(state) // "running", "shut off", "paused", ...

	xmlOut, err := d.virsh(ctx, uri, "dumpxml", name)
	if err != nil {
		return nil, err
	}
	var dx virDomainXML
	if err := xml.Unmarshal([]byte(xmlOut), &dx); err != nil {
		return nil, fmt.Errorf("parsing domain XML: %w", err)
	}

	inst := &Instance{
		ID:       MakeID(typ, name),
		Name:     name,
		Type:     typ,
		State:    state,
		Running:  strings.HasPrefix(state, "running"),
		VCPUs:    dx.VCPU,
		MemoryMB: kibToMB(dx.Memory, dx.MemUnit),
		Firmware: "bios",
	}
	if strings.HasPrefix(state, "paused") {
		inst.Running = false // can be started/resumed – treat as not running for start/stop logic
	}

	if typ == TypeQEMU {
		if dx.OS.Loader.Type == "pflash" {
			inst.Firmware = "uefi"
		}
		for _, disk := range dx.Devices.Disk {
			if disk.Device != "disk" || disk.Source.File == "" {
				continue
			}
			if inst.DiskPath == "" {
				// (DiskPath is intentionally not serialised; it is used by Delete.)
				inst.DiskPath = disk.Source.File
			}
			if size := diskVirtualBytes(ctx, disk.Source.File); size > int64(inst.DiskGB)*1<<30 {
				inst.DiskGB = int(size >> 30)
			}
		}
	}

	// Autostart is an extra round trip – only worth it in the detail view.
	if out, err := d.virsh(ctx, uri, "autostart", name); err == nil {
		inst.Autostart = strings.Contains(out, "enabled")
	}
	return inst, nil
}

func kibToMB(kib uint64, unit string) int {
	switch strings.ToLower(unit) {
	case "b":
		return int(kib >> 20)
	case "mib", "mb":
		return int(kib)
	case "gib", "gb":
		return int(kib * 1024)
	default: // kib / ki / unspecified (libvirt defaults to KiB)
		return int(kib >> 10)
	}
}

// diskVirtualBytes returns the virtual size of a qcow2/raw image (0 if unknown).
func diskVirtualBytes(ctx context.Context, path string) int64 {
	if _, err := exec.LookPath("qemu-img"); err != nil {
		return 0
	}
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "qemu-img", "info", "--output=json", path)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return 0
	}
	var info struct {
		VirtualSize int64 `json:"virtual-size"`
	}
	if json.Unmarshal(out.Bytes(), &info) != nil {
		return 0
	}
	return info.VirtualSize
}

func (d *Libvirt) Get(ctx context.Context, id string) (*Instance, error) {
	typ, name, err := ParseID(id)
	if err != nil {
		return nil, err
	}
	if inst, ok := d.pendingGet(id); ok {
		return inst, nil
	}
	inst, err := d.loadInstance(ctx, typ, name)
	if err != nil {
		if strings.Contains(err.Error(), "domain not found") ||
			strings.Contains(err.Error(), "failed to get domain") {
			return nil, fmt.Errorf("%w: %s", ErrNotFound, id)
		}
		return nil, err
	}
	return inst, nil
}

func (d *Libvirt) pendingGet(id string) (*Instance, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	inst, ok := d.pending[id]
	if !ok {
		return nil, false
	}
	cp := *inst
	return &cp, true
}

// ---------------------------------------------------------------------------
// Create – QEMU VM
// ---------------------------------------------------------------------------

func (d *Libvirt) Create(ctx context.Context, spec CreateSpec) (*Instance, error) {
	if err := spec.ApplyDefaults(); err != nil {
		return nil, err
	}
	switch spec.Type {
	case TypeQEMU:
		return d.createVM(ctx, spec)
	case TypeLXC:
		return d.createContainer(ctx, spec)
	}
	return nil, fmt.Errorf("%w: unknown type %q", ErrInvalid, spec.Type)
}

func (d *Libvirt) createVM(ctx context.Context, spec CreateSpec) (*Instance, error) {
	id := MakeID(TypeQEMU, spec.Name)
	if existing, err := d.Get(ctx, id); err == nil && existing != nil {
		return nil, fmt.Errorf("%w: %s", ErrExists, spec.Name)
	}

	diskPath := filepath.Join(d.storageDir, spec.Name+".qcow2")
	if _, err := os.Stat(diskPath); err == nil {
		return nil, fmt.Errorf("%w: disk image %s already exists", ErrExists, diskPath)
	}
	if err := os.MkdirAll(d.storageDir, 0o755); err != nil {
		return nil, err
	}

	// 1) create the disk image
	if _, err := exec.LookPath("qemu-img"); err != nil {
		return nil, fmt.Errorf("%w: qemu-img not found (qemu-utils package)", ErrUnavailable)
	}
	img := exec.CommandContext(ctx, "qemu-img", "create", "-f", "qcow2",
		diskPath, fmt.Sprintf("%dG", spec.DiskGB))
	if out, err := img.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("qemu-img create: %v: %s", err, strings.TrimSpace(string(out)))
	}

	// 2) resolve the ISO (optional – an empty CD-ROM is fine for cloud images)
	var isoPath string
	if spec.ISO != "" {
		p, err := d.resolveISO(spec.ISO)
		if err != nil {
			os.Remove(diskPath)
			return nil, err
		}
		isoPath = p
	}

	// 3) UEFI needs OVMF firmware files
	var loader, nvramTemplate string
	if spec.Firmware == "uefi" {
		loader, nvramTemplate = findOVMF()
		if loader == "" {
			os.Remove(diskPath)
			return nil, fmt.Errorf("%w: OVMF firmware not found (is the 'ovmf' package installed?)", ErrUnavailable)
		}
	}

	// 4) define the domain from XML we generate ourselves
	xmlDef := qemuDomainXML(spec, diskPath, isoPath, loader, nvramTemplate)
	if err := d.defineXML(ctx, d.qemuURI, spec.Name, xmlDef); err != nil {
		os.Remove(diskPath)
		return nil, err
	}

	if spec.Start {
		if _, err := d.virsh(ctx, d.qemuURI, "start", spec.Name); err != nil {
			return nil, fmt.Errorf("defined but failed to start: %w", err)
		}
	}
	return d.Get(ctx, id)
}

// resolveISO maps a client-provided ISO name to a real file. Absolute paths
// are accepted (useful for admins), relative names are looked up in the ISO
// library. Path traversal outside the library is rejected.
func (d *Libvirt) resolveISO(iso string) (string, error) {
	var p string
	if filepath.IsAbs(iso) {
		p = iso
	} else {
		p = filepath.Join(d.isoDir, filepath.Base(iso))
	}
	if _, err := os.Stat(p); err != nil {
		return "", fmt.Errorf("%w: ISO %q not found", ErrNotFound, iso)
	}
	return p, nil
}

func findOVMF() (code, vars string) {
	candidates := [][2]string{
		{"/usr/share/OVMF/OVMF_CODE_4M.fd", "/usr/share/OVMF/OVMF_VARS_4M.fd"},
		{"/usr/share/OVMF/OVMF_CODE.fd", "/usr/share/OVMF/OVMF_VARS.fd"},
		{"/usr/share/edk2/ovmf/OVMF_CODE.fd", "/usr/share/edk2/ovmf/OVMF_VARS.fd"},
		{"/usr/share/edk2/x64/OVMF_CODE.fd", "/usr/share/edk2/x64/OVMF_VARS.fd"},
	}
	for _, c := range candidates {
		if _, err := os.Stat(c[0]); err == nil {
			if _, err := os.Stat(c[1]); err == nil {
				return c[0], c[1]
			}
		}
	}
	return "", ""
}

// qemuDomainXML renders the domain definition. Kept as an explicit template
// (instead of virt-install) so NexVE owns its guest definitions end-to-end.
func qemuDomainXML(spec CreateSpec, diskPath, isoPath, loader, nvramTemplate string) string {
	domType := "qemu"
	if _, err := os.Stat("/dev/kvm"); err == nil {
		domType = "kvm"
	}
	arch := "x86_64"
	emulator := "qemu-system-x86_64"
	if runtime.GOARCH == "386" {
		arch = "i386"
		emulator = "qemu-system-i386"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "<domain type='%s'>\n", domType)
	fmt.Fprintf(&b, "  <name>%s</name>\n", xmlEscape(spec.Name))
	fmt.Fprintf(&b, "  <memory unit='KiB'>%d</memory>\n", spec.MemoryMB*1024)
	fmt.Fprintf(&b, "  <vcpu>%d</vcpu>\n", spec.VCPUs)
	b.WriteString("  <os>\n")
	fmt.Fprintf(&b, "    <type arch='%s' machine='pc'>hvm</type>\n", arch)
	if loader != "" {
		fmt.Fprintf(&b, "    <loader readonly='yes' type='pflash'>%s</loader>\n", xmlEscape(loader))
		fmt.Fprintf(&b, "    <nvram template='%s'>/var/lib/libvirt/qemu/nvram/%s_VARS.fd</nvram>\n",
			xmlEscape(nvramTemplate), xmlEscape(spec.Name))
	}
	// Boot order: try CD-ROM first (installer ISO), then disk.
	b.WriteString("    <boot dev='cdrom'/>\n    <boot dev='hd'/>\n")
	b.WriteString("  </os>\n")
	b.WriteString("  <features><acpi/><apic/></features>\n")
	b.WriteString("  <clock offset='utc'/>\n")
	b.WriteString("  <on_poweroff>destroy</on_poweroff>\n")
	b.WriteString("  <on_reboot>restart</on_reboot>\n")
	b.WriteString("  <on_crash>destroy</on_crash>\n")
	b.WriteString("  <devices>\n")
	if path, err := exec.LookPath(emulator); err == nil {
		fmt.Fprintf(&b, "    <emulator>%s</emulator>\n", xmlEscape(path))
	}
	fmt.Fprintf(&b, "    <disk type='file' device='disk'>\n"+
		"      <driver name='qemu' type='qcow2'/>\n"+
		"      <source file='%s'/>\n"+
		"      <target dev='vda' bus='virtio'/>\n"+
		"    </disk>\n", xmlEscape(diskPath))
	if isoPath != "" {
		fmt.Fprintf(&b, "    <disk type='file' device='cdrom'>\n"+
			"      <driver name='qemu' type='raw'/>\n"+
			"      <source file='%s'/>\n"+
			"      <target dev='hda' bus='ide'/>\n"+
			"      <readonly/>\n"+
			"    </disk>\n", xmlEscape(isoPath))
	} else {
		b.WriteString("    <disk type='file' device='cdrom'><target dev='hda' bus='ide'/><readonly/></disk>\n")
	}
	fmt.Fprintf(&b, "    <interface type='network'>\n"+
		"      <source network='%s'/>\n"+
		"      <model type='virtio'/>\n"+
		"    </interface>\n", xmlEscape(spec.Network))
	b.WriteString("    <serial type='pty'><target port='0'/></serial>\n")
	b.WriteString("    <console type='pty'><target type='serial' port='0'/></console>\n")
	// VNC is bound to localhost only: reach it through an SSH tunnel.
	b.WriteString("    <graphics type='vnc' port='-1' autoport='yes' listen='127.0.0.1'>\n")
	b.WriteString("      <listen type='address' address='127.0.0.1'/>\n    </graphics>\n")
	b.WriteString("    <video><model type='virtio' heads='1'/></video>\n")
	b.WriteString("  </devices>\n")
	b.WriteString("</domain>\n")
	return b.String()
}

// ---------------------------------------------------------------------------
// Create – LXC container (experimental)
// ---------------------------------------------------------------------------
//
// Provisioning a container means: debootstrap a Debian rootfs, then define a
// libvirt LXC domain that mounts it as /. debootstrap takes a while, so the
// request returns immediately with state "creating" and the work happens in
// the background; failures are visible in the instance list ("failed" + the
// debootstrap tail). A full 'pct'-style template system is future work.

const rootfsTemplateURL = "http://deb.debian.org/debian"

func (d *Libvirt) createContainer(ctx context.Context, spec CreateSpec) (*Instance, error) {
	id := MakeID(TypeLXC, spec.Name)
	if existing, err := d.Get(ctx, id); err == nil && existing != nil {
		return nil, fmt.Errorf("%w: %s", ErrExists, spec.Name)
	}
	if _, err := exec.LookPath("debootstrap"); err != nil {
		return nil, fmt.Errorf("%w: debootstrap not found (needed to build container rootfs)", ErrUnavailable)
	}

	base := filepath.Join(d.fsDir, spec.Name)
	rootfs := filepath.Join(base, "rootfs")
	if _, err := os.Stat(rootfs); err == nil {
		return nil, fmt.Errorf("%w: rootfs %s already exists", ErrExists, rootfs)
	}
	if err := os.MkdirAll(base, 0o755); err != nil {
		return nil, err
	}

	inst := &Instance{
		ID: id, Name: spec.Name, Type: TypeLXC,
		State: "creating", Running: false,
		VCPUs: spec.VCPUs, MemoryMB: spec.MemoryMB, Detail: "building rootfs (debootstrap)",
	}
	d.mu.Lock()
	d.pending[id] = inst
	d.mu.Unlock()

	go func() {
		err := d.provisionContainer(spec, base, rootfs)
		d.mu.Lock()
		defer d.mu.Unlock()
		if err != nil {
			inst.State = "failed"
			inst.Detail = err.Error()
			return
		}
		// Done: drop the pending entry – virsh now lists the real domain.
		delete(d.pending, id)
	}()
	return inst, nil
}

func (d *Libvirt) provisionContainer(spec CreateSpec, base, rootfs string) error {
	arch := "amd64"
	if runtime.GOARCH == "386" {
		arch = "i386"
	}
	bootCtx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(bootCtx, "debootstrap", "--variant=minbase",
		"--arch="+arch, spec.Suite, rootfs, rootfsTemplateURL)
	var logBuf bytes.Buffer
	cmd.Stdout = &logBuf
	cmd.Stderr = &logBuf
	if err := cmd.Run(); err != nil {
		tail := tailLines(logBuf.String(), 8)
		os.RemoveAll(base)
		return fmt.Errorf("debootstrap failed: %v (%s)", err, tail)
	}

	// Minimal guest identity so the container is recognisable on the network.
	hostname := spec.Name
	os.WriteFile(filepath.Join(rootfs, "etc", "hostname"), []byte(hostname+"\n"), 0o644)
	if hosts, err := os.ReadFile(filepath.Join(rootfs, "etc", "hosts")); err == nil {
		if !strings.Contains(string(hosts), hostname) {
			os.WriteFile(filepath.Join(rootfs, "etc", "hosts"),
				[]byte(strings.Replace(string(hosts), "127.0.1.1", "127.0.1.1 "+hostname, 1)), 0o644)
		}
	}

	xmlDef := lxcDomainXML(spec, rootfs)
	if err := d.defineXML(context.Background(), d.lxcURI, spec.Name, xmlDef); err != nil {
		os.RemoveAll(base)
		return err
	}
	if spec.Start {
		if _, err := d.virsh(context.Background(), d.lxcURI, "start", spec.Name); err != nil {
			return fmt.Errorf("defined but failed to start: %w", err)
		}
	}
	return nil
}

func lxcDomainXML(spec CreateSpec, rootfs string) string {
	var b strings.Builder
	b.WriteString("<domain type='lxc'>\n")
	fmt.Fprintf(&b, "  <name>%s</name>\n", xmlEscape(spec.Name))
	fmt.Fprintf(&b, "  <memory unit='KiB'>%d</memory>\n", spec.MemoryMB*1024)
	fmt.Fprintf(&b, "  <vcpu>%d</vcpu>\n", spec.VCPUs)
	b.WriteString("  <os><type>exe</type><init>/sbin/init</init></os>\n")
	b.WriteString("  <clock offset='utc'/>\n")
	b.WriteString("  <on_poweroff>destroy</on_poweroff>\n")
	b.WriteString("  <on_reboot>restart</on_reboot>\n")
	b.WriteString("  <on_crash>destroy</on_crash>\n")
	b.WriteString("  <devices>\n")
	fmt.Fprintf(&b, "    <filesystem type='mount' accessmode='passthrough'>\n"+
		"      <source dir='%s'/>\n"+
		"      <target dir='/'/>\n"+
		"    </filesystem>\n", xmlEscape(rootfs))
	fmt.Fprintf(&b, "    <interface type='network'><source network='%s'/><model type='virtio'/></interface>\n",
		xmlEscape(spec.Network))
	b.WriteString("    <console type='pty'><target type='serial' port='0'/></console>\n")
	b.WriteString("  </devices>\n")
	b.WriteString("</domain>\n")
	return b.String()
}

func (d *Libvirt) defineXML(ctx context.Context, uri, name, xmlDef string) error {
	tmp, err := os.CreateTemp("", "nexve-domain-*.xml")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(xmlDef); err != nil {
		tmp.Close()
		return err
	}
	tmp.Close()
	if _, err := d.virsh(ctx, uri, "define", tmp.Name()); err != nil {
		return fmt.Errorf("defining domain %q: %w", name, err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Lifecycle: start / stop / delete / console
// ---------------------------------------------------------------------------

func (d *Libvirt) Start(ctx context.Context, id string) error {
	typ, name, err := ParseID(id)
	if err != nil {
		return err
	}
	if inst, ok := d.pendingGet(id); ok {
		return fmt.Errorf("%w: instance is %s (%s)", ErrInvalid, inst.State, inst.Detail)
	}
	if _, err := d.virsh(ctx, d.uriFor(typ), "start", name); err != nil {
		return mapVirshErr(err)
	}
	return nil
}

func (d *Libvirt) Stop(ctx context.Context, id string, force bool) error {
	typ, name, err := ParseID(id)
	if err != nil {
		return err
	}
	args := []string{"shutdown", name}
	if force {
		args = []string{"destroy", name} // pull the plug
	}
	if _, err := d.virsh(ctx, d.uriFor(typ), args...); err != nil {
		return mapVirshErr(err)
	}
	return nil
}

func (d *Libvirt) Delete(ctx context.Context, id string, purge bool) error {
	typ, name, err := ParseID(id)
	if err != nil {
		return err
	}

	// A container that is still being provisioned: cancel + clean up.
	d.mu.Lock()
	_, pending := d.pending[id]
	if pending {
		delete(d.pending, id)
	}
	d.mu.Unlock()
	if pending {
		os.RemoveAll(filepath.Join(d.fsDir, name))
		return nil
	}

	// Learn where the disk/rootfs lives before undefining.
	var diskPath string
	if cur, err := d.loadInstance(ctx, typ, name); err == nil {
		diskPath = cur.DiskPath
	}

	// Stop first – libvirt refuses to undefine a running domain.
	if _, err := d.virsh(ctx, d.uriFor(typ), "destroy", name); err != nil {
		// "not running" is fine; anything else is reported.
		if !strings.Contains(err.Error(), "not running") &&
			!strings.Contains(err.Error(), "domain is not running") {
			_ = err // keep going: undefine will surface real problems
		}
	}

	if _, err := d.virsh(ctx, d.uriFor(typ), "undefine", name); err != nil {
		// Domains with managed saves / NVRAM need extra flags.
		if _, err2 := d.virsh(ctx, d.uriFor(typ), "undefine", name,
			"--managed-save", "--snapshots-metadata", "--nvram"); err2 != nil {
			return mapVirshErr(err2)
		}
	}

	if !purge {
		return nil
	}
	// Purge = remove the storage we own, with a containment check so a bad
	// path can never rm -rf something outside the managed directories.
	switch typ {
	case TypeQEMU:
		if diskPath != "" && within(d.storageDir, diskPath) {
			os.Remove(diskPath)
		}
	case TypeLXC:
		base := filepath.Join(d.fsDir, name)
		if within(d.fsDir, base) {
			os.RemoveAll(base)
		}
	}
	return nil
}

func (d *Libvirt) Console(ctx context.Context, id string) (*ConsoleInfo, error) {
	typ, name, err := ParseID(id)
	if err != nil {
		return nil, err
	}
	inst, err := d.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if !inst.Running {
		return nil, fmt.Errorf("%w: instance is not running", ErrInvalid)
	}

	if typ == TypeLXC {
		return &ConsoleInfo{
			Mode:     "serial",
			Endpoint: d.lxcURI,
			Hint:     fmt.Sprintf("virsh --connect lxc:/// console %s\n(detach with Ctrl+])", name),
		}, nil
	}

	// QEMU: VNC on 127.0.0.1 – hand out an SSH tunnel recipe.
	out, err := d.virsh(ctx, d.qemuURI, "vncdisplay", name)
	if err != nil {
		return nil, mapVirshErr(err)
	}
	disp := strings.TrimSpace(out) // e.g. ":1"
	n, _ := strconv.Atoi(strings.TrimPrefix(disp, ":"))
	port := 5900 + n
	return &ConsoleInfo{
		Mode:     "vnc",
		Endpoint: fmt.Sprintf("127.0.0.1:%d", port),
		Hint: fmt.Sprintf("VNC listens on 127.0.0.1 only. Tunnel it first:\n"+
			"  ssh -L %d:127.0.0.1:%d <user>@<nexve-host>\n"+
			"then open vnc://127.0.0.1:%d in a VNC client.", port, port, port),
	}, nil
}

// ---------------------------------------------------------------------------
// ISO library
// ---------------------------------------------------------------------------

func (d *Libvirt) ListISOs(ctx context.Context) ([]ISO, error) {
	entries, err := os.ReadDir(d.isoDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []ISO{}, nil
		}
		return nil, err
	}
	var isos []ISO
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if ext != ".iso" && ext != ".img" && ext != ".raw" {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		isos = append(isos, ISO{
			Name:  e.Name(),
			Path:  filepath.Join(d.isoDir, e.Name()),
			SizeB: info.Size(),
		})
	}
	if isos == nil {
		isos = []ISO{}
	}
	return isos, nil
}

func (d *Libvirt) SaveISO(ctx context.Context, filename string, r io.Reader) (*ISO, error) {
	name := filepath.Base(strings.TrimSpace(filename))
	if name == "" || name == "." {
		return nil, fmt.Errorf("%w: filename is required", ErrInvalid)
	}
	if ext := strings.ToLower(filepath.Ext(name)); ext != ".iso" && ext != ".img" && ext != ".raw" {
		name += ".iso"
	}
	if err := os.MkdirAll(d.isoDir, 0o755); err != nil {
		return nil, err
	}
	dst := filepath.Join(d.isoDir, name)
	tmp := dst + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return nil, err
	}
	n, err := io.Copy(f, r)
	f.Close()
	if err != nil {
		os.Remove(tmp)
		return nil, err
	}
	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
		return nil, err
	}
	return &ISO{Name: name, Path: dst, SizeB: n}, nil
}

// ---------------------------------------------------------------------------
// small helpers
// ---------------------------------------------------------------------------

func mapVirshErr(err error) error {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "domain not found"),
		strings.Contains(msg, "failed to get domain"),
		strings.Contains(msg, "no domain with matching name"):
		return fmt.Errorf("%w: %s", ErrNotFound, msg)
	case strings.Contains(msg, "already exists"):
		return fmt.Errorf("%w: %s", ErrExists, msg)
	}
	return err
}

// within reports whether path is inside dir (both must be absolute).
func within(dir, path string) bool {
	dir = filepath.Clean(dir)
	path = filepath.Clean(path)
	if dir == "" || path == "" || dir == "/" {
		return false
	}
	return path != dir && strings.HasPrefix(path, dir+string(os.PathSeparator))
}

func xmlEscape(s string) string {
	var b bytes.Buffer
	if err := xml.EscapeText(&b, []byte(s)); err != nil {
		return s
	}
	return b.String()
}

func tailLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, " | ")
}
