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
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/framemio/backend/internal/credits"
	"github.com/framemio/backend/internal/models"
	"github.com/framemio/backend/internal/store"
)

type textVideoGenerationRequest struct {
	Prompt      string `json:"prompt"`
	Seconds     int    `json:"seconds"`
	Resolution  string `json:"resolution"`
	AspectRatio string `json:"aspect_ratio"`
}

type textVideoGenerationResponse struct {
	ID          string                 `json:"id"`
	Status      models.TextVideoStatus `json:"status"`
	Progress    float64                `json:"progress"`
	VideoURL    string                 `json:"video_url,omitempty"`
	Error       string                 `json:"error,omitempty"`
	CreatedAt   time.Time              `json:"created_at"`
	CompletedAt *time.Time             `json:"completed_at,omitempty"`
	Prompt      string                 `json:"prompt"`
	Seconds     int                    `json:"seconds"`
	Resolution  string                 `json:"resolution"`
	AspectRatio string                 `json:"aspect_ratio"`
}

var textVideoPollerLock sync.Mutex

const textVideoDownloadURLExpiry = 2 * time.Hour

func (s *Server) StartBackground(ctx context.Context) {
	s.textVideoPollerOnce.Do(func() {
		go s.runTextVideoPoller(ctx)
	})
}

func (s *Server) runTextVideoPoller(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	s.Logger.Info("text video background poller started")
	for {
		if err := s.pollDueTextVideos(ctx); err != nil && !errors.Is(err, context.Canceled) {
			s.Logger.Error("text video background poller failed", "error", err)
		}
		select {
		case <-ctx.Done():
			s.Logger.Info("text video background poller stopped")
			return
		case <-s.textVideoPollerWake:
		case <-ticker.C:
		}
	}
}

func (s *Server) pollDueTextVideos(ctx context.Context) error {
	if s.Store == nil {
		return nil
	}
	generations, err := s.Store.ListActiveTextVideoGenerations(ctx, 10)
	if err != nil {
		return err
	}
	for _, generation := range generations {
		if err := s.advanceTextVideo(ctx, generation); err != nil {
			s.Logger.Error("text video generation advance failed", "generation_id", generation.ID, "error", err)
		}
	}
	return nil
}

func (s *Server) createTextVideo(response http.ResponseWriter, request *http.Request) {
	user, ok := s.authenticate(response, request)
	if !ok {
		return
	}
	idempotencyKey, ok := requiredIdempotencyKey(response, request)
	if !ok {
		return
	}
	var input textVideoGenerationRequest
	if !s.decodeJSON(response, request, &input) {
		return
	}
	input.Prompt = strings.TrimSpace(input.Prompt)
	if input.Seconds == 0 {
		input.Seconds = defaultVideoSeconds
	}
	if input.Resolution == "" {
		input.Resolution = defaultVideoResolution
	}
	if input.AspectRatio == "" {
		input.AspectRatio = defaultVideoAspectRatio
	}
	if input.Prompt == "" || input.Seconds < 1 || input.Seconds > 15 {
		s.writeError(response, http.StatusBadRequest, "invalid_text_video_request", "prompt and a duration between 1 and 15 seconds are required")
		return
	}
	if _, ok := supportedVideoResolutions[input.Resolution]; !ok {
		s.writeError(response, http.StatusBadRequest, "invalid_text_video_request", "resolution is invalid")
		return
	}
	if _, ok := supportedVideoAspectRatios[input.AspectRatio]; !ok {
		s.writeError(response, http.StatusBadRequest, "invalid_text_video_request", "aspect ratio is invalid")
		return
	}
	if s.Store != nil {
		existing, existingErr := s.Store.GetTextVideoGenerationByIdempotency(request.Context(), user.ID, idempotencyKey)
		if existingErr == nil {
			s.writeJSON(response, http.StatusOK, existing)
			return
		}
		if !errors.Is(existingErr, store.ErrNotFound) {
			s.internalError(response, existingErr, "get existing text video generation")
			return
		}
	}
	if s.Credits == nil {
		s.internalError(response, errors.New("credit service not configured"), "reserve text video credits")
		return
	}

	generation := models.TextVideoGeneration{
		ID:          models.NewID("text-video"),
		UserID:      user.ID,
		Prompt:      input.Prompt,
		Seconds:     input.Seconds,
		Resolution:  input.Resolution,
		AspectRatio: input.AspectRatio,
		Model:       videoModelForResolution(input.Resolution),
		Status:      models.TextVideoStatusQueued,
		CreatedAt:   s.Now().UTC(),
		UpdatedAt:   s.Now().UTC(),
		NextPollAt:  s.Now().UTC(),
	}
	_, _, _, err := s.Credits.ReserveWithCreation(
		request.Context(),
		user.ID,
		models.CreditActionCreateVideo,
		"text_video",
		generation.ID,
		credits.StoryMediaStats{},
		map[string]any{"seconds": input.Seconds, "resolution": input.Resolution, "aspect_ratio": input.AspectRatio},
	)
	if err != nil {
		if errors.Is(err, store.ErrInsufficientCredits) {
			quote, quoteErr := s.Credits.Quote(request.Context(), user.ID, models.CreditActionCreateVideo, credits.StoryMediaStats{})
			if quoteErr == nil {
				s.writeJSON(response, http.StatusPaymentRequired, map[string]any{"error": "insufficient_credits", "required": quote.Credits, "available": quote.Balance})
				return
			}
		}
		s.internalError(response, err, "reserve text video credits")
		return
	}
	stored, existing, err := s.Store.CreateTextVideoGeneration(request.Context(), generation, idempotencyKey)
	if err != nil {
		_, _ = s.Credits.ReleaseByReference(request.Context(), user.ID, models.CreditActionCreateVideo, "text_video", generation.ID)
		s.internalError(response, err, "create text video generation")
		return
	}
	if existing {
		_, _ = s.Credits.ReleaseByReference(request.Context(), user.ID, models.CreditActionCreateVideo, "text_video", generation.ID)
		s.writeJSON(response, http.StatusOK, stored)
		return
	}
	select {
	case s.textVideoPollerWake <- struct{}{}:
	default:
	}
	s.writeJSON(response, http.StatusAccepted, stored)
}

func (s *Server) listTextVideos(response http.ResponseWriter, request *http.Request) {
	user, ok := s.authenticate(response, request)
	if !ok {
		return
	}
	generations, err := s.Store.ListTextVideoGenerations(request.Context(), user.ID)
	if err != nil {
		s.internalError(response, err, "list text video generations")
		return
	}
	s.writeJSON(response, http.StatusOK, generations)
}

func (s *Server) getTextVideo(response http.ResponseWriter, request *http.Request, generationID string) {
	user, ok := s.authenticate(response, request)
	if !ok {
		return
	}
	generation, err := s.Store.GetTextVideoGeneration(request.Context(), user.ID, generationID)
	if errors.Is(err, store.ErrNotFound) {
		s.writeError(response, http.StatusNotFound, "text_video_not_found", "text video generation not found")
		return
	}
	if err != nil {
		s.internalError(response, err, "get text video generation")
		return
	}
	s.writeJSON(response, http.StatusOK, generation)
}

func (s *Server) deleteTextVideo(response http.ResponseWriter, request *http.Request, generationID string) {
	user, ok := s.authenticate(response, request)
	if !ok {
		return
	}
	generation, err := s.Store.GetTextVideoGeneration(request.Context(), user.ID, generationID)
	if err != nil {
		s.mapStoreError(response, err)
		return
	}
	if generation.S3Key != "" && s.ObjectStorage != nil {
		if err := s.ObjectStorage.DeleteObject(request.Context(), generation.S3Key); err != nil {
			s.internalError(response, err, "delete text video object")
			return
		}
	}
	if err := s.Store.DeleteTextVideoGeneration(request.Context(), user.ID, generation.ID); err != nil {
		s.mapStoreError(response, err)
		return
	}
	if !models.IsTerminalTextVideoStatus(generation.Status) && s.Credits != nil {
		if _, err := s.Credits.ReleaseByReference(request.Context(), user.ID, models.CreditActionCreateVideo, "text_video", generation.ID); err != nil {
			s.Logger.Warn("release deleted text video credits failed", "generation_id", generation.ID, "error", err)
		}
	}
	s.writeJSON(response, http.StatusOK, map[string]bool{"deleted": true})
}

func (s *Server) advanceTextVideo(ctx context.Context, generation models.TextVideoGeneration) error {
	textVideoPollerLock.Lock()
	defer textVideoPollerLock.Unlock()
	if models.IsTerminalTextVideoStatus(generation.Status) {
		return nil
	}
	if strings.TrimSpace(s.VideoProviderAPIKey) == "" || strings.TrimSpace(s.VideoProviderBaseURL) == "" {
		_, _ = s.Store.UpdateTextVideoGeneration(ctx, generation.ID, "", models.TextVideoStatusFailed, generation.Progress, "", "", "video generation is not configured")
		_, _ = s.Credits.ReleaseByReference(ctx, generation.UserID, models.CreditActionCreateVideo, "text_video", generation.ID)
		return nil
	}
	var data []byte
	var endpoint string
	if generation.ProviderTaskID == "" {
		body, err := json.Marshal(videoGenerationRequest{
			Model: generation.Model, Prompt: generation.Prompt, Seconds: generation.Seconds,
			Resolution: generation.Resolution, AspectRatio: generation.AspectRatio, Images: []string{},
		})
		if err != nil {
			return err
		}
		data = body
		endpoint = videoCreateURL(s.VideoProviderBaseURL)
	} else {
		endpoint = videoStatusURL(s.VideoProviderBaseURL, generation.ProviderTaskID)
	}
	method := http.MethodPost
	idempotencyKey := "text-video-" + generation.ID
	if generation.ProviderTaskID != "" {
		method = http.MethodGet
		data = nil
		idempotencyKey = ""
	}
	response, err := s.videoProviderRequestContext(ctx, method, endpoint, data, idempotencyKey)
	if err != nil {
		// Provider/network outages are retryable. The persisted job remains active
		// so a later poll can safely retry the same create request by idempotency key.
		_, _ = s.Store.UpdateTextVideoGeneration(ctx, generation.ID, generation.ProviderTaskID, generation.Status, generation.Progress, "", "", "")
		return nil
	}
	defer response.Body.Close()
	responseData, err := io.ReadAll(response.Body)
	if err != nil {
		return err
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		if response.StatusCode == http.StatusRequestTimeout ||
			response.StatusCode == http.StatusTooManyRequests ||
			response.StatusCode >= http.StatusInternalServerError {
			_, _ = s.Store.UpdateTextVideoGeneration(ctx, generation.ID, generation.ProviderTaskID, generation.Status, generation.Progress, "", "", "")
			return nil
		}
		_, _ = s.Store.UpdateTextVideoGeneration(ctx, generation.ID, "", models.TextVideoStatusFailed, generation.Progress, "", "", "video provider request failed")
		_, _ = s.Credits.ReleaseByReference(ctx, generation.UserID, models.CreditActionCreateVideo, "text_video", generation.ID)
		return nil
	}
	status := decodeProviderVideoStatus(responseData)
	if status.videoURL != "" {
		videoURL := normalizeProviderVideoURL(s.VideoProviderBaseURL, status.videoURL)
		s3Key := ""
		if s.ObjectStorage != nil {
			videoData, contentType, downloadErr := s.downloadProviderVideo(ctx, videoURL)
			if downloadErr != nil {
				// Keep the job active so the next poll can retry both the provider
				// fetch and the S3 upload without charging the user twice.
				_, _ = s.Store.UpdateTextVideoGeneration(ctx, generation.ID, status.taskID, models.TextVideoStatusRunning, generation.Progress, videoURL, "", "")
				return nil
			}
			s3Key = textVideoS3Key(generation.UserID, generation.ID)
			if uploadErr := s.ObjectStorage.PutObject(ctx, s3Key, contentType, videoData); uploadErr != nil {
				_, _ = s.Store.UpdateTextVideoGeneration(ctx, generation.ID, status.taskID, models.TextVideoStatusRunning, generation.Progress, videoURL, "", "")
				return nil
			}
		}
		_, err = s.Store.UpdateTextVideoGeneration(ctx, generation.ID, status.taskID, models.TextVideoStatusCompleted, 1, videoURL, s3Key, "")
		if err != nil && s3Key != "" && s.ObjectStorage != nil {
			if cleanupErr := s.ObjectStorage.DeleteObject(ctx, s3Key); cleanupErr != nil {
				s.Logger.Warn("cleanup orphaned text video object failed", "generation_id", generation.ID, "s3_key", s3Key, "error", cleanupErr)
			}
		}
		if err == nil {
			_, _ = s.Credits.CommitByReference(ctx, generation.UserID, models.CreditActionCreateVideo, "text_video", generation.ID)
		}
		return err
	}
	if status.failed {
		_, _ = s.Store.UpdateTextVideoGeneration(ctx, generation.ID, status.taskID, models.TextVideoStatusFailed, generation.Progress, "", "", "Video generation failed. Try a different prompt.")
		_, _ = s.Credits.ReleaseByReference(ctx, generation.UserID, models.CreditActionCreateVideo, "text_video", generation.ID)
		return nil
	}
	nextStatus := models.TextVideoStatusRunning
	if status.status == "queued" || status.status == "pending" {
		nextStatus = models.TextVideoStatusQueued
	}
	_, err = s.Store.UpdateTextVideoGeneration(ctx, generation.ID, status.taskID, nextStatus, status.progress, "", "", "")
	return err
}

func (s *Server) getTextVideoContent(response http.ResponseWriter, request *http.Request, generationID string) {
	user, ok := s.authenticate(response, request)
	if !ok {
		return
	}
	generation, err := s.Store.GetTextVideoGeneration(request.Context(), user.ID, generationID)
	if errors.Is(err, store.ErrNotFound) {
		s.writeError(response, http.StatusNotFound, "text_video_not_found", "text video generation not found")
		return
	}
	if err != nil {
		s.internalError(response, err, "get text video content")
		return
	}
	if generation.Status != models.TextVideoStatusCompleted {
		s.writeError(response, http.StatusConflict, "text_video_not_ready", "the generated video is not ready")
		return
	}

	if generation.S3Key != "" && s.ObjectStorage != nil {
		downloadURL, err := s.ObjectStorage.PresignedDownloadURL(request.Context(), generation.S3Key, textVideoDownloadURLExpiry)
		if err != nil {
			s.internalError(response, err, "presign text video content")
			return
		}
		http.Redirect(response, request, downloadURL, http.StatusTemporaryRedirect)
		return
	}
	if strings.TrimSpace(generation.VideoURL) == "" {
		s.writeError(response, http.StatusConflict, "text_video_not_ready", "the generated video is not ready")
		return
	}
	videoURL, err := resolveProviderVideoURL(s.VideoProviderBaseURL, generation.VideoURL)
	if err != nil {
		s.internalError(response, err, "resolve text video content url")
		return
	}
	providerRequest, err := http.NewRequestWithContext(request.Context(), http.MethodGet, videoURL, nil)
	if err != nil {
		s.internalError(response, err, "create text video content request")
		return
	}
	providerRequest.Header.Set("Accept", "video/*,application/octet-stream")
	if providerURL, baseURL := providerRequest.URL, strings.TrimSpace(s.VideoProviderBaseURL); baseURL != "" {
		base, parseErr := url.Parse(baseURL)
		if parseErr == nil && strings.EqualFold(providerURL.Host, base.Host) {
			providerRequest.Header.Set("Authorization", "Bearer "+s.VideoProviderAPIKey)
		}
	}
	client := s.VideoProviderHTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	upstreamResponse, err := client.Do(providerRequest)
	if err != nil {
		s.internalError(response, err, "fetch text video content")
		return
	}
	defer upstreamResponse.Body.Close()
	if upstreamResponse.StatusCode < http.StatusOK || upstreamResponse.StatusCode >= http.StatusMultipleChoices {
		s.writeError(response, http.StatusBadGateway, "text_video_content_failed", "generated video content could not be fetched")
		return
	}
	if contentType := upstreamResponse.Header.Get("Content-Type"); contentType != "" {
		response.Header().Set("Content-Type", contentType)
	} else {
		response.Header().Set("Content-Type", "video/mp4")
	}
	if contentLength := upstreamResponse.Header.Get("Content-Length"); contentLength != "" {
		response.Header().Set("Content-Length", contentLength)
	}
	if _, err := io.Copy(response, upstreamResponse.Body); err != nil {
		s.Logger.Error("copy text video content", "generation_id", generationID, "error", err)
	}
}

func (s *Server) downloadProviderVideo(ctx context.Context, videoURL string) ([]byte, string, error) {
	providerRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, videoURL, nil)
	if err != nil {
		return nil, "", err
	}
	providerRequest.Header.Set("Accept", "video/*,application/octet-stream")
	if providerBaseURL := strings.TrimSpace(s.VideoProviderBaseURL); providerBaseURL != "" {
		base, parseErr := url.Parse(providerBaseURL)
		if parseErr == nil && strings.EqualFold(providerRequest.URL.Host, base.Host) {
			providerRequest.Header.Set("Authorization", "Bearer "+s.VideoProviderAPIKey)
		}
	}
	client := s.VideoProviderHTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	providerResponse, err := client.Do(providerRequest)
	if err != nil {
		return nil, "", err
	}
	defer providerResponse.Body.Close()
	if providerResponse.StatusCode < http.StatusOK || providerResponse.StatusCode >= http.StatusMultipleChoices {
		return nil, "", fmt.Errorf("provider video download returned status %d", providerResponse.StatusCode)
	}
	data, err := io.ReadAll(providerResponse.Body)
	if err != nil {
		return nil, "", err
	}
	contentType := providerResponse.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "video/mp4"
	}
	return data, contentType, nil
}

func textVideoS3Key(userID, generationID string) string {
	return fmt.Sprintf("users/%s/text-videos/%s.mp4", userID, generationID)
}

func (s *Server) videoProviderRequestContext(ctx context.Context, method, endpoint string, body []byte, idempotencyKey string) (*http.Response, error) {
	providerRequest, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	providerRequest.Header.Set("Accept", "application/json")
	providerRequest.Header.Set("Authorization", "Bearer "+s.VideoProviderAPIKey)
	if body != nil {
		providerRequest.Header.Set("Content-Type", "application/json")
	}
	if idempotencyKey = strings.TrimSpace(idempotencyKey); idempotencyKey != "" {
		providerRequest.Header.Set("Idempotency-Key", idempotencyKey)
	}
	client := s.VideoProviderHTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	return client.Do(providerRequest)
}

func videoModelForResolution(resolution string) string {
	if resolution == "1080p" {
		return "grok-imagine-video-1.5-preview"
	}
	return "grok-imagine-video"
}

func normalizeProviderVideoURL(baseURL, videoURL string) string {
	resolved, err := resolveProviderVideoURL(baseURL, videoURL)
	if err != nil {
		return strings.TrimSpace(videoURL)
	}
	return resolved
}

func resolveProviderVideoURL(baseURL, videoURL string) (string, error) {
	videoURL = strings.TrimSpace(videoURL)
	if videoURL == "" {
		return "", errors.New("video content url is empty")
	}
	parsed, err := url.Parse(videoURL)
	if err != nil {
		return "", fmt.Errorf("parse video content url: %w", err)
	}
	if parsed.IsAbs() {
		return parsed.String(), nil
	}
	base, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || !base.IsAbs() {
		return "", errors.New("video provider base url is invalid")
	}
	return base.ResolveReference(parsed).String(), nil
}

type providerVideoStatus struct {
	taskID   string
	status   string
	progress float64
	videoURL string
	failed   bool
}

func decodeProviderVideoStatus(data []byte) providerVideoStatus {
	var object any
	if json.Unmarshal(data, &object) != nil {
		return providerVideoStatus{status: "failed", failed: true}
	}
	status := providerString(object, map[string]struct{}{"status": {}})
	taskID := providerString(object, map[string]struct{}{"id": {}, "video_id": {}, "task_id": {}, "job_id": {}})
	videoURL := providerString(object, map[string]struct{}{"video_url": {}, "download_url": {}, "url": {}, "output_url": {}, "file_url": {}, "content_url": {}})
	progressText := providerString(object, map[string]struct{}{"progress": {}})
	progress := 0.0
	if value, err := strconv.ParseFloat(progressText, 64); err == nil {
		progress = value / 100
		if progress > 1 {
			progress = 1
		}
	}
	status = strings.ToLower(strings.TrimSpace(status))
	return providerVideoStatus{
		taskID: taskID, status: status, progress: progress, videoURL: videoURL,
		failed: status == "failed" || status == "cancelled" || status == "canceled" || status == "error",
	}
}

func providerString(value any, keys map[string]struct{}) string {
	switch typed := value.(type) {
	case map[string]any:
		for key, nested := range typed {
			normalized := strings.ToLower(strings.ReplaceAll(key, "-", "_"))
			if _, ok := keys[normalized]; ok {
				if stringValue, ok := nested.(string); ok {
					return stringValue
				}
				return fmt.Sprint(nested)
			}
		}
		for _, nested := range typed {
			if found := providerString(nested, keys); found != "" {
				return found
			}
		}
	case []any:
		for _, nested := range typed {
			if found := providerString(nested, keys); found != "" {
				return found
			}
		}
	}
	return ""
}

const (
	defaultVideoModel       = "grok-imagine-video"
	defaultVideoSeconds     = 5
	defaultVideoResolution  = "720p"
	defaultVideoAspectRatio = "9:16"
)

var supportedVideoAspectRatios = map[string]struct{}{
	"3:4":  {},
	"1:1":  {},
	"4:3":  {},
	"9:16": {},
	"16:9": {},
}

var supportedVideoResolutions = map[string]struct{}{
	"480p":  {},
	"720p":  {},
	"1080p": {},
}

type videoGenerationRequest struct {
	Model       string   `json:"model"`
	Prompt      string   `json:"prompt"`
	Seconds     int      `json:"seconds"`
	Resolution  string   `json:"resolution"`
	AspectRatio string   `json:"aspect_ratio"`
	Images      []string `json:"images"`
}

func (input *videoGenerationRequest) normalize() {
	input.Model = strings.TrimSpace(input.Model)
	if input.Model == "" {
		input.Model = defaultVideoModel
	}
	input.Prompt = strings.TrimSpace(input.Prompt)
	if input.Seconds == 0 {
		input.Seconds = defaultVideoSeconds
	}
	input.Resolution = strings.TrimSpace(input.Resolution)
	if input.Resolution == "" {
		input.Resolution = defaultVideoResolution
	}
	if input.Resolution == "1080p" && input.Model == defaultVideoModel {
		input.Model = "grok-imagine-video-1.5-preview"
	}
	input.AspectRatio = strings.TrimSpace(input.AspectRatio)
	if input.AspectRatio == "" {
		input.AspectRatio = defaultVideoAspectRatio
	}
	if input.Images == nil {
		input.Images = []string{}
	}
}

func (input videoGenerationRequest) validate() string {
	if input.Prompt == "" {
		return "prompt is required"
	}
	if len(input.Prompt) > 20_000 {
		return "prompt must be 20,000 characters or fewer"
	}
	if input.Seconds < 1 || input.Seconds > 15 {
		return "seconds must be between 1 and 15"
	}
	if _, ok := supportedVideoResolutions[input.Resolution]; !ok {
		return "resolution must be 480p, 720p, or 1080p"
	}
	if _, ok := supportedVideoAspectRatios[input.AspectRatio]; !ok {
		return "aspect_ratio must be one of 3:4, 1:1, 4:3, 9:16, or 16:9"
	}
	for _, image := range input.Images {
		if strings.TrimSpace(image) == "" {
			return "images must contain non-empty values"
		}
	}
	return ""
}

func (s *Server) createVideo(response http.ResponseWriter, request *http.Request) {
	user, ok := s.authenticate(response, request)
	if !ok {
		return
	}
	idempotencyKey, ok := requiredIdempotencyKey(response, request)
	if !ok {
		return
	}
	if strings.TrimSpace(s.VideoProviderAPIKey) == "" || strings.TrimSpace(s.VideoProviderBaseURL) == "" {
		s.writeError(response, http.StatusServiceUnavailable, "video_provider_not_configured", "video generation is not configured")
		return
	}

	var input videoGenerationRequest
	if !s.decodeJSON(response, request, &input) {
		return
	}
	input.normalize()
	if message := input.validate(); message != "" {
		s.writeError(response, http.StatusBadRequest, "invalid_video_request", message)
		return
	}

	if s.Credits == nil {
		s.internalError(response, errors.New("credit service not configured"), "reserve video credits")
		return
	}
	reservation, _, reservationCreated, err := s.Credits.ReserveWithCreation(
		request.Context(),
		user.ID,
		models.CreditActionCreateVideo,
		"video",
		idempotencyKey,
		credits.StoryMediaStats{},
		map[string]any{
			"prompt_length": len(input.Prompt),
			"seconds":       input.Seconds,
			"resolution":    input.Resolution,
			"aspect_ratio":  input.AspectRatio,
		},
	)
	if err != nil {
		if errors.Is(err, store.ErrInsufficientCredits) {
			quote, quoteErr := s.Credits.Quote(request.Context(), user.ID, models.CreditActionCreateVideo, credits.StoryMediaStats{})
			if quoteErr == nil {
				s.writeJSON(response, http.StatusPaymentRequired, map[string]any{
					"error":     "insufficient_credits",
					"required":  quote.Credits,
					"available": quote.Balance,
				})
				return
			}
		}
		s.internalError(response, err, "reserve video credits")
		return
	}

	releaseReservation := reservationCreated && reservation.ID != ""
	defer func() {
		if !releaseReservation {
			return
		}
		if _, releaseErr := s.Credits.Release(request.Context(), user.ID, reservation.ID); releaseErr != nil &&
			!errors.Is(releaseErr, store.ErrConflict) &&
			!errors.Is(releaseErr, store.ErrNotFound) {
			s.Logger.Warn("video credit reservation cleanup failed",
				"user_id", user.ID,
				"reservation_id", reservation.ID,
				"error", releaseErr,
			)
		}
	}()

	body, err := json.Marshal(input)
	if err != nil {
		s.internalError(response, err, "encode video request")
		return
	}
	upstreamResponse, err := s.videoProviderRequest(request, http.MethodPost, videoCreateURL(s.VideoProviderBaseURL), body)
	if err != nil {
		s.internalError(response, err, "video provider create request")
		return
	}
	defer upstreamResponse.Body.Close()

	if upstreamResponse.StatusCode >= http.StatusOK && upstreamResponse.StatusCode < http.StatusMultipleChoices {
		if reservation.ID != "" {
			if _, err := s.Credits.Commit(request.Context(), user.ID, reservation.ID); err != nil {
				s.internalError(response, err, "commit video credits")
				return
			}
			releaseReservation = false
		}
	}
	s.copyVideoProviderResponse(response, upstreamResponse)
}

func (s *Server) getVideo(response http.ResponseWriter, request *http.Request, videoID string) {
	if strings.TrimSpace(s.VideoProviderAPIKey) == "" || strings.TrimSpace(s.VideoProviderBaseURL) == "" {
		s.writeError(response, http.StatusServiceUnavailable, "video_provider_not_configured", "video generation is not configured")
		return
	}
	videoID = strings.TrimSpace(videoID)
	if videoID == "" {
		s.writeError(response, http.StatusBadRequest, "invalid_video_id", "video id is required")
		return
	}

	upstreamResponse, err := s.videoProviderRequest(request, http.MethodGet, videoStatusURL(s.VideoProviderBaseURL, videoID), nil)
	if err != nil {
		s.internalError(response, err, "video provider status request")
		return
	}
	defer upstreamResponse.Body.Close()
	s.copyVideoProviderResponse(response, upstreamResponse)
}

func (s *Server) videoProviderRequest(request *http.Request, method, endpoint string, body []byte) (*http.Response, error) {
	providerRequest, err := http.NewRequestWithContext(request.Context(), method, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	providerRequest.Header.Set("Accept", "application/json")
	providerRequest.Header.Set("Authorization", "Bearer "+s.VideoProviderAPIKey)
	if body != nil {
		providerRequest.Header.Set("Content-Type", "application/json")
	}
	if idempotencyKey := strings.TrimSpace(request.Header.Get("Idempotency-Key")); idempotencyKey != "" {
		providerRequest.Header.Set("Idempotency-Key", idempotencyKey)
	}
	client := s.VideoProviderHTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	return client.Do(providerRequest)
}

func (s *Server) copyVideoProviderResponse(response http.ResponseWriter, upstreamResponse *http.Response) {
	status := upstreamResponse.StatusCode
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		status = http.StatusBadGateway
	}
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	if _, err := io.Copy(response, upstreamResponse.Body); err != nil {
		s.Logger.Error("copy video provider response", "error", err)
	}
}

func videoCreateURL(baseURL string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if strings.HasSuffix(baseURL, "/videos") {
		return baseURL
	}
	if strings.HasSuffix(baseURL, "/v1") {
		return baseURL + "/videos"
	}
	return baseURL + "/v1/videos"
}

func videoStatusURL(baseURL, videoID string) string {
	return videoCreateURL(baseURL) + "/" + url.PathEscape(videoID)
}
