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

type Authenticator interface {
	Authenticate(ctx context.Context, request *http.Request) (models.User, error)
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
}

func (a HMACTokenAuthenticator) IssueToken(user models.User, expiresAt time.Time) (string, error) {
	if user.ID == "" || len(a.Secret) < 32 {
		return "", ErrUnauthenticated
	}
	payload, err := json.Marshal(tokenClaims{Subject: user.ID, Email: user.Email, ExpiresAt: expiresAt.Unix()})
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
	if !ok || !strings.EqualFold(prefix, "Bearer") || token == "" || len(a.Secret) < 32 {
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
	if claims.ExpiresAt <= now.Unix() {
		return models.User{}, ErrUnauthenticated
	}
	return models.User{ID: claims.Subject, Email: claims.Email, CreatedAt: now.UTC()}, nil
}
