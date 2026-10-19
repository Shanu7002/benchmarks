package scenario

import (
	"context"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Shanu7002/benchmarks/database/internal/report"
)

type bulkInsert struct{}

func init() { Register("bulk_insert", bulkInsert{}) }

// Inserts params.rows rows in batches of params.batch_size across
// params.concurrency workers. Each batch is one timed operation.
func (bulkInsert) Run(ctx context.Context, env Env) (*report.Recorder, int, time.Duration, error) {
	p := env.Params
	if err := env.createTable(ctx); err != nil {
		return nil, 0, 0, err
	}
	defer env.dropTable(ctx)

	rec := &report.Recorder{}
	payload := strings.Repeat("A", p.PayloadBytes)
	var remaining atomic.Int64
	remaining.Store(int64(p.Rows))
	var inserted atomic.Int64

	start := time.Now()
	err := parallel(p.Concurrency, func(int) error {
		for ctx.Err() == nil {
			// Claim a batch; the last one may be smaller so the cap is exact.
			left := remaining.Add(-int64(p.BatchSize)) + int64(p.BatchSize)
			if left <= 0 {
				return nil
			}
			b := min(p.BatchSize, int(left))
			args := make([]any, 0, b*2)
			for i := 0; i < b; i++ {
				args = append(args, "BENCH", payload)
			}
			t := time.Now()
			err := env.exec(ctx, "insert_batch", map[string]any{"Batch": b}, args...)
			rec.Observe(time.Since(t), err)
			if err == nil {
				inserted.Add(int64(b))
			}
		}
		return ctx.Err()
	})
	return rec, int(inserted.Load()), time.Since(start), err
}
