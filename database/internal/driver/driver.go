// Package driver is the only place that knows about a specific database.
// To support a new DB: add a file here that registers a Driver, and a
// sql/<dialect>/ directory with the templates that differ from sql/common/.
package driver

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
)

type Driver interface {
	// Dialect is the name used in config and as the sql/<dialect> template dir.
	Dialect() string
	// Placeholder returns the bind-parameter token for the n-th argument (1-based).
	Placeholder(n int) string
	// Open connects using the DSN.
	Open(ctx context.Context, dsn string) (*sql.DB, error)
}

var registry = map[string]Driver{}

func Register(d Driver) { registry[d.Dialect()] = d }

func Get(dialect string) (Driver, error) {
	d, ok := registry[dialect]
	if !ok {
		return nil, fmt.Errorf("unknown dialect %q (have: %v)", dialect, Dialects())
	}
	return d, nil
}

func Dialects() []string {
	out := make([]string, 0, len(registry))
	for k := range registry {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func open(ctx context.Context, name, dsn string) (*sql.DB, error) {
	db, err := sql.Open(name, dsn)
	if err != nil {
		return nil, err
	}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}
	return db, nil
}
