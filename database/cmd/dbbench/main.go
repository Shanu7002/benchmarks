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

type options struct {
	cfgPath, target, scenario, outDir, settingsDir string
	dryRun, force                                  bool
	timeout                                        time.Duration // 0 = use config/defaults.yml
}

func main() {
	var o options
	flag.StringVar(&o.cfgPath, "config", "targets.yml", "targets file")
	flag.StringVar(&o.target, "target", "", "target name from the targets file")
	flag.StringVar(&o.scenario, "scenario", "", "scenario name")
	flag.StringVar(&o.outDir, "out", "results", "results directory")
	flag.StringVar(&o.settingsDir, "settings-dir", "", "directory with defaults.yml / limits.yml overriding the embedded config/ (missing files fall back to embedded)")
	flag.BoolVar(&o.dryRun, "dry-run", false, "validate config and print the plan, do not connect")
	flag.BoolVar(&o.force, "force", false, "skip the safety limits (use with care)")
	flag.DurationVar(&o.timeout, "timeout", 0, "abort the run after this long (default: run.timeout in defaults.yml)")
	list := flag.Bool("list", false, "list scenarios and dialects")
	flag.Parse()

	if *list {
		fmt.Println("scenarios:", scenario.Names())
		fmt.Println("dialects: ", driver.Dialects())
		return
	}
	if err := run(o); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(o options) error {
	if o.target == "" || o.scenario == "" {
		return fmt.Errorf("-target and -scenario are required (see -list)")
	}
	if o.settingsDir != "" {
		if st, err := os.Stat(o.settingsDir); err != nil || !st.IsDir() {
			return fmt.Errorf("-settings-dir %q is not a directory", o.settingsDir)
		}
	}
	settings, err := config.LoadSettings(o.settingsDir)
	if err != nil {
		return err
	}
	policy, err := guard.LoadPolicy(o.settingsDir)
	if err != nil {
		return err
	}
	cfg, err := config.Load(o.cfgPath)
	if err != nil {
		return err
	}
	t, ok := cfg.Targets[o.target]
	if !ok {
		return fmt.Errorf("target %q not in %s", o.target, o.cfgPath)
	}
	drv, err := driver.Get(t.Dialect)
	if err != nil {
		return err
	}
	sc, err := scenario.Get(o.scenario)
	if err != nil {
		return err
	}
	timeout := o.timeout
	if timeout == 0 {
		timeout = settings.Run.Timeout
	}

	params := cfg.Scenarios[o.scenario].WithDefaults(settings.ScenarioParams)
	limits, err := policy.Check(params, t.DSN, o.force)
	if err != nil {
		return err
	}
	kind := map[bool]string{true: "local", false: "remote"}[policy.IsLocal(t.DSN)]
	fmt.Printf("plan: %s on %s (%s, %s)\n  params: %+v\n  estimated data: ~%d MiB (limit %d MiB), timeout %s\n  settings: %s; %s\n",
		o.scenario, o.target, t.Dialect, kind, params,
		policy.EstimateBytes(params)>>20, limits.MaxDataMiB, timeout, settings.Source, policy.Source)
	if o.dryRun {
		fmt.Println("dry-run: not connecting")
		return nil
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	db, err := drv.Open(ctx, t.DSN)
	if err != nil {
		return fmt.Errorf("connect %s: %w", o.target, err)
	}
	defer db.Close()

	db.SetMaxOpenConns(params.Concurrency + 2)

	env := scenario.Env{
		DB: db, SQL: sqltpl.New(drv), Params: params,
		Table:     settings.Benchmark.TablePrefix + o.scenario,
		EventType: settings.Benchmark.EventType,
	}
	started := time.Now()
	rec, rows, elapsed, runErr := sc.Run(ctx, env)
	if rec == nil {
		return runErr
	}

	res := report.Result{
		Scenario: o.scenario, Target: o.target, Dialect: t.Dialect, Label: t.Label,
		StartedAt: started, Params: params,
	}
	rec.Summarize(&res, elapsed, rows)
	path, err := report.Write(o.outDir, res)
	if err != nil {
		return err
	}
	fmt.Printf("%s on %s: %d ops, %.0f ops/s, %.0f rows/s, p50=%.2fms p95=%.2fms p99=%.2fms errors=%d\n-> %s\n",
		o.scenario, o.target, res.Ops, res.OpsPerSec, res.RowsPerSec, res.P50ms, res.P95ms, res.P99ms, res.Errors, path)
	return runErr
}
