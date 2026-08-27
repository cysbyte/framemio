#!/usr/bin/env bash
set -euo pipefail

AWS_REGION="${AWS_REGION:-us-east-1}"
RDSHOST="${RDSHOST:-framemio.c4j042eqkxdd.us-east-1.rds.amazonaws.com}"
RDS_PORT="${RDS_PORT:-5432}"
RDS_DBNAME="${RDS_DBNAME:-postgres}"
RDS_USER="${RDS_USER:-postgres}"
RDS_SSLMODE="${RDS_SSLMODE:-require}"
AWS_USE_PROXY="${AWS_USE_PROXY:-false}"

if [[ "$AWS_USE_PROXY" != "true" ]]; then
  unset HTTP_PROXY HTTPS_PROXY ALL_PROXY http_proxy https_proxy all_proxy
fi

TOKEN="$(aws rds generate-db-auth-token \
  --hostname "$RDSHOST" \
  --port "$RDS_PORT" \
  --username "$RDS_USER" \
  --region "$AWS_REGION")"

export PGPASSWORD="$TOKEN"
exec psql \
  "host=$RDSHOST port=$RDS_PORT dbname=$RDS_DBNAME user=$RDS_USER sslmode=$RDS_SSLMODE" \
  "$@"
