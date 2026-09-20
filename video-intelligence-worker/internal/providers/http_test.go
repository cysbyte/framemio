package providers

import (
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/framemio/video-intelligence-worker/internal/domain"
	"github.com/framemio/video-intelligence-worker/internal/transcription"
	"github.com/framemio/video-intelligence-worker/internal/understanding"
)

func TestHTTPTranscriptionProviderPreservesSecondTimestamps(t *testing.T) {
	t.Parallel()
	client := &http.Client{Timeout: time.Second, Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodPost || request.Header.Get("Authorization") != "Bearer secret" {
			t.Fatalf("unexpected request: method=%s authorization=%s", request.Method, request.Header.Get("Authorization"))
		}
		var body struct {
			Model     string                       `json:"model"`
			MediaID   string                       `json:"media_id"`
			Reference transcription.MediaReference `json:"reference"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Model != "speech-model" || body.MediaID != "media-1" || body.Reference.URL == "" || body.Reference.DurationMicros != 10_000_000 {
			t.Fatalf("unexpected provider request: %+v", body)
		}
		return jsonResponse(`{"language":"en","segments":[{"start":3.2,"end":6.8,"text":"Let's go over there.","confidence":0.73}]}`), nil
	})}
	provider := HTTPTranscriptionProvider{
		URL: "https://transcription.example.test/v1/transcribe", APIKey: "secret",
		ProviderName: "test-provider", ModelName: "speech-model", Client: client,
	}
	result, err := provider.Transcribe(context.Background(), transcription.ProviderRequest{
		MediaID: "media-1", Reference: transcription.MediaReference{
			URL: "https://media.example.test/audio.flac", MediaType: "audio", DurationMicros: 10_000_000,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Transcript.Segments) != 1 || result.Transcript.Segments[0].StartMicros != 3_200_000 || result.Transcript.Segments[0].EndMicros != 6_800_000 {
		t.Fatalf("timestamps were not preserved: %+v", result.Transcript)
	}
	if len(result.RawArtifact) == 0 || provider.Name() != "test-provider" || provider.Model() != "speech-model" {
		t.Fatal("provider metadata or raw response was not preserved")
	}
}

func TestHTTPTranscriptionProviderSupportsNoSpeech(t *testing.T) {
	t.Parallel()
	provider := HTTPTranscriptionProvider{
		URL: "https://transcription.example.test/v1/transcribe",
		Client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return jsonResponse(`{"segments":[]}`), nil
		})},
	}
	result, err := provider.Transcribe(context.Background(), transcription.ProviderRequest{
		MediaID: "media-1", Reference: transcription.MediaReference{URL: "https://example.test/video.mov", MediaType: "video", DurationMicros: 2_000_000},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Transcript.Segments) != 0 {
		t.Fatalf("no-speech response contains segments: %+v", result.Transcript)
	}
}

func TestHTTPTranscriptionProviderClassifiesPermanentClientErrors(t *testing.T) {
	t.Parallel()
	provider := HTTPTranscriptionProvider{
		URL: "https://transcription.example.test/v1/transcribe",
		Client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return textResponse(http.StatusUnsupportedMediaType, "unsupported audio codec"), nil
		})},
	}
	_, err := provider.Transcribe(context.Background(), transcription.ProviderRequest{
		MediaID: "media-1", Reference: transcription.MediaReference{URL: "https://example.test/audio.flac", MediaType: "audio", DurationMicros: 2_000_000},
	})
	if err == nil || !domain.IsPermanent(err) {
		t.Fatalf("expected permanent provider error, got %v", err)
	}
}

func TestHTTPTranscriptionProviderLeavesRateLimitRetryable(t *testing.T) {
	t.Parallel()
	provider := HTTPTranscriptionProvider{
		URL: "https://transcription.example.test/v1/transcribe",
		Client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return textResponse(http.StatusTooManyRequests, "rate limited"), nil
		})},
	}
	_, err := provider.Transcribe(context.Background(), transcription.ProviderRequest{
		MediaID: "media-1", Reference: transcription.MediaReference{URL: "https://example.test/audio.flac", MediaType: "audio", DurationMicros: 2_000_000},
	})
	if err == nil || domain.IsPermanent(err) {
		t.Fatalf("expected retryable provider error, got %v", err)
	}
}

func TestHTTPVideoUnderstandingProviderSendsStructuredEvidenceAndStrictSchema(t *testing.T) {
	t.Parallel()
	input := providerUnderstandingInput()
	responseBody, err := json.Marshal(providerUnderstandingResponse())
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		var envelope struct {
			Model          string                           `json:"model"`
			ResponseSchema string                           `json:"response_schema"`
			Input          domain.VideoUnderstandingInputV1 `json:"input"`
		}
		if err := json.NewDecoder(request.Body).Decode(&envelope); err != nil {
			t.Fatal(err)
		}
		if envelope.Model != "memory-model" || envelope.ResponseSchema != domain.VideoAnalysisSchemaV1 || envelope.Input.Objective != understanding.Objective {
			t.Fatalf("unexpected understanding envelope: %+v", envelope)
		}
		if len(envelope.Input.Frames) != 1 || envelope.Input.Frames[0].URL == "" || len(envelope.Input.Segments) != 1 || envelope.Input.Metadata.DurationMicros == 0 {
			t.Fatalf("structured evidence is incomplete: %+v", envelope.Input)
		}
		encoded, err := json.Marshal(envelope.Input)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(encoded), "source_s3_key") || strings.Contains(string(encoded), "source.mov") {
			t.Fatalf("original video was sent despite sufficient evidence: %s", encoded)
		}
		return jsonResponse(string(responseBody)), nil
	})}
	provider := HTTPVideoUnderstandingProvider{
		URL: "https://understanding.example.test/v1/analyze", APIKey: "secret",
		ProviderName: "provider-a", ModelName: "memory-model", Client: client,
	}
	response, err := provider.Understand(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if response.SchemaVersion != domain.VideoAnalysisSchemaV1 || len(response.Candidates) != 1 || provider.Name() != "provider-a" || provider.Model() != "memory-model" {
		t.Fatalf("unexpected understanding response: %+v", response)
	}
}

func TestHTTPVideoUnderstandingProviderRejectsUnknownResponseFields(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		body string
	}{
		{name: "unknown field", body: `{"schema_version":"video-analysis.v1","candidates":[],"summary":"unsupported"}`},
		{name: "missing required field", body: `{"schema_version":"video-analysis.v1","candidates":[{"segment_id":"segment-0001","scene_id":"scene-0001","start_micros":0,"end_micros":4000000,"description":"A smile","importance_score":0.8,"visual_quality_score":0.7,"emotional_value_score":0.8,"story_relevance_score":0.8,"recommended_usage":"hero","reason":"Grounded","frame_ids":["frame-0001"]}]}`},
		{name: "trailing JSON", body: `{"schema_version":"video-analysis.v1","candidates":[]} {"extra":true}`},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			provider := HTTPVideoUnderstandingProvider{
				URL: "https://understanding.example.test/v1/analyze", ProviderName: "provider-a", ModelName: "memory-model",
				Client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
					return jsonResponse(test.body), nil
				})},
			}
			if _, err := provider.Understand(context.Background(), providerUnderstandingInput()); err == nil {
				t.Fatal("expected strict JSON decoding failure")
			}
		})
	}
}

func TestOpenAICompatibleTranscriptionProviderSendsMultipartAudio(t *testing.T) {
	t.Parallel()
	audioPath := t.TempDir() + "/speech.flac"
	if err := os.WriteFile(audioPath, []byte("fixture audio"), 0o600); err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodPost || request.URL.String() != "https://api2.65535.space/v1/audio/transcriptions" {
			t.Fatalf("unexpected request target: %s %s", request.Method, request.URL.String())
		}
		if request.Header.Get("Authorization") != "Bearer secret" {
			t.Fatalf("unexpected authorization header: %s", request.Header.Get("Authorization"))
		}
		reader := multipart.NewReader(request.Body, strings.TrimPrefix(request.Header.Get("Content-Type"), "multipart/form-data; boundary="))
		form, err := reader.ReadForm(1024)
		if err != nil {
			t.Fatal(err)
		}
		if got := form.Value["model"]; len(got) != 1 || got[0] != "gpt-5.4-mini-transcribe" {
			t.Fatalf("unexpected model field: %+v", got)
		}
		if got := form.Value["response_format"]; len(got) != 1 || got[0] != "verbose_json" {
			t.Fatalf("unexpected response_format field: %+v", got)
		}
		if len(form.File["file"]) != 1 {
			t.Fatalf("expected audio file part, got %+v", form.File)
		}
		return jsonResponse(`{"language":"en","text":"hello there","segments":[{"start":1.25,"end":2.75,"text":"hello there"}]}`), nil
	})}
	provider := OpenAICompatibleTranscriptionProvider{
		BaseURL: "https://api2.65535.space", APIKey: "secret", ProviderName: "openai-direct", ModelName: "gpt-5.4-mini-transcribe", Client: client,
	}
	result, err := provider.Transcribe(context.Background(), transcription.ProviderRequest{
		MediaID: "media-1", Reference: transcription.MediaReference{LocalPath: audioPath, MediaType: "audio", DurationMicros: 4_000_000},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Transcript.Language != "en" || len(result.Transcript.Segments) != 1 || result.Transcript.Segments[0].StartMicros != 1_250_000 || result.Transcript.Segments[0].EndMicros != 2_750_000 {
		t.Fatalf("unexpected transcript: %+v", result.Transcript)
	}
}

func TestOpenAIDirectPromptRequestsPersonalMemoryLanguage(t *testing.T) {
	t.Parallel()
	if !strings.Contains(directVideoUnderstandingPrompt, "personal memory app") || !strings.Contains(directVideoUnderstandingPrompt, "Avoid detached surveillance-style wording") {
		t.Fatalf("direct video understanding prompt should request memory-centered wording: %s", directVideoUnderstandingPrompt)
	}
}

func TestOpenAICompatibleVideoUnderstandingProviderSendsChatCompletionsRequest(t *testing.T) {
	t.Parallel()
	responseBody, err := json.Marshal(providerUnderstandingResponse())
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodPost || request.URL.String() != "https://api2.65535.space/v1/chat/completions" {
			t.Fatalf("unexpected request target: %s %s", request.Method, request.URL.String())
		}
		if request.Header.Get("Authorization") != "Bearer secret" {
			t.Fatalf("unexpected authorization header: %s", request.Header.Get("Authorization"))
		}
		var envelope struct {
			Model          string `json:"model"`
			MaxTokens      int    `json:"max_tokens"`
			ResponseFormat struct {
				Type string `json:"type"`
			} `json:"response_format"`
			Messages []struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(request.Body).Decode(&envelope); err != nil {
			t.Fatal(err)
		}
		if envelope.Model != "gemini-3.8-flash" || envelope.MaxTokens != 6000 || envelope.ResponseFormat.Type != "json_object" || len(envelope.Messages) != 2 {
			t.Fatalf("unexpected chat completions envelope: %+v", envelope)
		}
		content := string(envelope.Messages[1].Content)
		if !strings.Contains(content, `"type":"image_url"`) || !strings.Contains(content, `"detail":"high"`) || !strings.Contains(content, `https://evidence.example.test/frame.jpg`) {
			t.Fatalf("frame image was not sent as chat image content: %s", content)
		}
		body, err := json.Marshal(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": string(responseBody)}}}})
		if err != nil {
			t.Fatal(err)
		}
		return jsonResponse(string(body)), nil
	})}
	provider := OpenAICompatibleVideoUnderstandingProvider{
		BaseURL: "https://api2.65535.space", APIKey: "secret", ProviderName: "openai-direct",
		ModelName: "gemini-3.8-flash", WireAPI: "chat_completions", ImageDetail: "high", MaxOutputTokens: 6000, Client: client,
	}
	response, err := provider.Understand(context.Background(), providerUnderstandingInput())
	if err != nil {
		t.Fatal(err)
	}
	if response.SchemaVersion != domain.VideoAnalysisSchemaV1 || len(response.Candidates) != 1 || provider.Name() != "openai-direct" || provider.Model() != "gemini-3.8-flash" {
		t.Fatalf("unexpected direct understanding response: %+v", response)
	}
}

func TestOpenAICompatibleVideoUnderstandingProviderSupportsResponsesWireAPI(t *testing.T) {
	t.Parallel()
	responseBody, err := json.Marshal(providerUnderstandingResponse())
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodPost || request.URL.String() != "https://api2.65535.space/v1/responses" {
			t.Fatalf("unexpected request target: %s %s", request.Method, request.URL.String())
		}
		var envelope struct {
			Model           string `json:"model"`
			MaxOutputTokens int    `json:"max_output_tokens"`
			Input           []struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			} `json:"input"`
		}
		if err := json.NewDecoder(request.Body).Decode(&envelope); err != nil {
			t.Fatal(err)
		}
		if envelope.Model != "gpt-5.6-luna" || envelope.MaxOutputTokens != 5000 || len(envelope.Input) != 2 {
			t.Fatalf("unexpected responses envelope: %+v", envelope)
		}
		content := string(envelope.Input[1].Content)
		if !strings.Contains(content, `"type":"input_image"`) || !strings.Contains(content, `"image_url":"https://evidence.example.test/frame.jpg"`) {
			t.Fatalf("frame image was not sent as responses image content: %s", content)
		}
		body, err := json.Marshal(map[string]any{"output_text": "```json\n" + string(responseBody) + "\n```"})
		if err != nil {
			t.Fatal(err)
		}
		return jsonResponse(string(body)), nil
	})}
	provider := OpenAICompatibleVideoUnderstandingProvider{
		BaseURL: "https://api2.65535.space/v1", APIKey: "secret", ProviderName: "openai-direct",
		ModelName: "gpt-5.6-luna", WireAPI: "responses", Client: client,
	}
	response, err := provider.Understand(context.Background(), providerUnderstandingInput())
	if err != nil {
		t.Fatal(err)
	}
	if response.SchemaVersion != domain.VideoAnalysisSchemaV1 || len(response.Candidates) != 1 {
		t.Fatalf("unexpected direct understanding response: %+v", response)
	}
}

func TestGeminiAgenticVideoUnderstandingProviderUploadsPollsAndStreams(t *testing.T) {
	t.Parallel()
	responseBody, err := json.Marshal(providerUnderstandingResponse())
	if err != nil {
		t.Fatal(err)
	}
	var uploadBytes []byte
	pollCount := 0
	deleted := false
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Host != "source.example.test" && request.Header.Get("x-goog-api-key") != "gemini-secret" {
			t.Fatalf("missing Gemini API key header on %s %s", request.Method, request.URL)
		}
		switch {
		case request.Method == http.MethodGet && request.URL.String() == "https://source.example.test/video.mp4":
			return &http.Response{
				StatusCode:    http.StatusOK,
				Header:        http.Header{"Content-Type": []string{"video/mp4"}},
				Body:          io.NopCloser(strings.NewReader("video-bytes")),
				ContentLength: int64(len("video-bytes")),
			}, nil
		case request.Method == http.MethodPost && request.URL.Path == "/upload/v1beta/files":
			if request.Header.Get("X-Goog-Upload-Protocol") != "resumable" || request.Header.Get("X-Goog-Upload-Command") != "start" {
				t.Fatalf("unexpected Gemini upload start headers: %v", request.Header)
			}
			var body struct {
				File map[string]string `json:"file"`
			}
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body.File["displayName"] != "framemio-source-video" || body.File["mimeType"] != "video/mp4" {
				t.Fatalf("unexpected upload metadata: %+v", body.File)
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"X-Goog-Upload-Url": []string{"https://gemini.example.test/upload-session"}},
				Body:       io.NopCloser(strings.NewReader("")),
			}, nil
		case request.Method == http.MethodPost && request.URL.String() == "https://gemini.example.test/upload-session":
			if request.Header.Get("X-Goog-Upload-Command") != "upload, finalize" || request.Header.Get("X-Goog-Upload-Offset") != "0" {
				t.Fatalf("unexpected Gemini upload finalize headers: %v", request.Header)
			}
			uploadBytes, err = io.ReadAll(request.Body)
			if err != nil {
				t.Fatal(err)
			}
			return jsonResponse(`{"file":{"name":"files/frame-video","uri":"https://generativelanguage.googleapis.com/v1beta/files/frame-video","state":"PROCESSING"}}`), nil
		case request.Method == http.MethodGet && request.URL.Path == "/v1beta/files/frame-video":
			pollCount++
			if pollCount == 1 {
				return jsonResponse(`{"name":"files/frame-video","uri":"https://generativelanguage.googleapis.com/v1beta/files/frame-video","state":"PROCESSING"}`), nil
			}
			return jsonResponse(`{"name":"files/frame-video","uri":"https://generativelanguage.googleapis.com/v1beta/files/frame-video","state":"ACTIVE"}`), nil
		case request.Method == http.MethodPost && request.URL.Path == "/v1beta/interactions":
			var body struct {
				Model  string `json:"model"`
				Stream bool   `json:"stream"`
				Input  []struct {
					Type       string `json:"type"`
					URI        string `json:"uri"`
					Processing string `json:"processing"`
					Text       string `json:"text"`
				} `json:"input"`
			}
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body.Model != "gemini-3.8-flash" || !body.Stream || len(body.Input) != 2 ||
				body.Input[0].Type != "video" || body.Input[0].URI == "" || body.Input[0].Processing != "agentic" ||
				body.Input[1].Type != "text" || !strings.Contains(body.Input[1].Text, "video-analysis.v1") ||
				strings.Contains(body.Input[1].Text, "https://evidence.example.test/frame.jpg") {
				t.Fatalf("unexpected Gemini interaction request: %+v", body)
			}
			streamBody := `data: {"type":"interaction.completed","output_text":"` + strings.ReplaceAll(string(responseBody), `"`, `\"`) + `"}` + "\n\n"
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body:       io.NopCloser(strings.NewReader(streamBody)),
			}, nil
		case request.Method == http.MethodDelete && request.URL.Path == "/v1beta/files/frame-video":
			deleted = true
			return jsonResponse(`{}`), nil
		default:
			t.Fatalf("unexpected Gemini request: %s %s", request.Method, request.URL.String())
			return nil, nil
		}
	})}
	provider := GeminiAgenticVideoUnderstandingProvider{
		BaseURL: "https://gemini.example.test", APIKey: "gemini-secret", ProviderName: "gemini-agentic",
		ModelName: "gemini-3.8-flash", Client: client, PollInterval: time.Nanosecond, FileWaitTimeout: time.Second,
	}
	response, err := provider.UnderstandVideo(context.Background(), providerUnderstandingInput(), "https://source.example.test/video.mp4")
	if err != nil {
		t.Fatal(err)
	}
	if string(uploadBytes) != "video-bytes" || pollCount != 2 || !deleted {
		t.Fatalf("upload/poll/delete lifecycle incomplete: bytes=%q polls=%d deleted=%v", uploadBytes, pollCount, deleted)
	}
	if response.SchemaVersion != domain.VideoAnalysisSchemaV1 || len(response.Candidates) != 1 {
		t.Fatalf("unexpected Gemini response: %+v", response)
	}
}

func TestGeminiAgenticVideoUnderstandingProviderRejectsFailedFile(t *testing.T) {
	t.Parallel()
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch {
		case request.Method == http.MethodGet && request.URL.String() == "https://source.example.test/video.mp4":
			return &http.Response{
				StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"video/mp4"}},
				Body: io.NopCloser(strings.NewReader("video-bytes")), ContentLength: int64(len("video-bytes")),
			}, nil
		case request.Method == http.MethodPost && request.URL.Path == "/upload/v1beta/files":
			return &http.Response{
				StatusCode: http.StatusOK, Header: http.Header{"X-Goog-Upload-URL": []string{"https://gemini.example.test/upload-session"}},
				Body: io.NopCloser(strings.NewReader("")),
			}, nil
		case request.Method == http.MethodPost && request.URL.String() == "https://gemini.example.test/upload-session":
			return jsonResponse(`{"file":{"name":"files/failed-video","uri":"https://example.test/files/failed-video","state":"FAILED","error":{"message":"unsupported codec"}}}`), nil
		case request.Method == http.MethodDelete:
			return jsonResponse(`{}`), nil
		default:
			t.Fatalf("unexpected Gemini request: %s %s", request.Method, request.URL.String())
			return nil, nil
		}
	})}
	provider := GeminiAgenticVideoUnderstandingProvider{
		BaseURL: "https://gemini.example.test", APIKey: "gemini-secret", ModelName: "gemini-3.8-flash",
		Client: client, PollInterval: time.Nanosecond, FileWaitTimeout: time.Second,
	}
	if _, err := provider.UnderstandVideo(context.Background(), providerUnderstandingInput(), "https://source.example.test/video.mp4"); err == nil {
		t.Fatal("expected failed Gemini file processing error")
	}
}

func providerUnderstandingInput() domain.VideoUnderstandingInputV1 {
	return domain.VideoUnderstandingInputV1{
		SchemaVersion: domain.VideoUnderstandingInputSchemaV1, InputVersion: "video-evidence-v1",
		AnalysisVersion: "video-understanding-v1", Objective: understanding.Objective,
		GroundingRules: []string{"Do not invent evidence."}, MediaID: "media-1", ProjectID: "project-1",
		Metadata: domain.VideoMetadata{DurationMicros: 4_000_000, Width: 1920, Height: 1080, FPS: 30, VideoCodec: "hevc"},
		Scenes:   []domain.VideoScene{{ID: "scene-0001", StartMicros: 0, EndMicros: 4_000_000}},
		Segments: []domain.CandidateSegment{{ID: "segment-0001", SceneID: "scene-0001", StartMicros: 0, EndMicros: 4_000_000}},
		Frames: []domain.VideoUnderstandingFrameV1{{
			ID: "frame-0001", SceneID: "scene-0001", SegmentID: "segment-0001",
			TimestampMicros: 2_000_000, URL: "https://evidence.example.test/frame.jpg",
		}},
		Transcript: domain.Transcript{}, Quality: domain.QualitySignals{OverallScore: 0.7},
	}
}

func providerUnderstandingResponse() domain.VideoUnderstandingResponseV1 {
	return domain.VideoUnderstandingResponseV1{
		SchemaVersion: domain.VideoAnalysisSchemaV1,
		Candidates: []domain.VideoSegmentAnalysisV1{{
			SegmentID: "segment-0001", SceneID: "scene-0001", StartMicros: 0, EndMicros: 4_000_000,
			Description: "A person smiles toward the camera", ImportanceScore: 0.8,
			VisualQualityScore: 0.7, MemoryValueScore: 0.9, EmotionalValueScore: 0.8, StoryRelevanceScore: 0.85,
			RecommendedUsage: domain.RecommendedUsageHero, Reason: "A grounded personal interaction",
			People: []string{"person"}, Expressions: []string{"smiling"}, FrameIDs: []string{"frame-0001"},
		}},
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func jsonResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func textResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"text/plain"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestDeepgramTranscriptionProviderSendsPrerecordedAudioRequest(t *testing.T) {
	t.Parallel()
	audioFile, err := os.CreateTemp(t.TempDir(), "speech-*.flac")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := audioFile.WriteString("audio-bytes"); err != nil {
		t.Fatal(err)
	}
	if err := audioFile.Close(); err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodPost || request.URL.Path != "/v1/listen" {
			t.Fatalf("unexpected request target: %s %s", request.Method, request.URL.String())
		}
		if request.Header.Get("Authorization") != "Token deepgram-secret" {
			t.Fatalf("unexpected authorization header: %s", request.Header.Get("Authorization"))
		}
		if request.Header.Get("Content-Type") != "audio/flac" {
			t.Fatalf("unexpected content type: %s", request.Header.Get("Content-Type"))
		}
		query := request.URL.Query()
		if query.Get("model") != "nova-3" || query.Get("utterances") != "true" || query.Get("detect_language") != "true" || query.Get("smart_format") != "true" {
			t.Fatalf("unexpected query: %s", request.URL.RawQuery)
		}
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatal(err)
		}
		if string(body) != "audio-bytes" {
			t.Fatalf("unexpected body: %q", string(body))
		}
		return jsonResponse(`{"results":{"channels":[{"detected_language":"en","alternatives":[{"transcript":"hello there"}]}],"utterances":[{"start":1.25,"end":2.75,"transcript":"hello there","confidence":0.97}]}}`), nil
	})}
	provider := DeepgramTranscriptionProvider{BaseURL: "https://api.deepgram.com", APIKey: "deepgram-secret", ProviderName: "deepgram", ModelName: "nova-3", Client: client}
	result, err := provider.Transcribe(context.Background(), transcription.ProviderRequest{
		MediaID: "media-1", Reference: transcription.MediaReference{LocalPath: audioFile.Name(), MediaType: "audio", DurationMicros: 4_000_000},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Transcript.Language != "en" || result.Transcript.Text != "hello there" || len(result.Transcript.Segments) != 1 {
		t.Fatalf("unexpected transcript: %+v", result.Transcript)
	}
	segment := result.Transcript.Segments[0]
	if segment.StartMicros != 1_250_000 || segment.EndMicros != 2_750_000 || segment.Text != "hello there" || segment.Confidence != 0.97 {
		t.Fatalf("unexpected segment: %+v", segment)
	}
}

func TestDeepgramTranscriptionProviderFallsBackToWordTiming(t *testing.T) {
	t.Parallel()
	transcript, err := decodeDeepgramTranscription([]byte(`{"results":{"channels":[{"alternatives":[{"transcript":"hello world","languages":["ja"],"words":[{"start":0.5,"end":0.8,"word":"hello","confidence":0.8},{"start":0.9,"end":1.4,"word":"world","confidence":0.9}]}]}]}}`))
	if err != nil {
		t.Fatal(err)
	}
	if transcript.Language != "ja" || transcript.Text != "hello world" || len(transcript.Segments) != 1 {
		t.Fatalf("unexpected transcript: %+v", transcript)
	}
	if transcript.Segments[0].StartMicros != 500_000 || transcript.Segments[0].EndMicros != 1_400_000 {
		t.Fatalf("unexpected fallback timing: %+v", transcript.Segments[0])
	}
}

func TestDeepgramTranscriptionProviderSupportsNoSpeech(t *testing.T) {
	t.Parallel()
	transcript, err := decodeDeepgramTranscription([]byte(`{"results":{"channels":[{"detected_language":"zh","alternatives":[{"transcript":""}]}],"utterances":[]}}`))
	if err != nil {
		t.Fatal(err)
	}
	if transcript.Language != "zh" || transcript.Text != "" || len(transcript.Segments) != 0 {
		t.Fatalf("expected empty transcript with language, got %+v", transcript)
	}
}
