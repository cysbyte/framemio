package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const maxPromptAudioBytes int64 = 25 * 1024 * 1024

type PromptTranscriber interface {
	TranscribePrompt(ctx context.Context, audio []byte, contentType string) (string, error)
}

type PromptTranscriptionTokenGranter interface {
	GrantPromptToken(ctx context.Context, ttlSeconds int) (PromptTranscriptionToken, error)
}

type PromptTranscriptionToken struct {
	AccessToken  string `json:"accessToken"`
	ExpiresIn    int    `json:"expiresIn"`
	WebsocketURL string `json:"websocketUrl"`
	Model        string `json:"model"`
	Language     string `json:"language,omitempty"`
}

type deepgramGrantError struct {
	StatusCode int
	Body       string
}

func (e deepgramGrantError) Error() string {
	body := strings.TrimSpace(e.Body)
	if body == "" {
		return fmt.Sprintf("deepgram auth grant returned status %d", e.StatusCode)
	}
	return fmt.Sprintf("deepgram auth grant returned status %d: %s", e.StatusCode, body)
}

type DeepgramPromptTranscriber struct {
	APIKey        string
	Endpoint      string
	GrantEndpoint string
	WebsocketURL  string
	Model         string
	Language      string
	Client        *http.Client
}

func NewDeepgramPromptTranscriberFromEnv() PromptTranscriber {
	apiKey := strings.TrimSpace(os.Getenv("DEEPGRAM_API_KEY"))
	if apiKey == "" {
		return nil
	}
	endpoint := strings.TrimSpace(os.Getenv("DEEPGRAM_LISTEN_ENDPOINT"))
	if endpoint == "" {
		endpoint = "https://api.deepgram.com/v1/listen"
	}
	grantEndpoint := strings.TrimSpace(os.Getenv("DEEPGRAM_AUTH_GRANT_ENDPOINT"))
	if grantEndpoint == "" {
		grantEndpoint = "https://api.deepgram.com/v1/auth/grant"
	}
	websocketURL := strings.TrimSpace(os.Getenv("DEEPGRAM_LISTEN_WEBSOCKET_URL"))
	if websocketURL == "" {
		websocketURL = "wss://api.deepgram.com/v1/listen"
	}
	model := strings.TrimSpace(os.Getenv("DEEPGRAM_MODEL"))
	if model == "" {
		model = "nova-3"
	}
	language := strings.TrimSpace(os.Getenv("DEEPGRAM_LANGUAGE"))
	return DeepgramPromptTranscriber{
		APIKey:        apiKey,
		Endpoint:      endpoint,
		GrantEndpoint: grantEndpoint,
		WebsocketURL:  websocketURL,
		Model:         model,
		Language:      language,
		Client:        &http.Client{Timeout: 30 * time.Second},
	}
}

func (t DeepgramPromptTranscriber) GrantPromptToken(ctx context.Context, ttlSeconds int) (PromptTranscriptionToken, error) {
	if strings.TrimSpace(t.APIKey) == "" {
		return PromptTranscriptionToken{}, errors.New("deepgram api key is not configured")
	}
	if ttlSeconds <= 0 {
		ttlSeconds = 60
	}
	if ttlSeconds > 3600 {
		ttlSeconds = 3600
	}
	endpoint := strings.TrimSpace(t.GrantEndpoint)
	if endpoint == "" {
		endpoint = "https://api.deepgram.com/v1/auth/grant"
	}
	body, err := json.Marshal(map[string]int{"ttl_seconds": ttlSeconds})
	if err != nil {
		return PromptTranscriptionToken{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return PromptTranscriptionToken{}, err
	}
	request.Header.Set("Authorization", "Token "+strings.TrimSpace(t.APIKey))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")

	client := t.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	response, err := client.Do(request)
	if err != nil {
		return PromptTranscriptionToken{}, err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return PromptTranscriptionToken{}, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return PromptTranscriptionToken{}, deepgramGrantError{StatusCode: response.StatusCode, Body: string(data)}
	}
	var output deepgramAuthGrantResponse
	if err := json.Unmarshal(data, &output); err != nil {
		return PromptTranscriptionToken{}, err
	}
	accessToken := strings.TrimSpace(output.AccessToken)
	if accessToken == "" {
		return PromptTranscriptionToken{}, errors.New("deepgram returned an empty access token")
	}
	websocketURL := strings.TrimSpace(t.WebsocketURL)
	if websocketURL == "" {
		websocketURL = "wss://api.deepgram.com/v1/listen"
	}
	model := strings.TrimSpace(t.Model)
	if model == "" {
		model = "nova-3"
	}
	return PromptTranscriptionToken{
		AccessToken:  accessToken,
		ExpiresIn:    ttlSeconds,
		WebsocketURL: websocketURL,
		Model:        model,
		Language:     strings.TrimSpace(t.Language),
	}, nil
}

func (t DeepgramPromptTranscriber) TranscribePrompt(ctx context.Context, audio []byte, contentType string) (string, error) {
	if strings.TrimSpace(t.APIKey) == "" {
		return "", errors.New("deepgram api key is not configured")
	}
	if len(audio) == 0 {
		return "", errors.New("audio is empty")
	}
	endpoint := strings.TrimSpace(t.Endpoint)
	if endpoint == "" {
		endpoint = "https://api.deepgram.com/v1/listen"
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return "", fmt.Errorf("parse deepgram endpoint: %w", err)
	}
	query := parsed.Query()
	if model := strings.TrimSpace(t.Model); model != "" {
		query.Set("model", model)
	}
	if language := strings.TrimSpace(t.Language); language != "" {
		query.Set("language", language)
	}
	query.Set("smart_format", "true")
	parsed.RawQuery = query.Encode()

	client := t.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, parsed.String(), bytes.NewReader(audio))
	if err != nil {
		return "", err
	}
	request.Header.Set("Authorization", "Token "+strings.TrimSpace(t.APIKey))
	request.Header.Set("Content-Type", contentTypeOrDefault(contentType))
	request.Header.Set("Accept", "application/json")

	response, err := client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("deepgram returned status %d", response.StatusCode)
	}
	var output deepgramListenResponse
	if err := json.Unmarshal(data, &output); err != nil {
		return "", err
	}
	transcript := strings.TrimSpace(output.bestTranscript())
	if transcript == "" {
		return "", errors.New("deepgram returned an empty transcript")
	}
	return transcript, nil
}

func (s *Server) grantPromptTranscriptionToken(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		s.writeError(response, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	granter, ok := s.PromptTranscriber.(PromptTranscriptionTokenGranter)
	if !ok || granter == nil {
		s.writeError(response, http.StatusServiceUnavailable, "transcription_not_configured", "voice prompt transcription is not configured")
		return
	}
	token, err := granter.GrantPromptToken(request.Context(), 60)
	if err != nil {
		s.Logger.Warn("voice prompt token grant failed", "user_id", authenticatedUserIDForLog(request), "error", err)
		var grantErr deepgramGrantError
		if errors.As(err, &grantErr) && grantErr.StatusCode == http.StatusForbidden {
			s.writeError(response, http.StatusServiceUnavailable, "transcription_token_forbidden", "Voice prompt is not configured correctly. The Deepgram API key cannot grant live transcription tokens.")
			return
		}
		s.writeError(response, http.StatusBadGateway, "transcription_token_failed", "voice prompt transcription could not start")
		return
	}
	s.Logger.Info("voice prompt token granted", "user_id", authenticatedUserIDForLog(request), "expires_in", token.ExpiresIn, "model", token.Model)
	s.writeJSON(response, http.StatusOK, token)
}

func (s *Server) transcribePromptAudio(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		s.writeError(response, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	if s.PromptTranscriber == nil {
		s.writeError(response, http.StatusServiceUnavailable, "transcription_not_configured", "voice prompt transcription is not configured")
		return
	}
	rawContentType := strings.TrimSpace(request.Header.Get("Content-Type"))
	if rawContentType == "" {
		s.writeError(response, http.StatusBadRequest, "invalid_audio", "audio content type is required")
		return
	}
	contentType := contentTypeOrDefault(rawContentType)
	if !strings.HasPrefix(strings.ToLower(contentType), "audio/") {
		s.writeError(response, http.StatusBadRequest, "invalid_audio", "audio content type is required")
		return
	}
	request.Body = http.MaxBytesReader(response, request.Body, maxPromptAudioBytes)
	data, err := io.ReadAll(request.Body)
	if err != nil {
		s.writeError(response, http.StatusRequestEntityTooLarge, "audio_too_large", "audio recording is too large")
		return
	}
	if len(data) == 0 {
		s.writeError(response, http.StatusBadRequest, "invalid_audio", "audio recording is required")
		return
	}
	transcript, err := s.PromptTranscriber.TranscribePrompt(request.Context(), data, contentType)
	if err != nil {
		s.Logger.Warn("voice prompt transcription failed", "user_id", authenticatedUserIDForLog(request), "error", err)
		s.writeError(response, http.StatusBadGateway, "transcription_failed", "voice prompt could not be transcribed")
		return
	}
	transcript = truncateIOSConfigValue(sanitizeIOSStoryText(transcript), 480)
	if transcript == "" {
		s.writeError(response, http.StatusBadGateway, "empty_transcription", "voice prompt transcription was empty")
		return
	}
	s.Logger.Info("voice prompt transcription complete", "user_id", authenticatedUserIDForLog(request), "audio_bytes", len(data), "transcript_length", len(transcript))
	s.writeJSON(response, http.StatusOK, map[string]string{"transcript": transcript})
}

func contentTypeOrDefault(contentType string) string {
	contentType = strings.TrimSpace(strings.Split(contentType, ";")[0])
	if contentType == "" {
		return "audio/mp4"
	}
	return contentType
}

func authenticatedUserIDForLog(request *http.Request) string {
	if user, ok := authenticatedUserFromContext(request.Context()); ok {
		return user.ID
	}
	return "unknown"
}

type deepgramAuthGrantResponse struct {
	AccessToken string `json:"access_token"`
}

type deepgramListenResponse struct {
	Results struct {
		Channels []struct {
			Alternatives []struct {
				Transcript string `json:"transcript"`
			} `json:"alternatives"`
		} `json:"channels"`
	} `json:"results"`
}

func (r deepgramListenResponse) bestTranscript() string {
	for _, channel := range r.Results.Channels {
		for _, alternative := range channel.Alternatives {
			if transcript := strings.TrimSpace(alternative.Transcript); transcript != "" {
				return transcript
			}
		}
	}
	return ""
}
