// Package metrics collects in-memory operational data without retaining prompts
// or generated responses.
package metrics

import (
	"sync"
	"time"
)

// Request is one completed or failed chat request.
type Request struct {
	Succeeded          bool
	ErrorCategory      string
	QueueWait          time.Duration
	Latency            time.Duration
	GeneratedTokens    int
	GenerationDuration time.Duration
}

// Snapshot is the stable JSON representation of process-lifetime metrics.
type Snapshot struct {
	Requests struct {
		Total     uint64            `json:"total"`
		Succeeded uint64            `json:"succeeded"`
		Failed    uint64            `json:"failed"`
		ByError   map[string]uint64 `json:"by_error"`
	} `json:"requests"`
	Queue struct {
		WaitTotalMS   float64 `json:"wait_total_ms"`
		WaitAverageMS float64 `json:"wait_average_ms"`
	} `json:"queue"`
	Latency struct {
		TotalMS   float64 `json:"total_ms"`
		AverageMS float64 `json:"average_ms"`
	} `json:"latency"`
	Generation struct {
		Tokens          uint64  `json:"tokens"`
		DurationMS      float64 `json:"duration_ms"`
		TokensPerSecond float64 `json:"tokens_per_second"`
	} `json:"generation"`
}

// Collector stores counters and aggregates for the lifetime of LocalRunner.
type Collector struct {
	mu                 sync.RWMutex
	total              uint64
	succeeded          uint64
	failed             uint64
	byError            map[string]uint64
	queueWait          time.Duration
	latency            time.Duration
	generatedTokens    uint64
	generationDuration time.Duration
}

func New() *Collector { return &Collector{byError: make(map[string]uint64)} }

func (c *Collector) Record(request Request) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.total++
	c.queueWait += request.QueueWait
	c.latency += request.Latency
	if request.Succeeded {
		c.succeeded++
		if request.GeneratedTokens > 0 {
			c.generatedTokens += uint64(request.GeneratedTokens)
		}
		c.generationDuration += request.GenerationDuration
		return
	}
	c.failed++
	if request.ErrorCategory != "" {
		c.byError[request.ErrorCategory]++
	}
}

func (c *Collector) Snapshot() Snapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()
	var snapshot Snapshot
	snapshot.Requests.Total, snapshot.Requests.Succeeded, snapshot.Requests.Failed = c.total, c.succeeded, c.failed
	snapshot.Requests.ByError = make(map[string]uint64, len(c.byError))
	for category, count := range c.byError {
		snapshot.Requests.ByError[category] = count
	}
	snapshot.Queue.WaitTotalMS = float64(c.queueWait) / float64(time.Millisecond)
	snapshot.Latency.TotalMS = float64(c.latency) / float64(time.Millisecond)
	if c.total > 0 {
		snapshot.Queue.WaitAverageMS = snapshot.Queue.WaitTotalMS / float64(c.total)
		snapshot.Latency.AverageMS = snapshot.Latency.TotalMS / float64(c.total)
	}
	snapshot.Generation.Tokens = c.generatedTokens
	snapshot.Generation.DurationMS = float64(c.generationDuration) / float64(time.Millisecond)
	if c.generationDuration > 0 {
		snapshot.Generation.TokensPerSecond = float64(c.generatedTokens) / c.generationDuration.Seconds()
	}
	return snapshot
}
