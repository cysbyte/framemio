package api

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/framemio/backend/internal/models"
)

var ErrUnauthenticated = errors.New("unauthenticated")

type authContextKey struct{}

type Authenticator interface {
	Authenticate(ctx context.Context, request *http.Request) (models.User, error)
}

func contextWithAuthenticatedUser(ctx context.Context, user models.User) context.Context {
	return context.WithValue(ctx, authContextKey{}, user)
}

func authenticatedUserFromContext(ctx context.Context) (models.User, bool) {
	user, ok := ctx.Value(authContextKey{}).(models.User)
	return user, ok && user.ID != ""
}

func AuthenticatedUserID(ctx context.Context) (string, bool) {
	user, ok := authenticatedUserFromContext(ctx)
	return user.ID, ok
}

type StaticTokenAuthenticator struct {
	Token  string
	UserID string
	Email  string
}

func (a StaticTokenAuthenticator) Authenticate(_ context.Context, request *http.Request) (models.User, error) {
	header := request.Header.Get("Authorization")
	prefix, token, ok := strings.Cut(header, " ")
	if !ok || !strings.EqualFold(prefix, "Bearer") || token == "" || token != a.Token {
		return models.User{}, ErrUnauthenticated
	}
	return models.User{ID: a.UserID, Email: a.Email, CreatedAt: time.Now().UTC()}, nil
}

type HMACTokenAuthenticator struct {
	Secret []byte
	Now    func() time.Time
}

type tokenClaims struct {
	Subject   string `json:"sub"`
	Email     string `json:"email,omitempty"`
	ExpiresAt int64  `json:"exp"`
	TokenType string `json:"typ,omitempty"`
}

type issuedTokenPair struct {
	AccessToken      string
	AccessExpiresAt  time.Time
	RefreshToken     string
	RefreshExpiresAt time.Time
}

const (
	tokenTypeAccess  = "access"
	tokenTypeRefresh = "refresh"
)

func (a HMACTokenAuthenticator) IssueToken(user models.User, expiresAt time.Time) (string, error) {
	return a.issueToken(user, expiresAt, tokenTypeAccess)
}

func (a HMACTokenAuthenticator) IssueTokenPair(user models.User, accessExpiresAt, refreshExpiresAt time.Time) (issuedTokenPair, error) {
	accessToken, err := a.issueToken(user, accessExpiresAt, tokenTypeAccess)
	if err != nil {
		return issuedTokenPair{}, err
	}
	refreshToken, err := a.issueToken(user, refreshExpiresAt, tokenTypeRefresh)
	if err != nil {
		return issuedTokenPair{}, err
	}
	return issuedTokenPair{AccessToken: accessToken, AccessExpiresAt: accessExpiresAt, RefreshToken: refreshToken, RefreshExpiresAt: refreshExpiresAt}, nil
}

func (a HMACTokenAuthenticator) issueToken(user models.User, expiresAt time.Time, tokenType string) (string, error) {
	if user.ID == "" || len(a.Secret) < 32 {
		return "", ErrUnauthenticated
	}
	payload, err := json.Marshal(tokenClaims{Subject: user.ID, Email: user.Email, ExpiresAt: expiresAt.Unix(), TokenType: tokenType})
	if err != nil {
		return "", err
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, a.Secret)
	_, _ = mac.Write([]byte(encoded))
	return encoded + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func (a HMACTokenAuthenticator) Authenticate(_ context.Context, request *http.Request) (models.User, error) {
	header := request.Header.Get("Authorization")
	prefix, token, ok := strings.Cut(header, " ")
	if !ok || !strings.EqualFold(prefix, "Bearer") || token == "" {
		return models.User{}, ErrUnauthenticated
	}
	return a.ValidateToken(token, tokenTypeAccess, "")
}

func (a HMACTokenAuthenticator) ValidateRefreshToken(token string) (models.User, error) {
	return a.ValidateToken(strings.TrimSpace(token), tokenTypeRefresh)
}

func (a HMACTokenAuthenticator) ValidateToken(token string, allowedTypes ...string) (models.User, error) {
	if token == "" || len(a.Secret) < 32 {
		return models.User{}, ErrUnauthenticated
	}
	payload, signature, ok := strings.Cut(token, ".")
	if !ok {
		return models.User{}, ErrUnauthenticated
	}
	expected := hmac.New(sha256.New, a.Secret)
	_, _ = expected.Write([]byte(payload))
	provided, err := base64.RawURLEncoding.DecodeString(signature)
	if err != nil || !hmac.Equal(expected.Sum(nil), provided) {
		return models.User{}, ErrUnauthenticated
	}
	encodedClaims, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return models.User{}, ErrUnauthenticated
	}
	var claims tokenClaims
	if err := json.Unmarshal(encodedClaims, &claims); err != nil || claims.Subject == "" {
		return models.User{}, ErrUnauthenticated
	}
	now := time.Now()
	if a.Now != nil {
		now = a.Now()
	}
	if claims.ExpiresAt <= now.Unix() || !tokenTypeAllowed(claims.TokenType, allowedTypes) {
		return models.User{}, ErrUnauthenticated
	}
	return models.User{ID: claims.Subject, Email: claims.Email, CreatedAt: now.UTC()}, nil
}

func tokenTypeAllowed(tokenType string, allowed []string) bool {
	if len(allowed) == 0 {
		return true
	}
	for _, allowedType := range allowed {
		if tokenType == allowedType {
			return true
		}
		if allowedType == "" && tokenType == "" {
			return true
		}
	}
	return false
}
