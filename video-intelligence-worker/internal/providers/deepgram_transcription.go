package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/framemio/video-intelligence-worker/internal/domain"
	"github.com/framemio/video-intelligence-worker/internal/transcription"
)

const deepgramDefaultBaseURL = "https://api.deepgram.com"

type DeepgramTranscriptionProvider struct {
	BaseURL      string
	APIKey       string
	ProviderName string
	ModelName    string
	Client       *http.Client
}

func (p DeepgramTranscriptionProvider) Transcribe(ctx context.Context, request transcription.ProviderRequest) (transcription.ProviderResult, error) {
	if strings.TrimSpace(request.Reference.LocalPath) == "" {
		return transcription.ProviderResult{}, domain.Permanent(fmt.Errorf("deepgram transcription requires a local audio path"))
	}
	file, err := os.Open(request.Reference.LocalPath)
	if err != nil {
		return transcription.ProviderResult{}, err
	}
	defer file.Close()
	raw, err := p.postAudio(ctx, file, contentTypeForPath(request.Reference.LocalPath))
	if err != nil {
		return transcription.ProviderResult{}, fmt.Errorf("deepgram transcription request: %w", err)
	}
	transcript, err := decodeDeepgramTranscription(raw)
	if err != nil {
		return transcription.ProviderResult{}, err
	}
	return transcription.ProviderResult{Transcript: transcript, RawArtifact: append(json.RawMessage(nil), raw...)}, nil
}

func (p DeepgramTranscriptionProvider) Name() string {
	if strings.TrimSpace(p.ProviderName) != "" {
		return p.ProviderName
	}
	return "deepgram"
}

func (p DeepgramTranscriptionProvider) Model() string {
	if strings.TrimSpace(p.ModelName) != "" {
		return p.ModelName
	}
	return "nova-3"
}

func (p DeepgramTranscriptionProvider) postAudio(ctx context.Context, body io.Reader, contentType string) ([]byte, error) {
	client := p.Client
	if client == nil {
		client = http.DefaultClient
	}
	endpoint, err := url.Parse(strings.TrimRight(valueOrDefault(p.BaseURL, deepgramDefaultBaseURL), "/") + "/v1/listen")
	if err != nil {
		return nil, err
	}
	query := endpoint.Query()
	query.Set("model", p.Model())
	query.Set("smart_format", "true")
	query.Set("utterances", "true")
	query.Set("detect_language", "true")
	endpoint.RawQuery = query.Encode()
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), body)
	if err != nil {
		return nil, err
	}
	httpRequest.Header.Set("Accept", "application/json")
	httpRequest.Header.Set("Content-Type", contentType)
	if strings.TrimSpace(p.APIKey) != "" {
		httpRequest.Header.Set("Authorization", "Token "+p.APIKey)
	}
	response, err := client.Do(httpRequest)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		responseBody, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		err := fmt.Errorf("provider returned %d: %s", response.StatusCode, strings.TrimSpace(string(responseBody)))
		if response.StatusCode != http.StatusTooManyRequests && response.StatusCode >= 400 && response.StatusCode < 500 {
			return nil, domain.Permanent(err)
		}
		return nil, err
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 32<<20+1))
	if err != nil {
		return nil, err
	}
	if len(data) > 32<<20 {
		return nil, fmt.Errorf("provider response exceeds %d bytes", 32<<20)
	}
	return data, nil
}

type deepgramResponse struct {
	Results deepgramResults `json:"results"`
}

type deepgramResults struct {
	Utterances []deepgramUtterance `json:"utterances,omitempty"`
	Channels   []deepgramChannel   `json:"channels,omitempty"`
}

type deepgramUtterance struct {
	Start      float64 `json:"start"`
	End        float64 `json:"end"`
	Transcript string  `json:"transcript"`
	Confidence float64 `json:"confidence,omitempty"`
}

type deepgramChannel struct {
	DetectedLanguage string                `json:"detected_language,omitempty"`
	Alternatives     []deepgramAlternative `json:"alternatives,omitempty"`
}

type deepgramAlternative struct {
	Transcript string         `json:"transcript,omitempty"`
	Languages  []string       `json:"languages,omitempty"`
	Words      []deepgramWord `json:"words,omitempty"`
}

type deepgramWord struct {
	Start          float64 `json:"start"`
	End            float64 `json:"end"`
	Word           string  `json:"word,omitempty"`
	PunctuatedWord string  `json:"punctuated_word,omitempty"`
	Confidence     float64 `json:"confidence,omitempty"`
}

func decodeDeepgramTranscription(raw []byte) (domain.Transcript, error) {
	var response deepgramResponse
	if err := json.Unmarshal(raw, &response); err != nil {
		return domain.Transcript{}, fmt.Errorf("decode deepgram transcription response: %w", err)
	}
	language := deepgramLanguage(response.Results.Channels)
	segments := make([]domain.TranscriptSegment, 0, len(response.Results.Utterances))
	for _, utterance := range response.Results.Utterances {
		text := strings.TrimSpace(utterance.Transcript)
		if text == "" {
			continue
		}
		segments = append(segments, domain.TranscriptSegment{StartMicros: secondsToMicros(utterance.Start), EndMicros: secondsToMicros(utterance.End), Text: text, Confidence: utterance.Confidence})
	}
	fullText := deepgramFullText(response.Results.Channels)
	if len(segments) == 0 {
		segments = deepgramWordFallback(response.Results.Channels)
	}
	return domain.Transcript{Language: language, Text: fullText, Segments: segments}, nil
}

func deepgramLanguage(channels []deepgramChannel) string {
	for _, channel := range channels {
		if language := strings.TrimSpace(channel.DetectedLanguage); language != "" {
			return language
		}
		for _, alternative := range channel.Alternatives {
			if len(alternative.Languages) > 0 && strings.TrimSpace(alternative.Languages[0]) != "" {
				return strings.TrimSpace(alternative.Languages[0])
			}
		}
	}
	return ""
}

func deepgramFullText(channels []deepgramChannel) string {
	for _, channel := range channels {
		for _, alternative := range channel.Alternatives {
			if text := strings.TrimSpace(alternative.Transcript); text != "" {
				return text
			}
		}
	}
	return ""
}

func deepgramWordFallback(channels []deepgramChannel) []domain.TranscriptSegment {
	for _, channel := range channels {
		for _, alternative := range channel.Alternatives {
			text := strings.TrimSpace(alternative.Transcript)
			if text == "" || len(alternative.Words) == 0 {
				continue
			}
			return []domain.TranscriptSegment{{
				StartMicros: secondsToMicros(alternative.Words[0].Start),
				EndMicros:   secondsToMicros(alternative.Words[len(alternative.Words)-1].End),
				Text:        text,
				Confidence:  alternative.Words[0].Confidence,
			}}
		}
	}
	return nil
}

func contentTypeForPath(path string) string {
	if contentType := mime.TypeByExtension(strings.ToLower(filepath.Ext(path))); contentType != "" {
		return contentType
	}
	return "application/octet-stream"
}

func valueOrDefault(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	return fallback
}

var _ transcription.TranscriptionProvider = DeepgramTranscriptionProvider{}
