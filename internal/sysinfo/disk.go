package sysinfo

import (
	"syscall"
)

// DiskInfo holds disk usage for the root filesystem.
type DiskInfo struct {
	TotalBytes   uint64  `json:"total_bytes"`
	UsedBytes    uint64  `json:"used_bytes"`
	UsagePercent float64 `json:"usage_percent"`
	Path         string  `json:"path"`
}

func collectDisk() DiskInfo {
	info := DiskInfo{Path: "/"}

	var stat syscall.Statfs_t
	if err := syscall.Statfs("/", &stat); err != nil {
		return info
	}

	info.TotalBytes = stat.Blocks * uint64(stat.Bsize)
	freeBytes := stat.Bavail * uint64(stat.Bsize)
	if info.TotalBytes > freeBytes {
		info.UsedBytes = info.TotalBytes - freeBytes
	}
	if info.TotalBytes > 0 {
		info.UsagePercent = float64(info.UsedBytes) / float64(info.TotalBytes) * 100.0
	}
	return info
}
