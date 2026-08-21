package store

import (
	"strings"
	"testing"
)

func TestLogicalKeyIsStableAndLengthDelimited(t *testing.T) {
	t.Parallel()
	first := logicalKey("media-1", "video-evidence-v1", "video-understanding-v1", "provider-a", "model-a", "etag-1")
	second := logicalKey("media-1", "video-evidence-v1", "video-understanding-v1", "provider-a", "model-a", "etag-1")
	if first != second || first == "" {
		t.Fatalf("logical key is not stable: %q %q", first, second)
	}
	if logicalKey("ab", "c") == logicalKey("a", "bc") {
		t.Fatal("logical key does not preserve value boundaries")
	}
	if strings.ContainsRune(first, '\x00') {
		t.Fatal("logical key contains a PostgreSQL-incompatible NUL byte")
	}
	if first == logicalKey("media-1", "video-evidence-v1", "video-understanding-v1", "provider-a", "model-b", "etag-1") {
		t.Fatal("provider model is not represented in the logical key")
	}
}

func TestMigrationIndexesImmutableVideoAnalysisDimensions(t *testing.T) {
	t.Parallel()
	for _, expected := range []string{
		"framemio_video_analysis_video_id",
		"framemio_video_analysis_version",
		"framemio_video_analysis_project_id",
		"framemio_video_analysis_processing_status",
		"framemio_video_analysis_immutable",
	} {
		if !strings.Contains(migrationSQL, expected) {
			t.Fatalf("migration is missing %s", expected)
		}
	}
}
