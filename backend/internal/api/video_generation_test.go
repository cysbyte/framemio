package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/framemio/backend/internal/queue"
	"github.com/framemio/backend/internal/store"
)

func TestVideoGenerationProxyForwardsCreateRequest(t *testing.T) {
	t.Parallel()

	providerClient := roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodPost || request.URL.Path != "/v1/videos" {
			t.Fatalf("unexpected provider request: %s %s", request.Method, request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bearer provider-key" {
			t.Fatalf("provider authorization = %q", request.Header.Get("Authorization"))
		}
		if request.Header.Get("Idempotency-Key") != "ios-video-request" {
			t.Fatalf("provider idempotency key = %q", request.Header.Get("Idempotency-Key"))
		}

		var payload map[string]any
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatalf("decode provider payload: %v", err)
		}
		if payload["prompt"] != "A quiet ocean at sunset" || payload["seconds"] != float64(8) ||
			payload["resolution"] != "720p" || payload["aspect_ratio"] != "16:9" {
			t.Fatalf("unexpected provider payload: %#v", payload)
		}

		return jsonResponse(http.StatusOK, `{"id":"video_123","status":"queued"}`), nil
	})

	server := newTestServer(store.NewMemoryStore(), queue.NewMemoryPublisher())
	server.VideoProviderBaseURL = "https://provider.example"
	server.VideoProviderAPIKey = "provider-key"
	server.VideoProviderHTTPClient = &http.Client{Transport: providerClient}

	response := performJSON(
		t,
		server.Handler(),
		http.MethodPost,
		"/v1/videos",
		map[string]any{
			"prompt":       "A quiet ocean at sunset",
			"seconds":      8,
			"resolution":   "720p",
			"aspect_ratio": "16:9",
		},
		"ios-video-request",
		true,
		nil,
	)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if got := response.Body.String(); !strings.Contains(got, `"video_123"`) {
		t.Fatalf("response = %s", got)
	}
}

func TestVideoGenerationProxyForwardsStatusRequest(t *testing.T) {
	t.Parallel()

	providerClient := roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodGet || request.URL.Path != "/v1/videos/video_123" {
			t.Fatalf("unexpected provider request: %s %s", request.Method, request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bearer provider-key" {
			t.Fatalf("provider authorization = %q", request.Header.Get("Authorization"))
		}
		return jsonResponse(http.StatusOK, `{"id":"video_123","status":"completed","video_url":"https://cdn.example/video.mp4"}`), nil
	})

	server := newTestServer(store.NewMemoryStore(), queue.NewMemoryPublisher())
	server.VideoProviderBaseURL = "https://provider.example"
	server.VideoProviderAPIKey = "provider-key"
	server.VideoProviderHTTPClient = &http.Client{Transport: providerClient}

	response := performJSON(t, server.Handler(), http.MethodGet, "/v1/videos/video_123", nil, "", true, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if got := response.Body.String(); !strings.Contains(got, `"completed"`) {
		t.Fatalf("response = %s", got)
	}
}

func TestVideoGenerationProxyValidatesInput(t *testing.T) {
	t.Parallel()

	server := newTestServer(store.NewMemoryStore(), queue.NewMemoryPublisher())
	server.VideoProviderBaseURL = "https://provider.example"
	server.VideoProviderAPIKey = "provider-key"

	response := performJSON(
		t,
		server.Handler(),
		http.MethodPost,
		"/v1/videos",
		map[string]any{"prompt": " ", "seconds": 20},
		"ios-video-invalid",
		true,
		nil,
	)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"invalid_video_request"`) {
		t.Fatalf("response = %s", response.Body.String())
	}
}

type videoProviderTestServer struct {
	server *http.Server
	URL    string
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (roundTripper roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTripper(request)
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
