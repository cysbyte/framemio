#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
AWS_REGION="${AWS_REGION:-us-east-1}"
NEON_SECRET_NAME="${NEON_SECRET_NAME:-framemio/neon/database-url}"
APPLY_AWS_SECRET="${APPLY_AWS_SECRET:-true}"
APPLY_GITHUB_VARIABLES="${APPLY_GITHUB_VARIABLES:-true}"
APPLY_ECS_SECRET_POLICY="${APPLY_ECS_SECRET_POLICY:-true}"
ECS_EXECUTION_ROLE_NAME="${ECS_EXECUTION_ROLE_NAME:-ecsTaskExecutionRole}"
CLEAR_TEST_DATA="${CLEAR_TEST_DATA:-true}"
DATABASE_URL_SECRET_ARN="${DATABASE_URL_SECRET_ARN:-}"

require_var() {
  local name="$1"
  if [[ -z "${!name:-}" ]]; then
    echo "Missing required environment variable $name" >&2
    exit 1
  fi
}

require_var NEON_DATABASE_URL

create_or_update_secret() {
  if [[ "$APPLY_AWS_SECRET" != "true" ]]; then
    return
  fi

  if aws secretsmanager describe-secret \
    --region "$AWS_REGION" \
    --secret-id "$NEON_SECRET_NAME" >/dev/null 2>&1; then
    echo "Updating AWS Secrets Manager secret $NEON_SECRET_NAME"
    aws secretsmanager put-secret-value \
      --region "$AWS_REGION" \
      --secret-id "$NEON_SECRET_NAME" \
      --secret-string "$NEON_DATABASE_URL" >/dev/null
  else
    echo "Creating AWS Secrets Manager secret $NEON_SECRET_NAME"
    aws secretsmanager create-secret \
      --region "$AWS_REGION" \
      --name "$NEON_SECRET_NAME" \
      --secret-string "$NEON_DATABASE_URL" >/dev/null
  fi

  DATABASE_URL_SECRET_ARN="$(aws secretsmanager describe-secret \
    --region "$AWS_REGION" \
    --secret-id "$NEON_SECRET_NAME" \
    --query ARN \
    --output text)"
  export DATABASE_URL_SECRET_ARN
  echo "Resolved DATABASE_URL_SECRET_ARN=$DATABASE_URL_SECRET_ARN"
}

grant_ecs_secret_access() {
  if [[ "$APPLY_ECS_SECRET_POLICY" != "true" ]]; then
    return
  fi
  require_var DATABASE_URL_SECRET_ARN

  echo "Granting $ECS_EXECUTION_ROLE_NAME permission to read the Neon database secret"
  local policy_document
  policy_document="$(mktemp)"
  cat > "$policy_document" <<JSON
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Sid": "ReadFrameMioNeonDatabaseUrl",
      "Effect": "Allow",
      "Action": "secretsmanager:GetSecretValue",
      "Resource": "$DATABASE_URL_SECRET_ARN"
    }
  ]
}
JSON
  aws iam put-role-policy \
    --role-name "$ECS_EXECUTION_ROLE_NAME" \
    --policy-name FrameMioReadNeonDatabaseUrl \
    --policy-document "file://$policy_document"
  rm -f "$policy_document"
}

configure_github_variables() {
  if [[ "$APPLY_GITHUB_VARIABLES" != "true" ]]; then
    return
  fi
  require_var DATABASE_URL_SECRET_ARN

  echo "Configuring GitHub repository variables for Neon"
  gh variable set DATABASE_PROVIDER --body "neon"
  gh variable set DATABASE_AUTH_MODE --body "password"
  gh variable set RDS_AUTO_START_ENABLED --body "false"
  gh variable set DATABASE_URL_SECRET_ARN --body "$DATABASE_URL_SECRET_ARN"

  if gh variable list | awk '{print $1}' | grep -qx DATABASE_URL; then
    echo "Deleting plaintext GitHub DATABASE_URL variable"
    gh variable delete DATABASE_URL
  fi
}

create_schema_and_clear_test_data() {
  echo "Creating FrameMio schema on Neon"
  "$SCRIPT_DIR/neon-psql.sh" -X -v ON_ERROR_STOP=1 -P pager=off <<'SQL'
CREATE TABLE IF NOT EXISTS framemio_domain_records (
    kind TEXT NOT NULL,
    id TEXT NOT NULL,
    user_id TEXT NOT NULL,
    project_id TEXT NOT NULL DEFAULT '',
    media_id TEXT NOT NULL DEFAULT '',
    logical_key TEXT NOT NULL DEFAULT '',
    data JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (kind, id)
);

CREATE UNIQUE INDEX IF NOT EXISTS framemio_domain_records_logical_key
    ON framemio_domain_records (kind, user_id, logical_key)
    WHERE logical_key <> '';

CREATE INDEX IF NOT EXISTS framemio_domain_records_project
    ON framemio_domain_records (kind, user_id, project_id, created_at);

CREATE INDEX IF NOT EXISTS framemio_video_analysis_lookup
    ON framemio_domain_records (kind, user_id, media_id, logical_key, created_at DESC)
    WHERE kind IN ('video_preprocessing', 'video_analysis');

CREATE INDEX IF NOT EXISTS framemio_video_analysis_video_id
    ON framemio_domain_records (media_id)
    WHERE kind = 'video_analysis';

CREATE INDEX IF NOT EXISTS framemio_video_analysis_version
    ON framemio_domain_records ((data->>'analysis_version'))
    WHERE kind = 'video_analysis';

CREATE INDEX IF NOT EXISTS framemio_video_analysis_project_id
    ON framemio_domain_records (project_id)
    WHERE kind = 'video_analysis';

CREATE INDEX IF NOT EXISTS framemio_video_analysis_processing_status
    ON framemio_domain_records ((data->>'processing_status'))
    WHERE kind = 'video_analysis';

CREATE INDEX IF NOT EXISTS framemio_video_analysis_latest
    ON framemio_domain_records (user_id, media_id, created_at DESC)
    WHERE kind = 'video_analysis';

CREATE OR REPLACE FUNCTION framemio_reject_video_analysis_update()
RETURNS TRIGGER AS $$
BEGIN
    IF OLD.kind = 'video_analysis' THEN
        RAISE EXCEPTION 'video analysis records are immutable';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_trigger WHERE tgname = 'framemio_video_analysis_immutable') THEN
        CREATE TRIGGER framemio_video_analysis_immutable
        BEFORE UPDATE ON framemio_domain_records
        FOR EACH ROW EXECUTE FUNCTION framemio_reject_video_analysis_update();
    END IF;
END;
$$;

CREATE INDEX IF NOT EXISTS framemio_transcript_lookup
    ON framemio_domain_records (kind, user_id, media_id, logical_key, created_at DESC)
    WHERE kind = 'transcript';
SQL

  if [[ "$CLEAR_TEST_DATA" != "true" ]]; then
    return
  fi

  echo "Clearing Neon demo/test data"
  "$SCRIPT_DIR/neon-psql.sh" -X -v ON_ERROR_STOP=1 -P pager=off <<'SQL'
DROP TABLE IF EXISTS playing_with_neon;
TRUNCATE TABLE framemio_domain_records;
SQL
}

create_or_update_secret
grant_ecs_secret_access
configure_github_variables
create_schema_and_clear_test_data

echo "Neon database is ready for FrameMio. No RDS data was migrated."
