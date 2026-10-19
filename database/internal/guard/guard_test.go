package guard

import (
	"strings"
	"testing"

	"github.com/Shanu7002/benchmarks/database/internal/config"
)

const (
	local  = "postgres://u:p@127.0.0.1:5432/db"
	remote = "postgres://u:p@db.example.com:5432/db"
)

func TestDefaultsPassLocal(t *testing.T) {
	if _, err := Check(config.Params{}.WithDefaults(), local, false); err != nil {
		t.Fatal(err)
	}
}

func TestLocalIsStricterThanRemote(t *testing.T) {
	p := config.Params{Rows: 5_000_000, PayloadBytes: 128}.WithDefaults()
	if _, err := Check(p, local, false); err == nil {
		t.Fatal("expected local refusal")
	}
	if _, err := Check(p, remote, false); err != nil {
		t.Fatalf("remote should allow: %v", err)
	}
}

func TestDiskBombRefused(t *testing.T) {
	p := config.Params{Rows: 400_000, PayloadBytes: 16 << 10}.WithDefaults() // ~6 GiB
	err := Check2(p, local)
	if err == nil || !strings.Contains(err.Error(), "estimated data") {
		t.Fatalf("got %v", err)
	}
}

func TestHugeBatchRefused(t *testing.T) {
	p := config.Params{Rows: 100_000, BatchSize: 50_000, PayloadBytes: 1024}.WithDefaults()
	if _, err := Check(p, remote, false); err == nil {
		t.Fatal("expected batch size refusal")
	}
}

func TestForceBypasses(t *testing.T) {
	p := config.Params{Rows: 900_000_000}.WithDefaults()
	if _, err := Check(p, local, true); err != nil {
		t.Fatal(err)
	}
}

func Check2(p config.Params, dsn string) error { _, err := Check(p, dsn, false); return err }
