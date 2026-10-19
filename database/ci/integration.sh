#!/usr/bin/env bash
# End-to-end check of the "base": every scenario on every dialect against real
# databases, plus the guardrails. Runs in CI and locally.
#
#   PG_DSN='postgres://postgres:pw@127.0.0.1:55432/bench?sslmode=disable' \
#   MYSQL_DSN='root:pw@tcp(127.0.0.1:53306)/bench' ci/integration.sh
#
# Needs: go, jq. If psql / mysql clients are installed it also checks that no
# benchmark tables are left behind.
set -euo pipefail

cd "$(dirname "$0")/.."
: "${PG_DSN:?set PG_DSN}" "${MYSQL_DSN:?set MYSQL_DSN}"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
fail() { echo "FAIL: $*" >&2; exit 1; }

go build -o "$tmp/dbbench" ./cmd/dbbench

echo "== scenarios x dialects"
for target in pg-ci mysql-ci; do
  for scenario in bulk_insert point_lookup; do
    "$tmp/dbbench" -config ci/targets.ci.yml -target "$target" -scenario "$scenario" -out "$tmp/results"
  done
done

echo "== results are sane"
n=$(find "$tmp/results" -name '*.json' | wc -l | tr -d ' ')
[ "$n" -eq 4 ] || fail "expected 4 result files, got $n"
for f in "$tmp"/results/*.json; do
  jq -e '.errors == 0 and .ops > 0 and .rows > 0 and .p50_ms > 0 and .p99_ms >= .p50_ms and (.label | length > 0)' "$f" >/dev/null \
    || fail "bad result in $f: $(cat "$f")"
done
# bulk_insert must write exactly the configured number of rows (the cap is exact)
for f in "$tmp"/results/*bulk_insert.json; do
  jq -e '.rows == 20000' "$f" >/dev/null || fail "bulk_insert row cap not exact in $f"
done

echo "== guardrail: oversized config is refused before any connection"
cat >"$tmp/bomb.yml" <<'YAML'
targets:
  pg-local: {dialect: postgres, dsn: "postgres://u:p@127.0.0.1:1/db"}
scenarios:
  bulk_insert: {rows: 80000000, concurrency: 64, batch_size: 20000}
YAML
if out=$("$tmp/dbbench" -config "$tmp/bomb.yml" -target pg-local -scenario bulk_insert 2>&1); then
  fail "oversized config was accepted"
fi
grep -q "refusing to run" <<<"$out" || fail "refusal message missing: $out"
grep -q "connect" <<<"$out" && fail "guard ran after connecting: $out"

echo "== -dry-run never connects (unreachable DSN must still succeed)"
cat >"$tmp/dry.yml" <<'YAML'
targets:
  nowhere: {dialect: postgres, dsn: "postgres://u:p@127.0.0.1:1/db"}
scenarios:
  point_lookup: {rows: 100, batch_size: 10, operations: 10}
YAML
# capture first: `cmd | grep -q` can SIGPIPE the writer and fail under pipefail
out=$("$tmp/dbbench" -config "$tmp/dry.yml" -target nowhere -scenario point_lookup -dry-run)
grep -q "dry-run: not connecting" <<<"$out" || fail "dry-run did not report"

echo "== -settings-dir override is honoured and reported"
mkdir "$tmp/settings"
cat >"$tmp/settings/limits.yml" <<'YAML'
row_overhead_bytes: 64
max_batch_mib: 16
local_hosts: ["127.0.0.1"]
local:  {max_rows: 10, max_operations: 10, max_concurrency: 1, max_payload_bytes: 100, max_data_mib: 1}
remote: {max_rows: 20, max_operations: 20, max_concurrency: 2, max_payload_bytes: 200, max_data_mib: 2}
YAML
if out=$("$tmp/dbbench" -config ci/targets.ci.yml -target pg-ci -scenario bulk_insert -settings-dir "$tmp/settings" -dry-run 2>&1); then
  fail "override limits not applied"
fi
grep -q "$tmp/settings/limits.yml" <<<"$out" || fail "refusal did not name the override file: $out"

echo "== unknown keys in targets.yml are rejected"
printf 'scenarios:\n  bulk_insert: {batchsize: 5}\n' >"$tmp/typo.yml"
if out=$("$tmp/dbbench" -config "$tmp/typo.yml" -target x -scenario bulk_insert 2>&1); then
  fail "typo in config accepted"
fi
grep -q "batchsize" <<<"$out" || fail "typo error should name the key: $out"

echo "== no benchmark tables left behind"
if command -v psql >/dev/null; then
  left=$(psql "$PG_DSN" -tAc "select count(*) from pg_tables where tablename like 'bench\_%'")
  [ "$left" = "0" ] || fail "postgres: $left leftover bench_ tables"
else
  echo "(psql not installed: skipped)"
fi
if command -v mysql >/dev/null && [ -n "${MYSQL_CLI_ARGS:-}" ]; then
  # shellcheck disable=SC2086 # MYSQL_CLI_ARGS is a deliberate multi-word option string
  left=$(mysql $MYSQL_CLI_ARGS -N -e "select count(*) from information_schema.tables where table_name like 'bench\_%'")
  [ "$left" = "0" ] || fail "mysql: $left leftover bench_ tables"
else
  echo "(mysql client or MYSQL_CLI_ARGS not set: skipped)"
fi

echo "OK: integration checks passed"
