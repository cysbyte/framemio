package store

import (
	"strings"
	"testing"
)

func TestVideoProjectCompatibilityMigrationDefaultsOnlyMissingTypes(t *testing.T) {
	t.Parallel()
	migration := videoProjectCompatibilityMigration
	for _, fragment := range []string{
		"UPDATE framemio_domain_records",
		"kind = 'project'",
		"data->>'type' IS NULL",
		"BTRIM(data->>'type') = ''",
		"jsonb_set(data, '{type}', '\"story\"'::jsonb, true)",
		"updated_at = NOW()",
	} {
		if !strings.Contains(migration, fragment) {
			t.Fatalf("migration is missing safe legacy default clause %q: %s", fragment, migration)
		}
	}
	if strings.Contains(strings.ToUpper(migration), "DELETE") {
		t.Fatal("video project migration must not delete existing data")
	}
	if strings.Contains(strings.ToUpper(migration), "DROP") {
		t.Fatal("video project migration must not drop existing schema or data")
	}
}
