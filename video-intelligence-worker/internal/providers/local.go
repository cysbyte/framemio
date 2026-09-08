package providers

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/framemio/video-intelligence-worker/internal/domain"
)

type HeuristicVideoUnderstandingProvider struct {
	ProviderName string
	ModelName    string
}

func (p HeuristicVideoUnderstandingProvider) Understand(_ context.Context, input domain.VideoUnderstandingInputV1) (domain.VideoUnderstandingResponseV1, error) {
	ranks := rankedSegments(input)
	candidates := make([]domain.VideoSegmentAnalysisV1, 0, len(input.Segments))
	for _, segment := range input.Segments {
		frames := framesForSegment(input.Frames, segment.ID)
		frameIDs := frameIDs(frames)
		rank := ranks[segment.ID]
		usage := domain.RecommendedUsageExclude
		if len(frameIDs) > 0 {
			switch rank {
			case 0:
				usage = domain.RecommendedUsageHero
			case 1:
				usage = domain.RecommendedUsageSupporting
			default:
				if segmentEvidenceScore(frames, input.Quality) >= 0.45 {
					usage = domain.RecommendedUsageContext
				}
			}
		}
		quality := segmentQuality(frames, input.Quality)
		importance := clamp01(segmentEvidenceScore(frames, input.Quality) - float64(rank)*0.03)
		candidates = append(candidates, domain.VideoSegmentAnalysisV1{
			SegmentID:           segment.ID,
			SceneID:             segment.SceneID,
			StartMicros:         segment.StartMicros,
			EndMicros:           segment.EndMicros,
			Description:         "Video moment selected from visual evidence.",
			ImportanceScore:     importance,
			VisualQualityScore:  quality,
			MemoryValueScore:    clamp01(importance * 0.95),
			EmotionalValueScore: clamp01(importance * 0.9),
			StoryRelevanceScore: clamp01(importance * 0.92),
			EmotionalTone:       "warm",
			RecommendedUsage:    usage,
			Reason:              "Selected from available frame evidence while external AI understanding is disabled.",
			FrameIDs:            frameIDs,
		})
	}
	return domain.VideoUnderstandingResponseV1{SchemaVersion: domain.VideoAnalysisSchemaV1, Candidates: candidates}, nil
}

func (p HeuristicVideoUnderstandingProvider) DetectScenes(_ context.Context, input domain.VideoUnderstandingInputV1) (domain.VideoSceneDetectionResponseV1, error) {
	if len(input.Segments) == 0 {
		return domain.VideoSceneDetectionResponseV1{}, domain.Permanent(errors.New("missing segments"))
	}
	if input.Quality.LikelyStatic || input.Quality.SceneChangesPM < 1 {
		segmentIDs := make([]string, 0, len(input.Segments))
		frameIDs := make([]string, 0, len(input.Frames))
		for _, segment := range input.Segments {
			segmentIDs = append(segmentIDs, segment.ID)
		}
		for _, frame := range input.Frames {
			frameIDs = append(frameIDs, frame.ID)
		}
		return domain.VideoSceneDetectionResponseV1{SchemaVersion: domain.VideoSceneAnalysisSchemaV1, Scenes: []domain.VideoSceneGroupV1{{
			SegmentIDs:  segmentIDs,
			Description: "Single stable scene",
			Reason:      "The clip appears visually stable, so one scene better matches the content than multiple artificial cuts.",
			FrameIDs:    frameIDs,
		}}}, nil
	}
	scenes := make([]domain.VideoSceneGroupV1, 0)
	currentSceneID := strings.TrimSpace(input.Segments[0].SceneID)
	currentSegments := make([]string, 0)
	currentFrames := make([]string, 0)
	flush := func() {
		if len(currentSegments) == 0 {
			return
		}
		scenes = append(scenes, domain.VideoSceneGroupV1{
			SegmentIDs:  append([]string(nil), currentSegments...),
			Description: "Scene inferred from nearby visual evidence",
			Reason:      "Segment boundaries align with detected visual changes and representative frames.",
			FrameIDs:    append([]string(nil), currentFrames...),
		})
		currentSegments = currentSegments[:0]
		currentFrames = currentFrames[:0]
	}
	for _, segment := range input.Segments {
		if currentSceneID != "" && segment.SceneID != currentSceneID {
			flush()
			currentSceneID = segment.SceneID
		}
		currentSegments = append(currentSegments, segment.ID)
		for _, frame := range input.Frames {
			if frame.SegmentID == segment.ID {
				currentFrames = append(currentFrames, frame.ID)
			}
		}
	}
	flush()
	if len(scenes) == 0 {
		return domain.VideoSceneDetectionResponseV1{}, domain.Permanent(errors.New("could not infer scenes"))
	}
	return domain.VideoSceneDetectionResponseV1{SchemaVersion: domain.VideoSceneAnalysisSchemaV1, Scenes: scenes}, nil
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

type segmentRank struct {
	segmentID string
	score     float64
	start     int64
}

func rankedSegments(input domain.VideoUnderstandingInputV1) map[string]int {
	ranked := make([]segmentRank, 0, len(input.Segments))
	for _, segment := range input.Segments {
		ranked = append(ranked, segmentRank{segmentID: segment.ID, score: segmentEvidenceScore(framesForSegment(input.Frames, segment.ID), input.Quality), start: segment.StartMicros})
	}
	sort.SliceStable(ranked, func(left, right int) bool {
		if ranked[left].score == ranked[right].score {
			return ranked[left].start < ranked[right].start
		}
		return ranked[left].score > ranked[right].score
	})
	ranks := make(map[string]int, len(ranked))
	for index, segment := range ranked {
		ranks[segment.segmentID] = index
	}
	return ranks
}

func framesForSegment(frames []domain.VideoUnderstandingFrameV1, segmentID string) []domain.VideoUnderstandingFrameV1 {
	result := make([]domain.VideoUnderstandingFrameV1, 0, len(frames))
	for _, frame := range frames {
		if frame.SegmentID == segmentID {
			result = append(result, frame)
		}
	}
	return result
}

func frameIDs(frames []domain.VideoUnderstandingFrameV1) []string {
	ids := make([]string, 0, len(frames))
	for _, frame := range frames {
		ids = append(ids, frame.ID)
	}
	return ids
}

func segmentQuality(frames []domain.VideoUnderstandingFrameV1, quality domain.QualitySignals) float64 {
	if len(frames) == 0 {
		fallback := clamp01(quality.OverallScore)
		if fallback == 0 {
			return 0.6
		}
		return fallback
	}
	var total float64
	for _, frame := range frames {
		total += clamp01(frame.QualityScore)
	}
	return clamp01(total / float64(len(frames)))
}

func segmentEvidenceScore(frames []domain.VideoUnderstandingFrameV1, quality domain.QualitySignals) float64 {
	if len(frames) == 0 {
		return 0
	}
	visual := segmentQuality(frames, quality)
	motionBonus := 0.0
	for _, frame := range frames {
		switch frame.SelectionReason {
		case "motion-quarter", "motion-three-quarter":
			motionBonus += 0.05
		}
	}
	if quality.FastMoving {
		motionBonus += 0.05
	}
	return clamp01(0.55 + visual*0.35 + motionBonus)
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
