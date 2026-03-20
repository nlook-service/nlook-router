package eval

import "time"

// PerfMetrics holds latency and token usage for a single evaluation call.
type PerfMetrics struct {
	LatencyMs int64
	TokensIn  int
	TokensOut int
}

// Measure calls fn and records elapsed time alongside token counts returned by fn.
func Measure(fn func() (string, int, int, error)) (output string, metrics PerfMetrics, err error) {
	start := time.Now()
	output, metrics.TokensIn, metrics.TokensOut, err = fn()
	metrics.LatencyMs = time.Since(start).Milliseconds()
	return
}
