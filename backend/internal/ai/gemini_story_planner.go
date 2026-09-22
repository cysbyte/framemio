package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/framemio/backend/internal/models"
)

// GeminiStoryPlanner calls a Gemini model through the OpenAI-compatible gateway
// used by the video-understanding worker. The response is decoded through the
// same StoryPlan contract used by the other external planner.
type GeminiStoryPlanner struct {
	BaseURL         string
	APIKey          string
	ProviderName    string
	ModelName       string
	MaxOutputTokens int
	Client          *http.Client
	Logger          *slog.Logger
}

var _ StoryPlanner = GeminiStoryPlanner{}

func (p GeminiStoryPlanner) Name() string {
	if strings.TrimSpace(p.ProviderName) != "" {
		return strings.TrimSpace(p.ProviderName)
	}
	return "gemini"
}

func (p GeminiStoryPlanner) Model() string {
	if strings.TrimSpace(p.ModelName) != "" {
		return strings.TrimSpace(p.ModelName)
	}
	return "gemini-3.7-flash"
}

func (p GeminiStoryPlanner) Plan(ctx context.Context, request Request) (models.StoryPlan, error) {
	request = normalizeRequest(request)
	if strings.TrimSpace(p.APIKey) == "" {
		return models.StoryPlan{}, errors.New("Gemini story planner API key is required")
	}
	started := time.Now()
	if p.Logger != nil {
		p.Logger.Info("story_planner_external_request_started",
			slog.String("provider", p.Name()),
			slog.String("model", p.Model()),
			slog.String("wire_api", "chat_completions"),
		)
	}
	input, err := boundedStoryPlannerUserPrompt(storyPlannerInputFromRequest(request), storyModelMaxUserPromptBytes)
	if err != nil {
		return models.StoryPlan{}, err
	}
	raw, err := p.post(ctx, input)
	if err != nil {
		if p.Logger != nil {
			p.Logger.Warn("story_planner_external_request_failed",
				slog.String("provider", p.Name()),
				slog.String("model", p.Model()),
				slog.Int64("latency_ms", time.Since(started).Milliseconds()),
				slog.Any("error", err),
			)
		}
		return models.StoryPlan{}, fmt.Errorf("story planner provider request: %w", err)
	}
	if p.Logger != nil {
		responseMetadata := storyPlannerProviderResponseMetadata(raw, "chat_completions")
		p.Logger.Info("story_planner_external_response_received",
			slog.String("provider", p.Name()),
			slog.String("model", p.Model()),
			slog.Int64("latency_ms", time.Since(started).Milliseconds()),
			slog.Int("response_bytes", len(raw)),
			slog.String("finish_reason", responseMetadata.FinishReason),
			slog.String("response_status", responseMetadata.Status),
			slog.String("incomplete_reason", responseMetadata.IncompleteReason),
		)
	}
	text, err := extractTypographyModelText(raw, "chat_completions")
	if err != nil {
		if p.Logger != nil {
			p.Logger.Warn("story_planner_external_response_invalid",
				slog.String("provider", p.Name()),
				slog.String("model", p.Model()),
				slog.Int64("latency_ms", time.Since(started).Milliseconds()),
				slog.Any("error", err),
			)
		}
		return models.StoryPlan{}, err
	}
	plan, decodeMetrics, err := decodeStoryPlannerTextWithMetrics(text, request)
	if err != nil {
		if p.Logger != nil {
			p.Logger.Warn("story_planner_external_output_invalid",
				slog.String("provider", p.Name()),
				slog.String("model", p.Model()),
				slog.Int64("latency_ms", time.Since(started).Milliseconds()),
				slog.Any("error", err),
			)
		}
		return models.StoryPlan{}, err
	}
	if p.Logger != nil {
		if decodeMetrics.JSONSanitization.changed() {
			p.Logger.Info("story_planner_external_json_sanitized",
				slog.String("provider", p.Name()),
				slog.String("model", p.Model()),
				slog.Int64("latency_ms", time.Since(started).Milliseconds()),
				slog.Bool("audio_original_audio_coerced", decodeMetrics.JSONSanitization.AudioOriginalAudioCoerced),
				slog.Bool("audio_original_audio_dropped", decodeMetrics.JSONSanitization.AudioOriginalAudioDropped),
			)
		}
		if decodeMetrics.RawCaptionCount != len(plan.Captions) ||
			decodeMetrics.RawInvalidCaptionCount > 0 ||
			decodeMetrics.RawOverlappingCaptionCount > 0 ||
			decodeMetrics.FinalOverlappingCaptionCount > 0 ||
			decodeMetrics.RawInvalidVideoAnalysisVersions > 0 ||
			decodeMetrics.RawBRollMissingIDCount > 0 ||
			decodeMetrics.RawBRollMissingScoreCount > 0 ||
			decodeMetrics.RawTemplateID != request.TemplateID ||
			decodeMetrics.RawTemplateVersion != request.TemplateVersion {
			p.Logger.Info("story_planner_external_output_sanitized",
				slog.String("provider", p.Name()),
				slog.String("model", p.Model()),
				slog.Int64("latency_ms", time.Since(started).Milliseconds()),
				slog.Int("raw_caption_count", decodeMetrics.RawCaptionCount),
				slog.Int("raw_invalid_caption_count", decodeMetrics.RawInvalidCaptionCount),
				slog.Int("raw_overlapping_caption_count", decodeMetrics.RawOverlappingCaptionCount),
				slog.Int("raw_invalid_video_analysis_version_count", decodeMetrics.RawInvalidVideoAnalysisVersions),
				slog.Int("raw_broll_missing_id_count", decodeMetrics.RawBRollMissingIDCount),
				slog.Int("raw_broll_missing_score_count", decodeMetrics.RawBRollMissingScoreCount),
				slog.String("raw_template_id", decodeMetrics.RawTemplateID),
				slog.Int("raw_template_version", decodeMetrics.RawTemplateVersion),
				slog.String("final_template_id", plan.TemplateID),
				slog.Int("final_template_version", plan.TemplateVersion),
				slog.Int("final_caption_count", len(plan.Captions)),
				slog.Int("final_overlapping_caption_count", decodeMetrics.FinalOverlappingCaptionCount),
				slog.Int("final_broll_count", len(plan.BRoll)),
				slog.Int("final_broll_with_id_count", decodeMetrics.FinalBRollWithIDCount),
				slog.Int("final_broll_with_score_count", decodeMetrics.FinalBRollWithScoreCount),
			)
		}
		p.Logger.Info("story_planner_external_output_valid",
			slog.String("provider", p.Name()),
			slog.String("model", p.Model()),
			slog.Int64("latency_ms", time.Since(started).Milliseconds()),
			slog.Int("moment_count", len(plan.Moments)),
			slog.Int("broll_count", len(plan.BRoll)),
			slog.Int("broll_with_score_count", decodeMetrics.FinalBRollWithScoreCount),
			slog.Int("caption_count", len(plan.Captions)),
		)
	}
	return plan, nil
}

func (p GeminiStoryPlanner) post(ctx context.Context, userPrompt string) ([]byte, error) {
	payload := map[string]any{
		"model":       p.Model(),
		"temperature": 0.25,
		"max_tokens":  storyPlannerMaxOutputTokens(p.MaxOutputTokens),
		"response_format": map[string]string{
			"type": "json_object",
		},
		"messages": []map[string]string{
			{"role": "system", "content": storyPlannerSystemPrompt},
			{"role": "user", "content": userPrompt},
		},
	}
	return postTypographyJSONWithRequestLimit(
		ctx,
		p.Client,
		typographyEndpointFor(p.BaseURL, "chat/completions"),
		p.APIKey,
		payload,
		storyModelMaxProviderRequestBytes,
		4<<20,
	)
}

func decodeStoryPlannerText(text string, request Request) (models.StoryPlan, error) {
	plan, _, err := decodeStoryPlannerTextWithMetrics(text, request)
	return plan, err
}

type storyPlannerDecodeMetrics struct {
	JSONSanitization                storyPlannerJSONSanitization
	RawCaptionCount                 int
	RawInvalidCaptionCount          int
	RawOverlappingCaptionCount      int
	RawInvalidVideoAnalysisVersions int
	RawTemplateID                   string
	RawTemplateVersion              int
	RawBRollMissingIDCount          int
	RawBRollMissingScoreCount       int
	FinalBRollWithIDCount           int
	FinalBRollWithScoreCount        int
	FinalOverlappingCaptionCount    int
}

func decodeStoryPlannerTextWithMetrics(text string, request Request) (models.StoryPlan, storyPlannerDecodeMetrics, error) {
	var metrics storyPlannerDecodeMetrics
	responseJSON, err := extractTypographyJSONObject([]byte(text))
	if err != nil {
		return models.StoryPlan{}, metrics, fmt.Errorf("extract story plan JSON: %w", err)
	}
	responseJSON, metrics.JSONSanitization, err = sanitizeStoryPlannerResponseJSON(responseJSON)
	if err != nil {
		return models.StoryPlan{}, metrics, fmt.Errorf("sanitize story plan JSON: %w", err)
	}
	var plan models.StoryPlan
	if err := json.Unmarshal(responseJSON, &plan); err != nil {
		return models.StoryPlan{}, metrics, fmt.Errorf("decode story plan JSON: %w", err)
	}
	metrics.RawCaptionCount = len(plan.Captions)
	metrics.RawInvalidCaptionCount = invalidStoryPlannerCaptionCount(plan)
	metrics.RawOverlappingCaptionCount = overlappingStoryPlannerCaptionCount(plan)
	metrics.RawInvalidVideoAnalysisVersions = invalidStoryPlannerVideoAnalysisVersionCount(plan, request)
	metrics.RawTemplateID = plan.TemplateID
	metrics.RawTemplateVersion = plan.TemplateVersion
	metrics.RawBRollMissingIDCount, metrics.RawBRollMissingScoreCount = storyPlannerBRollMetadataGaps(plan)
	if err := normalizeStoryPlannerOutput(&plan, request); err != nil {
		return models.StoryPlan{}, metrics, err
	}
	metrics.FinalBRollWithIDCount, metrics.FinalBRollWithScoreCount = storyPlannerBRollMetadataCounts(plan)
	metrics.FinalOverlappingCaptionCount = overlappingStoryPlannerCaptionCount(plan)
	return plan, metrics, nil
}
