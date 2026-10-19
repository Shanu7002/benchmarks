# Getting started with dbbench

This guide explains **what the tool does, how it works inside, and how to test it
locally without hurting your machine**. Every command and output below was run for real.

## 1. What is this?

`dbbench` answers one question: *"how does database X behave under workload Y?"*

You describe **a workload once** (for example "insert 50k rows in batches of 500 using
2 parallel workers"). `dbbench` runs that exact workload against **any database** you
point it at, times every operation, and saves the result as a JSON file. Run the same
workload against Postgres and MySQL and you can compare them fairly, because the
client, the data, and the batch sizes are identical.

It is **not** a database and it does **not** start databases. It is a client that
connects to one you already have (a Docker container, an RDS instance, a VM...).

## 2. The mental model

Three things are kept separate on purpose:

| Concept | What it is | Where it lives | Example |
|---|---|---|---|
| **Target** | *Which* database to talk to | `targets.yml` | `pg-local` → postgres at 127.0.0.1:55432 |
| **Scenario** | *What* to do to it (the workload) | `internal/scenario/*.go` | `bulk_insert`, `point_lookup` |
| **Dialect** | *How* that DB spells SQL | `internal/driver/` + `internal/sqltpl/templates/` | Postgres uses `$1`, MySQL uses `?` |

A scenario never contains database-specific SQL. That is what makes it reusable.

```
 you run:  dbbench -target pg-local -scenario bulk_insert
                    │
   targets.yml ─────┤  1. load target + scenario params
                    │  2. GUARD checks the params are safe  ──✗──> refuse, exit (nothing touched)
                    │  3. connect using the target's driver
                    │  4. scenario runs, asking the SQL renderer for templates
                    │  5. every operation is timed
                    │  6. scenario drops its table
                    └─ 7. write results/<time>_<target>_<scenario>.json
```

## 3. How it works, step by step (with a real example)

### 3.1 The target (`targets.yml`)

```yaml
targets:
  pg-local:
    dialect: postgres                 # picks the driver + SQL templates
    dsn: ${PG_DSN}                    # connection string, read from an env var
    label: "docker postgres:16 local 512m"   # free text saved in the results
```

The DSN comes from an environment variable so passwords never end up in a file you might
commit. `label` is only for humans: put instance size and region there, because numbers
from different machines are not comparable without it.

### 3.2 The scenario params

```yaml
scenarios:
  bulk_insert:
    rows: 50000        # total rows to write (a hard cap)
    batch_size: 500    # rows per INSERT statement
    concurrency: 2     # parallel workers
    payload_bytes: 1024  # size of the text column per row
```

Anything you leave out gets a safe default (100k rows, batch 1000, 1 worker, 1 KiB
payload, 10k operations).

### 3.3 SQL templates: one workload, many dialects

SQL lives in Go templates under `internal/sqltpl/templates/`. For a template name, the
renderer looks in `templates/<dialect>/` first and falls back to `templates/common/`.

`common/insert_batch.sql.tmpl`:
```
INSERT INTO {{.Table}} (event_type, payload) VALUES {{values .Batch 2}}
```

`{{values 2 2}}` expands to the placeholders for 2 rows of 2 columns, using the dialect's
style:

| Dialect | Rendered SQL |
|---|---|
| postgres | `INSERT INTO t (event_type, payload) VALUES ($1,$2),($3,$4)` |
| mysql | `INSERT INTO t (event_type, payload) VALUES (?,?),(?,?)` |

Only things that really differ get a per-dialect file. Today that is just
`create_table` (Postgres `BIGSERIAL` vs MySQL `BIGINT AUTO_INCREMENT`). This is checked by
`go test ./...`, which needs no database.

### 3.4 The scenarios

**`bulk_insert`**: measures write throughput.
1. Drops and creates `bench_bulk_insert`.
2. Starts `concurrency` workers. Each repeatedly claims a batch of up to `batch_size`
   rows (the last batch is smaller so the total is *exactly* `rows`), sends one multi-row
   INSERT, and records how long it took.
3. Drops the table when done (even on Ctrl-C).
One **operation** = one batch. So `rows=50000, batch_size=500` → 100 operations.

**`point_lookup`**: measures read latency by primary key.
1. Creates the table and loads `rows` rows (this part is **not** timed).
2. Prepares `SELECT payload FROM t WHERE id = ?`.
3. `concurrency` workers issue `operations` random lookups, timing each one.
4. Drops the table.
One **operation** = one lookup.

### 3.5 Reading the output

```
bulk_insert on pg-local: 100 ops, 339 ops/s, 169497 rows/s, p50=4.88ms p95=13.75ms p99=26.43ms errors=0
```

| Field | Meaning |
|---|---|
| `ops` | Operations that succeeded (for bulk_insert: batches) |
| `ops/s` | Operations per second |
| `rows/s` | Rows written/read per second (ops/s × rows per op) |
| `p50` | Half of the operations were faster than this (the typical case) |
| `p95` / `p99` | 95% / 99% were faster. The slow tail; this is where databases differ most |
| `errors` | Failed operations. Anything above 0 means don't trust the run |

The same data plus the parameters, label, and timing is saved as JSON in `results/`:

```json
{ "scenario": "point_lookup", "target": "pg-local", "dialect": "postgres",
  "label": "docker postgres:16 local 512m", "ops": 5000, "errors": 0,
  "ops_per_sec": 13213.7, "p50_ms": 0.287, "p95_ms": 0.393, "p99_ms": 0.487,
  "params": { "Rows": 20000, "Concurrency": 4, "PayloadBytes": 1024, ... } }
```

> Latency numbers from a laptop talking to a Docker container only prove the tool works.
> They say nothing about how the databases compare in production.

## 4. Guardrails: how it avoids burning your machine

Safety is enforced in **four layers**. The first two are in the code and cannot be
skipped by accident.

### Layer 1: pre-flight limits (`internal/guard`)

Before connecting to anything, the params are checked. **Local targets** (DSN contains
`127.0.0.1`, `localhost` or `::1`) get tight limits; **remote** targets get larger ones.

| Limit | Local | Remote |
|---|---|---|
| `rows` | 500,000 | 50,000,000 |
| `operations` | 200,000 | 10,000,000 |
| `concurrency` | 8 | 128 |
| `payload_bytes` | 16 KiB | 256 KiB |
| estimated data (`rows × (payload+64)`) | 512 MiB | 20 GiB |
| one batch (`batch_size × payload`) | 16 MiB | 16 MiB |

A config that is too big is **refused before any connection is made**. Real output for
`rows: 80000000, concurrency: 64, batch_size: 20000` against a local DB:

```
error: refusing to run (local target limits):
  - rows=80000000 exceeds max 500000
  - concurrency=64 exceeds max 8
  - one batch is ~19 MiB (batch_size*payload_bytes); keep it under 16 MiB
  - estimated data ~83007 MiB exceeds max 512 MiB
lower the values, or pass -force if you really mean it
```

`-force` skips these checks. Don't use it on a laptop.

### Layer 2: runtime limits

- **`-timeout`** (default `10m`): the whole run is aborted after this long.
- **Ctrl-C** stops the run and still drops the benchmark table.
- **Connection pool** is capped at `concurrency + 2`, so the tool can't open hundreds of connections.
- **Every scenario is capped by rows/operations, never by "fill the disk".** The old
  fill-until-full approach is gone on purpose.

### Layer 3: dry run

See exactly what would happen without connecting:

```
$ dbbench -target pg-local -scenario bulk_insert -dry-run
plan: bulk_insert on pg-local (postgres, local)
  params: {Rows:50000 BatchSize:500 Concurrency:2 PayloadBytes:1024 Operations:10000}
  estimated data: ~51 MiB (limit 512 MiB), timeout 10m0s
dry-run: not connecting
```

### Layer 4: limit the container itself

The code limits what the *client* asks for. Also cap the *database* container so it
physically can't take over the machine:

```bash
docker run -d --rm --name dbbench-pg \
  --cpus 1 --memory 512m --pids-limit 200 \
  -e POSTGRES_PASSWORD=pw -e POSTGRES_DB=bench \
  -p 55432:5432 postgres:16
```

- `--cpus` / `--memory` / `--pids-limit`: hard CPU, RAM and process caps.
- `--rm`: the container and its data are deleted when it stops. No volume is created, so nothing lingers on disk.
- Docker Desktop (macOS/Windows) stores all container data in one VM disk. Check
  *Settings → Resources → Disk image size* if you want a hard disk cap.
- Never bind-mount a big file or loopback image for this tool.

## 5. Run it locally: full walkthrough

### Step 1: prerequisites
```bash
go version                                    # Go 1.22+  (brew install go)
docker info --format '{{.ServerVersion}}'     # Docker running
```

### Step 2: make sure the code is healthy (no database needed)
```bash
go vet ./...
go test ./...
go run ./cmd/dbbench -list
# scenarios: [bulk_insert point_lookup]
# dialects:  [mysql postgres]
```

### Step 3: start limited local databases
```bash
docker run -d --rm --name dbbench-pg --cpus 1 --memory 512m --pids-limit 200 \
  -e POSTGRES_PASSWORD=pw -e POSTGRES_DB=bench -p 55432:5432 postgres:16

docker run -d --rm --name dbbench-mysql --cpus 1 --memory 768m --pids-limit 400 \
  -e MYSQL_ROOT_PASSWORD=pw -e MYSQL_DATABASE=bench -p 53306:3306 mysql:8.0
```
Wait until they accept connections (MySQL needs ~30-60 s on first start):
```bash
docker exec dbbench-pg pg_isready -U postgres
docker exec dbbench-mysql mysqladmin ping -uroot -ppw --silent
```

### Step 4: configure
```bash
cp targets.example.yml targets.yml        # git-ignored
export PG_DSN='postgres://postgres:pw@127.0.0.1:55432/bench?sslmode=disable'
export MYSQL_DSN='root:pw@tcp(127.0.0.1:53306)/bench'
```
Edit `targets.yml` so the scenarios use small values (the local limits will stop you if
you go too big):
```yaml
scenarios:
  bulk_insert:  { rows: 50000, batch_size: 500, concurrency: 2 }
  point_lookup: { rows: 20000, operations: 5000, concurrency: 4 }
```

### Step 5: preview, then run
```bash
go run ./cmd/dbbench -target pg-local -scenario bulk_insert -dry-run   # check the plan
go run ./cmd/dbbench -target pg-local -scenario bulk_insert
go run ./cmd/dbbench -target pg-local -scenario point_lookup
go run ./cmd/dbbench -target mysql-local -scenario bulk_insert
go run ./cmd/dbbench -target mysql-local -scenario point_lookup
```
Each run takes around a second at these sizes.

### Step 6: watch resource use (optional)
```bash
docker stats --no-stream dbbench-pg
# mem=118.5MiB / 512MiB cpu=0.05%
```

### Step 7: check it cleaned up after itself
```bash
docker exec dbbench-pg psql -U postgres -d bench -tAc "select count(*) from pg_tables where tablename like 'bench_%'"
# 0
```

### Step 8: tear everything down
```bash
docker stop dbbench-pg dbbench-mysql      # --rm deletes them
docker ps -a | grep dbbench               # should print nothing
rm -f results/*.json                      # optional
```

## 6. Scaling up safely

1. Increase one parameter at a time and run `-dry-run` first.
2. Do bigger runs on a **remote** DB, not your laptop. Local limits are intentionally low.
3. Fill in `label` (instance size, region) for every remote target.
4. Run the client in the same region as the DB, or network latency will dominate.
5. Only use `-force` on machines you're happy to stress.

## 7. Troubleshooting

| Symptom | Likely cause |
|---|---|
| `refusing to run (...)` | A param is over the limit. Lower it (see section 4) |
| `connect pg-local: ping: ...` | DB not ready yet, wrong port, or env var not exported |
| `target "x" not in targets.yml` | Typo in `-target`, or config file missing |
| `unknown scenario` / `unknown dialect` | Check `-list` |
| MySQL `packet too large` | Lower `batch_size` or `payload_bytes` |
| `context deadline exceeded` | `-timeout` hit. Lower the work or raise `-timeout` |
| `errors` > 0 in output | Don't trust the run; look at DB logs |

## 8. Extending it

**Add a database**
1. `internal/driver/<db>.go`: implement `Driver` (`Dialect`, `Placeholder`, `Open`) and `Register` it in `init()`.
2. `internal/sqltpl/templates/<db>/`: add only the templates that differ from `common/`.
3. Add a target with `dialect: <db>`; add its placeholder style to `internal/sqltpl/sqltpl_test.go`.

**Add a scenario**
1. `internal/scenario/<name>.go`: `Register("<name>", ...)` in `init()`.
2. All SQL via `env.SQL.Render("<template>", data)`; new shared SQL goes in `templates/common/`.
3. Cap the work by `rows` / `operations`.
4. Add its params to `targets.yml`.

## 9. Project map

```
cmd/dbbench/main.go            CLI, wires everything together
internal/config/               loads targets.yml, defaults for params
internal/guard/                safety limits (pre-flight)
internal/driver/               one file per database + registry
internal/sqltpl/templates/     SQL as Go templates (common/ + per-dialect)
internal/scenario/             the workloads
internal/report/               percentiles + JSON output
results/                       output (git-ignored)
deploy/                        future Terraform / CloudFormation
```

## 10. Known limits / next steps

- Two scenarios only. Update, mixed read/write, range scan and a capped capacity test are not built yet.
- No tool yet to compare results files side by side.
- `point_lookup` assumes ids are `1..rows` on a fresh table.
- Next: real remote targets, CI (`go vet` + `go test`), then Terraform/CloudFormation in `deploy/`.
