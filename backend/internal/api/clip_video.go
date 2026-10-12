package api

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/framemio/backend/internal/models"
	"github.com/framemio/backend/internal/store"
)

const clipGenerationUserID = "service-product-video"

func (s *Server) dispatchInternalClipVideo(response http.ResponseWriter, request *http.Request) {
	provided := request.Header.Get("X-FrameMio-Clip-Token")
	if s.ClipGenerationToken == "" || len(provided) != len(s.ClipGenerationToken) || subtle.ConstantTimeCompare([]byte(provided), []byte(s.ClipGenerationToken)) != 1 {
		s.writeError(response, http.StatusUnauthorized, "unauthorized", "clip generation authentication required")
		return
	}
	if s.Store == nil || s.ObjectStorage == nil || s.Credits == nil {
		s.writeError(response, http.StatusServiceUnavailable, "clip_generation_unavailable", "clip generation is not configured")
		return
	}

	user, err := s.Store.EnsureUser(request.Context(), models.User{ID: clipGenerationUserID, Email: "product-video@internal.invalid", CreatedAt: s.Now().UTC()})
	if err != nil {
		s.internalError(response, err, "ensure clip generation account")
		return
	}
	if _, _, err := s.Credits.EnsureInitialGrant(request.Context(), user.ID); err != nil {
		s.internalError(response, err, "ensure clip generation credit grant")
		return
	}
	request = request.WithContext(contextWithAuthenticatedUser(request.Context(), user))

	if request.URL.Path == "/internal/clip-media/upload-session" && request.Method == http.MethodPost {
		s.createUploadSession(response, request)
		return
	}
	if request.URL.Path == "/internal/clip-media/complete" && request.Method == http.MethodPost {
		s.completeMedia(response, request)
		return
	}
	if request.URL.Path == "/internal/clip-videos" {
		if request.Method != http.MethodPost {
			s.writeError(response, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
			return
		}
		idempotencyKey, ok := requiredIdempotencyKey(response, request)
		if !ok {
			return
		}
		if existing, err := s.Store.GetTextVideoGenerationByIdempotency(request.Context(), user.ID, idempotencyKey); err == nil {
			s.writeJSON(response, http.StatusOK, existing)
			return
		} else if err != store.ErrNotFound {
			s.internalError(response, err, "find clip generation request")
			return
		}
		s.createTextVideo(response, request)
		return
	}

	parts := strings.Split(strings.Trim(request.URL.Path, "/"), "/")
	if request.Method == http.MethodGet && len(parts) == 3 && parts[2] != "" {
		s.getTextVideo(response, request, parts[2])
		return
	}
	if request.Method == http.MethodGet && len(parts) == 4 && parts[2] != "" && parts[3] == "content" {
		s.getTextVideoContent(response, request, parts[2])
		return
	}
	s.writeError(response, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
}
