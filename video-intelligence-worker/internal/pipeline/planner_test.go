package pipeline

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/framemio/video-intelligence-worker/internal/ffmpeg"
)

func TestPlannerAdaptsToRepresentativeIPhoneProfiles(t *testing.T) {
	t.Parallel()
	configuration := PlannerConfig{
		MinSceneDuration: 750 * time.Millisecond, MaxSegmentDuration: 12 * time.Second,
		LongVideoThreshold: 5 * time.Minute, MaxFramesShort: 24, MaxFramesLong: 48,
	}
	tests := []struct {
		name              string
		profile           string
		expectDenseMotion bool
		expectStatic      bool
		expectLongLimit   bool
	}{
		{name: "portrait family moment", profile: "short-portrait-hevc"},
		{name: "high FPS action", profile: "high-fps-action", expectDenseMotion: true},
		{name: "long trip", profile: "long-trip-4k", expectLongLimit: true},
		{name: "static low quality", profile: "low-quality-static", expectStatic: true},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			probeData, err := os.ReadFile(filepath.Join("..", "..", "testdata", "profiles", test.profile+".ffprobe.json"))
			if err != nil {
				t.Fatal(err)
			}
			sceneData, err := os.ReadFile(filepath.Join("..", "..", "testdata", "profiles", test.profile+".scenes.txt"))
			if err != nil {
				t.Fatal(err)
			}
			metadata, err := ffmpeg.ParseProbe(probeData)
			if err != nil {
				t.Fatal(err)
			}
			boundaries := ffmpeg.ParseSceneMetadata(sceneData, 0.32, 240)
			scenes := BuildScenes(metadata, boundaries, configuration.MinSceneDuration)
			segments := BuildSegments(scenes, configuration.MaxSegmentDuration)
			candidates := PlanFrameCandidates(metadata, scenes, segments, configuration)
			if len(scenes) == 0 || len(segments) == 0 || len(candidates) == 0 {
				t.Fatalf("planner returned empty evidence: scenes=%d segments=%d candidates=%d", len(scenes), len(segments), len(candidates))
			}
			if test.expectStatic && len(scenes) != 1 {
				t.Fatalf("static profile scenes = %d, want 1", len(scenes))
			}
			if test.expectLongLimit && MaxSelectedFrames(metadata, configuration) != 48 {
				t.Fatalf("long profile did not use long frame budget")
			}
			if test.expectDenseMotion {
				foundMotionCandidate := false
				for _, candidate := range candidates {
					if candidate.Role == "motion-quarter" || candidate.Role == "motion-three-quarter" {
						foundMotionCandidate = true
						break
					}
				}
				if !foundMotionCandidate {
					t.Fatal("high-FPS profile did not receive motion-aware candidates")
				}
			}
		})
	}
}
