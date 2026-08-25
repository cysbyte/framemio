package providers

import (
	"context"
	"testing"

	"github.com/framemio/video-intelligence-worker/internal/domain"
)

func TestHeuristicProviderRanksSegmentsByEvidence(t *testing.T) {
	t.Parallel()
	input := domain.VideoUnderstandingInputV1{
		SchemaVersion: domain.VideoUnderstandingInputSchemaV1,
		Quality:       domain.QualitySignals{OverallScore: 0.6},
		Segments: []domain.CandidateSegment{
			{ID: "segment-1", SceneID: "scene-1", StartMicros: 0, EndMicros: 4_000_000},
			{ID: "segment-2", SceneID: "scene-2", StartMicros: 4_000_000, EndMicros: 8_000_000},
			{ID: "segment-3", SceneID: "scene-3", StartMicros: 8_000_000, EndMicros: 12_000_000},
			{ID: "segment-4", SceneID: "scene-4", StartMicros: 12_000_000, EndMicros: 16_000_000},
		},
		Frames: []domain.VideoUnderstandingFrameV1{
			{ID: "frame-1", SegmentID: "segment-1", QualityScore: 0.2},
			{ID: "frame-2", SegmentID: "segment-2", QualityScore: 0.95, SelectionReason: "motion-quarter"},
			{ID: "frame-3", SegmentID: "segment-3", QualityScore: 0.3},
			{ID: "frame-4", SegmentID: "segment-4", QualityScore: 0.9, SelectionReason: "motion-three-quarter"},
		},
	}

	response, err := HeuristicVideoUnderstandingProvider{}.Understand(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	usageBySegment := make(map[string]string)
	for _, candidate := range response.Candidates {
		usageBySegment[candidate.SegmentID] = candidate.RecommendedUsage
	}
	if usageBySegment["segment-2"] != domain.RecommendedUsageHero || usageBySegment["segment-4"] != domain.RecommendedUsageSupporting {
		t.Fatalf("expected best evidence segments to be recommended, got %+v", response.Candidates)
	}
}
