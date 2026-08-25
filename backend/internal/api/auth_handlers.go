package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/framemio/backend/internal/models"
)

const (
	anonymousAccessTokenTTL  = 15 * time.Minute
	anonymousRefreshTokenTTL = 90 * 24 * time.Hour
)

type tokenIssuer interface {
	IssueToken(user models.User, expiresAt time.Time) (string, error)
}

type tokenPairIssuer interface {
	IssueTokenPair(user models.User, accessExpiresAt, refreshExpiresAt time.Time) (issuedTokenPair, error)
}

type refreshTokenValidator interface {
	ValidateRefreshToken(token string) (models.User, error)
}

type anonymousAuthRequest struct {
	InstallationID string `json:"installation_id"`
}

type refreshAuthRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type authTokenResponse struct {
	AccessToken      string      `json:"access_token"`
	RefreshToken     string      `json:"refresh_token,omitempty"`
	ExpiresAt        time.Time   `json:"expires_at"`
	RefreshExpiresAt *time.Time  `json:"refresh_expires_at,omitempty"`
	User             models.User `json:"user"`
}

func (s *Server) createAnonymousToken(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		s.writeError(response, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
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

	s.writeIssuedTokens(response, user, now)
}

func (s *Server) refreshAnonymousToken(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		s.writeError(response, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	validator, ok := s.Authenticator.(refreshTokenValidator)
	if !ok {
		s.writeError(response, http.StatusInternalServerError, "auth_not_configured", "refresh token validation is not configured")
		return
	}
	var input refreshAuthRequest
	if !s.decodeJSON(response, request, &input) {
		return
	}
	user, err := validator.ValidateRefreshToken(input.RefreshToken)
	if err != nil {
		s.writeError(response, http.StatusUnauthorized, "unauthorized", "refresh token is invalid or expired")
		return
	}
	if s.Store != nil {
		user, err = s.Store.EnsureUser(request.Context(), user)
		if err != nil {
			s.internalError(response, err, "ensure anonymous user")
			return
		}
	}
	if user.ID == "" {
		s.writeError(response, http.StatusUnauthorized, "unauthorized", "refresh token is invalid or expired")
		return
	}
	s.writeIssuedTokens(response, user, s.Now().UTC())
}

func (s *Server) writeIssuedTokens(response http.ResponseWriter, user models.User, now time.Time) {
	accessExpiresAt := now.Add(anonymousAccessTokenTTL)
	refreshExpiresAt := now.Add(anonymousRefreshTokenTTL)
	if pairIssuer, ok := s.Authenticator.(tokenPairIssuer); ok {
		pair, err := pairIssuer.IssueTokenPair(user, accessExpiresAt, refreshExpiresAt)
		if err != nil {
			s.internalError(response, err, "issue anonymous token")
			return
		}
		s.writeJSON(response, http.StatusOK, authTokenResponse{
			AccessToken:      pair.AccessToken,
			RefreshToken:     pair.RefreshToken,
			ExpiresAt:        pair.AccessExpiresAt,
			RefreshExpiresAt: &pair.RefreshExpiresAt,
			User:             user,
		})
		return
	}
	issuer, ok := s.Authenticator.(tokenIssuer)
	if !ok {
		s.writeError(response, http.StatusInternalServerError, "auth_not_configured", "token issuing is not configured")
		return
	}
	token, err := issuer.IssueToken(user, accessExpiresAt)
	if err != nil {
		s.internalError(response, err, "issue anonymous token")
		return
	}
	s.writeJSON(response, http.StatusOK, authTokenResponse{AccessToken: token, ExpiresAt: accessExpiresAt, User: user})
}
