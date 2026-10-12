package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/framemio/backend/internal/models"
	"github.com/framemio/backend/internal/queue"
	"github.com/framemio/backend/internal/storage"
	"github.com/framemio/backend/internal/store"
)

const testClipToken = "clip-token-with-at-least-thirty-two-characters"

func TestInternalClipVideoRequiresDedicatedToken(t *testing.T) {
	t.Parallel()
	server := newTestServer(store.NewMemoryStore(), queue.NewMemoryPublisher())
	server.ClipGenerationToken = testClipToken
	for _, headers := range []map[string]string{
		nil,
		{"X-FrameMio-Internal-Token": testInternalToken},
		{"X-FrameMio-Clip-Token": "wrong-token"},
	} {
		response := performJSON(t, server.Handler(), http.MethodPost, "/internal/clip-videos", map[string]any{"prompt": "A thoughtful support agent"}, "clip-unauthorized", false, headers)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("create status = %d, body = %s", response.Code, response.Body.String())
		}
	}
	server.ClipGenerationToken = ""
	response := performJSON(t, server.Handler(), http.MethodGet, "/internal/clip-videos/unknown", nil, "", false, map[string]string{"X-FrameMio-Clip-Token": testClipToken})
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("disabled route status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestInternalClipVideoCreatePollAndIdempotency(t *testing.T) {
	t.Parallel()
	server := newTestServer(store.NewMemoryStore(), queue.NewMemoryPublisher())
	server.ClipGenerationToken = testClipToken
	headers := map[string]string{"X-FrameMio-Clip-Token": testClipToken}
	input := map[string]any{"prompt": "A support agent switches between systems", "seconds": 6, "resolution": "720p", "aspect_ratio": "16:9"}
	created := performJSON(t, server.Handler(), http.MethodPost, "/internal/clip-videos", input, "fde-f1-clip-1", false, headers)
	if created.Code != http.StatusAccepted {
		t.Fatalf("create status = %d, body = %s", created.Code, created.Body.String())
	}
	var generation models.TextVideoGeneration
	decodeResponse(t, created, &generation)
	if generation.UserID != clipGenerationUserID || generation.Seconds != 6 || generation.AspectRatio != "16:9" {
		t.Fatalf("unexpected generation: %+v", generation)
	}
	duplicate := performJSON(t, server.Handler(), http.MethodPost, "/internal/clip-videos", input, "fde-f1-clip-1", false, headers)
	if duplicate.Code != http.StatusOK {
		t.Fatalf("duplicate status = %d, body = %s", duplicate.Code, duplicate.Body.String())
	}
	var existing models.TextVideoGeneration
	decodeResponse(t, duplicate, &existing)
	if existing.ID != generation.ID {
		t.Fatalf("duplicate returned a new job: %s != %s", existing.ID, generation.ID)
	}
	polled := performJSON(t, server.Handler(), http.MethodGet, "/internal/clip-videos/"+generation.ID, nil, "", false, headers)
	if polled.Code != http.StatusOK {
		t.Fatalf("poll status = %d, body = %s", polled.Code, polled.Body.String())
	}
	unknown := performJSON(t, server.Handler(), http.MethodGet, "/internal/clip-videos/unknown", nil, "", false, headers)
	if unknown.Code != http.StatusNotFound {
		t.Fatalf("unknown status = %d, body = %s", unknown.Code, unknown.Body.String())
	}
	userLookup := performJSON(t, server.Handler(), http.MethodGet, "/v1/text-videos/"+generation.ID, nil, "", true, nil)
	if userLookup.Code != http.StatusNotFound {
		t.Fatalf("user lookup status = %d, body = %s", userLookup.Code, userLookup.Body.String())
	}
	balance, err := server.Store.GetCreditBalance(context.Background(), clipGenerationUserID)
	if err != nil || balance.Reserved == 0 {
		t.Fatalf("clip account credits were not reserved: %+v, %v", balance, err)
	}
}

func TestInternalClipVideoValidatesInputAndLimitsRequests(t *testing.T) {
	t.Parallel()
	server := newTestServer(store.NewMemoryStore(), queue.NewMemoryPublisher())
	server.ClipGenerationToken = testClipToken
	headers := map[string]string{"X-FrameMio-Clip-Token": testClipToken}
	invalid := performJSON(t, server.Handler(), http.MethodPost, "/internal/clip-videos", map[string]any{"prompt": "A person", "seconds": 16}, "clip-invalid", false, headers)
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid status = %d, body = %s", invalid.Code, invalid.Body.String())
	}
	for index, key := range []string{"clip-one", "clip-two", "clip-three"} {
		response := performJSON(t, server.Handler(), http.MethodPost, "/internal/clip-videos", map[string]any{"prompt": "A person"}, key, false, headers)
		want := http.StatusAccepted
		if index == 2 {
			want = http.StatusTooManyRequests
		}
		if response.Code != want {
			t.Fatalf("request %d status = %d, want %d: %s", index, response.Code, want, response.Body.String())
		}
	}
}

func TestInternalClipVideoRejectsOtherUsersImageAndDoesNotDelete(t *testing.T) {
	t.Parallel()
	memoryStore := store.NewMemoryStore()
	createReadyIOSMediaInStore(t, memoryStore, "private-photo", models.MediaTypePhoto, 1024)
	server := newTestServer(memoryStore, queue.NewMemoryPublisher())
	server.ClipGenerationToken = testClipToken
	headers := map[string]string{"X-FrameMio-Clip-Token": testClipToken}
	response := performJSON(t, server.Handler(), http.MethodPost, "/internal/clip-videos", map[string]any{"prompt": "Animate", "image_media_id": "media-private-photo"}, "clip-private-image", false, headers)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "invalid_image_media") {
		t.Fatalf("image status = %d, body = %s", response.Code, response.Body.String())
	}
	deleteResponse := performJSON(t, server.Handler(), http.MethodDelete, "/internal/clip-videos/anything", nil, "", false, headers)
	if deleteResponse.Code != http.StatusMethodNotAllowed {
		t.Fatalf("delete status = %d, body = %s", deleteResponse.Code, deleteResponse.Body.String())
	}
}

func TestInternalClipVideoImageUploadAndGeneration(t *testing.T) {
	t.Parallel()
	server := newTestServer(store.NewMemoryStore(), queue.NewMemoryPublisher())
	server.ClipGenerationToken = testClipToken
	headers := map[string]string{"X-FrameMio-Clip-Token": testClipToken}
	invalid := performJSON(t, server.Handler(), http.MethodPost, "/internal/clip-media/upload-session", map[string]any{
		"file_name": "source.mp4", "content_type": "video/mp4", "file_size_bytes": 1024, "media_type": "video",
	}, "clip-invalid-upload", false, headers)
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid photo status = %d, body = %s", invalid.Code, invalid.Body.String())
	}
	upload := performJSON(t, server.Handler(), http.MethodPost, "/internal/clip-media/upload-session", map[string]any{
		"file_name": "support-agent.jpg", "content_type": "image/jpeg", "file_size_bytes": 1024, "media_type": "photo", "pixel_width": 1280, "pixel_height": 720,
	}, "clip-image-upload", false, headers)
	if upload.Code != http.StatusCreated {
		t.Fatalf("upload status = %d, body = %s", upload.Code, upload.Body.String())
	}
	var authorized uploadSessionResponse
	decodeResponse(t, upload, &authorized)
	completed := performJSON(t, server.Handler(), http.MethodPost, "/internal/clip-media/complete", map[string]any{
		"media_id": authorized.Media.ID, "upload_id": authorized.Session.ID, "s3_key": authorized.Session.S3Key, "media_type": "photo",
	}, "", false, headers)
	if completed.Code != http.StatusOK {
		t.Fatalf("complete status = %d, body = %s", completed.Code, completed.Body.String())
	}
	generated := performJSON(t, server.Handler(), http.MethodPost, "/internal/clip-videos", map[string]any{
		"prompt": "A thoughtful support agent", "image_media_id": authorized.Media.ID, "aspect_ratio": "16:9",
	}, "clip-image-video", false, headers)
	if generated.Code != http.StatusAccepted {
		t.Fatalf("generation status = %d, body = %s", generated.Code, generated.Body.String())
	}
	var generation models.TextVideoGeneration
	decodeResponse(t, generated, &generation)
	if generation.ImageMediaID != authorized.Media.ID || generation.UserID != clipGenerationUserID {
		t.Fatalf("unexpected generation: %+v", generation)
	}
}

func TestInternalClipVideoContentUsesStoredObject(t *testing.T) {
	t.Parallel()
	objects := &textVideoTestObjectStorage{StaticObjectStorage: storage.StaticObjectStorage{BaseURL: "https://downloads.example.test"}, objects: make(map[string][]byte)}
	server := newTestServerWithStorage(store.NewMemoryStore(), queue.NewMemoryPublisher(), objects)
	server.ClipGenerationToken = testClipToken
	headers := map[string]string{"X-FrameMio-Clip-Token": testClipToken}
	created := performJSON(t, server.Handler(), http.MethodPost, "/internal/clip-videos", map[string]any{"prompt": "A support agent listens"}, "clip-content", false, headers)
	if created.Code != http.StatusAccepted {
		t.Fatalf("create status = %d, body = %s", created.Code, created.Body.String())
	}
	var generation models.TextVideoGeneration
	decodeResponse(t, created, &generation)
	key := textVideoS3Key(clipGenerationUserID, generation.ID)
	if err := objects.PutObject(context.Background(), key, "video/mp4", []byte("video bytes")); err != nil {
		t.Fatal(err)
	}
	if _, err := server.Store.UpdateTextVideoGeneration(context.Background(), generation.ID, "provider-task", models.TextVideoStatusCompleted, 1, "", key, ""); err != nil {
		t.Fatal(err)
	}
	content := performJSON(t, server.Handler(), http.MethodGet, "/internal/clip-videos/"+generation.ID+"/content", nil, "", false, headers)
	if content.Code != http.StatusTemporaryRedirect || content.Header().Get("Location") != "https://downloads.example.test/"+key {
		t.Fatalf("content status = %d, location = %q, body = %s", content.Code, content.Header().Get("Location"), content.Body.String())
	}
}
