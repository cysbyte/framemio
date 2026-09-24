package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/framemio/backend/internal/models"
	"github.com/framemio/backend/internal/queue"
	"github.com/framemio/backend/internal/storage"
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
	balance, err := server.Store.GetCreditBalance(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("get credit balance: %v", err)
	}
	if balance.Available != 60 || balance.Reserved != 0 {
		t.Fatalf("unexpected balance after video generation: %+v", balance)
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

func TestVideoGenerationProxyReleasesCreditsWhenProviderRejectsRequest(t *testing.T) {
	t.Parallel()

	providerClient := roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusBadRequest, `{"error":"invalid_prompt"}`), nil
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
		map[string]any{"prompt": "A quiet ocean at sunset"},
		"ios-video-provider-rejected",
		true,
		nil,
	)
	if response.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}

	balance, err := server.Store.GetCreditBalance(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("get credit balance: %v", err)
	}
	if balance.Available != 100 || balance.Reserved != 0 {
		t.Fatalf("unexpected balance after rejected video generation: %+v", balance)
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

func TestTextVideoCompletionPersistsProviderVideoToObjectStorage(t *testing.T) {
	t.Parallel()

	objects := &textVideoTestObjectStorage{
		StaticObjectStorage: storage.StaticObjectStorage{BaseURL: "https://downloads.example.test"},
		objects:             make(map[string][]byte),
	}
	providerClient := roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		switch {
		case request.Method == http.MethodPost && request.URL.Path == "/v1/videos":
			return jsonResponse(http.StatusOK, `{"id":"provider-job","status":"queued"}`), nil
		case request.Method == http.MethodGet && request.URL.Path == "/v1/videos/provider-job":
			return jsonResponse(http.StatusOK, `{"id":"provider-job","status":"completed","video_url":"https://provider.example/video.mp4"}`), nil
		case request.Method == http.MethodGet && request.URL.Path == "/video.mp4":
			return &http.Response{
				StatusCode: http.StatusOK,
				Status:     "200 OK",
				Header:     http.Header{"Content-Type": []string{"video/mp4"}},
				Body:       io.NopCloser(strings.NewReader("video-bytes")),
			}, nil
		default:
			t.Fatalf("unexpected provider request: %s %s", request.Method, request.URL.String())
			return nil, nil
		}
	})

	server := newTestServerWithStorage(store.NewMemoryStore(), queue.NewMemoryPublisher(), objects)
	server.VideoProviderBaseURL = "https://provider.example"
	server.VideoProviderAPIKey = "provider-key"
	server.VideoProviderHTTPClient = &http.Client{Transport: providerClient}

	createResponse := performJSON(
		t,
		server.Handler(),
		http.MethodPost,
		"/v1/text-videos",
		map[string]any{"prompt": "A cinematic ocean sunset"},
		"text-video-storage-test",
		true,
		nil,
	)
	if createResponse.Code != http.StatusAccepted {
		t.Fatalf("create status = %d, body = %s", createResponse.Code, createResponse.Body.String())
	}
	var generation models.TextVideoGeneration
	decodeResponse(t, createResponse, &generation)

	if err := server.advanceTextVideo(context.Background(), generation); err != nil {
		t.Fatalf("submit text video: %v", err)
	}
	generation, err := server.Store.GetTextVideoGeneration(context.Background(), "user-1", generation.ID)
	if err != nil {
		t.Fatalf("get submitted generation: %v", err)
	}
	if err := server.advanceTextVideo(context.Background(), generation); err != nil {
		t.Fatalf("complete text video: %v", err)
	}
	generation, err = server.Store.GetTextVideoGeneration(context.Background(), "user-1", generation.ID)
	if err != nil {
		t.Fatalf("get completed generation: %v", err)
	}
	if generation.Status != models.TextVideoStatusCompleted {
		t.Fatalf("status = %q, want completed", generation.Status)
	}
	if generation.S3Key == "" {
		t.Fatal("completed generation did not persist an S3 key")
	}
	if got := string(objects.object(generation.S3Key)); got != "video-bytes" {
		t.Fatalf("stored video = %q, want video-bytes", got)
	}

	contentResponse := performJSON(
		t,
		server.Handler(),
		http.MethodGet,
		"/v1/text-videos/"+generation.ID+"/content",
		nil,
		"",
		true,
		nil,
	)
	if contentResponse.Code != http.StatusTemporaryRedirect {
		t.Fatalf("content status = %d, body = %s", contentResponse.Code, contentResponse.Body.String())
	}
	if got := contentResponse.Header().Get("Location"); got != "https://downloads.example.test/"+generation.S3Key {
		t.Fatalf("content location = %q", got)
	}

	deleteResponse := performJSON(
		t,
		server.Handler(),
		http.MethodDelete,
		"/v1/text-videos/"+generation.ID,
		nil,
		"",
		true,
		nil,
	)
	if deleteResponse.Code != http.StatusOK {
		t.Fatalf("delete status = %d, body = %s", deleteResponse.Code, deleteResponse.Body.String())
	}
	if len(objects.object(generation.S3Key)) != 0 {
		t.Fatalf("stored video still exists after deletion")
	}
	if _, err := server.Store.GetTextVideoGeneration(context.Background(), "user-1", generation.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("get deleted generation error = %v, want not found", err)
	}
}

func TestImageToVideoResolvesOwnedMediaAndForwardsSignedImageURL(t *testing.T) {
	t.Parallel()

	memoryStore := store.NewMemoryStore()
	createReadyIOSMediaInStore(t, memoryStore, "source-photo", models.MediaTypePhoto, 1_024)
	providerClient := roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodPost || request.URL.Path != "/v1/videos" {
			t.Fatalf("unexpected provider request: %s %s", request.Method, request.URL.Path)
		}

		var payload map[string]any
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatalf("decode provider payload: %v", err)
		}
		images, ok := payload["images"].([]any)
		if !ok || len(images) != 1 || images[0] != "https://uploads.example.test/users/user-1/media/media-source-photo" {
			t.Fatalf("unexpected provider images: %#v", payload["images"])
		}
		return jsonResponse(http.StatusOK, `{"id":"image-video-job","status":"queued"}`), nil
	})

	server := newTestServer(memoryStore, queue.NewMemoryPublisher())
	server.VideoProviderBaseURL = "https://provider.example"
	server.VideoProviderAPIKey = "provider-key"
	server.VideoProviderHTTPClient = &http.Client{Transport: providerClient}

	response := performJSON(
		t,
		server.Handler(),
		http.MethodPost,
		"/v1/text-videos",
		map[string]any{
			"prompt":         "Slowly push toward the subject and add gentle movement",
			"image_media_id": "media-source-photo",
		},
		"image-video-request",
		true,
		nil,
	)
	if response.Code != http.StatusAccepted {
		t.Fatalf("create status = %d, body = %s", response.Code, response.Body.String())
	}

	var generation models.TextVideoGeneration
	decodeResponse(t, response, &generation)
	if generation.ImageMediaID != "media-source-photo" {
		t.Fatalf("image media id = %q", generation.ImageMediaID)
	}
	if err := server.advanceTextVideo(context.Background(), generation); err != nil {
		t.Fatalf("advance image video: %v", err)
	}
}

func TestImageToVideoRejectsMissingOrNonOwnedMedia(t *testing.T) {
	t.Parallel()

	server := newTestServer(store.NewMemoryStore(), queue.NewMemoryPublisher())
	response := performJSON(
		t,
		server.Handler(),
		http.MethodPost,
		"/v1/text-videos",
		map[string]any{
			"prompt":         "Animate this photo",
			"image_media_id": "media-does-not-exist",
		},
		"image-video-invalid-media",
		true,
		nil,
	)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"invalid_image_media"`) {
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

type textVideoTestObjectStorage struct {
	storage.StaticObjectStorage
	mu      sync.Mutex
	objects map[string][]byte
}

func (s *textVideoTestObjectStorage) PutObject(_ context.Context, key, _ string, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.objects[key] = append([]byte(nil), data...)
	return nil
}

func (s *textVideoTestObjectStorage) DeleteObject(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.objects, key)
	return nil
}

func (s *textVideoTestObjectStorage) object(key string) []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]byte(nil), s.objects[key]...)
}
