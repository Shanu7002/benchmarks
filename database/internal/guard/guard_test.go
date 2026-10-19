package guard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Shanu7002/benchmarks/database/internal/config"
)

const (
	local  = "postgres://u:p@127.0.0.1:5432/db"
	remote = "postgres://u:p@db.example.com:5432/db"
)

func policy(t *testing.T) *Policy {
	t.Helper()
	p, err := LoadPolicy("")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func settings(t *testing.T) *config.Settings {
	t.Helper()
	s, err := config.LoadSettings("")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func check(t *testing.T, p config.Params, dsn string, force bool) error {
	t.Helper()
	_, err := policy(t).Check(p, dsn, force)
	return err
}

func TestDefaultsPassLocal(t *testing.T) {
	d := settings(t).ScenarioParams
	if err := check(t, config.Params{}.WithDefaults(d), local, false); err != nil {
		t.Fatalf("shipped defaults must run on a laptop: %v", err)
	}
}

func TestLocalIsStricterThanRemote(t *testing.T) {
	p := config.Params{Rows: 5_000_000, PayloadBytes: 128}.WithDefaults(settings(t).ScenarioParams)
	if err := check(t, p, local, false); err == nil {
		t.Fatal("expected local refusal")
	}
	if err := check(t, p, remote, false); err != nil {
		t.Fatalf("remote should allow: %v", err)
	}
}

func TestDiskBombRefused(t *testing.T) {
	p := config.Params{Rows: 400_000, PayloadBytes: 16 << 10}.WithDefaults(settings(t).ScenarioParams) // ~6 GiB
	err := check(t, p, local, false)
	if err == nil || !strings.Contains(err.Error(), "estimated data") {
		t.Fatalf("got %v", err)
	}
}

func TestHugeBatchRefused(t *testing.T) {
	p := config.Params{Rows: 100_000, BatchSize: 50_000, PayloadBytes: 1024}.WithDefaults(settings(t).ScenarioParams)
	if err := check(t, p, remote, false); err == nil {
		t.Fatal("expected batch size refusal")
	}
}

func TestForceBypasses(t *testing.T) {
	p := config.Params{Rows: 900_000_000}.WithDefaults(settings(t).ScenarioParams)
	if err := check(t, p, local, true); err != nil {
		t.Fatal(err)
	}
}

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestOverrideDirChangesLimits(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "limits.yml", `
row_overhead_bytes: 64
max_batch_mib: 16
local_hosts: ["127.0.0.1"]
local:  {max_rows: 10, max_operations: 10, max_concurrency: 1, max_payload_bytes: 100, max_data_mib: 1}
remote: {max_rows: 20, max_operations: 20, max_concurrency: 2, max_payload_bytes: 200, max_data_mib: 2}
`)
	p, err := LoadPolicy(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(p.Source, "limits.yml") || strings.HasPrefix(p.Source, "embedded") {
		t.Errorf("source = %q", p.Source)
	}
	params := config.Params{Rows: 11, BatchSize: 1, Concurrency: 1, PayloadBytes: 10, Operations: 1}
	if _, err := p.Check(params, local, false); err == nil {
		t.Fatal("rows=11 should exceed the overridden local max of 10")
	}
}

func TestMissingFileInOverrideDirFallsBackToEmbedded(t *testing.T) {
	p, err := LoadPolicy(t.TempDir()) // empty dir
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(p.Source, "embedded") {
		t.Errorf("source = %q", p.Source)
	}
}

func TestTypoInConfigFailsLoudly(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "limits.yml", "row_overhead_bytes: 64\nmax_batch_mib: 16\nlocal_hosts: [a]\nlocal: {max_row: 1}\nremote: {}\n")
	if _, err := LoadPolicy(dir); err == nil || !strings.Contains(err.Error(), "max_row") {
		t.Fatalf("want unknown-field error naming max_row, got %v", err)
	}
}

func TestLocalAboveRemoteRejected(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "limits.yml", `
row_overhead_bytes: 64
max_batch_mib: 16
local_hosts: ["127.0.0.1"]
local:  {max_rows: 100, max_operations: 1, max_concurrency: 1, max_payload_bytes: 1, max_data_mib: 1}
remote: {max_rows: 10,  max_operations: 1, max_concurrency: 1, max_payload_bytes: 1, max_data_mib: 1}
`)
	if _, err := LoadPolicy(dir); err == nil || !strings.Contains(err.Error(), "must not exceed remote") {
		t.Fatalf("got %v", err)
	}
}

func TestZeroLimitRejected(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "limits.yml", `
row_overhead_bytes: 64
max_batch_mib: 16
local_hosts: ["127.0.0.1"]
local:  {max_rows: 0, max_operations: 1, max_concurrency: 1, max_payload_bytes: 1, max_data_mib: 1}
remote: {max_rows: 1, max_operations: 1, max_concurrency: 1, max_payload_bytes: 1, max_data_mib: 1}
`)
	if _, err := LoadPolicy(dir); err == nil {
		t.Fatal("zero limit must be rejected (it would silently disable the check)")
	}
}
