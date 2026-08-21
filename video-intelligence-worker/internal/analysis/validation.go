package analysis

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strings"

	"github.com/framemio/video-intelligence-worker/internal/domain"
)

func Decode(data []byte) (domain.VideoAnalysis, error) {
	var value domain.VideoAnalysis
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return domain.VideoAnalysis{}, fmt.Errorf("decode video analysis: %w", err)
	}
	if err := ensureJSONEnd(decoder); err != nil {
		return domain.VideoAnalysis{}, err
	}
	if err := Validate(value); err != nil {
		return domain.VideoAnalysis{}, err
	}
	return value, nil
}

func Validate(value domain.VideoAnalysis) error {
	if strings.TrimSpace(value.ID) == "" || strings.TrimSpace(value.UserID) == "" ||
		strings.TrimSpace(value.ProjectID) == "" || strings.TrimSpace(value.VideoID) == "" {
		return fmt.Errorf("video analysis identity fields are required")
	}
	if value.SchemaVersion != domain.VideoAnalysisSchemaV1 {
		return fmt.Errorf("schema_version must be %s", domain.VideoAnalysisSchemaV1)
	}
	if strings.TrimSpace(value.AnalysisVersion) == "" || strings.TrimSpace(value.InputVersion) == "" ||
		strings.TrimSpace(value.UnderstandingVersion) == "" || strings.TrimSpace(value.Provider) == "" ||
		strings.TrimSpace(value.Model) == "" || strings.TrimSpace(value.EvidenceS3Key) == "" {
		return fmt.Errorf("analysis provenance fields are required")
	}
	if value.ProcessingStatus != domain.VideoAnalysisStatusCompleted {
		return fmt.Errorf("processing_status must be %s", domain.VideoAnalysisStatusCompleted)
	}
	if value.DurationMicros <= 0 {
		return fmt.Errorf("duration_micros must be positive")
	}
	if value.CreatedAt.IsZero() {
		return fmt.Errorf("created_at is required")
	}
	if err := validateQuality(value.QualitySignals); err != nil {
		return err
	}
	if len(value.Scenes) == 0 || len(value.Segments) == 0 {
		return fmt.Errorf("video analysis requires scenes and segments")
	}
	sceneIDs := make(map[string]domain.Scene, len(value.Scenes))
	frameIDs := make(map[string]struct{})
	for index, scene := range value.Scenes {
		if strings.TrimSpace(scene.SceneID) == "" {
			return fmt.Errorf("scene %d requires scene_id", index)
		}
		if _, exists := sceneIDs[scene.SceneID]; exists {
			return fmt.Errorf("duplicate scene_id %s", scene.SceneID)
		}
		sceneIDs[scene.SceneID] = scene
		if err := validateRange("scene "+scene.SceneID, scene.StartMicros, scene.EndMicros, value.DurationMicros); err != nil {
			return err
		}
		for frameIndex, frame := range scene.RepresentativeFrames {
			if strings.TrimSpace(frame.FrameID) == "" || strings.TrimSpace(frame.S3Key) == "" {
				return fmt.Errorf("scene %s frame %d requires frame_id and s3_key", scene.SceneID, frameIndex)
			}
			if _, exists := frameIDs[frame.FrameID]; exists {
				return fmt.Errorf("duplicate frame_id %s", frame.FrameID)
			}
			frameIDs[frame.FrameID] = struct{}{}
			if frame.TimestampMicros < scene.StartMicros || frame.TimestampMicros > scene.EndMicros {
				return fmt.Errorf("frame %s timestamp is outside scene %s", frame.FrameID, scene.SceneID)
			}
		}
	}
	if err := validateTranscript(value.Transcript, value.DurationMicros); err != nil {
		return err
	}
	segmentIDs := make(map[string]struct{}, len(value.Segments))
	for index, segment := range value.Segments {
		if strings.TrimSpace(segment.SegmentID) == "" || strings.TrimSpace(segment.SceneID) == "" || strings.TrimSpace(segment.Description) == "" {
			return fmt.Errorf("segment %d requires segment_id, scene_id, and description", index)
		}
		if _, exists := segmentIDs[segment.SegmentID]; exists {
			return fmt.Errorf("duplicate segment_id %s", segment.SegmentID)
		}
		segmentIDs[segment.SegmentID] = struct{}{}
		scene, exists := sceneIDs[segment.SceneID]
		if !exists {
			return fmt.Errorf("segment %s references unknown scene %s", segment.SegmentID, segment.SceneID)
		}
		if err := validateRange("segment "+segment.SegmentID, segment.StartMicros, segment.EndMicros, value.DurationMicros); err != nil {
			return err
		}
		if segment.StartMicros < scene.StartMicros || segment.EndMicros > scene.EndMicros {
			return fmt.Errorf("segment %s timestamps are outside scene %s", segment.SegmentID, segment.SceneID)
		}
		if !score(segment.Importance) || !score(segment.VisualQuality) || !score(segment.MemoryValue) ||
			!score(segment.EmotionalValue) || !score(segment.StoryRelevance) {
			return fmt.Errorf("segment %s contains a score outside zero to one", segment.SegmentID)
		}
		if !recommendedUsage(segment.RecommendedUsage) {
			return fmt.Errorf("segment %s has invalid recommended_usage", segment.SegmentID)
		}
		if segment.Recommended != (segment.RecommendedUsage != domain.RecommendedUsageExclude) {
			return fmt.Errorf("segment %s recommended flag conflicts with recommended_usage", segment.SegmentID)
		}
		if len(segment.Reasons) == 0 {
			return fmt.Errorf("segment %s requires at least one reason", segment.SegmentID)
		}
		for _, reason := range segment.Reasons {
			if strings.TrimSpace(reason) == "" {
				return fmt.Errorf("segment %s contains an empty reason", segment.SegmentID)
			}
		}
	}
	if err := validateLabels("detected_people", value.DetectedPeople); err != nil {
		return err
	}
	if err := validateLabels("detected_activities", value.DetectedActivities); err != nil {
		return err
	}
	return validateLabels("detected_locations", value.DetectedLocations)
}

func validateRange(name string, start, end, duration int64) error {
	if start < 0 || end <= start || end > duration {
		return fmt.Errorf("%s timestamps must reference the original source duration", name)
	}
	return nil
}

func validateTranscript(transcript domain.Transcript, duration int64) error {
	if !transcript.HasSpeech && len(transcript.Segments) > 0 {
		return fmt.Errorf("transcript without speech cannot contain segments")
	}
	for index, segment := range transcript.Segments {
		if strings.TrimSpace(segment.Text) == "" {
			return fmt.Errorf("transcript segment %d requires text", index)
		}
		if err := validateRange(fmt.Sprintf("transcript segment %d", index), segment.StartMicros, segment.EndMicros, duration); err != nil {
			return err
		}
		if segment.Confidence != 0 && !score(segment.Confidence) {
			return fmt.Errorf("transcript segment %d has invalid confidence", index)
		}
	}
	return nil
}

func validateQuality(value domain.QualitySignals) error {
	if !score(value.OverallScore) || !optionalScore(value.AverageSharpness) || !optionalScore(value.AverageBrightness) {
		return fmt.Errorf("quality signal score is outside zero to one")
	}
	if math.IsNaN(value.SceneChangesPM) || math.IsInf(value.SceneChangesPM, 0) || value.SceneChangesPM < 0 {
		return fmt.Errorf("scene_changes_per_minute must be non-negative")
	}
	return nil
}

func validateLabels(name string, labels []string) error {
	seen := make(map[string]struct{}, len(labels))
	for _, label := range labels {
		trimmed := strings.TrimSpace(label)
		if trimmed == "" || trimmed != label {
			return fmt.Errorf("%s contains an empty or untrimmed label", name)
		}
		key := strings.ToLower(trimmed)
		if _, exists := seen[key]; exists {
			return fmt.Errorf("%s contains duplicate label %s", name, label)
		}
		seen[key] = struct{}{}
	}
	return nil
}

func score(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0 && value <= 1
}

func optionalScore(value float64) bool { return value == 0 || score(value) }

func recommendedUsage(value string) bool {
	switch value {
	case domain.RecommendedUsageHero, domain.RecommendedUsageSupporting, domain.RecommendedUsageContext,
		domain.RecommendedUsageAudioOnly, domain.RecommendedUsageExclude:
		return true
	default:
		return false
	}
}

func ensureJSONEnd(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err == io.EOF {
		return nil
	} else if err != nil {
		return fmt.Errorf("decode trailing video analysis data: %w", err)
	}
	return fmt.Errorf("video analysis contains trailing JSON values")
}
