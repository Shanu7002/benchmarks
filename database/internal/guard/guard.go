// Package guard refuses configurations that could exhaust the machine running
// the benchmark (or the database host). It runs BEFORE any connection is made.
package guard

import (
	"fmt"
	"strings"

	"github.com/Shanu7002/benchmarks/database/internal/config"
)

type Limits struct {
	MaxRows         int
	MaxOperations   int
	MaxConcurrency  int
	MaxPayloadBytes int
	MaxDataBytes    int64 // rows * (payload + rowOverhead): rough on-disk estimate
}

// Local targets (laptop/Docker) get tight limits. Remote targets are the ones
// meant for big runs, so they get room but are still bounded.
var (
	Local  = Limits{MaxRows: 500_000, MaxOperations: 200_000, MaxConcurrency: 8, MaxPayloadBytes: 16 << 10, MaxDataBytes: 512 << 20}
	Remote = Limits{MaxRows: 50_000_000, MaxOperations: 10_000_000, MaxConcurrency: 128, MaxPayloadBytes: 256 << 10, MaxDataBytes: 20 << 30}
)

// rowOverhead approximates per-row cost beyond the payload (id, event_type,
// timestamp, row header). Indexes/WAL add more, so limits are deliberately low.
const rowOverhead = 64

func IsLocal(dsn string) bool {
	for _, h := range []string{"127.0.0.1", "localhost", "[::1]", "@::1"} {
		if strings.Contains(dsn, h) {
			return true
		}
	}
	return false
}

func EstimateBytes(p config.Params) int64 {
	return int64(p.Rows) * int64(p.PayloadBytes+rowOverhead)
}

// Check validates p against the limits for the target. force skips the
// checks (the caller must make that choice explicit).
func Check(p config.Params, dsn string, force bool) (Limits, error) {
	l := Remote
	if IsLocal(dsn) {
		l = Local
	}
	if force {
		return l, nil
	}
	var bad []string
	if p.Rows > l.MaxRows {
		bad = append(bad, fmt.Sprintf("rows=%d exceeds max %d", p.Rows, l.MaxRows))
	}
	if p.Operations > l.MaxOperations {
		bad = append(bad, fmt.Sprintf("operations=%d exceeds max %d", p.Operations, l.MaxOperations))
	}
	if p.Concurrency > l.MaxConcurrency {
		bad = append(bad, fmt.Sprintf("concurrency=%d exceeds max %d", p.Concurrency, l.MaxConcurrency))
	}
	if p.PayloadBytes > l.MaxPayloadBytes {
		bad = append(bad, fmt.Sprintf("payload_bytes=%d exceeds max %d", p.PayloadBytes, l.MaxPayloadBytes))
	}
	if p.BatchSize > p.Rows {
		bad = append(bad, fmt.Sprintf("batch_size=%d is larger than rows=%d", p.BatchSize, p.Rows))
	}
	// Bound the size of a single statement: batch * payload must stay well
	// under typical server packet limits (MySQL max_allowed_packet default 64MB).
	if int64(p.BatchSize)*int64(p.PayloadBytes) > 16<<20 {
		bad = append(bad, fmt.Sprintf("one batch is ~%d MiB (batch_size*payload_bytes); keep it under 16 MiB", int64(p.BatchSize)*int64(p.PayloadBytes)>>20))
	}
	if est := EstimateBytes(p); est > l.MaxDataBytes {
		bad = append(bad, fmt.Sprintf("estimated data ~%d MiB exceeds max %d MiB", est>>20, l.MaxDataBytes>>20))
	}
	if len(bad) > 0 {
		kind := "remote"
		if IsLocal(dsn) {
			kind = "local"
		}
		return l, fmt.Errorf("refusing to run (%s target limits):\n  - %s\nlower the values, or pass -force if you really mean it", kind, strings.Join(bad, "\n  - "))
	}
	return l, nil
}
