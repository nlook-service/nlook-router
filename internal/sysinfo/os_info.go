package sysinfo

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// OSInfo holds operating system metadata.
type OSInfo struct {
	Name     string `json:"name"`
	Version  string `json:"version"`
	Arch     string `json:"arch"`
	Hostname string `json:"hostname"`
}

func collectOS() OSInfo {
	info := OSInfo{
		Arch: runtime.GOARCH,
	}
	info.Hostname, _ = os.Hostname()

	switch runtime.GOOS {
	case "darwin":
		info.Name = "macOS"
		if out, err := exec.Command("sw_vers", "-productVersion").Output(); err == nil {
			info.Version = strings.TrimSpace(string(out))
		}
	case "linux":
		info.Name = "Linux"
		if data, err := os.ReadFile("/etc/os-release"); err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				if strings.HasPrefix(line, "VERSION_ID=") {
					info.Version = strings.Trim(strings.TrimPrefix(line, "VERSION_ID="), "\"")
					break
				}
			}
		}
	default:
		info.Name = runtime.GOOS
	}
	return info
}
