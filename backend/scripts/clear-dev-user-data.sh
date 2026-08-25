#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BACKEND_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
ENV_FILE="${ENV_FILE:-$BACKEND_DIR/.env}"

if [[ -f "$ENV_FILE" ]]; then
  set -a
  # shellcheck disable=SC1090
  source "$ENV_FILE"
  set +a
fi

AWS_REGION="${AWS_REGION:-us-east-1}"
DATABASE_AUTH_MODE="${DATABASE_AUTH_MODE:-}"
DATABASE_URL="${DATABASE_URL:-}"
S3_MEDIA_BUCKET="${S3_MEDIA_BUCKET:-}"
USER_ID="${USER_ID:-}"
DRY_RUN="${DRY_RUN:-true}"
DELETE_USER_RECORD="${DELETE_USER_RECORD:-false}"
DELETE_S3_OBJECTS="${DELETE_S3_OBJECTS:-true}"
ABORT_MULTIPART_UPLOADS="${ABORT_MULTIPART_UPLOADS:-true}"
LIST_S3_OBJECTS="${LIST_S3_OBJECTS:-false}"
AWS_USE_PROXY="${AWS_USE_PROXY:-false}"
CONFIRM_DELETE_USER_DATA="${CONFIRM_DELETE_USER_DATA:-}"

if [[ "$AWS_USE_PROXY" != "true" ]]; then
  unset HTTP_PROXY HTTPS_PROXY ALL_PROXY http_proxy https_proxy all_proxy
fi

require_var() {
  local name="$1"
  if [[ -z "${!name:-}" ]]; then
    echo "Missing required environment variable $name" >&2
    exit 1
  fi
}

psql_query() {
  psql "$DATABASE_URL" -X -v ON_ERROR_STOP=1 -P pager=off "$@"
}

configure_database_auth() {
  require_var DATABASE_URL
  local auth_mode
  auth_mode="$(printf '%s' "$DATABASE_AUTH_MODE" | tr '[:upper:]' '[:lower:]')"
  if [[ "$auth_mode" != "iam" ]]; then
    return
  fi

  local parsed
  parsed="$(python3 - "$DATABASE_URL" <<'PY'
import sys
from urllib.parse import urlparse
url = urlparse(sys.argv[1])
if not url.hostname or not url.username:
    raise SystemExit('DATABASE_URL must include host and username for IAM auth')
print(url.hostname)
print(url.port or 5432)
print(url.username)
PY
)"
  local db_host db_port db_user
  db_host="$(printf '%s\n' "$parsed" | sed -n '1p')"
  db_port="$(printf '%s\n' "$parsed" | sed -n '2p')"
  db_user="$(printf '%s\n' "$parsed" | sed -n '3p')"
  echo "Generating IAM database auth token for $db_user@$db_host:$db_port"
  PGPASSWORD="$(aws rds generate-db-auth-token \
    --hostname "$db_host" \
    --port "$db_port" \
    --username "$db_user" \
    --region "$AWS_REGION")"
  export PGPASSWORD
}

resolve_user_id() {
  if [[ -n "$USER_ID" ]]; then
    return
  fi
  USER_ID="$(psql_query -At -c "
    SELECT user_id
    FROM framemio_domain_records
    WHERE kind = 'user' AND user_id LIKE 'ios-anon-%'
    ORDER BY updated_at DESC
    LIMIT 1;
  " | tr -d '[:space:]')"
  if [[ -z "$USER_ID" ]]; then
    echo "Could not auto-detect a dev user. Set USER_ID=ios-anon-... and retry." >&2
    exit 1
  fi
}

show_database_summary() {
  echo "Database records for $USER_ID:"
  psql_query -v user_id="$USER_ID" -c "
    SELECT kind, count(*) AS records
    FROM framemio_domain_records
    WHERE user_id = :'user_id'
    GROUP BY kind
    ORDER BY kind;
  "
}

show_s3_summary() {
  if [[ "$DELETE_S3_OBJECTS" != "true" ]]; then
    return
  fi
  require_var S3_MEDIA_BUCKET
  local prefix="users/$USER_ID/"
  echo "S3 summary for s3://$S3_MEDIA_BUCKET/$prefix:"
  if [[ "$LIST_S3_OBJECTS" == "true" ]]; then
    aws s3 ls "s3://$S3_MEDIA_BUCKET/$prefix" --recursive --summarize || true
  else
    aws s3 ls "s3://$S3_MEDIA_BUCKET/$prefix" --recursive --summarize 2>/dev/null | tail -n 2 || true
  fi
}

abort_multipart_uploads() {
  if [[ "$ABORT_MULTIPART_UPLOADS" != "true" || "$DELETE_S3_OBJECTS" != "true" ]]; then
    return
  fi
  require_var S3_MEDIA_BUCKET
  local prefix="users/$USER_ID/"
  echo "Aborting multipart uploads under s3://$S3_MEDIA_BUCKET/$prefix"
  local uploads
  uploads="$(aws s3api list-multipart-uploads \
    --bucket "$S3_MEDIA_BUCKET" \
    --prefix "$prefix" \
    --query 'Uploads[].join(`\t`, [Key, UploadId])' \
    --output text 2>/dev/null || true)"
  if [[ -z "$uploads" || "$uploads" == "None" ]]; then
    echo "No multipart uploads found."
    return
  fi
  while IFS=$'\t' read -r key upload_id; do
    [[ -z "$key" || -z "$upload_id" || "$key" == "None" ]] && continue
    echo "Aborting multipart upload $upload_id for $key"
    aws s3api abort-multipart-upload \
      --bucket "$S3_MEDIA_BUCKET" \
      --key "$key" \
      --upload-id "$upload_id" >/dev/null
  done <<< "$uploads"
}

delete_s3_objects() {
  if [[ "$DELETE_S3_OBJECTS" != "true" ]]; then
    return
  fi
  require_var S3_MEDIA_BUCKET
  local prefix="users/$USER_ID/"
  echo "Deleting S3 objects under s3://$S3_MEDIA_BUCKET/$prefix"
  aws s3 rm "s3://$S3_MEDIA_BUCKET/$prefix" --recursive --only-show-errors
}

delete_database_records() {
  echo "Deleting database records for $USER_ID"
  psql_query -v user_id="$USER_ID" -v delete_user_record="$DELETE_USER_RECORD" <<'SQL'
BEGIN;
WITH deleted AS (
  DELETE FROM framemio_domain_records
  WHERE user_id = :'user_id'
    AND (kind <> 'user' OR :'delete_user_record' = 'true')
  RETURNING kind
)
SELECT kind, count(*) AS deleted_records
FROM deleted
GROUP BY kind
ORDER BY kind;
COMMIT;
SQL
}

configure_database_auth
resolve_user_id

echo "Target dev user: $USER_ID"
echo "Dry run: $DRY_RUN"
echo "Delete S3 objects: $DELETE_S3_OBJECTS"
echo "Abort multipart uploads: $ABORT_MULTIPART_UPLOADS"
echo "List S3 objects: $LIST_S3_OBJECTS"
echo "Delete user record: $DELETE_USER_RECORD"
echo
show_database_summary
echo
show_s3_summary

if [[ "$DRY_RUN" != "false" ]]; then
  cat <<EOF

Dry run only. Nothing was deleted.
To delete this user's uploaded media, generated renders, derived artifacts, and backend records, run:

  USER_ID='$USER_ID' DRY_RUN=false CONFIRM_DELETE_USER_DATA='$USER_ID' $0

EOF
  exit 0
fi

if [[ "$CONFIRM_DELETE_USER_DATA" != "$USER_ID" ]]; then
  cat >&2 <<EOF
Refusing to delete without explicit confirmation.
Set CONFIRM_DELETE_USER_DATA to exactly the target USER_ID:

  USER_ID='$USER_ID' DRY_RUN=false CONFIRM_DELETE_USER_DATA='$USER_ID' $0

EOF
  exit 1
fi

abort_multipart_uploads
delete_s3_objects
delete_database_records

echo
echo "Cleanup complete for $USER_ID."
