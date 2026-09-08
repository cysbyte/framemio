package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/framemio/video-intelligence-worker/internal/domain"
	"github.com/framemio/video-intelligence-worker/internal/understanding"
)

const directVideoUnderstandingPrompt = `You are FrameMio's video understanding model. Analyze the provided iPhone video evidence for a personal memory video.
Return only strict JSON matching this schema:
{
  "schema_version": "video-analysis.v1",
  "candidates": [
    {
      "segment_id": "string",
      "scene_id": "string",
      "start_micros": 0,
      "end_micros": 0,
      "description": "string",
      "importance_score": 0.0,
      "visual_quality_score": 0.0,
      "memory_value_score": 0.0,
      "emotional_value_score": 0.0,
      "story_relevance_score": 0.0,
      "recommended_usage": "hero|supporting|context|audio_only|exclude",
      "reason": "string",
      "frame_ids": ["existing frame id"],
      "transcript_segment_indexes": [0]
    }
  ]
}
Return exactly one candidate for every input segment. Do not invent segment IDs, scene IDs, timestamps, frame IDs, or transcript indexes. Ground visual claims in frame_ids and speech/audio claims in transcript_segment_indexes.
Write descriptions and reasons for a personal memory app: warm, concise, and user-centered. Avoid detached surveillance-style wording like "a woman", "a man", "a child", or "people visible" when a memory-oriented phrase such as "a family moment", "a lakeside picnic", or "a happy everyday moment" is more appropriate. Do not claim identity or say "you" unless the evidence explicitly supports it.`

const directVideoScenePrompt = `You are FrameMio's video scene detection model. Analyze the provided iPhone video evidence and return only strict JSON matching this schema:
{
  "schema_version": "video-scene-analysis.v1",
  "scenes": [
    {
      "segment_ids": ["existing segment id"],
      "description": "string",
      "reason": "string",
      "frame_ids": ["existing frame id"]
    }
  ]
}
Return at least one scene and cover every input segment exactly once. Use segment_ids from the supplied evidence only. Prefer one scene for static content, and more scenes only when the content actually changes. Ground scene descriptions in supplied frame_ids and transcript evidence when available. Do not invent timestamps, frames, or segment IDs.`

type OpenAICompatibleVideoUnderstandingProvider struct {
	BaseURL         string
	APIKey          string
	ProviderName    string
	ModelName       string
	WireAPI         string
	ImageDetail     string
	MaxOutputTokens int
	Client          *http.Client
}

func (p OpenAICompatibleVideoUnderstandingProvider) DetectScenes(ctx context.Context, input domain.VideoUnderstandingInputV1) (domain.VideoSceneDetectionResponseV1, error) {
	wireAPI := normalizeWireAPI(p.WireAPI)
	var raw []byte
	var err error
	switch wireAPI {
	case "responses":
		raw, err = p.postSceneResponses(ctx, input)
	case "chat_completions":
		raw, err = p.postChatSceneCompletions(ctx, input)
	default:
		return domain.VideoSceneDetectionResponseV1{}, domain.Permanent(fmt.Errorf("unsupported direct video understanding wire API %q", p.WireAPI))
	}
	if err != nil {
		return domain.VideoSceneDetectionResponseV1{}, fmt.Errorf("direct video scene detection request: %w", err)
	}
	text, err := extractModelText(raw, wireAPI)
	if err != nil {
		return domain.VideoSceneDetectionResponseV1{}, err
	}
	responseJSON, err := extractJSONObject([]byte(text))
	if err != nil {
		return domain.VideoSceneDetectionResponseV1{}, fmt.Errorf("extract direct video scene JSON: %w", err)
	}
	return understanding.DecodeSceneResponseV1(responseJSON)
}

func (p OpenAICompatibleVideoUnderstandingProvider) Understand(ctx context.Context, input domain.VideoUnderstandingInputV1) (domain.VideoUnderstandingResponseV1, error) {
	wireAPI := normalizeWireAPI(p.WireAPI)
	var raw []byte
	var err error
	switch wireAPI {
	case "responses":
		raw, err = p.postResponses(ctx, input)
	case "chat_completions":
		raw, err = p.postChatCompletions(ctx, input)
	default:
		return domain.VideoUnderstandingResponseV1{}, domain.Permanent(fmt.Errorf("unsupported direct video understanding wire API %q", p.WireAPI))
	}
	if err != nil {
		return domain.VideoUnderstandingResponseV1{}, fmt.Errorf("direct video understanding request: %w", err)
	}
	text, err := extractModelText(raw, wireAPI)
	if err != nil {
		return domain.VideoUnderstandingResponseV1{}, err
	}
	responseJSON, err := extractJSONObject([]byte(text))
	if err != nil {
		return domain.VideoUnderstandingResponseV1{}, fmt.Errorf("extract direct video understanding JSON: %w", err)
	}
	return understanding.DecodeResponseV1(responseJSON)
}

func (p OpenAICompatibleVideoUnderstandingProvider) Name() string {
	if strings.TrimSpace(p.ProviderName) != "" {
		return p.ProviderName
	}
	return "openai-direct"
}

func (p OpenAICompatibleVideoUnderstandingProvider) Model() string { return p.ModelName }

func (p OpenAICompatibleVideoUnderstandingProvider) postChatCompletions(ctx context.Context, input domain.VideoUnderstandingInputV1) ([]byte, error) {
	type textPart struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	type imageURL struct {
		URL    string `json:"url"`
		Detail string `json:"detail,omitempty"`
	}
	type imagePart struct {
		Type     string   `json:"type"`
		ImageURL imageURL `json:"image_url"`
	}
	type message struct {
		Role    string `json:"role"`
		Content any    `json:"content"`
	}
	content := make([]any, 0, 1+len(input.Frames)*2)
	content = append(content, textPart{Type: "text", Text: userPrompt(input)})
	for _, frame := range input.Frames {
		content = append(content, textPart{Type: "text", Text: frameReferenceText(frame)})
		content = append(content, imagePart{Type: "image_url", ImageURL: imageURL{URL: frame.URL, Detail: imageDetail(p.ImageDetail)}})
	}
	request := map[string]any{
		"model":       p.ModelName,
		"temperature": 0.2,
		"max_tokens":  maxOutputTokens(p.MaxOutputTokens),
		"response_format": map[string]string{
			"type": "json_object",
		},
		"messages": []message{
			{Role: "system", Content: directVideoUnderstandingPrompt},
			{Role: "user", Content: content},
		},
	}
	return postJSONBytes(ctx, p.Client, endpointFor(p.BaseURL, "chat/completions"), p.APIKey, request, 16<<20)
}

func (p OpenAICompatibleVideoUnderstandingProvider) postResponses(ctx context.Context, input domain.VideoUnderstandingInputV1) ([]byte, error) {
	type inputText struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	type inputImage struct {
		Type     string `json:"type"`
		ImageURL string `json:"image_url"`
		Detail   string `json:"detail,omitempty"`
	}
	type inputMessage struct {
		Role    string `json:"role"`
		Content any    `json:"content"`
	}
	content := make([]any, 0, 1+len(input.Frames)*2)
	content = append(content, inputText{Type: "input_text", Text: userPrompt(input)})
	for _, frame := range input.Frames {
		content = append(content, inputText{Type: "input_text", Text: frameReferenceText(frame)})
		content = append(content, inputImage{Type: "input_image", ImageURL: frame.URL, Detail: imageDetail(p.ImageDetail)})
	}
	request := map[string]any{
		"model":             p.ModelName,
		"temperature":       0.2,
		"max_output_tokens": maxOutputTokens(p.MaxOutputTokens),
		"text": map[string]any{
			"format": map[string]string{"type": "json_object"},
		},
		"input": []inputMessage{
			{Role: "system", Content: []inputText{{Type: "input_text", Text: directVideoUnderstandingPrompt}}},
			{Role: "user", Content: content},
		},
	}
	return postJSONBytes(ctx, p.Client, endpointFor(p.BaseURL, "responses"), p.APIKey, request, 16<<20)
}

func (p OpenAICompatibleVideoUnderstandingProvider) postChatSceneCompletions(ctx context.Context, input domain.VideoUnderstandingInputV1) ([]byte, error) {
	type textPart struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	type imageURL struct {
		URL    string `json:"url"`
		Detail string `json:"detail,omitempty"`
	}
	type imagePart struct {
		Type     string   `json:"type"`
		ImageURL imageURL `json:"image_url"`
	}
	type message struct {
		Role    string `json:"role"`
		Content any    `json:"content"`
	}
	content := make([]any, 0, 1+len(input.Frames)*2)
	content = append(content, textPart{Type: "text", Text: scenePrompt(input)})
	for _, frame := range input.Frames {
		content = append(content, textPart{Type: "text", Text: frameReferenceText(frame)})
		content = append(content, imagePart{Type: "image_url", ImageURL: imageURL{URL: frame.URL, Detail: imageDetail(p.ImageDetail)}})
	}
	request := map[string]any{
		"model":       p.ModelName,
		"temperature": 0.1,
		"max_tokens":  maxOutputTokens(p.MaxOutputTokens),
		"response_format": map[string]string{
			"type": "json_object",
		},
		"messages": []message{
			{Role: "system", Content: directVideoScenePrompt},
			{Role: "user", Content: content},
		},
	}
	return postJSONBytes(ctx, p.Client, endpointFor(p.BaseURL, "chat/completions"), p.APIKey, request, 16<<20)
}

func (p OpenAICompatibleVideoUnderstandingProvider) postSceneResponses(ctx context.Context, input domain.VideoUnderstandingInputV1) ([]byte, error) {
	type inputText struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	type inputImage struct {
		Type     string `json:"type"`
		ImageURL string `json:"image_url"`
		Detail   string `json:"detail,omitempty"`
	}
	type inputMessage struct {
		Role    string `json:"role"`
		Content any    `json:"content"`
	}
	content := make([]any, 0, 1+len(input.Frames)*2)
	content = append(content, inputText{Type: "input_text", Text: scenePrompt(input)})
	for _, frame := range input.Frames {
		content = append(content, inputText{Type: "input_text", Text: frameReferenceText(frame)})
		content = append(content, inputImage{Type: "input_image", ImageURL: frame.URL, Detail: imageDetail(p.ImageDetail)})
	}
	request := map[string]any{
		"model":             p.ModelName,
		"temperature":       0.1,
		"max_output_tokens": maxOutputTokens(p.MaxOutputTokens),
		"text": map[string]any{
			"format": map[string]string{"type": "json_object"},
		},
		"input": []inputMessage{
			{Role: "system", Content: []inputText{{Type: "input_text", Text: directVideoScenePrompt}}},
			{Role: "user", Content: content},
		},
	}
	return postJSONBytes(ctx, p.Client, endpointFor(p.BaseURL, "responses"), p.APIKey, request, 16<<20)
}

func scenePrompt(input domain.VideoUnderstandingInputV1) string {
	payload, err := json.Marshal(input)
	if err != nil {
		return input.Objective
	}
	return strings.Join([]string{
		"Analyze this FrameMio video evidence and return the required JSON only.",
		"Prefer one scene for static content, and more scenes only when the content actually changes.",
		"Evidence JSON:",
		string(payload),
	}, "\n")
}

func userPrompt(input domain.VideoUnderstandingInputV1) string {
	payload, err := json.Marshal(input)
	if err != nil {
		return input.Objective
	}
	return strings.Join([]string{
		"Analyze this FrameMio video evidence and return the required JSON only.",
		"Use all timestamps as original-video microseconds.",
		"Evidence JSON:",
		string(payload),
	}, "\n")
}

func frameReferenceText(frame domain.VideoUnderstandingFrameV1) string {
	return fmt.Sprintf("Frame %s belongs to segment %s, scene %s, timestamp_micros=%d.", frame.ID, frame.SegmentID, frame.SceneID, frame.TimestampMicros)
}

func endpointFor(baseURL, suffix string) string {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if strings.HasSuffix(base, "/"+suffix) {
		return base
	}
	if strings.HasSuffix(base, "/v1") {
		return base + "/" + suffix
	}
	return base + "/v1/" + suffix
}

func normalizeWireAPI(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "chat", "chat-completions", "chat_completions":
		return "chat_completions"
	case "response", "responses":
		return "responses"
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

func imageDetail(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "low"
	}
	return value
}

func maxOutputTokens(value int) int {
	if value > 0 {
		return value
	}
	return 5000
}

func extractModelText(data []byte, wireAPI string) (string, error) {
	switch wireAPI {
	case "chat_completions":
		return extractChatCompletionsText(data)
	case "responses":
		return extractResponsesText(data)
	default:
		return "", domain.Permanent(fmt.Errorf("unsupported direct video understanding wire API %q", wireAPI))
	}
}

func extractChatCompletionsText(data []byte) (string, error) {
	var response struct {
		Choices []struct {
			Message struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return "", fmt.Errorf("decode direct video understanding response: %w", err)
	}
	if len(response.Choices) == 0 || len(response.Choices[0].Message.Content) == 0 {
		return "", fmt.Errorf("direct video understanding response is missing choices text")
	}
	return rawTextContent(response.Choices[0].Message.Content)
}

func extractResponsesText(data []byte) (string, error) {
	var response struct {
		OutputText string `json:"output_text,omitempty"`
		Output     []struct {
			Content []struct {
				Type string `json:"type,omitempty"`
				Text string `json:"text,omitempty"`
			} `json:"content,omitempty"`
		} `json:"output,omitempty"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return "", fmt.Errorf("decode direct video understanding response: %w", err)
	}
	if strings.TrimSpace(response.OutputText) != "" {
		return response.OutputText, nil
	}
	var parts []string
	for _, output := range response.Output {
		for _, content := range output.Content {
			if strings.TrimSpace(content.Text) != "" {
				parts = append(parts, content.Text)
			}
		}
	}
	if len(parts) == 0 {
		return "", fmt.Errorf("direct video understanding response is missing output text")
	}
	return strings.Join(parts, "\n"), nil
}

func rawTextContent(data json.RawMessage) (string, error) {
	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		return text, nil
	}
	var parts []struct {
		Type string `json:"type,omitempty"`
		Text string `json:"text,omitempty"`
	}
	if err := json.Unmarshal(data, &parts); err != nil {
		return "", fmt.Errorf("decode model text content: %w", err)
	}
	items := make([]string, 0, len(parts))
	for _, part := range parts {
		if strings.TrimSpace(part.Text) != "" {
			items = append(items, part.Text)
		}
	}
	if len(items) == 0 {
		return "", fmt.Errorf("model text content is empty")
	}
	return strings.Join(items, "\n"), nil
}

func extractJSONObject(data []byte) ([]byte, error) {
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

var _ understanding.VideoUnderstandingProvider = OpenAICompatibleVideoUnderstandingProvider{}
