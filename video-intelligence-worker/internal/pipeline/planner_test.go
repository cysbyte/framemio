package pipeline

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/framemio/video-intelligence-worker/internal/domain"
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

func TestBuildSegmentsCreatesStorySizedCandidates(t *testing.T) {
	t.Parallel()
	segments := BuildSegments([]domain.VideoScene{{ID: "scene-0001", StartMicros: 0, EndMicros: 18_000_000}}, 12*time.Second)
	if len(segments) != 4 {
		t.Fatalf("18-second video segments = %d, want 4: %+v", len(segments), segments)
	}
	if segments[1].StartMicros != 5_000_000 || segments[1].EndMicros != 10_000_000 {
		t.Fatalf("second segment = %+v, want 5s-10s", segments[1])
	}
	if segments[3].StartMicros != 15_000_000 || segments[3].EndMicros != 18_000_000 {
		t.Fatalf("fourth segment = %+v, want 15s-18s", segments[3])
	}
}

func TestDurationBasedFrameBudgets(t *testing.T) {
	t.Parallel()
	configuration := PlannerConfig{
		MaxFramesShort: 12, MaxFramesLong: 24, MaxCandidateFrames: 450,
		MaxModelFramesShort: 120, MaxModelFramesMedium: 120, MaxModelFramesLong: 150, MaxModelFramesVeryLong: 180,
	}
	tests := []struct {
		name               string
		duration           time.Duration
		expectedCandidates int
		expectedSelected   int
	}{
		{name: "short uses three fps candidate budget", duration: 45 * time.Second, expectedCandidates: 135, expectedSelected: 120},
		{name: "one to two minutes uses two fps candidate budget", duration: 90 * time.Second, expectedCandidates: 180, expectedSelected: 120},
		{name: "two to five minutes uses one fps candidate budget", duration: 3 * time.Minute, expectedCandidates: 180, expectedSelected: 150},
		{name: "five to ten minutes uses half fps candidate budget", duration: 8 * time.Minute, expectedCandidates: 240, expectedSelected: 150},
		{name: "very long uses capped quarter fps candidate budget", duration: 30 * time.Minute, expectedCandidates: 450, expectedSelected: 180},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			metadata := domain.VideoMetadata{DurationMicros: test.duration.Microseconds(), FPS: 30}
			if got := MaxCandidateFrames(metadata, configuration); got != test.expectedCandidates {
				t.Fatalf("MaxCandidateFrames = %d, want %d", got, test.expectedCandidates)
			}
			if got := MaxSelectedFrames(metadata, configuration); got != test.expectedSelected {
				t.Fatalf("MaxSelectedFrames = %d, want %d", got, test.expectedSelected)
			}
		})
	}
}

func TestPlanFrameCandidatesAddsDurationSamples(t *testing.T) {
	t.Parallel()
	configuration := PlannerConfig{
		MaxFramesShort: 12, MaxFramesLong: 24, MaxCandidateFrames: 450,
		MaxModelFramesShort: 120, MaxModelFramesMedium: 120, MaxModelFramesLong: 150, MaxModelFramesVeryLong: 180,
	}
	metadata := domain.VideoMetadata{DurationMicros: (10 * time.Second).Microseconds(), FPS: 30}
	scenes := []domain.VideoScene{{ID: "scene-0001", StartMicros: 0, EndMicros: metadata.DurationMicros, ChangeScore: 1}}
	segments := BuildSegments(scenes, 5*time.Second)
	candidates := PlanFrameCandidates(metadata, scenes, segments, configuration)
	var durationSamples int
	for _, candidate := range candidates {
		if candidate.Role == "duration-sample" {
			durationSamples++
		}
	}
	if durationSamples == 0 {
		t.Fatalf("expected duration-sample candidates, got %+v", candidates)
	}
}
