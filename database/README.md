# dbbench

> New here? Read [GETTING_STARTED.md](GETTING_STARTED.md): how it works, safety guardrails, and a local walkthrough.
> Want to add a database or a test? See [CONTRIBUTING.md](CONTRIBUTING.md).
> Running it against real AWS databases? See [deploy/README.md](deploy/README.md).

DB-agnostic benchmark runner. The workload is defined once in Go; each
database only contributes a driver and the SQL that differs from the common
templates. Runs are capped by row/operation count, never by disk.

```
go run ./cmd/dbbench -list
cp targets.example.yml targets.yml && export PG_DSN=... MYSQL_DSN=...
go run ./cmd/dbbench -target pg-remote -scenario bulk_insert
go test ./...
```

Tunable values live in `config/` (`defaults.yml`, `limits.yml`) and `targets.yml`, not in the code. See GETTING_STARTED.md section 3.6.

Results are written as JSON to `results/` (latency p50/p95/p99, throughput, params, target label).

## Adding a database
1. `internal/driver/<db>.go`: implement `Driver` and `Register` it in `init()`.
2. `internal/sqltpl/templates/<db>/`: add only templates that differ from `common/` (usually `create_table`).
3. Add a target to `targets.yml`.

## Adding a scenario
Add `internal/scenario/<name>.go`, `Register` it in `init()`, and use `env.SQL.Render("<template>", data)` for all SQL so it works on every dialect. Add new templates under `templates/common/`.

## Notes
See `NOTES.md` for reference numbers from the original fill-the-disk tests.
