// Package scenario holds the DB-agnostic workloads. A scenario never writes
// dialect-specific SQL: it asks the Renderer for a named template.
package scenario

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Shanu7002/benchmarks/database/internal/config"
	"github.com/Shanu7002/benchmarks/database/internal/report"
	"github.com/Shanu7002/benchmarks/database/internal/sqltpl"
)

// Env is everything a scenario needs to run.
type Env struct {
	DB     *sql.DB
	SQL    *sqltpl.Renderer
	Params config.Params
	Table  string
}

type Scenario interface {
	// Run executes the workload and returns measurements. It owns its own
	// setup/teardown so scenarios stay independent.
	Run(ctx context.Context, env Env) (*report.Recorder, int, time.Duration, error)
}

var registry = map[string]Scenario{}

func Register(name string, s Scenario) { registry[name] = s }

func Get(name string) (Scenario, error) {
	s, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("unknown scenario %q (have: %s)", name, strings.Join(Names(), ", "))
	}
	return s, nil
}

func Names() []string {
	out := make([]string, 0, len(registry))
	for k := range registry {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// --- shared helpers ---

func (e Env) exec(ctx context.Context, tmpl string, data map[string]any, args ...any) error {
	data["Table"] = e.Table
	q, err := e.SQL.Render(tmpl, data)
	if err != nil {
		return err
	}
	_, err = e.DB.ExecContext(ctx, q, args...)
	return err
}

func (e Env) createTable(ctx context.Context) error {
	if err := e.exec(ctx, "drop_table", map[string]any{}); err != nil {
		return err
	}
	return e.exec(ctx, "create_table", map[string]any{})
}

func (e Env) dropTable(ctx context.Context) {
	// Best effort, and detached from ctx so Ctrl-C still cleans up.
	c, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	_ = e.exec(c, "drop_table", map[string]any{})
}

// insertRows loads n rows using the batch_size from params. Not timed.
func (e Env) insertRows(ctx context.Context, n int) error {
	payload := strings.Repeat("A", e.Params.PayloadBytes)
	for done := 0; done < n; {
		b := min(e.Params.BatchSize, n-done)
		args := make([]any, 0, b*2)
		for i := 0; i < b; i++ {
			args = append(args, "BENCH", payload)
		}
		if err := e.exec(ctx, "insert_batch", map[string]any{"Batch": b}, args...); err != nil {
			return err
		}
		done += b
	}
	return nil
}

// parallel runs fn(worker) on Concurrency goroutines and returns the first error.
func parallel(n int, fn func(worker int) error) error {
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for w := 0; w < n; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			if err := fn(w); err != nil {
				errs <- err
			}
		}(w)
	}
	wg.Wait()
	close(errs)
	return <-errs
}
