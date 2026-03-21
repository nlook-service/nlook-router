package sysinfo

import (
	"encoding/json"
	"os/exec"
	"runtime"
	"strings"
)

// GPUInfo holds GPU metadata.
type GPUInfo struct {
	Model          string  `json:"model"`
	VRAMTotalBytes uint64  `json:"vram_total_bytes"`
	VRAMUsedBytes  uint64  `json:"vram_used_bytes"`
	UsagePercent   float64 `json:"usage_percent"`
	Available      bool    `json:"available"`
}

func collectGPU() GPUInfo {
	switch runtime.GOOS {
	case "darwin":
		return darwinGPU()
	case "linux":
		return linuxGPU()
	}
	return GPUInfo{}
}

func darwinGPU() GPUInfo {
	// macOS: use system_profiler for GPU info
	// On Apple Silicon, GPU shares unified memory with CPU
	out, err := exec.Command("system_profiler", "SPDisplaysDataType", "-json").Output()
	if err != nil {
		return GPUInfo{}
	}

	var result struct {
		SPDisplaysDataType []struct {
			ChipsetModel string `json:"sppci_model"`
			VRAM         string `json:"spdisplays_vram"`
			MetalFamily  string `json:"sppci_metal"`
		} `json:"SPDisplaysDataType"`
	}
	if err := json.Unmarshal(out, &result); err != nil || len(result.SPDisplaysDataType) == 0 {
		return GPUInfo{}
	}

	gpu := result.SPDisplaysDataType[0]
	info := GPUInfo{
		Model:     gpu.ChipsetModel,
		Available: true,
	}

	// On Apple Silicon, VRAM = unified memory (same as total RAM)
	if strings.Contains(gpu.ChipsetModel, "Apple") {
		mem := collectMemory()
		info.VRAMTotalBytes = mem.TotalBytes
	}

	return info
}

func linuxGPU() GPUInfo {
	// Try nvidia-smi for NVIDIA GPUs
	out, err := exec.Command("nvidia-smi",
		"--query-gpu=name,memory.total,memory.used,utilization.gpu",
		"--format=csv,noheader,nounits").Output()
	if err != nil {
		return GPUInfo{}
	}

	line := strings.TrimSpace(string(out))
	parts := strings.Split(line, ", ")
	if len(parts) < 4 {
		return GPUInfo{}
	}

	totalMiB, _ := parseFloat(strings.TrimSpace(parts[1]))
	usedMiB, _ := parseFloat(strings.TrimSpace(parts[2]))
	utilPct, _ := parseFloat(strings.TrimSpace(parts[3]))

	return GPUInfo{
		Model:          strings.TrimSpace(parts[0]),
		VRAMTotalBytes: uint64(totalMiB * 1024 * 1024),
		VRAMUsedBytes:  uint64(usedMiB * 1024 * 1024),
		UsagePercent:   utilPct,
		Available:      true,
	}
}
