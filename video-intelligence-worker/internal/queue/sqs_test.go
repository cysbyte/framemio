package queue

import "testing"

func TestIsVideoAnalysisJobType(t *testing.T) {
	t.Parallel()

	accepted := []string{"analysis", "MEDIA_PREPROCESS", "TRANSCRIBE", "VIDEO_UNDERSTAND"}
	for _, jobType := range accepted {
		jobType := jobType
		t.Run(jobType, func(t *testing.T) {
			t.Parallel()
			if !isVideoAnalysisJobType(jobType) {
				t.Fatalf("expected %s to be owned by video intelligence worker", jobType)
			}
		})
	}

	released := []string{"STORY_PLAN", "STORY_VALIDATE", "VIDEO_RENDER", "", "RENDER"}
	for _, jobType := range released {
		jobType := jobType
		t.Run("release_"+jobType, func(t *testing.T) {
			t.Parallel()
			if isVideoAnalysisJobType(jobType) {
				t.Fatalf("expected %s to be released for another worker", jobType)
			}
		})
	}
}
