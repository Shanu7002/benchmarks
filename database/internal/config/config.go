// Package config loads the benchmark configuration (targets + scenario params).
package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
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
	if err := yaml.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	for name, t := range c.Targets {
		t.DSN = os.ExpandEnv(t.DSN)
		c.Targets[name] = t
	}
	return &c, nil
}

// WithDefaults fills zero values so scenarios can rely on sane numbers.
func (p Params) WithDefaults() Params {
	if p.Rows == 0 {
		p.Rows = 100_000
	}
	if p.BatchSize == 0 {
		p.BatchSize = 1000
	}
	if p.Concurrency == 0 {
		p.Concurrency = 1
	}
	if p.PayloadBytes == 0 {
		p.PayloadBytes = 1024
	}
	if p.Operations == 0 {
		p.Operations = 10_000
	}
	return p
}
