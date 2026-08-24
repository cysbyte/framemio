package store

import "testing"

// VideoAnalysis rows are written by video-intelligence-worker and read back by
// this service, so both must derive logical_key identically. The fixture is a
// real (video_id, analysis_version, logical_key) triple observed in the
// production table. If this digest ever changes, every stored analysis becomes
// unreachable here and the StoryPlan validator rejects plans whose analysis is
// present but unfindable — so treat a failure as a cross-service break, not a
// test to update.
func TestVideoAnalysisLogicalKeyMatchesVideoIntelligenceWorker(t *testing.T) {
	t.Parallel()
	const (
		videoID         = "media-5e0a12162aae1a21375ca3f1a48348de"
		analysisVersion = "va_bb96d014c2c6981fba72d7a35f5c5b95aa3910de934895ecc66c7b4651c4530e"
		want            = "2659a066c46e66df6fdbca4617407d265f760766654069d24e313f496e493976"
	)
	if got := videoAnalysisLogicalKey(videoID, analysisVersion); got != want {
		t.Fatalf("videoAnalysisLogicalKey = %s, want %s", got, want)
	}
}

// The length prefix is what makes the digest unambiguous: without it, values
// that concatenate to the same bytes would collide onto one row.
func TestVideoAnalysisLogicalKeyDisambiguatesBoundaries(t *testing.T) {
	t.Parallel()
	if videoAnalysisLogicalKey("ab", "c") == videoAnalysisLogicalKey("a", "bc") {
		t.Fatal("logical key collides across value boundaries")
	}
}

// Photo analysis rows are written and read only by this service and already
// exist in production under the plain colon-joined form, so that key must not
// be migrated to the hashed encoding.
func TestPhotoAnalysisLogicalKeyStaysPlain(t *testing.T) {
	t.Parallel()
	const want = "project-d9de2671bce6e250ca23013d7e117a56:media-5e0a12162aae1a21375ca3f1a48348de"
	if got := postgresLogicalKey("project-d9de2671bce6e250ca23013d7e117a56", "media-5e0a12162aae1a21375ca3f1a48348de"); got != want {
		t.Fatalf("postgresLogicalKey = %s, want %s", got, want)
	}
}
