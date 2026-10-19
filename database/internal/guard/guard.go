// Package guard refuses configurations that could exhaust the machine running
// the benchmark (or the database host). It runs BEFORE any connection is made.
// The numbers live in config/limits.yml, not in this file.
package guard

import (
	"fmt"
	"strings"

	configfiles "github.com/Shanu7002/benchmarks/database/config"
	"github.com/Shanu7002/benchmarks/database/internal/config"
)

type Limits struct {
	MaxRows         int   `yaml:"max_rows"`
	MaxOperations   int   `yaml:"max_operations"`
	MaxConcurrency  int   `yaml:"max_concurrency"`
	MaxPayloadBytes int   `yaml:"max_payload_bytes"`
	MaxDataMiB      int64 `yaml:"max_data_mib"` // rows * (payload + row overhead): rough on-disk estimate
}

// Policy is the full contents of config/limits.yml.
type Policy struct {
	RowOverheadBytes int      `yaml:"row_overhead_bytes"`
	MaxBatchMiB      int      `yaml:"max_batch_mib"`
	LocalHosts       []string `yaml:"local_hosts"`
	Local            Limits   `yaml:"local"`
	Remote           Limits   `yaml:"remote"`

	Source string `yaml:"-"` // where the values came from, for display
}

// LoadPolicy reads limits.yml from dir (when set) or the embedded copy.
func LoadPolicy(dir string) (*Policy, error) {
	var p Policy
	src, err := configfiles.Decode(dir, "limits.yml", &p)
	if err != nil {
		return nil, err
	}
	p.Source = src
	if err := p.validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", src, err)
	}
	return &p, nil
}

func (p *Policy) validate() error {
	if p.RowOverheadBytes < 0 || p.MaxBatchMiB <= 0 || len(p.LocalHosts) == 0 {
		return fmt.Errorf("row_overhead_bytes must be >= 0, max_batch_mib > 0 and local_hosts non-empty")
	}
	l, r := p.Local, p.Remote
	for _, v := range []struct {
		name         string
		local, remot int64
	}{
		{"max_rows", int64(l.MaxRows), int64(r.MaxRows)},
		{"max_operations", int64(l.MaxOperations), int64(r.MaxOperations)},
		{"max_concurrency", int64(l.MaxConcurrency), int64(r.MaxConcurrency)},
		{"max_payload_bytes", int64(l.MaxPayloadBytes), int64(r.MaxPayloadBytes)},
		{"max_data_mib", l.MaxDataMiB, r.MaxDataMiB},
	} {
		if v.local <= 0 || v.remot <= 0 {
			return fmt.Errorf("%s must be > 0 for both local and remote", v.name)
		}
		if v.local > v.remot {
			return fmt.Errorf("local %s (%d) must not exceed remote (%d)", v.name, v.local, v.remot)
		}
	}
	return nil
}

func (p *Policy) IsLocal(dsn string) bool {
	for _, h := range p.LocalHosts {
		if strings.Contains(dsn, h) {
			return true
		}
	}
	return false
}

func (p *Policy) EstimateBytes(params config.Params) int64 {
	return int64(params.Rows) * int64(params.PayloadBytes+p.RowOverheadBytes)
}

// Check validates params against the limits for the target. force skips the
// checks (the caller must make that choice explicit). It returns the limits that
// apply to the target either way.
func (p *Policy) Check(params config.Params, dsn string, force bool) (Limits, error) {
	l, kind := p.Remote, "remote"
	if p.IsLocal(dsn) {
		l, kind = p.Local, "local"
	}
	if force {
		return l, nil
	}
	var bad []string
	if params.Rows > l.MaxRows {
		bad = append(bad, fmt.Sprintf("rows=%d exceeds max %d", params.Rows, l.MaxRows))
	}
	if params.Operations > l.MaxOperations {
		bad = append(bad, fmt.Sprintf("operations=%d exceeds max %d", params.Operations, l.MaxOperations))
	}
	if params.Concurrency > l.MaxConcurrency {
		bad = append(bad, fmt.Sprintf("concurrency=%d exceeds max %d", params.Concurrency, l.MaxConcurrency))
	}
	if params.PayloadBytes > l.MaxPayloadBytes {
		bad = append(bad, fmt.Sprintf("payload_bytes=%d exceeds max %d", params.PayloadBytes, l.MaxPayloadBytes))
	}
	if params.BatchSize > params.Rows {
		bad = append(bad, fmt.Sprintf("batch_size=%d is larger than rows=%d", params.BatchSize, params.Rows))
	}
	if batch := int64(params.BatchSize) * int64(params.PayloadBytes); batch > int64(p.MaxBatchMiB)<<20 {
		bad = append(bad, fmt.Sprintf("one batch is ~%d MiB (batch_size*payload_bytes); keep it under %d MiB", batch>>20, p.MaxBatchMiB))
	}
	if est := p.EstimateBytes(params); est > l.MaxDataMiB<<20 {
		bad = append(bad, fmt.Sprintf("estimated data ~%d MiB exceeds max %d MiB", est>>20, l.MaxDataMiB))
	}
	if len(bad) > 0 {
		return l, fmt.Errorf("refusing to run (%s target limits from %s):\n  - %s\nlower the values, or pass -force if you really mean it", kind, p.Source, strings.Join(bad, "\n  - "))
	}
	return l, nil
}
