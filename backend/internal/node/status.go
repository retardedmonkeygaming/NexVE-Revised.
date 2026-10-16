// Package node collects host-level status (CPU, memory, storage, uptime) for
// the dashboard. A background goroutine samples the expensive counters so the
// HTTP handler answers instantly.
package node

import (
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"
)

// Status is the JSON payload behind GET /api/v1/node/status.
type Status struct {
	Time          string        `json:"time"`
	Hostname      string        `json:"hostname"`
	OS            string        `json:"os"`
	Kernel        string        `json:"kernel"`
	Arch          string        `json:"arch"`
	Firmware      string        `json:"firmware"` // "uefi" | "bios" (host boot firmware)
	UptimeS       int64         `json:"uptime_seconds"`
	CPU           CPUStatus     `json:"cpu"`
	Memory        MemoryStatus  `json:"memory"`
	Storage       []DiskStatus  `json:"storage"`
	StorageTotals StorageTotals `json:"storage_totals"`
	KVM           bool          `json:"kvm"`
}

type CPUStatus struct {
	Model    string  `json:"model"`
	Logical  int     `json:"logical"`
	Physical int     `json:"physical"`
	Percent  float64 `json:"percent"`
	Load1    float64 `json:"load1"`
	Load5    float64 `json:"load5"`
	Load15   float64 `json:"load15"`
}

type MemoryStatus struct {
	Total     uint64  `json:"total_bytes"`
	Used      uint64  `json:"used_bytes"`
	Available uint64  `json:"available_bytes"`
	Percent   float64 `json:"percent"`
	SwapTotal uint64  `json:"swap_total_bytes"`
	SwapUsed  uint64  `json:"swap_used_bytes"`
}

type DiskStatus struct {
	Device  string  `json:"device"`
	Mount   string  `json:"mount"`
	Fs      string  `json:"fs"`
	Total   uint64  `json:"total_bytes"`
	Used    uint64  `json:"used_bytes"`
	Percent float64 `json:"percent"`
}

type StorageTotals struct {
	Total   uint64  `json:"total_bytes"`
	Used    uint64  `json:"used_bytes"`
	Free    uint64  `json:"free_bytes"`
	Percent float64 `json:"percent"`
}

// sample is the expensive-to-collect part, refreshed in the background.
type sample struct {
	cpuPercent float64
	avg        *load.AvgStat
	memInfo    *mem.VirtualMemoryStat
	disks      []DiskStatus
}

// Collector keeps the latest sample and static host info.
type Collector struct {
	last atomic.Pointer[sample]
	stop chan struct{}
}

// NewCollector starts the background sampler (1s CPU/memory, 10s disk).
func NewCollector() *Collector {
	c := &Collector{stop: make(chan struct{})}
	c.sampleOnce(true) // prime the first value
	go c.loop()
	return c
}

func (c *Collector) loop() {
	tick := time.NewTicker(1 * time.Second)
	defer tick.Stop()
	n := 0
	for {
		select {
		case <-c.stop:
			return
		case <-tick.C:
			n++
			c.sampleOnce(n%10 == 0) // refresh disk usage every 10s
		}
	}
}

// Close stops the background sampler.
func (c *Collector) Close() { close(c.stop) }

func (c *Collector) sampleOnce(withDisks bool) {
	prev := c.last.Load()

	s := &sample{}
	// 500ms interval keeps the CPU number responsive without much load.
	if pcts, err := cpu.Percent(500*time.Millisecond, false); err == nil && len(pcts) > 0 {
		s.cpuPercent = pcts[0]
	} else if prev != nil {
		s.cpuPercent = prev.cpuPercent
	}
	if a, err := load.Avg(); err == nil {
		s.avg = a
	}
	if m, err := mem.VirtualMemory(); err == nil {
		s.memInfo = m
	}
	if withDisks || prev == nil {
		s.disks = physicalDisks()
	} else if prev != nil {
		s.disks = prev.disks
	}
	c.last.Store(s)
}

// Status assembles the full snapshot served by the API.
func (c *Collector) Status() Status {
	s := c.last.Load()
	if s == nil {
		s = &sample{}
	}

	st := Status{
		Time:    time.Now().UTC().Format(time.RFC3339),
		Arch:    runtime.GOARCH,
		CPU:     CPUStatus{Percent: round1(s.cpuPercent)},
		Memory:  MemoryStatus{},
		Storage: s.disks,
	}
	if h, err := host.Info(); err == nil {
		st.Hostname = h.Hostname
		st.OS = h.Platform + " " + h.PlatformVersion
		st.Kernel = h.KernelVersion + " " + h.OS
	}
	if _, err := os.Stat("/sys/firmware/efi"); err == nil {
		st.Firmware = "uefi"
	} else {
		st.Firmware = "bios"
	}
	if h, err := host.Uptime(); err == nil {
		st.UptimeS = int64(h)
	}
	if infos, err := cpu.Info(); err == nil && len(infos) > 0 {
		st.CPU.Model = strings.TrimSpace(infos[0].ModelName)
	}
	// Counts() is authoritative for core counts (cpu.Info() is per-package on
	// some platforms, e.g. macOS).
	if n, err := cpu.Counts(true); err == nil {
		st.CPU.Logical = n
	}
	if p, err := cpu.Counts(false); err == nil {
		st.CPU.Physical = p
	}
	if s.avg != nil {
		st.CPU.Load1, st.CPU.Load5, st.CPU.Load15 = round1(s.avg.Load1), round1(s.avg.Load5), round1(s.avg.Load15)
	}
	if s.memInfo != nil {
		st.Memory = MemoryStatus{
			Total:     s.memInfo.Total,
			Used:      s.memInfo.Used,
			Available: s.memInfo.Available,
			Percent:   round1(s.memInfo.UsedPercent),
			SwapTotal: s.memInfo.SwapTotal,
			SwapUsed:  s.memInfo.SwapTotal - s.memInfo.SwapFree,
		}
	}
	var tot StorageTotals
	for _, d := range s.disks {
		tot.Total += d.Total
		tot.Used += d.Used
	}
	tot.Free = tot.Total - tot.Used
	if tot.Total > 0 {
		tot.Percent = round1(float64(tot.Used) / float64(tot.Total) * 100)
	}
	st.StorageTotals = tot
	st.KVM = kvmAvailable()
	return st
}

func kvmAvailable() bool {
	_, err := os.Stat("/dev/kvm")
	return err == nil
}

// pseudo filesystems we never want to show as "storage"
var skipFS = map[string]bool{
	"tmpfs": true, "proc": true, "sysfs": true, "cgroup": true, "cgroup2": true,
	"devpts": true, "devtmpfs": true, "squashfs": true, "overlay": true,
	"ramfs": true, "tracefs": true, "debugfs": true, "mqueue": true,
	"hugetlbfs": true, "pstore": true, "bpf": true, "securityfs": true,
	"configfs": true, "fusectl": true, "binfmt_misc": true, "autofs": true,
	"efivarfs": true, "nsfs": true, "rpc_pipefs": true, "smackfs": true,
	"devfs": true, "fdescfs": true,
}

// physicalDisks lists real mounted filesystems (skips loops/pseudo FS and
// duplicated bind mounts of the same device on the same mount family).
func physicalDisks() []DiskStatus {
	parts, err := disk.Partitions(false)
	if err != nil {
		return []DiskStatus{}
	}
	seen := map[string]bool{}
	var out []DiskStatus
	for _, p := range parts {
		fs := strings.ToLower(p.Fstype)
		if skipFS[fs] || strings.HasPrefix(p.Device, "/dev/loop") {
			continue
		}
		u, err := disk.Usage(p.Mountpoint)
		if err != nil || u.Total == 0 {
			continue
		}
		// Some platforms expose the same volume under several devices/slices
		// (e.g. APFS). One entry per (device, size) keeps the list readable.
		key := p.Device + "|" + strconv.FormatUint(u.Total, 10)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, DiskStatus{
			Device:  p.Device,
			Mount:   p.Mountpoint,
			Fs:      fs,
			Total:   u.Total,
			Used:    u.Used,
			Percent: round1(u.UsedPercent),
		})
	}
	if out == nil {
		out = []DiskStatus{}
	}
	return out
}

func round1(f float64) float64 { return float64(int64(f*10+0.5)) / 10 }
