package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"

	"github.com/framemio/video-intelligence-worker/internal/domain"
	"github.com/framemio/video-intelligence-worker/internal/transcription"
	"github.com/framemio/video-intelligence-worker/internal/understanding"
)

type HTTPTranscriptionProvider struct {
	URL          string
	APIKey       string
	ProviderName string
	ModelName    string
	Client       *http.Client
}

func (p HTTPTranscriptionProvider) Transcribe(ctx context.Context, request transcription.ProviderRequest) (transcription.ProviderResult, error) {
	type requestEnvelope struct {
		Model        string                       `json:"model"`
		MediaID      string                       `json:"media_id"`
		Reference    transcription.MediaReference `json:"reference"`
		LanguageHint string                       `json:"language_hint,omitempty"`
	}
	type responseSegment struct {
		Start      float64 `json:"start"`
		End        float64 `json:"end"`
		Text       string  `json:"text"`
		Confidence float64 `json:"confidence,omitempty"`
	}
	type responseDocument struct {
		Language string            `json:"language,omitempty"`
		Text     string            `json:"text,omitempty"`
		Segments []responseSegment `json:"segments"`
	}
	raw, err := postJSONBytes(ctx, p.Client, p.URL, p.APIKey, requestEnvelope{
		Model: p.ModelName, MediaID: request.MediaID, Reference: request.Reference, LanguageHint: request.LanguageHint,
	}, 32<<20)
	if err != nil {
		return transcription.ProviderResult{}, fmt.Errorf("transcription request: %w", err)
	}
	var response responseDocument
	if err := decodeStrictJSON(raw, &response); err != nil {
		return transcription.ProviderResult{}, fmt.Errorf("decode transcription response: %w", err)
	}
	segments := make([]domain.TranscriptSegment, 0, len(response.Segments))
	for _, segment := range response.Segments {
		segments = append(segments, domain.TranscriptSegment{
			StartMicros: secondsToMicros(segment.Start), EndMicros: secondsToMicros(segment.End),
			Text: segment.Text, Confidence: segment.Confidence,
		})
	}
	return transcription.ProviderResult{
		Transcript:  domain.Transcript{Language: response.Language, Text: response.Text, Segments: segments},
		RawArtifact: append(json.RawMessage(nil), raw...),
	}, nil
}

func (p HTTPTranscriptionProvider) Name() string  { return p.ProviderName }
func (p HTTPTranscriptionProvider) Model() string { return p.ModelName }

type HTTPVideoUnderstandingProvider struct {
	URL          string
	APIKey       string
	ProviderName string
	ModelName    string
	Client       *http.Client
}

func (p HTTPVideoUnderstandingProvider) Understand(ctx context.Context, input domain.VideoUnderstandingInputV1) (domain.VideoUnderstandingResponseV1, error) {
	type requestEnvelope struct {
		Model          string                           `json:"model"`
		ResponseSchema string                           `json:"response_schema"`
		Input          domain.VideoUnderstandingInputV1 `json:"input"`
	}
	request := requestEnvelope{Model: p.ModelName, ResponseSchema: domain.VideoAnalysisSchemaV1, Input: input}
	raw, err := postJSONBytes(ctx, p.Client, p.URL, p.APIKey, request, 10<<20)
	if err != nil {
		return domain.VideoUnderstandingResponseV1{}, fmt.Errorf("video understanding request: %w", err)
	}
	response, err := understanding.DecodeResponseV1(raw)
	if err != nil {
		return domain.VideoUnderstandingResponseV1{}, err
	}
	return response, nil
}

func (p HTTPVideoUnderstandingProvider) Name() string  { return p.ProviderName }
func (p HTTPVideoUnderstandingProvider) Model() string { return p.ModelName }

var _ understanding.VideoUnderstandingProvider = HTTPVideoUnderstandingProvider{}

func decodeStrictJSON(data []byte, output any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(output); err != nil {
		return fmt.Errorf("decode provider response: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("decode provider response: multiple JSON values")
		}
		return fmt.Errorf("decode provider response trailing data: %w", err)
	}
	return nil
}

func postJSONBytes(ctx context.Context, client *http.Client, endpoint, apiKey string, input any, limit int64) ([]byte, error) {
	if client == nil {
		client = http.DefaultClient
	}
	data, err := json.Marshal(input)
	if err != nil {
		return nil, err
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
	data, err = io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("provider response exceeds %d bytes", limit)
	}
	return data, nil
}

func secondsToMicros(value float64) int64 {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return -1
	}
	return int64(math.Round(value * 1_000_000))
}
