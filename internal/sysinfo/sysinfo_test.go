package sysinfo

import (
	"context"
	"testing"
)

func TestCollect(t *testing.T) {
	res, err := Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect() error: %v", err)
	}

	// OS
	if res.OS.Name == "" {
		t.Error("OS.Name is empty")
	}
	if res.OS.Arch == "" {
		t.Error("OS.Arch is empty")
	}
	if res.OS.Hostname == "" {
		t.Error("OS.Hostname is empty")
	}

	// CPU
	if res.CPU.Cores <= 0 {
		t.Errorf("CPU.Cores = %d, want > 0", res.CPU.Cores)
	}

	// Memory
	if res.Memory.TotalBytes == 0 {
		t.Error("Memory.TotalBytes is 0")
	}
	if res.Memory.UsagePercent <= 0 || res.Memory.UsagePercent > 100 {
		t.Errorf("Memory.UsagePercent = %.1f, want 0-100", res.Memory.UsagePercent)
	}

	// Disk
	if res.Disk.TotalBytes == 0 {
		t.Error("Disk.TotalBytes is 0")
	}
	if res.Disk.Path != "/" {
		t.Errorf("Disk.Path = %s, want /", res.Disk.Path)
	}

	// Models should be non-nil (empty array, not null)
	if res.Models == nil {
		t.Error("Models is nil, want empty slice")
	}

	t.Logf("OS: %s %s (%s)", res.OS.Name, res.OS.Version, res.OS.Arch)
	t.Logf("CPU: %s (%d cores, %.1f%%)", res.CPU.Model, res.CPU.Cores, res.CPU.UsagePercent)
	t.Logf("Memory: %s / %s (%.1f%%)", formatBytes(int64(res.Memory.UsedBytes)), formatBytes(int64(res.Memory.TotalBytes)), res.Memory.UsagePercent)
	t.Logf("GPU: %s (available=%v)", res.GPU.Model, res.GPU.Available)
	t.Logf("Disk: %s / %s (%.1f%%)", formatBytes(int64(res.Disk.UsedBytes)), formatBytes(int64(res.Disk.TotalBytes)), res.Disk.UsagePercent)
	t.Logf("Ollama: running=%v, models=%d", res.Ollama.Running, len(res.Models))
}

func TestCollectCaching(t *testing.T) {
	// First call
	res1, _ := Collect(context.Background())
	// Second call should return cached data
	res2, _ := Collect(context.Background())

	if res1 != res2 {
		t.Error("expected cached pointer to be the same")
	}
}
