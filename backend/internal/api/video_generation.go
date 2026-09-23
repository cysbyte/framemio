package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/framemio/backend/internal/credits"
	"github.com/framemio/backend/internal/models"
	"github.com/framemio/backend/internal/store"
)

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
