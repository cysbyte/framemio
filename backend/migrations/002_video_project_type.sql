-- Backfill the shared project type for records created before VideoProject.
-- Existing project JSON is preserved; only a missing or blank type is added.
UPDATE framemio_domain_records
SET data = jsonb_set(data, '{type}', '"story"'::jsonb, true),
    updated_at = NOW()
WHERE kind = 'project'
  AND (data->>'type' IS NULL OR BTRIM(data->>'type') = '');
