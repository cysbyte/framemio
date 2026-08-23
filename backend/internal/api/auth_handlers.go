package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/framemio/backend/internal/models"
)

type tokenIssuer interface {
	IssueToken(user models.User, expiresAt time.Time) (string, error)
}

type anonymousAuthRequest struct {
	InstallationID string `json:"installation_id"`
}

type authTokenResponse struct {
	AccessToken string      `json:"access_token"`
	ExpiresAt   time.Time   `json:"expires_at"`
	User        models.User `json:"user"`
}

func (s *Server) createAnonymousToken(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		s.writeError(response, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	issuer, ok := s.Authenticator.(tokenIssuer)
	if !ok {
		s.writeError(response, http.StatusInternalServerError, "auth_not_configured", "token issuing is not configured")
		return
	}

	var input anonymousAuthRequest
	if !s.decodeJSON(response, request, &input) {
		return
	}
	installationID := strings.TrimSpace(input.InstallationID)
	if installationID == "" || len(installationID) > 128 {
		s.writeError(response, http.StatusBadRequest, "invalid_installation", "installation_id is required")
		return
	}

	now := s.Now().UTC()
	user := models.User{ID: "ios-anon-" + installationID, CreatedAt: now}
	if s.Store != nil {
		var err error
		user, err = s.Store.EnsureUser(request.Context(), user)
		if err != nil {
			s.internalError(response, err, "ensure anonymous user")
			return
		}
	}

	expiresAt := now.Add(30 * 24 * time.Hour)
	token, err := issuer.IssueToken(user, expiresAt)
	if err != nil {
		s.internalError(response, err, "issue anonymous token")
		return
	}
	s.writeJSON(response, http.StatusOK, authTokenResponse{AccessToken: token, ExpiresAt: expiresAt, User: user})
}
