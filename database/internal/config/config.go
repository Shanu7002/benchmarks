// Package config loads the benchmark configuration: targets and scenario params
// (targets.yml) plus the tunable defaults (config/defaults.yml).
package config

import (
	"fmt"
	"os"
	"regexp"
	"time"

	configfiles "github.com/Shanu7002/benchmarks/database/config"
)

type Target struct {
	Dialect string `yaml:"dialect"` // registered driver name, e.g. postgres, mysql
	DSN     string `yaml:"dsn"`     // supports ${ENV_VAR} expansion
	Label   string `yaml:"label"`   // free text recorded in results (instance size, region...)
}

// Params are shared by all scenarios; each scenario uses what it needs.
type Params struct {
	Rows         int `yaml:"rows"`          // hard cap on rows written
	BatchSize    int `yaml:"batch_size"`    // rows per INSERT statement
	Concurrency  int `yaml:"concurrency"`   // parallel workers
	PayloadBytes int `yaml:"payload_bytes"` // size of the payload column
	Operations   int `yaml:"operations"`    // reads/updates to issue
}

type Config struct {
	Targets   map[string]Target `yaml:"targets"`
	Scenarios map[string]Params `yaml:"scenarios"`
}

func Load(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c Config
	if err := configfiles.Strict(b, &c); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	for name, t := range c.Targets {
		t.DSN = os.ExpandEnv(t.DSN)
		c.Targets[name] = t
	}
	return &c, nil
}

// WithDefaults fills zero values from d so scenarios can rely on sane numbers.
func (p Params) WithDefaults(d Params) Params {
	if p.Rows == 0 {
		p.Rows = d.Rows
	}
	if p.BatchSize == 0 {
		p.BatchSize = d.BatchSize
	}
	if p.Concurrency == 0 {
		p.Concurrency = d.Concurrency
	}
	if p.PayloadBytes == 0 {
		p.PayloadBytes = d.PayloadBytes
	}
	if p.Operations == 0 {
		p.Operations = d.Operations
	}
	return p
}

// Settings are the tunables from config/defaults.yml.
type Settings struct {
	Run struct {
		Timeout time.Duration `yaml:"timeout"`
	} `yaml:"run"`
	ScenarioParams Params `yaml:"scenario_params"`
	Benchmark      struct {
		TablePrefix string `yaml:"table_prefix"`
		EventType   string `yaml:"event_type"`
	} `yaml:"benchmark"`

	Source string `yaml:"-"` // where the values came from, for display
}

// The table prefix is interpolated into SQL identifiers, so keep it a plain identifier.
var identRe = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]{0,30}$`)

// LoadSettings reads defaults.yml from dir (when set) or the embedded copy.
func LoadSettings(dir string) (*Settings, error) {
	var s Settings
	src, err := configfiles.Decode(dir, "defaults.yml", &s)
	if err != nil {
		return nil, err
	}
	s.Source = src
	if err := s.validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", src, err)
	}
	return &s, nil
}

func (s *Settings) validate() error {
	p := s.ScenarioParams
	switch {
	case s.Run.Timeout <= 0:
		return fmt.Errorf("run.timeout must be > 0")
	case p.Rows <= 0 || p.BatchSize <= 0 || p.Concurrency <= 0 || p.PayloadBytes <= 0 || p.Operations <= 0:
		return fmt.Errorf("scenario_params must all be > 0 (got %+v)", p)
	case !identRe.MatchString(s.Benchmark.TablePrefix):
		return fmt.Errorf("benchmark.table_prefix %q must match %s", s.Benchmark.TablePrefix, identRe)
	case s.Benchmark.EventType == "" || len(s.Benchmark.EventType) > 50:
		return fmt.Errorf("benchmark.event_type must be 1-50 chars")
	}
	return nil
}
