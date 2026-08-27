package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/framemio/video-intelligence-worker/internal/domain"
	"github.com/framemio/video-intelligence-worker/internal/transcription"
)

type OpenAICompatibleTranscriptionProvider struct {
	BaseURL      string
	APIKey       string
	ProviderName string
	ModelName    string
	Client       *http.Client
}

func (p OpenAICompatibleTranscriptionProvider) Transcribe(ctx context.Context, request transcription.ProviderRequest) (transcription.ProviderResult, error) {
	if strings.TrimSpace(request.Reference.LocalPath) == "" {
		return transcription.ProviderResult{}, domain.Permanent(fmt.Errorf("direct transcription requires a local audio path"))
	}
	body, contentType, err := p.multipartBody(request.Reference.LocalPath)
	if err != nil {
		return transcription.ProviderResult{}, err
	}
	raw, err := postMultipartBytes(ctx, p.Client, endpointFor(p.BaseURL, "audio/transcriptions"), p.APIKey, contentType, body, 32<<20)
	if err != nil {
		return transcription.ProviderResult{}, fmt.Errorf("direct transcription request: %w", err)
	}
	result, err := decodeOpenAITranscription(raw)
	if err != nil {
		return transcription.ProviderResult{}, err
	}
	return transcription.ProviderResult{Transcript: result, RawArtifact: append(json.RawMessage(nil), raw...)}, nil
}

func (p OpenAICompatibleTranscriptionProvider) Name() string {
	if strings.TrimSpace(p.ProviderName) != "" {
		return p.ProviderName
	}
	return "openai-direct"
}

func (p OpenAICompatibleTranscriptionProvider) Model() string { return p.ModelName }

func (p OpenAICompatibleTranscriptionProvider) multipartBody(audioPath string) (io.Reader, string, error) {
	file, err := os.Open(audioPath)
	if err != nil {
		return nil, "", err
	}
	reader, writerPipe := io.Pipe()
	writer := multipart.NewWriter(writerPipe)
	contentType := writer.FormDataContentType()
	go func() {
		defer file.Close()
		defer writerPipe.Close()
		if err := writer.WriteField("model", p.ModelName); err != nil {
			_ = writerPipe.CloseWithError(err)
			return
		}
		if err := writer.WriteField("response_format", "verbose_json"); err != nil {
			_ = writerPipe.CloseWithError(err)
			return
		}
		if err := writer.WriteField("timestamp_granularities[]", "segment"); err != nil {
			_ = writerPipe.CloseWithError(err)
			return
		}
		part, err := writer.CreateFormFile("file", filepath.Base(audioPath))
		if err != nil {
			_ = writerPipe.CloseWithError(err)
			return
		}
		if _, err := io.Copy(part, file); err != nil {
			_ = writerPipe.CloseWithError(err)
			return
		}
		if err := writer.Close(); err != nil {
			_ = writerPipe.CloseWithError(err)
		}
	}()
	return reader, contentType, nil
}

func postMultipartBytes(ctx context.Context, client *http.Client, endpoint, apiKey, contentType string, body io.Reader, limit int64) ([]byte, error) {
	if client == nil {
		client = http.DefaultClient
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, body)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", contentType)
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
		responseBody, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		err := fmt.Errorf("provider returned %d: %s", response.StatusCode, strings.TrimSpace(string(responseBody)))
		if response.StatusCode != http.StatusTooManyRequests && response.StatusCode >= 400 && response.StatusCode < 500 {
			return nil, domain.Permanent(err)
		}
		return nil, err
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("provider response exceeds %d bytes", limit)
	}
	return data, nil
}

func decodeOpenAITranscription(raw []byte) (domain.Transcript, error) {
	var response struct {
		Language string `json:"language,omitempty"`
		Text     string `json:"text,omitempty"`
		Segments []struct {
			Start      float64 `json:"start"`
			End        float64 `json:"end"`
			Text       string  `json:"text"`
			Confidence float64 `json:"confidence,omitempty"`
		} `json:"segments,omitempty"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		return domain.Transcript{}, fmt.Errorf("decode direct transcription response: %w", err)
	}
	segments := make([]domain.TranscriptSegment, 0, len(response.Segments))
	for _, segment := range response.Segments {
		segments = append(segments, domain.TranscriptSegment{
			StartMicros: secondsToMicros(segment.Start), EndMicros: secondsToMicros(segment.End),
			Text: segment.Text, Confidence: segment.Confidence,
		})
	}
	return domain.Transcript{Language: response.Language, Text: response.Text, Segments: segments}, nil
}

var _ transcription.TranscriptionProvider = OpenAICompatibleTranscriptionProvider{}
