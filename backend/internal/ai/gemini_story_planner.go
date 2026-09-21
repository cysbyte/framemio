package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/framemio/backend/internal/models"
)

// GeminiStoryPlanner calls Gemini's Interactions API with the structured
// planner input. The response is decoded through the same StoryPlan contract
// used by the OpenAI-compatible planner.
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
	return "gemini-3.8-flash"
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
			slog.String("wire_api", "gemini_interactions"),
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
		p.Logger.Info("story_planner_external_response_received",
			slog.String("provider", p.Name()),
			slog.String("model", p.Model()),
			slog.Int64("latency_ms", time.Since(started).Milliseconds()),
			slog.Int("response_bytes", len(raw)),
		)
	}
	text, err := extractGeminiInteractionText(raw)
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
		"model": p.Model(),
		"input": []any{
			map[string]any{"type": "text", "text": storyPlannerSystemPrompt},
			map[string]any{"type": "text", "text": userPrompt},
		},
		"stream": true,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > storyModelMaxProviderRequestBytes {
		return nil, fmt.Errorf("provider request exceeds %d bytes", storyModelMaxProviderRequestBytes)
	}
	endpoint := geminiStoryPlannerEndpoint(p.BaseURL)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "text/event-stream, application/json")
	request.Header.Set("x-goog-api-key", p.APIKey)
	client := p.Client
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 8192))
		return nil, fmt.Errorf("Gemini returned %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if strings.Contains(strings.ToLower(response.Header.Get("Content-Type")), "text/event-stream") {
		text, streamErr := extractGeminiSSEText(bytes.NewReader(body))
		if streamErr != nil {
			return nil, streamErr
		}
		return json.Marshal(map[string]string{"output_text": text})
	}
	return body, nil
}

func geminiStoryPlannerEndpoint(baseURL string) string {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		base = "https://generativelanguage.googleapis.com"
	}
	if strings.HasSuffix(base, "/v1beta") {
		return base + "/interactions"
	}
	return base + "/v1beta/interactions"
}

func extractGeminiInteractionText(data []byte) (string, error) {
	var response map[string]json.RawMessage
	if err := json.Unmarshal(data, &response); err != nil {
		return "", fmt.Errorf("decode Gemini story planner response: %w", err)
	}
	var outputText string
	_ = json.Unmarshal(response["output_text"], &outputText)
	if strings.TrimSpace(outputText) != "" {
		return outputText, nil
	}
	if text := extractGeminiOutputText(response["output"]); strings.TrimSpace(text) != "" {
		return text, nil
	}
	if text := extractGeminiOutputText(response["data"]); strings.TrimSpace(text) != "" {
		return text, nil
	}
	return "", errors.New("Gemini story planner response is missing output text")
}

func extractGeminiOutputText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var values []json.RawMessage
	if json.Unmarshal(raw, &values) != nil {
		return rawGeminiText(raw)
	}
	var parts []string
	for _, value := range values {
		if text := rawGeminiText(value); strings.TrimSpace(text) != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n")
}

func rawGeminiText(raw json.RawMessage) string {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil {
		return ""
	}
	for _, key := range []string{"text", "output_text", "content"} {
		if value, ok := object[key]; ok {
			if text := extractGeminiOutputText(value); strings.TrimSpace(text) != "" {
				return text
			}
		}
	}
	return ""
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

func extractGeminiSSEText(body io.Reader) (string, error) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 64<<10), 8<<20)
	var text strings.Builder
	var completedText string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") || !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}
		var event map[string]json.RawMessage
		if json.Unmarshal([]byte(data), &event) == nil {
			if value := rawGeminiText(event["delta"]); value != "" {
				text.WriteString(value)
			}
			if value := rawGeminiText(event["output_text"]); value != "" {
				completedText = value
			}
			if value := extractGeminiOutputText(event["output"]); value != "" {
				completedText = value
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	if strings.TrimSpace(text.String()) == "" {
		text.WriteString(completedText)
	}
	if strings.TrimSpace(text.String()) == "" {
		return "", errors.New("Gemini story planner stream contained no text")
	}
	return text.String(), nil
}
