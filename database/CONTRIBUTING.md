# Contributing to dbbench

This project is a **template**: the goal is to add new databases and new tests
easily, without rewriting what already works. This guide tells you which files to
change for each kind of update, and which ones you should normally leave alone.

Read [GETTING_STARTED.md](GETTING_STARTED.md) first if you haven't. It explains the
concepts (target, scenario, dialect) used below.

## The golden rules

1. **A scenario never contains database-specific SQL.** It asks for a template by name.
2. **A driver never contains workload logic.** It only knows how to connect and how that DB writes placeholders.
3. **Every scenario is capped by `rows` / `operations`**, never by disk usage or "run until it breaks".
4. **Keep results comparable.** Don't change what a scenario measures without saying so (see section 6).
5. **No secrets in the repo.** DSNs come from environment variables.

## What to touch, by task

| I want to... | Touch | Don't touch |
|---|---|---|
| Add a database | `internal/driver/<db>.go`, `internal/sqltpl/templates/<db>/`, `internal/sqltpl/sqltpl_test.go`, `go.mod`, `targets.example.yml` | scenarios, `report`, `guard`, `main.go` |
| Add a scenario | `internal/scenario/<name>.go`, `internal/sqltpl/templates/common/`, `targets.example.yml` | drivers, `report`, `main.go` |
| Change a scenario's numbers (rows, batch...) | your `targets.yml` (local) or `targets.example.yml` (shared defaults) | Go code |
| Make a DB's SQL different | `internal/sqltpl/templates/<db>/<name>.sql.tmpl` | the scenario |
| Change safety limits | `config/limits.yml` (values only) | `guard.go`, unless the check logic itself changes |
| Change defaults (timeout, default params, table prefix, event type) | `config/defaults.yml` | Go code |
| Add a new tunable parameter | `internal/config/config.go`, `config/defaults.yml`, then `config/limits.yml` + `guard.go` if it affects load | drivers |
| Add a deploy target (Terraform/CloudFormation) | `deploy/` | the Go code |

## Files you normally should NOT touch

These are the shared core. Changing them affects **every** database and scenario, so
only do it deliberately, in its own PR, with tests.

| File | Why it's sensitive |
|---|---|
| `internal/guard/guard.go` | The *logic* of the safety net (limit values live in `config/limits.yml`). Only change it to add or fix a check |
| `config/limits.yml` | The safety numbers themselves. Loosening one needs a reason in the PR. It is validated on load: local must not exceed remote, no zeros |
| `internal/report/report.go` | Defines how latencies are computed and the result JSON. Changing it breaks comparisons with old results |
| `internal/driver/driver.go` | The `Driver` interface and registry. Changing the interface forces every driver to change |
| `internal/sqltpl/sqltpl.go` | Template lookup (`<dialect>/` then `common/`) and the `ph` / `values` helpers |
| `internal/scenario/scenario.go` | Shared helpers (`exec`, `createTable`, `dropTable`, `parallel`). Used by all scenarios |
| `cmd/dbbench/main.go` | Wiring and flags. A new DB or scenario needs **no** change here, because they self-register |
| `internal/scenario/bulk_insert.go`, `point_lookup.go` | Existing benchmarks. Edit only to fix a bug, not to "tune" them (see section 6) |
| `results/`, `targets.yml` | Git-ignored local output and config. Never commit them |

If you find yourself editing one of these to add a database or a scenario, stop and
check whether the task really needs it. Usually it doesn't.

## 1. Adding a database

Example: adding SQL Server (`sqlserver`).

1. **Add the Go driver dependency**
   ```bash
   go get github.com/microsoft/go-mssqldb@latest
   ```
2. **Create `internal/driver/sqlserver.go`** (copy `postgres.go` or `mysql.go`):
   ```go
   package driver

   import (
       "context"
       "database/sql"
       "fmt"

       _ "github.com/microsoft/go-mssqldb"
   )

   type sqlserver struct{}

   func init() { Register(sqlserver{}) }

   func (sqlserver) Dialect() string          { return "sqlserver" }
   func (sqlserver) Placeholder(n int) string { return fmt.Sprintf("@p%d", n) }
   func (sqlserver) Open(ctx context.Context, dsn string) (*sql.DB, error) {
       return open(ctx, "sqlserver", dsn)
   }
   ```
   `Dialect()` is the name used in `targets.yml` **and** the template folder name.
   `Placeholder(n)` returns the bind token for the n-th argument (1-based).
3. **Create `internal/sqltpl/templates/sqlserver/`** and add **only** the templates that
   differ from `common/`. Usually just `create_table.sql.tmpl`. If the DB has
   different syntax for something else (e.g. `LIMIT`), override that template too.
4. **Extend `internal/sqltpl/sqltpl_test.go`** with the new dialect's expected
   `insert_batch` output (see `TestInsertBatchPlaceholders`).
5. **Add a target** to `targets.example.yml`.
6. **Verify**
   ```bash
   gofmt -l . ; go vet ./... ; go test ./...
   go run ./cmd/dbbench -list            # new dialect appears
   go run ./cmd/dbbench -target <new> -scenario point_lookup -dry-run
   ```
   Then run both existing scenarios against a real (local, limited) instance. See
   GETTING_STARTED.md section 5. Confirm the benchmark table was dropped afterwards.

Things that commonly differ between databases: placeholder style, auto-increment
syntax, maximum parameters per statement (Postgres 65535, SQL Server 2100), and
maximum statement/packet size. If a DB's limit is lower than what the scenarios
generate, the guard or the scenario may need a DB-aware cap. Raise that in the PR.

## 2. Adding a scenario

Example: an `update_by_id` scenario.

1. **Add SQL as a template** in `internal/sqltpl/templates/common/` (the template
   `update_by_id.sql.tmpl` already exists and is currently unused). Use `{{.Table}}`
   for the table and `{{ph n}}` for parameters. Only put it in `<dialect>/` if a
   database genuinely needs different SQL.
2. **Create `internal/scenario/update_by_id.go`**. Model it on `point_lookup.go`:
   ```go
   func init() { Register("update_by_id", updateByID{}) }

   func (updateByID) Run(ctx context.Context, env Env) (*report.Recorder, int, time.Duration, error) {
       if err := env.createTable(ctx); err != nil { ... }
       defer env.dropTable(ctx)
       if err := env.insertRows(ctx, env.Params.Rows); err != nil { ... } // untimed setup
       // render SQL via env.SQL.Render("update_by_id", ...), then time each op
       // with rec.Observe(time.Since(t), err), using parallel(env.Params.Concurrency, ...)
   }
   ```
   Conventions:
   - Do setup **before** starting the timer, and measure only the thing you name.
   - Create and drop your own table (`bench_<scenario>`). Don't leave data behind.
   - Respect `ctx` so Ctrl-C and `-timeout` work.
   - Return `(recorder, rowsTouched, elapsed, err)`.
   - Never hard-code SQL strings.
3. **Add default params** to `targets.example.yml`.
4. **Check the guard covers it.** If the scenario can create significant load through
   a *new* parameter, add that parameter (section 3).
5. **Verify** with `go vet`, `go test`, `-list`, `-dry-run`, and a real local run on
   **both** Postgres and MySQL. A scenario that only works on one dialect isn't done.

## 3. Adding a new parameter

Only needed when a scenario needs a knob that doesn't exist yet.

1. Add the field to `Params` in `internal/config/config.go` with a `yaml:"..."` tag.
2. Give it a default in `config/defaults.yml` under `scenario_params`, and make `WithDefaults()`
   and `Settings.validate()` in `internal/config/config.go` handle it. The default must stay within the local limits.
3. If it can increase load (rows, size, threads, duration), add a max for it to `Limits`
   in `internal/guard/guard.go` and to both `local:` and `remote:` in `config/limits.yml`,
   check it in `Check`, validate it in `Policy.validate()`, and add a test in `guard_test.go`.
4. Document it in GETTING_STARTED.md (params and guardrail table).

## 4. Changing safety limits

The limits protect other people's laptops. They live in `config/limits.yml`, so changing a
number is a one-file edit. Changing them:
- Needs a reason in the PR description.
- Must keep `TestDefaultsPassLocal` passing: default params must always run on a laptop.
- Must keep local limits stricter than remote limits.
- Requires updating the table in GETTING_STARTED.md section 4.

## 5. Testing checklist (run before every PR)

```bash
gofmt -l .                     # must print nothing
go vet ./...
go test ./...
go run ./cmd/dbbench -list
```
For anything that touches a driver, template or scenario, also run it for real:
1. Start **resource-limited** containers (GETTING_STARTED.md section 5).
2. Run every affected scenario against every affected target. `errors` must be 0.
3. Confirm no `bench_*` tables are left behind.
4. Stop the containers and verify `docker ps -a | grep dbbench` prints nothing.

State in the PR which databases and scenarios you ran, and the results' `label`.

## 6. Keeping results comparable

Results from different versions of a scenario can't be compared. So:
- Don't change what an existing scenario does (batch logic, data shape, what's timed) just to make numbers look better.
- If you must change behavior, **add a new scenario** (e.g. `bulk_insert_v2`) or note the change clearly in the PR so older results aren't misread.
- Don't change `report.Result` field names. Add new fields instead, so old JSON files still parse.
- Always fill `label` (instance type, region, version) for remote targets.

## 7. Project hygiene

- **Never commit** `targets.yml`, anything in `results/`, DSNs, passwords or tokens.
- Format with `gofmt`. Keep packages small and single-purpose.
- Comments explain *why*, not *what*. Match the surrounding style.
- Keep the Terraform/CloudFormation in `deploy/` separate from Go code. Infra changes shouldn't need Go changes, and vice versa.
- One concern per PR: a new database, or a new scenario, or a core change. Not all at once.

## 8. Quick decision guide

```
Need to support a new DB?          -> section 1  (driver + templates, nothing else)
Need a new kind of test?           -> section 2  (scenario + templates)
DB syntax differs for one query?   -> add templates/<db>/<name>.sql.tmpl
Need a new tunable?                -> section 3
Tempted to edit main.go/report.go? -> probably wrong; re-read "Files you should NOT touch"
```
