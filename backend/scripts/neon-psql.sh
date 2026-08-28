#!/usr/bin/env bash
set -euo pipefail

AWS_REGION="${AWS_REGION:-us-east-1}"
ENV_FILE="${ENV_FILE:-}"
DATABASE_URL_SECRET_ARN="${DATABASE_URL_SECRET_ARN:-}"
DATABASE_PROVIDER="${DATABASE_PROVIDER:-}"
DATABASE_URL="${DATABASE_URL:-}"
NEON_DATABASE_URL="${NEON_DATABASE_URL:-}"
PSQL_BIN="${PSQL_BIN:-psql}"

if [[ -n "$ENV_FILE" ]]; then
  if [[ ! -f "$ENV_FILE" ]]; then
    echo "ENV_FILE does not exist: $ENV_FILE" >&2
    exit 1
  fi
  set -a
  # shellcheck disable=SC1090
  source "$ENV_FILE"
  set +a
  DATABASE_URL_SECRET_ARN="${DATABASE_URL_SECRET_ARN:-}"
  DATABASE_PROVIDER="${DATABASE_PROVIDER:-}"
  DATABASE_URL="${DATABASE_URL:-}"
  NEON_DATABASE_URL="${NEON_DATABASE_URL:-}"
fi

if [[ -z "$NEON_DATABASE_URL" && -n "$DATABASE_URL_SECRET_ARN" ]]; then
  echo "Loading Neon DATABASE_URL from AWS secret/parameter ARN"
  if [[ "$DATABASE_URL_SECRET_ARN" == arn:aws:ssm:* ]]; then
    NEON_DATABASE_URL="$(aws ssm get-parameter \
      --region "$AWS_REGION" \
      --name "$DATABASE_URL_SECRET_ARN" \
      --with-decryption \
      --query Parameter.Value \
      --output text)"
  else
    NEON_DATABASE_URL="$(aws secretsmanager get-secret-value \
      --region "$AWS_REGION" \
      --secret-id "$DATABASE_URL_SECRET_ARN" \
      --query SecretString \
      --output text)"
  fi
fi

database_provider_lower="$(printf '%s' "$DATABASE_PROVIDER" | tr '[:upper:]' '[:lower:]')"
if [[ -z "$NEON_DATABASE_URL" && "$database_provider_lower" == "neon" && -n "$DATABASE_URL" ]]; then
  NEON_DATABASE_URL="$DATABASE_URL"
fi

if [[ -z "$NEON_DATABASE_URL" ]]; then
  cat >&2 <<'MESSAGE'
Missing Neon database URL.

Set one of:
  NEON_DATABASE_URL='postgresql://USER:PASSWORD@HOST/DB?sslmode=require&channel_binding=require'
  DATABASE_PROVIDER=neon DATABASE_URL='postgresql://USER:PASSWORD@HOST/DB?sslmode=require&channel_binding=require'
  DATABASE_URL_SECRET_ARN='arn:aws:secretsmanager:REGION:ACCOUNT:secret:NAME'

Tip: avoid pasting password-bearing URLs directly into your shell history.
Use: read -rs NEON_DATABASE_URL; export NEON_DATABASE_URL
MESSAGE
  exit 1
fi

parsed_env="$(NEON_DATABASE_URL="$NEON_DATABASE_URL" python3 - <<'PY'
import os
import shlex
from urllib.parse import parse_qs, unquote, urlparse

url = urlparse(os.environ["NEON_DATABASE_URL"])
if url.scheme not in {"postgres", "postgresql"}:
    raise SystemExit("NEON_DATABASE_URL must use postgres:// or postgresql://")
if not url.hostname:
    raise SystemExit("NEON_DATABASE_URL must include a host")
if not url.username:
    raise SystemExit("NEON_DATABASE_URL must include a username")
if url.password is None:
    raise SystemExit("NEON_DATABASE_URL must include a password")
database = unquote(url.path.lstrip("/")) or "postgres"
query = parse_qs(url.query, keep_blank_values=True)
values = {
    "PGHOST": url.hostname,
    "PGPORT": str(url.port or 5432),
    "PGDATABASE": database,
    "PGUSER": unquote(url.username),
    "PGPASSWORD": unquote(url.password),
    "PGSSLMODE": query.get("sslmode", ["require"])[0] or "require",
}
channel_binding = query.get("channel_binding", [""])[0]
if channel_binding:
    values["PGCHANNELBINDING"] = channel_binding
for key, value in values.items():
    print(f"export {key}={shlex.quote(value)}")
PY
)"

eval "$parsed_env"

if [[ "${PGHOST:-}" != *neon.tech ]]; then
  echo "Refusing to connect: resolved host does not look like Neon (*.neon.tech): ${PGHOST:-<empty>}" >&2
  exit 1
fi

exec "$PSQL_BIN" "$@"
