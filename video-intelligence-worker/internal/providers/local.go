package providers

import (
	"context"

	"github.com/framemio/video-intelligence-worker/internal/domain"
)

type HeuristicVideoUnderstandingProvider struct {
	ProviderName string
	ModelName    string
}

func (p HeuristicVideoUnderstandingProvider) Understand(_ context.Context, input domain.VideoUnderstandingInputV1) (domain.VideoUnderstandingResponseV1, error) {
	candidates := make([]domain.VideoSegmentAnalysisV1, 0, len(input.Segments))
	for index, segment := range input.Segments {
		frameIDs := frameIDsForSegment(input.Frames, segment.ID)
		usage := domain.RecommendedUsageExclude
		if len(frameIDs) > 0 {
			switch index {
			case 0:
				usage = domain.RecommendedUsageHero
			case 1:
				usage = domain.RecommendedUsageSupporting
			default:
				usage = domain.RecommendedUsageContext
			}
		}
		quality := clamp01(input.Quality.OverallScore)
		if quality == 0 {
			quality = 0.6
		}
		candidates = append(candidates, domain.VideoSegmentAnalysisV1{
			SegmentID:           segment.ID,
			SceneID:             segment.SceneID,
			StartMicros:         segment.StartMicros,
			EndMicros:           segment.EndMicros,
			Description:         "Video moment selected from visual evidence.",
			ImportanceScore:     scoreByRank(index, 0.78),
			VisualQualityScore:  quality,
			MemoryValueScore:    scoreByRank(index, 0.74),
			EmotionalValueScore: scoreByRank(index, 0.68),
			StoryRelevanceScore: scoreByRank(index, 0.72),
			EmotionalTone:       "warm",
			RecommendedUsage:    usage,
			Reason:              "Selected from available frame evidence while external AI understanding is disabled.",
			FrameIDs:            frameIDs,
		})
	}
	return domain.VideoUnderstandingResponseV1{SchemaVersion: domain.VideoAnalysisSchemaV1, Candidates: candidates}, nil
}

func (p HeuristicVideoUnderstandingProvider) Name() string {
	if p.ProviderName != "" {
		return p.ProviderName
	}
	return "local"
}

func (p HeuristicVideoUnderstandingProvider) Model() string {
	if p.ModelName != "" {
		return p.ModelName
	}
	return "heuristic-v1"
}

func frameIDsForSegment(frames []domain.VideoUnderstandingFrameV1, segmentID string) []string {
	ids := make([]string, 0, len(frames))
	for _, frame := range frames {
		if frame.SegmentID == segmentID {
			ids = append(ids, frame.ID)
		}
	}
	return ids
}

func scoreByRank(index int, base float64) float64 {
	return clamp01(base - float64(index)*0.04)
}

func clamp01(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}
