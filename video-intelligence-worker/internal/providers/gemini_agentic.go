package providers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/framemio/video-intelligence-worker/internal/domain"
	"github.com/framemio/video-intelligence-worker/internal/understanding"
)

const geminiAgenticVideoPrompt = `You are FrameMio's native video understanding model. Analyze the original video with Gemini's agentic video tools and return only one strict JSON object matching the FrameMio video-analysis.v1 schema.

Requirements:
- Return exactly one candidate for every supplied segment.
- Copy each segment_id, scene_id, start_micros, and end_micros exactly from the supplied evidence.
- Use only supplied segment IDs, scene IDs, frame IDs, and transcript segment indexes.
- Ground visual claims with frame_ids. The supplied frame IDs are deterministic evidence points near each segment; choose the nearest supplied frame ID for visual observations.
- Ground speech and meaningful audio claims with transcript_segment_indexes.
- Never invent timestamps, IDs, people, actions, expressions, objects, locations, events, speech, or audio.
- If a segment cannot be supported, use recommended_usage "exclude", keep claims empty, and explain the evidence limitation.
- Keep visual quality separate from memory value, emotional value, importance, and story relevance.
- Prefer warm, concise, personal-memory descriptions over surveillance-style descriptions.
- Return JSON only, with no Markdown fences or commentary.

FrameMio response shape:
{
  "schema_version": "video-analysis.v1",
  "candidates": [
    {
      "segment_id": "existing segment id",
      "scene_id": "existing scene id",
      "start_micros": 0,
      "end_micros": 0,
      "description": "string",
      "importance_score": 0.0,
      "visual_quality_score": 0.0,
      "memory_value_score": 0.0,
      "emotional_value_score": 0.0,
      "story_relevance_score": 0.0,
      "emotional_tone": "string",
      "recommended_usage": "hero|supporting|context|audio_only|exclude",
      "reason": "string",
      "people": [],
      "activities": [],
      "interactions": [],
      "expressions": [],
      "objects": [],
      "food": [],
      "locations": [],
      "scenery": [],
      "events": [],
      "interesting_actions": [],
      "speech": [],
      "meaningful_audio": [],
      "frame_ids": ["existing frame id"],
      "transcript_segment_indexes": [0]
    }
  ]
}`

type GeminiAgenticVideoUnderstandingProvider struct {
	BaseURL         string
	APIKey          string
	ProviderName    string
	ModelName       string
	Client          *http.Client
	PollInterval    time.Duration
	FileWaitTimeout time.Duration
}

func (p GeminiAgenticVideoUnderstandingProvider) Understand(_ context.Context, _ domain.VideoUnderstandingInputV1) (domain.VideoUnderstandingResponseV1, error) {
	return domain.VideoUnderstandingResponseV1{}, domain.Permanent(errors.New("Gemini agentic video provider requires the original video URL"))
}

func (p GeminiAgenticVideoUnderstandingProvider) UnderstandVideo(ctx context.Context, input domain.VideoUnderstandingInputV1, videoURL string) (domain.VideoUnderstandingResponseV1, error) {
	if strings.TrimSpace(videoURL) == "" {
		return domain.VideoUnderstandingResponseV1{}, domain.Permanent(errors.New("Gemini agentic video provider requires a source video URL"))
	}
	if strings.TrimSpace(p.APIKey) == "" {
		return domain.VideoUnderstandingResponseV1{}, domain.Permanent(errors.New("Gemini API key is required"))
	}

	body, size, contentType, closeBody, err := p.downloadVideo(ctx, videoURL)
	if err != nil {
		return domain.VideoUnderstandingResponseV1{}, fmt.Errorf("download source video for Gemini: %w", err)
	}
	defer closeBody()

	file, err := p.uploadVideo(ctx, body, size, contentType)
	if err != nil {
		return domain.VideoUnderstandingResponseV1{}, fmt.Errorf("upload source video to Gemini: %w", err)
	}
	if file.Name == "" || file.URI == "" {
		return domain.VideoUnderstandingResponseV1{}, errors.New("Gemini upload response is missing file name or URI")
	}
	defer func() {
		cleanupContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		p.deleteFile(cleanupContext, file.Name)
	}()

	if err := p.waitForFile(ctx, file); err != nil {
		return domain.VideoUnderstandingResponseV1{}, err
	}

	text, err := p.createInteraction(ctx, input, file.URI)
	if err != nil {
		return domain.VideoUnderstandingResponseV1{}, fmt.Errorf("create Gemini agentic interaction: %w", err)
	}
	responseJSON, err := extractJSONObject([]byte(text))
	if err != nil {
		return domain.VideoUnderstandingResponseV1{}, fmt.Errorf("extract Gemini video JSON: %w", err)
	}
	response, err := understanding.DecodeResponseV1(responseJSON)
	if err != nil {
		return domain.VideoUnderstandingResponseV1{}, err
	}
	return groundMissingFrameReferences(response, input), nil
}

func (p GeminiAgenticVideoUnderstandingProvider) Name() string {
	if strings.TrimSpace(p.ProviderName) != "" {
		return p.ProviderName
	}
	return "gemini-agentic"
}

func (p GeminiAgenticVideoUnderstandingProvider) Model() string {
	if strings.TrimSpace(p.ModelName) != "" {
		return p.ModelName
	}
	return "gemini-3.8-flash"
}

type geminiFile struct {
	Name  string `json:"name"`
	URI   string `json:"uri"`
	State string `json:"state"`
	Error any    `json:"error,omitempty"`
}

func (p GeminiAgenticVideoUnderstandingProvider) downloadVideo(ctx context.Context, videoURL string) (io.Reader, int64, string, func(), error) {
	client := p.client()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, videoURL, nil)
	if err != nil {
		return nil, 0, "", func() {}, err
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, 0, "", func() {}, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		defer response.Body.Close()
		data, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return nil, 0, "", func() {}, fmt.Errorf("source video returned %d: %s", response.StatusCode, strings.TrimSpace(string(data)))
	}
	contentType := strings.TrimSpace(response.Header.Get("Content-Type"))
	if contentType == "" {
		contentType = "video/mp4"
	}
	if semicolon := strings.IndexByte(contentType, ';'); semicolon >= 0 {
		contentType = strings.TrimSpace(contentType[:semicolon])
	}
	size := response.ContentLength
	if size <= 0 {
		response.Body.Close()
		return nil, 0, "", func() {}, errors.New("source video response did not include a positive Content-Length")
	}
	return response.Body, size, contentType, func() { _ = response.Body.Close() }, nil
}

func (p GeminiAgenticVideoUnderstandingProvider) uploadVideo(ctx context.Context, body io.Reader, size int64, contentType string) (geminiFile, error) {
	startPayload := map[string]any{
		"file": map[string]string{
			"displayName": "framemio-source-video",
			"mimeType":    contentType,
		},
	}
	startData, err := json.Marshal(startPayload)
	if err != nil {
		return geminiFile{}, err
	}
	startRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint("upload/v1beta/files"), bytes.NewReader(startData))
	if err != nil {
		return geminiFile{}, err
	}
	startRequest.Header.Set("Content-Type", "application/json")
	startRequest.Header.Set("X-Goog-Upload-Protocol", "resumable")
	startRequest.Header.Set("X-Goog-Upload-Command", "start")
	startRequest.Header.Set("X-Goog-Upload-Header-Content-Length", fmt.Sprintf("%d", size))
	startRequest.Header.Set("X-Goog-Upload-Header-Content-Type", contentType)
	p.setAPIKey(startRequest)
	startResponse, err := p.client().Do(startRequest)
	if err != nil {
		return geminiFile{}, err
	}
	defer startResponse.Body.Close()
	if startResponse.StatusCode < 200 || startResponse.StatusCode >= 300 {
		return geminiFile{}, readGeminiHTTPError(startResponse)
	}
	uploadURL := strings.TrimSpace(startResponse.Header.Get("X-Goog-Upload-URL"))
	if uploadURL == "" {
		return geminiFile{}, errors.New("Gemini upload response is missing X-Goog-Upload-URL")
	}

	uploadRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, uploadURL, body)
	if err != nil {
		return geminiFile{}, err
	}
	uploadRequest.ContentLength = size
	uploadRequest.Header.Set("Content-Type", contentType)
	uploadRequest.Header.Set("Content-Length", fmt.Sprintf("%d", size))
	uploadRequest.Header.Set("X-Goog-Upload-Offset", "0")
	uploadRequest.Header.Set("X-Goog-Upload-Command", "upload, finalize")
	p.setAPIKey(uploadRequest)
	uploadResponse, err := p.client().Do(uploadRequest)
	if err != nil {
		return geminiFile{}, err
	}
	defer uploadResponse.Body.Close()
	if uploadResponse.StatusCode < 200 || uploadResponse.StatusCode >= 300 {
		return geminiFile{}, readGeminiHTTPError(uploadResponse)
	}
	var envelope struct {
		File geminiFile `json:"file"`
	}
	if err := json.NewDecoder(io.LimitReader(uploadResponse.Body, 1<<20)).Decode(&envelope); err != nil {
		return geminiFile{}, fmt.Errorf("decode Gemini upload response: %w", err)
	}
	return envelope.File, nil
}

func (p GeminiAgenticVideoUnderstandingProvider) waitForFile(ctx context.Context, file geminiFile) error {
	interval := p.PollInterval
	if interval <= 0 {
		interval = 2 * time.Second
	}
	timeout := p.FileWaitTimeout
	if timeout <= 0 {
		timeout = 15 * time.Minute
	}
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		switch strings.ToUpper(strings.TrimSpace(file.State)) {
		case "ACTIVE":
			return nil
		case "FAILED", "ERROR":
			return fmt.Errorf("Gemini file processing failed: %s", formatGeminiFileError(file.Error))
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("Gemini file processing timed out after %s", timeout)
		case <-ticker.C:
			next, err := p.getFile(ctx, file.Name)
			if err != nil {
				return fmt.Errorf("poll Gemini file %s: %w", file.Name, err)
			}
			file = next
		}
	}
}

func (p GeminiAgenticVideoUnderstandingProvider) getFile(ctx context.Context, name string) (geminiFile, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, p.endpoint(name), nil)
	if err != nil {
		return geminiFile{}, err
	}
	p.setAPIKey(request)
	response, err := p.client().Do(request)
	if err != nil {
		return geminiFile{}, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return geminiFile{}, readGeminiHTTPError(response)
	}
	var file geminiFile
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&file); err != nil {
		return geminiFile{}, fmt.Errorf("decode Gemini file status: %w", err)
	}
	return file, nil
}

func (p GeminiAgenticVideoUnderstandingProvider) createInteraction(ctx context.Context, input domain.VideoUnderstandingInputV1, fileURI string) (string, error) {
	evidence := input
	for index := range evidence.Frames {
		evidence.Frames[index].URL = ""
	}
	evidenceJSON, err := json.Marshal(evidence)
	if err != nil {
		return "", err
	}
	prompt := strings.Join([]string{
		geminiAgenticVideoPrompt,
		"FrameMio evidence JSON:",
		string(evidenceJSON),
	}, "\n\n")
	payload := map[string]any{
		"model": p.Model(),
		"input": []any{
			map[string]any{"type": "video", "uri": fileURI, "processing": "agentic"},
			map[string]any{"type": "text", "text": prompt},
		},
		"stream": true,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint("interactions"), bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "text/event-stream, application/json")
	p.setAPIKey(request)
	response, err := p.client().Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", readGeminiHTTPError(response)
	}
	contentType := strings.ToLower(response.Header.Get("Content-Type"))
	if strings.Contains(contentType, "text/event-stream") {
		return readGeminiSSE(response.Body)
	}
	data, err = io.ReadAll(io.LimitReader(response.Body, 32<<20))
	if err != nil {
		return "", err
	}
	return extractGeminiInteractionText(data)
}

func (p GeminiAgenticVideoUnderstandingProvider) deleteFile(ctx context.Context, name string) {
	if strings.TrimSpace(name) == "" {
		return
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodDelete, p.endpoint(name), nil)
	if err != nil {
		return
	}
	p.setAPIKey(request)
	response, err := p.client().Do(request)
	if err == nil {
		response.Body.Close()
	}
}

func (p GeminiAgenticVideoUnderstandingProvider) endpoint(resource string) string {
	base := strings.TrimRight(strings.TrimSpace(p.BaseURL), "/")
	if base == "" {
		base = "https://generativelanguage.googleapis.com"
	}
	resource = strings.TrimLeft(resource, "/")
	if strings.HasSuffix(base, "/v1beta") {
		if strings.HasPrefix(resource, "upload/") {
			return strings.TrimSuffix(base, "/v1beta") + "/" + resource
		}
		return base + "/" + resource
	}
	if strings.HasPrefix(resource, "upload/") {
		return base + "/" + resource
	}
	return base + "/v1beta/" + resource
}

func (p GeminiAgenticVideoUnderstandingProvider) setAPIKey(request *http.Request) {
	request.Header.Set("x-goog-api-key", p.APIKey)
}

func (p GeminiAgenticVideoUnderstandingProvider) client() *http.Client {
	if p.Client != nil {
		return p.Client
	}
	return http.DefaultClient
}

func readGeminiHTTPError(response *http.Response) error {
	data, _ := io.ReadAll(io.LimitReader(response.Body, 8192))
	return fmt.Errorf("Gemini returned %d: %s", response.StatusCode, strings.TrimSpace(string(data)))
}

func formatGeminiFileError(value any) string {
	if value == nil {
		return "unknown error"
	}
	data, err := json.Marshal(value)
	if err != nil {
		return "unknown error"
	}
	return string(data)
}

func readGeminiSSE(body io.Reader) (string, error) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 64<<10), 8<<20)
	var text strings.Builder
	var completedText string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := bytes.TrimSpace([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))))
		if bytes.Equal(data, []byte("[DONE]")) {
			break
		}
		delta, completed, err := extractGeminiStreamText(data)
		if err != nil {
			return "", err
		}
		if delta != "" {
			text.WriteString(delta)
		}
		if completed != "" {
			completedText = completed
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	if strings.TrimSpace(text.String()) == "" {
		text.WriteString(completedText)
	}
	if strings.TrimSpace(text.String()) == "" {
		return "", errors.New("Gemini interaction stream contained no text")
	}
	return text.String(), nil
}

func extractGeminiStreamText(data []byte) (string, string, error) {
	var event map[string]json.RawMessage
	if err := json.Unmarshal(data, &event); err != nil {
		return "", "", fmt.Errorf("decode Gemini interaction event: %w", err)
	}
	var eventType string
	_ = json.Unmarshal(event["type"], &eventType)
	var delta string
	if raw := event["delta"]; len(raw) > 0 {
		delta = rawTextOrNestedText(raw)
	}
	var completed string
	if raw := event["output_text"]; len(raw) > 0 {
		_ = json.Unmarshal(raw, &completed)
	}
	if completed == "" && (strings.Contains(eventType, "completed") || strings.Contains(eventType, "complete")) {
		completed = extractTextFromOutput(event["output"])
	}
	return delta, completed, nil
}

func extractGeminiInteractionText(data []byte) (string, error) {
	var response map[string]json.RawMessage
	if err := json.Unmarshal(data, &response); err != nil {
		return "", fmt.Errorf("decode Gemini interaction response: %w", err)
	}
	var outputText string
	_ = json.Unmarshal(response["output_text"], &outputText)
	if strings.TrimSpace(outputText) != "" {
		return outputText, nil
	}
	text := extractTextFromOutput(response["output"])
	if strings.TrimSpace(text) == "" {
		return "", errors.New("Gemini interaction response is missing output text")
	}
	return text, nil
}

func extractTextFromOutput(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var values []json.RawMessage
	if json.Unmarshal(raw, &values) != nil {
		return rawTextOrNestedText(raw)
	}
	var parts []string
	for _, value := range values {
		if text := rawTextOrNestedText(value); strings.TrimSpace(text) != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n")
}

func rawTextOrNestedText(raw json.RawMessage) string {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil {
		return ""
	}
	for _, key := range []string{"text", "output_text", "delta"} {
		if value, ok := object[key]; ok {
			if text := rawTextOrNestedText(value); strings.TrimSpace(text) != "" {
				return text
			}
		}
	}
	if content, ok := object["content"]; ok {
		return extractTextFromOutput(content)
	}
	return ""
}

func groundMissingFrameReferences(response domain.VideoUnderstandingResponseV1, input domain.VideoUnderstandingInputV1) domain.VideoUnderstandingResponseV1 {
	framesBySegment := make(map[string][]domain.VideoUnderstandingFrameV1)
	framesByID := make(map[string]domain.VideoUnderstandingFrameV1, len(input.Frames))
	for _, frame := range input.Frames {
		framesBySegment[frame.SegmentID] = append(framesBySegment[frame.SegmentID], frame)
		framesByID[frame.ID] = frame
	}
	for index := range response.Candidates {
		candidate := &response.Candidates[index]
		if len(candidate.FrameIDs) != 0 || candidate.RecommendedUsage == domain.RecommendedUsageAudioOnly ||
			candidate.RecommendedUsage == domain.RecommendedUsageExclude {
			valid := make([]string, 0, len(candidate.FrameIDs))
			for _, frameID := range candidate.FrameIDs {
				frame, ok := framesByID[frameID]
				if ok && frame.SegmentID == candidate.SegmentID {
					valid = append(valid, frameID)
				}
			}
			candidate.FrameIDs = valid
		}
		if len(candidate.FrameIDs) != 0 || candidate.RecommendedUsage == domain.RecommendedUsageAudioOnly ||
			candidate.RecommendedUsage == domain.RecommendedUsageExclude {
			continue
		}
		frames := framesBySegment[candidate.SegmentID]
		if len(frames) > 0 {
			target := candidate.StartMicros + (candidate.EndMicros-candidate.StartMicros)/2
			nearest := frames[0]
			nearestDistance := absoluteDifference(nearest.TimestampMicros, target)
			for _, frame := range frames[1:] {
				distance := absoluteDifference(frame.TimestampMicros, target)
				if distance < nearestDistance {
					nearest = frame
					nearestDistance = distance
				}
			}
			candidate.FrameIDs = []string{nearest.ID}
		}
	}
	return response
}

func absoluteDifference(left, right int64) int64 {
	if left < right {
		return right - left
	}
	return left - right
}

var _ understanding.VideoUnderstandingProvider = GeminiAgenticVideoUnderstandingProvider{}
var _ understanding.VideoFileUnderstandingProvider = GeminiAgenticVideoUnderstandingProvider{}
