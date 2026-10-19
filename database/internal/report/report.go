// Package report collects latencies and writes a JSON result per run.
package report

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Recorder is safe for concurrent use.
type Recorder struct {
	mu   sync.Mutex
	lats []time.Duration
	errs int
}

func (r *Recorder) Observe(d time.Duration, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err != nil {
		r.errs++
		return
	}
	r.lats = append(r.lats, d)
}

type Result struct {
	Scenario    string    `json:"scenario"`
	Target      string    `json:"target"`
	Dialect     string    `json:"dialect"`
	Label       string    `json:"label,omitempty"`
	StartedAt   time.Time `json:"started_at"`
	DurationSec float64   `json:"duration_sec"`
	Ops         int       `json:"ops"`
	Rows        int       `json:"rows"`
	Errors      int       `json:"errors"`
	OpsPerSec   float64   `json:"ops_per_sec"`
	RowsPerSec  float64   `json:"rows_per_sec"`
	P50ms       float64   `json:"p50_ms"`
	P95ms       float64   `json:"p95_ms"`
	P99ms       float64   `json:"p99_ms"`
	Params      any       `json:"params"`
}

// Summarize fills the latency/throughput fields. rows is the number of rows
// touched in total (an op may touch many rows, e.g. a batch insert).
func (r *Recorder) Summarize(res *Result, elapsed time.Duration, rows int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	sort.Slice(r.lats, func(i, j int) bool { return r.lats[i] < r.lats[j] })
	res.DurationSec = elapsed.Seconds()
	res.Ops = len(r.lats)
	res.Rows = rows
	res.Errors = r.errs
	if res.DurationSec > 0 {
		res.OpsPerSec = float64(res.Ops) / res.DurationSec
		res.RowsPerSec = float64(rows) / res.DurationSec
	}
	res.P50ms, res.P95ms, res.P99ms = pct(r.lats, .50), pct(r.lats, .95), pct(r.lats, .99)
}

func pct(s []time.Duration, p float64) float64 {
	if len(s) == 0 {
		return 0
	}
	i := int(float64(len(s)-1) * p)
	return float64(s[i].Microseconds()) / 1000
}

func Write(dir string, res Result) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	name := res.StartedAt.UTC().Format("20060102T150405Z") + "_" + res.Target + "_" + res.Scenario + ".json"
	path := filepath.Join(dir, name)
	b, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		return "", err
	}
	return path, os.WriteFile(path, b, 0o644)
}
