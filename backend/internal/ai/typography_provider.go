package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const typographySystemPrompt = `You are FrameMio's typography planner for personal memory films.
Return only strict JSON matching this schema:
{
  "overlays": [
    {
      "moment_id": "existing moment id",
      "enabled": true,
      "type": "moment_title|moment_caption|location|date",
      "title": "short title, max 40 characters",
      "subtitle": "optional subtitle, max 100 characters",
      "style": "cinematic|minimal|editorial|memory|location",
      "placement": "top_left|top_center|top_right|center_left|center|center_right|bottom_left|bottom_center|bottom_right",
      "start_ms": 350,
      "duration_ms": 3000,
      "max_width": 0.72,
      "safe_area": {"top": 0.08, "right": 0.08, "bottom": 0.08, "left": 0.08},
      "animation": "fade|fade_up|fade_down|subtle_scale|none",
      "source": "ai",
      "reason": "brief private-safe rationale",
      "confidence": 0.9
    }
  ]
}
Do not add text to every photo. Add typography only when it improves the story.
Avoid literal captions, hashtags, memes, calls to action, and generic phrases.
Prefer short, emotionally meaningful language that feels cinematic/editorial.
Do not invent locations, dates, names, events, or facts. Only use reliable supplied metadata.
Avoid covering faces, subjects, OCR/text, or visually complex areas. Prefer safe low-detail areas.
Use only the supplied enum values. Never invent CSS, fonts, colors, commands, file paths, or HTML.
If no photo benefits from typography, return {"overlays": []}.`

// OpenAICompatibleTypographyPlanner calls an OpenAI-compatible text endpoint but
// still returns the provider-independent TypographyPlanner interface.
type OpenAICompatibleTypographyPlanner struct {
	BaseURL         string
	APIKey          string
	ProviderName    string
	ModelName       string
	WireAPI         string
	MaxOutputTokens int
	Client          *http.Client
}

func (p OpenAICompatibleTypographyPlanner) Generate(ctx context.Context, input TypographyPlanningInput) (TypographyPlan, error) {
	wireAPI := normalizeTypographyWireAPI(p.WireAPI)
	var raw []byte
	var err error
	switch wireAPI {
	case "responses":
		raw, err = p.postResponses(ctx, input)
	case "chat_completions":
		raw, err = p.postChatCompletions(ctx, input)
	default:
		return TypographyPlan{}, fmt.Errorf("unsupported typography wire API %q", p.WireAPI)
	}
	if err != nil {
		return TypographyPlan{}, fmt.Errorf("typography provider request: %w", err)
	}
	text, err := extractTypographyModelText(raw, wireAPI)
	if err != nil {
		return TypographyPlan{}, err
	}
	responseJSON, err := extractTypographyJSONObject([]byte(text))
	if err != nil {
		return TypographyPlan{}, fmt.Errorf("extract typography JSON: %w", err)
	}
	return ParseTypographyPlanJSON(responseJSON, typographyMomentDurations(input.Moments))
}

func (p OpenAICompatibleTypographyPlanner) Name() string {
	if strings.TrimSpace(p.ProviderName) != "" {
		return p.ProviderName
	}
	return "openai-compatible"
}

func (p OpenAICompatibleTypographyPlanner) Model() string { return p.ModelName }

func (p OpenAICompatibleTypographyPlanner) postChatCompletions(ctx context.Context, input TypographyPlanningInput) ([]byte, error) {
	type message struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	request := map[string]any{
		"model":       p.ModelName,
		"temperature": 0.35,
		"max_tokens":  typographyMaxOutputTokens(p.MaxOutputTokens),
		"response_format": map[string]string{
			"type": "json_object",
		},
		"messages": []message{
			{Role: "system", Content: typographySystemPrompt},
			{Role: "user", Content: typographyUserPrompt(input)},
		},
	}
	return postTypographyJSON(ctx, p.Client, typographyEndpointFor(p.BaseURL, "chat/completions"), p.APIKey, request, 1<<20)
}

func (p OpenAICompatibleTypographyPlanner) postResponses(ctx context.Context, input TypographyPlanningInput) ([]byte, error) {
	type inputText struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	type inputMessage struct {
		Role    string      `json:"role"`
		Content []inputText `json:"content"`
	}
	request := map[string]any{
		"model":             p.ModelName,
		"temperature":       0.35,
		"max_output_tokens": typographyMaxOutputTokens(p.MaxOutputTokens),
		"text": map[string]any{
			"format": map[string]string{"type": "json_object"},
		},
		"input": []inputMessage{
			{Role: "system", Content: []inputText{{Type: "input_text", Text: typographySystemPrompt}}},
			{Role: "user", Content: []inputText{{Type: "input_text", Text: typographyUserPrompt(input)}}},
		},
	}
	return postTypographyJSON(ctx, p.Client, typographyEndpointFor(p.BaseURL, "responses"), p.APIKey, request, 1<<20)
}

func typographyUserPrompt(input TypographyPlanningInput) string {
	payload, err := json.Marshal(input)
	if err != nil {
		return "Plan FrameMio typography overlays from the supplied story context."
	}
	return strings.Join([]string{
		"Plan optional cinematic typography for this FrameMio Story.",
		"Use the Story context and moment metadata as source of truth.",
		"Return overlays only for photo/live_photo moments that benefit from text.",
		"Do not repeat speech captions or describe the image literally.",
		"Story planning JSON:",
		string(payload),
	}, "\n")
}

func typographyMomentDurations(moments []TypographyMomentContext) map[string]int {
	durations := make(map[string]int, len(moments))
	for _, moment := range moments {
		if strings.TrimSpace(moment.MomentID) != "" {
			durations[moment.MomentID] = moment.DurationMS
		}
	}
	return durations
}

func typographyEndpointFor(baseURL, suffix string) string {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		base = "https://api.openai.com"
	}
	if strings.HasSuffix(base, "/"+suffix) {
		return base
	}
	if strings.HasSuffix(base, "/v1") {
		return base + "/" + suffix
	}
	return base + "/v1/" + suffix
}

func normalizeTypographyWireAPI(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "chat", "chat-completions", "chat_completions":
		return "chat_completions"
	case "response", "responses":
		return "responses"
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

func typographyMaxOutputTokens(value int) int {
	if value > 0 {
		return value
	}
	return 1400
}

func typographyHTTPClient(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = 12 * time.Second
	}
	return &http.Client{Timeout: timeout}
}

func postTypographyJSON(ctx context.Context, client *http.Client, endpoint, apiKey string, input any, limit int64) ([]byte, error) {
	return postTypographyJSONWithRequestLimit(ctx, client, endpoint, apiKey, input, 0, limit)
}

func postTypographyJSONWithRequestLimit(ctx context.Context, client *http.Client, endpoint, apiKey string, input any, requestLimit, responseLimit int64) ([]byte, error) {
	if client == nil {
		client = http.DefaultClient
	}
	data, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	if requestLimit > 0 && int64(len(data)) > requestLimit {
		return nil, fmt.Errorf("provider request exceeds %d bytes", requestLimit)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	if strings.TrimSpace(apiKey) != "" {
		request.Header.Set("Authorization", "Bearer "+apiKey)
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return nil, fmt.Errorf("provider returned %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}
	data, err = io.ReadAll(io.LimitReader(response.Body, responseLimit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > responseLimit {
		return nil, fmt.Errorf("provider response exceeds %d bytes", responseLimit)
	}
	return data, nil
}

func extractTypographyModelText(data []byte, wireAPI string) (string, error) {
	switch wireAPI {
	case "chat_completions":
		return extractTypographyChatText(data)
	case "responses":
		return extractTypographyResponsesText(data)
	default:
		return "", fmt.Errorf("unsupported typography wire API %q", wireAPI)
	}
}

func extractTypographyChatText(data []byte) (string, error) {
	var response struct {
		Choices []struct {
			Message struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return "", fmt.Errorf("decode typography chat response: %w", err)
	}
	if len(response.Choices) == 0 || len(response.Choices[0].Message.Content) == 0 {
		return "", errors.New("typography chat response is missing content")
	}
	return rawTypographyTextContent(response.Choices[0].Message.Content)
}

func extractTypographyResponsesText(data []byte) (string, error) {
	var response struct {
		OutputText string `json:"output_text,omitempty"`
		Output     []struct {
			Content []struct {
				Text string `json:"text,omitempty"`
			} `json:"content,omitempty"`
		} `json:"output,omitempty"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return "", fmt.Errorf("decode typography responses response: %w", err)
	}
	if strings.TrimSpace(response.OutputText) != "" {
		return response.OutputText, nil
	}
	parts := make([]string, 0)
	for _, output := range response.Output {
		for _, content := range output.Content {
			if strings.TrimSpace(content.Text) != "" {
				parts = append(parts, content.Text)
			}
		}
	}
	if len(parts) == 0 {
		return "", errors.New("typography responses response is missing output text")
	}
	return strings.Join(parts, "\n"), nil
}

func rawTypographyTextContent(data json.RawMessage) (string, error) {
	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		return text, nil
	}
	var parts []struct {
		Text string `json:"text,omitempty"`
	}
	if err := json.Unmarshal(data, &parts); err != nil {
		return "", fmt.Errorf("decode typography text content: %w", err)
	}
	items := make([]string, 0, len(parts))
	for _, part := range parts {
		if strings.TrimSpace(part.Text) != "" {
			items = append(items, part.Text)
		}
	}
	if len(items) == 0 {
		return "", errors.New("typography text content is empty")
	}
	return strings.Join(items, "\n"), nil
}

func extractTypographyJSONObject(data []byte) ([]byte, error) {
	trimmed := bytes.TrimSpace(data)
	if json.Valid(trimmed) {
		return trimmed, nil
	}
	trimmed = bytes.TrimPrefix(trimmed, []byte("```json"))
	trimmed = bytes.TrimPrefix(trimmed, []byte("```"))
	trimmed = bytes.TrimSuffix(trimmed, []byte("```"))
	trimmed = bytes.TrimSpace(trimmed)
	if json.Valid(trimmed) {
		return trimmed, nil
	}
	start := bytes.IndexByte(trimmed, '{')
	if start < 0 {
		return nil, errors.New("no JSON object found")
	}
	decoder := json.NewDecoder(bytes.NewReader(trimmed[start:]))
	var raw json.RawMessage
	if err := decoder.Decode(&raw); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, errors.New("no JSON object found")
		}
		return nil, err
	}
	if !json.Valid(raw) {
		return nil, errors.New("invalid JSON object")
	}
	return raw, nil
}
