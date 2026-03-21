package sysinfo

import (
	"os/exec"
	"runtime"
	"strings"
)

// MemoryInfo holds RAM usage.
type MemoryInfo struct {
	TotalBytes   uint64  `json:"total_bytes"`
	UsedBytes    uint64  `json:"used_bytes"`
	UsagePercent float64 `json:"usage_percent"`
}

func collectMemory() MemoryInfo {
	switch runtime.GOOS {
	case "darwin":
		return darwinMemory()
	case "linux":
		return linuxMemory()
	}
	return MemoryInfo{}
}

func darwinMemory() MemoryInfo {
	var info MemoryInfo

	// Total memory
	if out, err := exec.Command("sysctl", "-n", "hw.memsize").Output(); err == nil {
		info.TotalBytes, _ = parseUint(strings.TrimSpace(string(out)))
	}

	// Used memory via vm_stat
	if out, err := exec.Command("vm_stat").Output(); err == nil {
		pageSize := uint64(16384) // Apple Silicon default
		if ps, err := exec.Command("sysctl", "-n", "hw.pagesize").Output(); err == nil {
			pageSize, _ = parseUint(strings.TrimSpace(string(ps)))
		}

		var active, wired, compressed uint64
		for _, line := range strings.Split(string(out), "\n") {
			line = strings.TrimSpace(line)
			if v := vmStatValue(line, "Pages active:"); v > 0 {
				active = v
			} else if v := vmStatValue(line, "Pages wired down:"); v > 0 {
				wired = v
			} else if v := vmStatValue(line, "Pages occupied by compressor:"); v > 0 {
				compressed = v
			}
		}
		info.UsedBytes = (active + wired + compressed) * pageSize
	}

	if info.TotalBytes > 0 {
		info.UsagePercent = float64(info.UsedBytes) / float64(info.TotalBytes) * 100.0
	}
	return info
}

func vmStatValue(line, prefix string) uint64 {
	if !strings.HasPrefix(line, prefix) {
		return 0
	}
	s := strings.TrimPrefix(line, prefix)
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, ".")
	v, _ := parseUint(s)
	return v
}

func linuxMemory() MemoryInfo {
	var info MemoryInfo
	out, err := exec.Command("cat", "/proc/meminfo").Output()
	if err != nil {
		return info
	}
	var total, available uint64
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "MemTotal:") {
			total, _ = parseUint(strings.TrimSpace(strings.Fields(line)[1]))
			total *= 1024 // kB → bytes
		} else if strings.HasPrefix(line, "MemAvailable:") {
			available, _ = parseUint(strings.TrimSpace(strings.Fields(line)[1]))
			available *= 1024
		}
	}
	info.TotalBytes = total
	if total > available {
		info.UsedBytes = total - available
	}
	if total > 0 {
		info.UsagePercent = float64(info.UsedBytes) / float64(total) * 100.0
	}
	return info
}
