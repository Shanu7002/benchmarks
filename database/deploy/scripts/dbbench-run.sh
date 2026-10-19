#!/usr/bin/env bash
# Runs dbbench on the runner EC2 host. Shared by the Terraform and CloudFormation
# stacks: both install a tiny `dbbench-run` wrapper that downloads THIS file from
# the artifacts bucket and executes it, so the logic lives in exactly one place.
#
# usage: dbbench-run list
#        dbbench-run <target> <scenario> [dbbench flags...]
#   e.g. dbbench-run pg-rds bulk_insert -dry-run
#
# Reads /etc/dbbench/env (written by the stack's user-data): REGION, BUCKET and,
# DB_NAME and, per enabled database, *_HOST, *_PORT, *_SECRET_ARN, *_LABEL.
# No passwords live there.
set -euo pipefail

. "${DBBENCH_ENV:-/etc/dbbench/env}"
DIR="${DBBENCH_DIR:-$HOME/dbbench}"
mkdir -p "$DIR/results"

s3() { aws s3 "$@" --region "$REGION" --only-show-errors; }
secret() { aws secretsmanager get-secret-value --region "$REGION" --secret-id "$1" --query SecretString --output text; }

# The binary is uploaded by you (see deploy/README.md). Always fetch the latest.
s3 cp "s3://$BUCKET/dbbench" "$DIR/dbbench"
chmod +x "$DIR/dbbench"

if [ "${1:-}" = "list" ]; then
  exec "$DIR/dbbench" -list
fi
if [ $# -lt 2 ]; then
  echo "usage: dbbench-run list | dbbench-run <target> <scenario> [dbbench flags...]" >&2
  exit 2
fi
TARGET=$1
SCENARIO=$2
shift 2

# Optional override: put your own targets.yml in the bucket root.
if s3 cp "s3://$BUCKET/targets.yml" "$DIR/targets.yml" 2>/dev/null; then
  echo "using targets.yml from s3://$BUCKET/targets.yml"
else
  # Default: one target per enabled database. \$ keeps ${..}DSN literal; dbbench expands it.
  {
    echo "targets:"
    if [ -n "${PG_HOST:-}" ]; then
      printf '  pg-rds:\n    dialect: postgres\n    dsn: ${PG_DSN}\n    label: "%s"\n' "$PG_LABEL"
    fi
    if [ -n "${MYSQL_HOST:-}" ]; then
      printf '  mysql-rds:\n    dialect: mysql\n    dsn: ${MYSQL_DSN}\n    label: "%s"\n' "$MYSQL_LABEL"
    fi
    cat <<'YAML'
scenarios:
  bulk_insert:
    rows: 1000000
    batch_size: 1000
    concurrency: 4
    payload_bytes: 1024
  point_lookup:
    rows: 100000
    operations: 50000
    concurrency: 8
YAML
  } >"$DIR/targets.yml"
  echo "using generated default targets.yml (override by uploading one to the bucket)"
fi

# Build DSNs from the Secrets Manager secrets RDS manages for us.
if [ -n "${PG_SECRET_ARN:-}" ]; then
  s=$(secret "$PG_SECRET_ARN")
  user=$(jq -r .username <<<"$s")
  pass=$(jq -rn --arg v "$(jq -r .password <<<"$s")" '$v|@uri') # URL-encode the password
  export PG_DSN="postgres://$user:$pass@$PG_HOST:${PG_PORT:-5432}/${DB_NAME:-bench}?sslmode=require"
fi
if [ -n "${MYSQL_SECRET_ARN:-}" ]; then
  s=$(secret "$MYSQL_SECRET_ARN")
  user=$(jq -r .username <<<"$s")
  pass=$(jq -r .password <<<"$s")
  export MYSQL_DSN="$user:$pass@tcp($MYSQL_HOST:${MYSQL_PORT:-3306})/${DB_NAME:-bench}?tls=preferred"
fi

# Optional override of the embedded config/defaults.yml and config/limits.yml:
# upload them to s3://$BUCKET/settings/ (no rebuild needed). Missing files fall back to embedded.
SETTINGS_ARGS=()
mkdir -p "$DIR/settings"
if s3 sync "s3://$BUCKET/settings" "$DIR/settings" --delete 2>/dev/null && ls "$DIR"/settings/*.yml >/dev/null 2>&1; then
  SETTINGS_ARGS=(-settings-dir "$DIR/settings")
  echo "using settings from s3://$BUCKET/settings/"
fi

# ${arr[@]+...} keeps an empty array safe under `set -u` on older bash.
"$DIR/dbbench" -config "$DIR/targets.yml" -target "$TARGET" -scenario "$SCENARIO" -out "$DIR/results" \
  ${SETTINGS_ARGS[@]+"${SETTINGS_ARGS[@]}"} "$@"

# Keep results somewhere that survives the instance.
s3 sync "$DIR/results" "s3://$BUCKET/results/"
echo "results synced to s3://$BUCKET/results/"
