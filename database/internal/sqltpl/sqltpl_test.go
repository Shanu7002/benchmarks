package sqltpl

import (
	"testing"

	"github.com/Shanu7002/benchmarks/database/internal/driver"
)

func render(t *testing.T, dialect, name string, data any) string {
	t.Helper()
	d, err := driver.Get(dialect)
	if err != nil {
		t.Fatal(err)
	}
	out, err := New(d).Render(name, data)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestInsertBatchPlaceholders(t *testing.T) {
	data := map[string]any{"Table": "t", "Batch": 2}
	cases := map[string]string{
		"postgres": "INSERT INTO t (event_type, payload) VALUES ($1,$2),($3,$4)",
		"mysql":    "INSERT INTO t (event_type, payload) VALUES (?,?),(?,?)",
	}
	for dialect, want := range cases {
		if got := render(t, dialect, "insert_batch", data); got != want {
			t.Errorf("%s: got %q want %q", dialect, got, want)
		}
	}
}

func TestDialectOverrideAndFallback(t *testing.T) {
	data := map[string]any{"Table": "t"}
	if got := render(t, "postgres", "create_table", data); got[:12] != "CREATE TABLE" {
		t.Errorf("postgres create_table: %q", got)
	}
	// common fallback works for every dialect
	for _, d := range driver.Dialects() {
		if got := render(t, d, "drop_table", data); got != "DROP TABLE IF EXISTS t" {
			t.Errorf("%s drop_table: %q", d, got)
		}
	}
}

func TestMissingTemplate(t *testing.T) {
	d, _ := driver.Get("mysql")
	if _, err := New(d).Render("nope", nil); err == nil {
		t.Fatal("expected error")
	}
}
