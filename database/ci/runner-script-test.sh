#!/usr/bin/env bash
# Tests deploy/scripts/dbbench-run.sh with a stubbed `aws` CLI. No AWS needed.
# Protects the part of the deploy path that is easy to break silently: building
# DSNs from secrets (URL-encoding), DB name/port wiring, settings override.
set -euo pipefail

cd "$(dirname "$0")/.."
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
fail() { echo "FAIL: $*" >&2; exit 1; }

mkdir -p "$tmp/bin" "$tmp/home" "$tmp/bucket"
go build -o "$tmp/real-dbbench" ./cmd/dbbench

cat >"$tmp/env" <<'ENV'
REGION=eu-west-1
BUCKET=fake-bucket
DB_NAME=mydb
PG_HOST=pg.abc.rds.amazonaws.com
PG_PORT=15432
PG_SECRET_ARN=arn:fake:pg
PG_LABEL="rds postgres"
MYSQL_HOST=my.abc.rds.amazonaws.com
MYSQL_PORT=13306
MYSQL_SECRET_ARN=arn:fake:my
MYSQL_LABEL="rds mysql"
ENV

# Fake aws: the "bucket" is the directory in $FAKE_BUCKET.
cat >"$tmp/bin/aws" <<'STUB'
#!/usr/bin/env bash
case "$1 $2" in
  "s3 cp")
    case "$3" in
      s3://fake-bucket/*)
        key=${3#s3://fake-bucket/}
        [ -f "$FAKE_BUCKET/$key" ] || exit 1
        cp "$FAKE_BUCKET/$key" "$4" ;;
    esac ;;
  "s3 sync")
    case "$3" in
      s3://fake-bucket/settings)
        [ -d "$FAKE_BUCKET/settings" ] || exit 1
        cp "$FAKE_BUCKET"/settings/*.yml "$4"/ ;;
    esac ;; # syncing results up to s3 is a no-op
  "secretsmanager get-secret-value")
    echo '{"username":"dbbench_admin","password":"p:a?s#s%w&rd!"}' ;;
esac
STUB
chmod +x "$tmp/bin/aws"

export PATH="$tmp/bin:$PATH" DBBENCH_ENV="$tmp/env" DBBENCH_DIR="$tmp/home" FAKE_BUCKET="$tmp/bucket"
run() { bash deploy/scripts/dbbench-run.sh "$@"; }

echo "== real binary: list, dry-run, generated targets.yml, remote classification"
cp "$tmp/real-dbbench" "$tmp/bucket/dbbench"
# capture first: `cmd | grep -q` can SIGPIPE the writer and fail under pipefail
out=$(run list)
grep -q "bulk_insert" <<<"$out" || fail "list did not show scenarios"
out=$(run pg-rds bulk_insert -dry-run 2>&1)
grep -q "(postgres, remote)" <<<"$out" || fail "RDS host not classified remote: $out"
grep -q "embedded defaults.yml" <<<"$out" || fail "should use embedded settings: $out"
# shellcheck disable=SC2016 # literal ${PG_DSN} is what we are asserting
grep -q 'dsn: ${PG_DSN}' "$tmp/home/targets.yml" || fail "generated targets.yml must keep \${PG_DSN} literal"
grep -q 'label: "rds mysql"' "$tmp/home/targets.yml" || fail "mysql label missing from generated targets.yml"

echo "== settings uploaded to the bucket override the embedded ones"
mkdir "$tmp/bucket/settings"
cat >"$tmp/bucket/settings/limits.yml" <<'YAML'
row_overhead_bytes: 64
max_batch_mib: 16
local_hosts: ["127.0.0.1"]
local:  {max_rows: 100, max_operations: 100, max_concurrency: 1, max_payload_bytes: 100, max_data_mib: 1}
remote: {max_rows: 500, max_operations: 500, max_concurrency: 8, max_payload_bytes: 4096, max_data_mib: 10}
YAML
if out=$(run pg-rds bulk_insert -dry-run 2>&1); then fail "bucket limits not applied"; fi
grep -q "settings/limits.yml" <<<"$out" || fail "refusal should name the settings file: $out"
rm -r "$tmp/bucket/settings"

echo "== DSNs: URL-encoded for postgres, raw for mysql, DB name and ports wired"
# shellcheck disable=SC2016 # the stub binary must expand these when it runs, not now
printf '#!/bin/bash\necho "PG_DSN=$PG_DSN"\necho "MYSQL_DSN=$MYSQL_DSN"\n' >"$tmp/bucket/dbbench"
out=$(run pg-rds bulk_insert 2>&1)
want_pg='PG_DSN=postgres://dbbench_admin:p%3Aa%3Fs%23s%25w%26rd%21@pg.abc.rds.amazonaws.com:15432/mydb?sslmode=require'
want_my='MYSQL_DSN=dbbench_admin:p:a?s#s%w&rd!@tcp(my.abc.rds.amazonaws.com:13306)/mydb?tls=preferred'
grep -qxF "$want_pg" <<<"$out" || fail "postgres DSN wrong. got: $(grep PG_DSN <<<"$out")"
grep -qxF "$want_my" <<<"$out" || fail "mysql DSN wrong. got: $(grep MYSQL_DSN <<<"$out")"

echo "== usage errors"
if run onlyonearg 2>/dev/null; then fail "missing scenario should fail"; fi

echo "OK: runner script checks passed"
