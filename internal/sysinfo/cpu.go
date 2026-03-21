package sysinfo

import (
	"os/exec"
	"runtime"
	"strings"
)

// CPUInfo holds CPU metadata and usage.
type CPUInfo struct {
	Model        string  `json:"model"`
	Cores        int     `json:"cores"`
	UsagePercent float64 `json:"usage_percent"`
}

func collectCPU() CPUInfo {
	info := CPUInfo{
		Cores: runtime.NumCPU(),
	}

	switch runtime.GOOS {
	case "darwin":
		if out, err := exec.Command("sysctl", "-n", "machdep.cpu.brand_string").Output(); err == nil {
			info.Model = strings.TrimSpace(string(out))
		}
		info.UsagePercent = darwinCPUUsage()
	case "linux":
		info.Model = linuxCPUModel()
		info.UsagePercent = linuxCPUUsage()
	}
	return info
}

func darwinCPUUsage() float64 {
	// Use top -l 1 to get CPU usage snapshot
	out, err := exec.Command("top", "-l", "1", "-n", "0", "-s", "0").Output()
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "CPU usage:") {
			// "CPU usage: 5.26% user, 3.94% sys, 90.79% idle"
			parts := strings.Fields(line)
			for i, p := range parts {
				if p == "idle" && i > 0 {
					idleStr := strings.TrimSuffix(parts[i-1], "%")
					var idle float64
					if _, err := parseFloat(idleStr); err == nil {
						idle, _ = parseFloat(idleStr)
						return 100.0 - idle
					}
				}
			}
		}
	}
	return 0
}

func linuxCPUModel() string {
	out, err := exec.Command("grep", "-m", "1", "model name", "/proc/cpuinfo").Output()
	if err != nil {
		return ""
	}
	parts := strings.SplitN(string(out), ":", 2)
	if len(parts) == 2 {
		return strings.TrimSpace(parts[1])
	}
	return ""
}

func linuxCPUUsage() float64 {
	// Read /proc/stat for a quick snapshot (not a delta, but a rough average since boot)
	out, err := exec.Command("grep", "-m", "1", "cpu ", "/proc/stat").Output()
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(out))
	if len(fields) < 5 {
		return 0
	}
	user, _ := parseFloat(fields[1])
	nice, _ := parseFloat(fields[2])
	system, _ := parseFloat(fields[3])
	idle, _ := parseFloat(fields[4])
	total := user + nice + system + idle
	if total == 0 {
		return 0
	}
	return (total - idle) / total * 100.0
}
