package sysinfo

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/nlook-service/nlook-router/internal/ollama"
)

// SystemResources is the complete system status payload.
type SystemResources struct {
	OS     OSInfo      `json:"os"`
	CPU    CPUInfo     `json:"cpu"`
	Memory MemoryInfo  `json:"memory"`
	GPU    GPUInfo     `json:"gpu"`
	Disk   DiskInfo    `json:"disk"`
	Models []ModelInfo `json:"models"`
	Ollama OllamaInfo  `json:"ollama"`
}

// ModelInfo describes an installed AI model.
type ModelInfo struct {
	Name         string `json:"name"`
	Size         string `json:"size"`
	Family       string `json:"family"`
	Quantization string `json:"quantization"`
	ModifiedAt   string `json:"modified_at"`
}

// OllamaInfo holds Ollama process status.
type OllamaInfo struct {
	Running     bool   `json:"running"`
	LoadedModel string `json:"loaded_model"`
}

// Cache to avoid expensive collection on every call.
var (
	cacheMu    sync.Mutex
	cachedData *SystemResources
	cachedAt   time.Time
	cacheTTL   = 10 * time.Second
)

// Collect gathers all system resource info with caching.
func Collect(ctx context.Context) (*SystemResources, error) {
	cacheMu.Lock()
	defer cacheMu.Unlock()

	if cachedData != nil && time.Since(cachedAt) < cacheTTL {
		return cachedData, nil
	}

	res := &SystemResources{
		OS:     collectOS(),
		CPU:    collectCPU(),
		Memory: collectMemory(),
		GPU:    collectGPU(),
		Disk:   collectDisk(),
	}

	// Collect Ollama models
	client := ollama.NewClient()
	if client.IsRunning(ctx) {
		res.Ollama.Running = true
		if models, err := client.List(ctx); err == nil {
			for _, m := range models {
				mi := ModelInfo{
					Name:       m.Name,
					Size:       formatBytes(m.Size),
					ModifiedAt: m.ModifiedAt.Format(time.RFC3339),
				}
				// Get detailed info
				if detail, err := client.Show(ctx, m.Name); err == nil {
					mi.Family = detail.Family
					mi.Quantization = detail.QuantizationLevel
				}
				res.Models = append(res.Models, mi)
			}
		}
	}
	if res.Models == nil {
		res.Models = []ModelInfo{}
	}

	cachedData = res
	cachedAt = time.Now()
	return res, nil
}

func formatBytes(b int64) string {
	gb := float64(b) / (1024 * 1024 * 1024)
	if gb >= 1.0 {
		return fmt.Sprintf("%.1f GB", gb)
	}
	mb := float64(b) / (1024 * 1024)
	return fmt.Sprintf("%.0f MB", mb)
}

func parseFloat(s string) (float64, error) {
	return strconv.ParseFloat(s, 64)
}

func parseUint(s string) (uint64, error) {
	return strconv.ParseUint(s, 10, 64)
}
