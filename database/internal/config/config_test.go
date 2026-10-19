package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEmbeddedSettingsAreValid(t *testing.T) {
	s, err := LoadSettings("")
	if err != nil {
		t.Fatal(err)
	}
	if s.Run.Timeout != 10*time.Minute {
		t.Errorf("timeout = %v", s.Run.Timeout)
	}
	if s.Benchmark.TablePrefix == "" || s.Benchmark.EventType == "" {
		t.Errorf("benchmark settings empty: %+v", s.Benchmark)
	}
}

func TestWithDefaultsKeepsExplicitValues(t *testing.T) {
	d := Params{Rows: 1, BatchSize: 2, Concurrency: 3, PayloadBytes: 4, Operations: 5}
	got := Params{Rows: 99}.WithDefaults(d)
	want := Params{Rows: 99, BatchSize: 2, Concurrency: 3, PayloadBytes: 4, Operations: 5}
	if got != want {
		t.Errorf("got %+v want %+v", got, want)
	}
}

func TestBadTablePrefixRejected(t *testing.T) {
	dir := t.TempDir()
	body := "run: {timeout: 1m}\nscenario_params: {rows: 1, batch_size: 1, concurrency: 1, payload_bytes: 1, operations: 1}\nbenchmark: {table_prefix: \"x; DROP TABLE y;--\", event_type: B}\n"
	if err := os.WriteFile(filepath.Join(dir, "defaults.yml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadSettings(dir); err == nil || !strings.Contains(err.Error(), "table_prefix") {
		t.Fatalf("got %v", err)
	}
}

func TestTargetsFileTypoRejected(t *testing.T) {
	p := filepath.Join(t.TempDir(), "targets.yml")
	if err := os.WriteFile(p, []byte("scenarios:\n  bulk_insert: {batchsize: 5}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(p); err == nil || !strings.Contains(err.Error(), "batchsize") {
		t.Fatalf("got %v", err)
	}
}
