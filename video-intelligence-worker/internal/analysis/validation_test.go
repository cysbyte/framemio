package analysis

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/framemio/video-intelligence-worker/internal/domain"
)

func TestValidateAcceptsProviderNeutralVideoAnalysis(t *testing.T) {
	t.Parallel()
	if err := Validate(validAnalysis()); err != nil {
		t.Fatal(err)
	}
}

func TestValidateRejectsInvalidPersistentAnalysis(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		mutate func(*domain.VideoAnalysis)
	}{
		{name: "frame outside scene", mutate: func(value *domain.VideoAnalysis) { value.Scenes[0].RepresentativeFrames[0].TimestampMicros = 5_000_000 }},
		{name: "segment outside source", mutate: func(value *domain.VideoAnalysis) { value.Segments[0].EndMicros = 9_000_000 }},
		{name: "segment outside scene", mutate: func(value *domain.VideoAnalysis) { value.Segments[0].EndMicros = 5_000_000 }},
		{name: "transcript outside source", mutate: func(value *domain.VideoAnalysis) { value.Transcript.Segments[0].EndMicros = 9_000_000 }},
		{name: "duplicate scene", mutate: func(value *domain.VideoAnalysis) { value.Scenes[1].SceneID = value.Scenes[0].SceneID }},
		{name: "duplicate frame", mutate: func(value *domain.VideoAnalysis) {
			value.Scenes[1].RepresentativeFrames[0].FrameID = value.Scenes[0].RepresentativeFrames[0].FrameID
		}},
		{name: "duplicate segment", mutate: func(value *domain.VideoAnalysis) { value.Segments[1].SegmentID = value.Segments[0].SegmentID }},
		{name: "missing reason", mutate: func(value *domain.VideoAnalysis) { value.Segments[0].Reasons = nil }},
		{name: "invalid score", mutate: func(value *domain.VideoAnalysis) { value.Segments[0].StoryRelevance = 1.1 }},
		{name: "invalid status", mutate: func(value *domain.VideoAnalysis) { value.ProcessingStatus = "running" }},
		{name: "duplicate label", mutate: func(value *domain.VideoAnalysis) { value.DetectedPeople = []string{"Friends", "friends"} }},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			value := validAnalysis()
			test.mutate(&value)
			if err := Validate(value); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestDecodeIsStrictAndValidatesSchema(t *testing.T) {
	t.Parallel()
	data, err := json.Marshal(validAnalysis())
	if err != nil {
		t.Fatal(err)
	}
	data = []byte(strings.TrimSuffix(string(data), "}") + `,"renderer":"remotion"}`)
	if _, err := Decode(data); err == nil {
		t.Fatal("unknown renderer field was accepted")
	}
}

func validAnalysis() domain.VideoAnalysis {
	return domain.VideoAnalysis{
		ID: "video_analysis_1", SchemaVersion: domain.VideoAnalysisSchemaV1,
		UserID: "user-1", ProjectID: "project-1", VideoID: "video-1",
		AnalysisVersion: "va_1", InputVersion: "evidence-v1", UnderstandingVersion: "understanding-v1",
		Provider: "provider-a", Model: "model-a", SourceETag: "etag-1", EvidenceS3Key: "evidence/video-1.json",
		ProcessingStatus: domain.VideoAnalysisStatusCompleted, DurationMicros: 8_000_000,
		Scenes: []domain.Scene{
			{SceneID: "scene-1", StartMicros: 0, EndMicros: 4_000_000, RepresentativeFrames: []domain.Frame{{FrameID: "frame-1", TimestampMicros: 2_000_000, S3Key: "frames/1.jpg"}}},
			{SceneID: "scene-2", StartMicros: 4_000_000, EndMicros: 8_000_000, RepresentativeFrames: []domain.Frame{{FrameID: "frame-2", TimestampMicros: 6_000_000, S3Key: "frames/2.jpg"}}},
		},
		Transcript: domain.Transcript{Language: "en", HasSpeech: true, Segments: []domain.TranscriptSegment{{StartMicros: 1_000_000, EndMicros: 2_000_000, Text: "Hello", Confidence: 0.9}}},
		Segments: []domain.Segment{
			{SegmentID: "segment-1", SceneID: "scene-1", StartMicros: 0, EndMicros: 4_000_000, Description: "Friends laugh", Importance: 0.9, VisualQuality: 0.5, MemoryValue: 0.95, EmotionalValue: 0.9, StoryRelevance: 0.9, Recommended: true, RecommendedUsage: domain.RecommendedUsageHero, Reasons: []string{"A meaningful interaction"}},
			{SegmentID: "segment-2", SceneID: "scene-2", StartMicros: 4_000_000, EndMicros: 8_000_000, Description: "Street context", Importance: 0.3, VisualQuality: 0.8, MemoryValue: 0.2, EmotionalValue: 0.1, StoryRelevance: 0.3, Recommended: true, RecommendedUsage: domain.RecommendedUsageContext, Reasons: []string{"Useful context"}},
		},
		DetectedPeople: []string{"friends"}, DetectedActivities: []string{"laughing"}, DetectedLocations: []string{"street"},
		QualitySignals: domain.QualitySignals{OverallScore: 0.7, AverageSharpness: 0.6, AverageBrightness: 0.5},
		CreatedAt:      time.Date(2026, time.August, 19, 14, 0, 0, 0, time.UTC),
	}
}
