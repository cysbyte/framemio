package pipeline

import (
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/framemio/video-intelligence-worker/internal/domain"
)

type PlannerConfig struct {
	MinSceneDuration   time.Duration
	MaxSegmentDuration time.Duration
	LongVideoThreshold time.Duration
	MaxFramesShort     int
	MaxFramesLong      int
}

const (
	defaultStorySegmentDuration = 5 * time.Second
)

func BuildScenes(metadata domain.VideoMetadata, boundaries []domain.SceneBoundary, minimumDuration time.Duration) []domain.VideoScene {
	duration := metadata.DurationMicros
	minimum := minimumDuration.Microseconds()
	if minimum <= 0 {
		minimum = 750_000
	}
	filtered := make([]domain.SceneBoundary, 0, len(boundaries))
	last := int64(0)
	for _, boundary := range boundaries {
		if boundary.TimestampMicros < minimum || boundary.TimestampMicros >= duration-minimum {
			continue
		}
		if boundary.TimestampMicros-last < minimum {
			continue
		}
		filtered = append(filtered, boundary)
		last = boundary.TimestampMicros
	}
	scenes := make([]domain.VideoScene, 0, len(filtered)+1)
	start := int64(0)
	changeScore := 1.0
	for index, boundary := range append(filtered, domain.SceneBoundary{TimestampMicros: duration}) {
		end := boundary.TimestampMicros
		if end <= start {
			continue
		}
		scenes = append(scenes, domain.VideoScene{
			ID: fmt.Sprintf("scene-%04d", index+1), StartMicros: start, EndMicros: end, ChangeScore: changeScore,
		})
		start = end
		changeScore = boundary.Score
	}
	if len(scenes) == 0 {
		return []domain.VideoScene{{ID: "scene-0001", StartMicros: 0, EndMicros: duration, ChangeScore: 1}}
	}
	return scenes
}

func BuildSegments(scenes []domain.VideoScene, maximumDuration time.Duration) []domain.CandidateSegment {
	maximum := maximumDuration.Microseconds()
	if maximum <= 0 {
		maximum = 12_000_000
	}
	if storyMaximum := defaultStorySegmentDuration.Microseconds(); maximum > storyMaximum {
		maximum = storyMaximum
	}
	segments := make([]domain.CandidateSegment, 0, len(scenes))
	segmentNumber := 1
	for _, scene := range scenes {
		for start := scene.StartMicros; start < scene.EndMicros; start += maximum {
			end := min(scene.EndMicros, start+maximum)
			segments = append(segments, domain.CandidateSegment{
				ID: fmt.Sprintf("segment-%04d", segmentNumber), SceneID: scene.ID, StartMicros: start, EndMicros: end,
			})
			segmentNumber++
		}
	}
	return segments
}

func PlanFrameCandidates(metadata domain.VideoMetadata, scenes []domain.VideoScene, segments []domain.CandidateSegment, configuration PlannerConfig) []domain.FrameCandidate {
	maximumFrames := configuration.MaxFramesShort
	if time.Duration(metadata.DurationMicros)*time.Microsecond >= configuration.LongVideoThreshold {
		maximumFrames = configuration.MaxFramesLong
	}
	if maximumFrames <= 0 {
		maximumFrames = 24
	}
	sceneScores := make(map[string]float64, len(scenes))
	for _, scene := range scenes {
		sceneScores[scene.ID] = scene.ChangeScore
	}
	sceneChangesPerMinute := float64(max(0, len(scenes)-1)) / maxFloat(float64(metadata.DurationMicros)/60_000_000, 1.0/60)
	denseMotion := sceneChangesPerMinute >= 20 || metadata.FPS >= 60
	candidates := make([]domain.FrameCandidate, 0, len(segments)*5)
	seenTimestamp := make(map[int64]struct{})
	add := func(segment domain.CandidateSegment, timestamp int64, role string, priority float64) {
		timestamp = max(segment.StartMicros, min(segment.EndMicros-1, timestamp))
		if timestamp < 0 {
			return
		}
		if _, exists := seenTimestamp[timestamp]; exists {
			return
		}
		seenTimestamp[timestamp] = struct{}{}
		candidates = append(candidates, domain.FrameCandidate{
			ID: fmt.Sprintf("candidate-%05d", len(candidates)+1), SceneID: segment.SceneID, SegmentID: segment.ID,
			TimestampMicros: timestamp, Role: role, Priority: priority + sceneScores[segment.SceneID],
		})
	}
	for _, segment := range segments {
		length := segment.EndMicros - segment.StartMicros
		if length <= 0 {
			continue
		}
		margin := min(int64(300_000), max(int64(1), length/10))
		add(segment, segment.StartMicros+margin, "scene-entry", 1.2)
		add(segment, segment.StartMicros+length/2, "segment-midpoint", 1.0)
		add(segment, segment.EndMicros-margin, "scene-exit", 1.1)
		if denseMotion && length >= 2_000_000 {
			add(segment, segment.StartMicros+length/4, "motion-quarter", 0.9)
			add(segment, segment.StartMicros+3*length/4, "motion-three-quarter", 0.9)
		}
	}
	limit := maximumFrames * 4
	if len(candidates) <= limit {
		return candidates
	}
	ranked := append([]domain.FrameCandidate(nil), candidates...)
	sort.SliceStable(ranked, func(left, right int) bool {
		if ranked[left].Priority == ranked[right].Priority {
			return ranked[left].TimestampMicros < ranked[right].TimestampMicros
		}
		return ranked[left].Priority > ranked[right].Priority
	})
	ranked = ranked[:limit]
	sort.Slice(ranked, func(left, right int) bool { return ranked[left].TimestampMicros < ranked[right].TimestampMicros })
	return ranked
}

func MaxSelectedFrames(metadata domain.VideoMetadata, configuration PlannerConfig) int {
	if time.Duration(metadata.DurationMicros)*time.Microsecond >= configuration.LongVideoThreshold {
		return configuration.MaxFramesLong
	}
	return configuration.MaxFramesShort
}

func ComputeQuality(metadata domain.VideoMetadata, scenes []domain.VideoScene, frames []domain.SelectedFrame) domain.QualitySignals {
	durationMinutes := maxFloat(float64(metadata.DurationMicros)/60_000_000, 1.0/60)
	changesPerMinute := float64(max(0, len(scenes)-1)) / durationMinutes
	var sharpness, brightness, frameQuality float64
	for _, frame := range frames {
		sharpness += frame.Sharpness
		brightness += frame.Brightness
		frameQuality += frame.QualityScore
	}
	if len(frames) > 0 {
		sharpness /= float64(len(frames))
		brightness /= float64(len(frames))
		frameQuality /= float64(len(frames))
	}
	lowResolution := metadata.Width*metadata.Height < 1280*720
	lowBitRate := metadata.BitRate > 0 && metadata.BitRate < 900_000
	resolutionScore := 1.0
	if lowResolution {
		resolutionScore = 0.55
	}
	bitRateScore := 1.0
	if lowBitRate {
		bitRateScore = 0.6
	}
	overall := clamp(frameQuality*0.6+resolutionScore*0.25+bitRateScore*0.15, 0, 1)
	return domain.QualitySignals{
		OverallScore: overall, LowResolution: lowResolution, LowBitRate: lowBitRate,
		HighFrameRate: metadata.FPS >= 60, LikelyStatic: changesPerMinute < 0.5 && len(frames) <= 2,
		FastMoving: changesPerMinute >= 20, SceneChangesPM: changesPerMinute,
		AverageSharpness: sharpness, AverageBrightness: brightness,
	}
}

func clamp(value, minimum, maximum float64) float64 {
	return math.Max(minimum, math.Min(maximum, value))
}

func maxFloat(left, right float64) float64 {
	if left > right {
		return left
	}
	return right
}
