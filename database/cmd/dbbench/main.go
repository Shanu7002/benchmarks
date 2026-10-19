// dbbench runs DB-agnostic benchmark scenarios against any configured target.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/Shanu7002/benchmarks/database/internal/config"
	"github.com/Shanu7002/benchmarks/database/internal/driver"
	"github.com/Shanu7002/benchmarks/database/internal/guard"
	"github.com/Shanu7002/benchmarks/database/internal/report"
	"github.com/Shanu7002/benchmarks/database/internal/scenario"
	"github.com/Shanu7002/benchmarks/database/internal/sqltpl"
)

func main() {
	var (
		cfgPath = flag.String("config", "targets.yml", "config file")
		target  = flag.String("target", "", "target name from config")
		scn     = flag.String("scenario", "", "scenario name")
		outDir  = flag.String("out", "results", "results directory")
		list    = flag.Bool("list", false, "list scenarios and dialects")
		dryRun  = flag.Bool("dry-run", false, "validate config and print the plan, do not connect")
		force   = flag.Bool("force", false, "skip the safety limits (use with care)")
		timeout = flag.Duration("timeout", 10*time.Minute, "abort the run after this long")
	)
	flag.Parse()

	if *list {
		fmt.Println("scenarios:", scenario.Names())
		fmt.Println("dialects: ", driver.Dialects())
		return
	}
	if err := run(*cfgPath, *target, *scn, *outDir, *dryRun, *force, *timeout); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(cfgPath, targetName, scnName, outDir string, dryRun, force bool, timeout time.Duration) error {
	if targetName == "" || scnName == "" {
		return fmt.Errorf("-target and -scenario are required (see -list)")
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return err
	}
	t, ok := cfg.Targets[targetName]
	if !ok {
		return fmt.Errorf("target %q not in %s", targetName, cfgPath)
	}
	drv, err := driver.Get(t.Dialect)
	if err != nil {
		return err
	}
	sc, err := scenario.Get(scnName)
	if err != nil {
		return err
	}

	params := cfg.Scenarios[scnName].WithDefaults()
	limits, err := guard.Check(params, t.DSN, force)
	if err != nil {
		return err
	}
	fmt.Printf("plan: %s on %s (%s, %s)\n  params: %+v\n  estimated data: ~%d MiB (limit %d MiB), timeout %s\n",
		scnName, targetName, t.Dialect, map[bool]string{true: "local", false: "remote"}[guard.IsLocal(t.DSN)],
		params, guard.EstimateBytes(params)>>20, limits.MaxDataBytes>>20, timeout)
	if dryRun {
		fmt.Println("dry-run: not connecting")
		return nil
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	db, err := drv.Open(ctx, t.DSN)
	if err != nil {
		return fmt.Errorf("connect %s: %w", targetName, err)
	}
	defer db.Close()

	db.SetMaxOpenConns(params.Concurrency + 2)

	env := scenario.Env{DB: db, SQL: sqltpl.New(drv), Params: params, Table: "bench_" + scnName}
	started := time.Now()
	rec, rows, elapsed, runErr := sc.Run(ctx, env)
	if rec == nil {
		return runErr
	}

	res := report.Result{
		Scenario: scnName, Target: targetName, Dialect: t.Dialect, Label: t.Label,
		StartedAt: started, Params: params,
	}
	rec.Summarize(&res, elapsed, rows)
	path, err := report.Write(outDir, res)
	if err != nil {
		return err
	}
	fmt.Printf("%s on %s: %d ops, %.0f ops/s, %.0f rows/s, p50=%.2fms p95=%.2fms p99=%.2fms errors=%d\n-> %s\n",
		scnName, targetName, res.Ops, res.OpsPerSec, res.RowsPerSec, res.P50ms, res.P95ms, res.P99ms, res.Errors, path)
	return runErr
}
