//go:build windows

package sysinfo

import (
	"syscall"
	"unsafe"
)

// DiskInfo holds disk usage for the root filesystem.
type DiskInfo struct {
	TotalBytes   uint64  `json:"total_bytes"`
	UsedBytes    uint64  `json:"used_bytes"`
	UsagePercent float64 `json:"usage_percent"`
	Path         string  `json:"path"`
}

func collectDisk() DiskInfo {
	info := DiskInfo{Path: "C:\\"}

	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	getDiskFreeSpace := kernel32.NewProc("GetDiskFreeSpaceExW")

	var freeBytesAvailable, totalBytes, totalFreeBytes uint64
	pathPtr, _ := syscall.UTF16PtrFromString("C:\\")

	ret, _, _ := getDiskFreeSpace.Call(
		uintptr(unsafe.Pointer(pathPtr)),
		uintptr(unsafe.Pointer(&freeBytesAvailable)),
		uintptr(unsafe.Pointer(&totalBytes)),
		uintptr(unsafe.Pointer(&totalFreeBytes)),
	)
	if ret == 0 {
		return info
	}

	info.TotalBytes = totalBytes
	if totalBytes > freeBytesAvailable {
		info.UsedBytes = totalBytes - freeBytesAvailable
	}
	if totalBytes > 0 {
		info.UsagePercent = float64(info.UsedBytes) / float64(totalBytes) * 100.0
	}
	return info
}
