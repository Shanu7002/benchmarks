package scenario

import (
	"context"
	"math/rand/v2"
	"sync/atomic"
	"time"

	"github.com/Shanu7002/benchmarks/database/internal/report"
)

type pointLookup struct{}

func init() { Register("point_lookup", pointLookup{}) }

// Preloads params.rows rows (untimed), then issues params.operations primary
// key reads spread across params.concurrency workers.
func (pointLookup) Run(ctx context.Context, env Env) (*report.Recorder, int, time.Duration, error) {
	p := env.Params
	if err := env.createTable(ctx); err != nil {
		return nil, 0, 0, err
	}
	defer env.dropTable(ctx)
	if err := env.insertRows(ctx, p.Rows); err != nil {
		return nil, 0, 0, err
	}

	q, err := env.SQL.Render("select_by_id", map[string]any{"Table": env.Table})
	if err != nil {
		return nil, 0, 0, err
	}
	stmt, err := env.DB.PrepareContext(ctx, q)
	if err != nil {
		return nil, 0, 0, err
	}
	defer stmt.Close()

	rec := &report.Recorder{}
	var remaining atomic.Int64
	remaining.Store(int64(p.Operations))
	var ok atomic.Int64

	start := time.Now()
	err = parallel(p.Concurrency, func(int) error {
		for ctx.Err() == nil && remaining.Add(-1) >= 0 {
			id := rand.Int64N(int64(p.Rows)) + 1 // ids are 1..rows on a fresh table
			var payload string
			t := time.Now()
			err := stmt.QueryRowContext(ctx, id).Scan(&payload)
			rec.Observe(time.Since(t), err)
			if err == nil {
				ok.Add(1)
			}
		}
		return ctx.Err()
	})
	return rec, int(ok.Load()), time.Since(start), err
}
