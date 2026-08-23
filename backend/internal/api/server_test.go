package api

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/framemio/backend/internal/models"
	"github.com/framemio/backend/internal/queue"
	"github.com/framemio/backend/internal/storage"
	"github.com/framemio/backend/internal/store"
)

const testInternalToken = "internal-token-with-at-least-thirty-two-characters"

func TestMediaProjectAndJobFlowIsIdempotent(t *testing.T) {
	t.Parallel()

	memoryStore := store.NewMemoryStore()
	publisher := queue.NewMemoryPublisher()
	server := newTestServer(memoryStore, publisher)
	handler := server.Handler()

	uploadBody := map[string]any{
		"file_name":       "birthday.mov",
		"content_type":    "video/quicktime",
		"file_size_bytes": 1_048_576,
		"media_type":      "video",
		"duration":        42.3,
		"pixel_width":     1920,
		"pixel_height":    1080,
		"orientation":     "landscape",
	}
	firstUpload := performJSON(t, handler, http.MethodPost, "/media/upload-session", uploadBody, "upload-key", true, nil)
	if firstUpload.Code != http.StatusCreated {
		t.Fatalf("create upload status = %d, body = %s", firstUpload.Code, firstUpload.Body.String())
	}
	var uploadResponse uploadSessionResponse
	decodeResponse(t, firstUpload, &uploadResponse)

	duplicateUpload := performJSON(t, handler, http.MethodPost, "/media/upload-session", uploadBody, "upload-key", true, nil)
	if duplicateUpload.Code != http.StatusOK {
		t.Fatalf("duplicate upload status = %d, body = %s", duplicateUpload.Code, duplicateUpload.Body.String())
	}
	var duplicateUploadResponse uploadSessionResponse
	decodeResponse(t, duplicateUpload, &duplicateUploadResponse)
	if duplicateUploadResponse.Media.ID != uploadResponse.Media.ID || duplicateUploadResponse.Session.ID != uploadResponse.Session.ID {
		t.Fatal("idempotent upload request created a duplicate media record")
	}

	completion := performJSON(t, handler, http.MethodPost, "/media", map[string]any{
		"media_id":   uploadResponse.Media.ID,
		"upload_id":  uploadResponse.Session.ID,
		"s3_key":     uploadResponse.Session.S3Key,
		"media_type": "video",
		"duration":   42.3,
	}, "", true, nil)
	if completion.Code != http.StatusOK {
		t.Fatalf("complete media status = %d, body = %s", completion.Code, completion.Body.String())
	}

	projectBody := map[string]any{
		"title":     "Birthday weekend",
		"context":   "birthday",
		"media_ids": []string{uploadResponse.Media.ID},
	}
	projectResponse := performJSON(t, handler, http.MethodPost, "/projects", projectBody, "project-key", true, nil)
	if projectResponse.Code != http.StatusCreated {
		t.Fatalf("create project status = %d, body = %s", projectResponse.Code, projectResponse.Body.String())
	}
	var project models.StoryProject
	decodeResponse(t, projectResponse, &project)

	duplicateProject := performJSON(t, handler, http.MethodPost, "/projects", projectBody, "project-key", true, nil)
	if duplicateProject.Code != http.StatusOK {
		t.Fatalf("duplicate project status = %d", duplicateProject.Code)
	}
	var duplicateProjectRecord models.StoryProject
	decodeResponse(t, duplicateProject, &duplicateProjectRecord)
	if duplicateProjectRecord.ID != project.ID {
		t.Fatal("idempotent project request created a duplicate project")
	}

	analysisResponse := performJSON(t, handler, http.MethodPost, "/projects/"+project.ID+"/analyze", nil, "analysis-key", true, nil)
	if analysisResponse.Code != http.StatusAccepted {
		t.Fatalf("analysis status = %d, body = %s", analysisResponse.Code, analysisResponse.Body.String())
	}
	var analysisJobs jobsResponse
	decodeResponse(t, analysisResponse, &analysisJobs)
	if len(analysisJobs.Jobs) != 1 || analysisJobs.JobID == "" {
		t.Fatalf("unexpected analysis jobs: %+v", analysisJobs)
	}

	duplicateAnalysis := performJSON(t, handler, http.MethodPost, "/projects/"+project.ID+"/analyze", nil, "analysis-key", true, nil)
	var duplicateAnalysisJobs jobsResponse
	decodeResponse(t, duplicateAnalysis, &duplicateAnalysisJobs)
	if duplicateAnalysisJobs.JobID != analysisJobs.JobID {
		t.Fatal("idempotent analysis request created a duplicate job")
	}
	if len(publisher.Messages()) != 1 {
		t.Fatalf("published analysis messages = %d, want 1", len(publisher.Messages()))
	}
	assertSmallIdentifierOnlyMessage(t, publisher.Messages()[0])

	statusUpdate := performJSON(t, handler, http.MethodPost, "/internal/jobs/"+analysisJobs.JobID+"/status", map[string]any{
		"status":   "running",
		"progress": 0.42,
	}, "", false, map[string]string{"X-FrameMio-Internal-Token": testInternalToken})
	if statusUpdate.Code != http.StatusOK {
		t.Fatalf("status update = %d, body = %s", statusUpdate.Code, statusUpdate.Body.String())
	}

	projectStatus := performJSON(t, handler, http.MethodGet, "/projects/"+project.ID+"/status", nil, "", true, nil)
	if projectStatus.Code != http.StatusOK {
		t.Fatalf("project status = %d, body = %s", projectStatus.Code, projectStatus.Body.String())
	}
	var statusBody struct {
		Project models.StoryProject    `json:"project"`
		Jobs    []models.ProcessingJob `json:"jobs"`
	}
	decodeResponse(t, projectStatus, &statusBody)
	if len(statusBody.Jobs) != 1 || statusBody.Jobs[0].Progress != 0.42 || statusBody.Jobs[0].Status != models.JobStatusRunning {
		t.Fatalf("unexpected project job status: %+v", statusBody.Jobs)
	}

	videoAnalysis := models.VideoAnalysis{
		ID:              "video-analysis-1",
		VideoID:         uploadResponse.Media.ID,
		ProjectID:       project.ID,
		AnalysisVersion: "video-analysis-v1",
		DurationMicros:  42_300_000,
		Transcript:      models.VideoTranscript{HasSpeech: true},
		QualitySignals:  models.VideoQualitySignals{OverallScore: 0.9},
		CreatedAt:       time.Now().UTC(),
	}
	if err := memoryStore.SaveVideoAnalysis(context.Background(), "user-1", videoAnalysis); err != nil {
		t.Fatal(err)
	}
	plan := models.StoryPlan{
		SchemaVersion:       models.StoryPlanSchemaVersionV1,
		ID:                  "plan-1",
		ProjectID:           project.ID,
		Version:             1,
		RequestedDurationMS: 4000,
		StoryType:           models.StoryTypeToday,
		VisualStyle:         models.VisualStyleCinematic,
		TitleText:           "Your moments",
		EndingText:          "A story made from the moments that mattered most.",
		MusicMood:           "calm and reflective",
		Moments: []models.StoryMoment{{
			MomentID:             "moment-1",
			AssetID:              uploadResponse.Media.ID,
			MediaType:            models.MediaTypeVideo,
			VideoAnalysisVersion: videoAnalysis.AnalysisVersion,
			StartMicros:          1_000_000,
			EndMicros:            4_000_000,
			DurationMS:           4000,
			CropIntent:           "fill",
			Transition:           "cut",
		}},
		Audio:           &models.StoryAudio{Source: "adaptive", Mood: "calm and reflective"},
		Output:          models.StoryOutput{AspectRatio: "9:16", MaxDurationS: 60, Preset: "social"},
		TotalDurationMS: 4000,
		CreatedAt:       time.Now().UTC(),
	}
	if err := memoryStore.SaveStoryPlan(context.Background(), "user-1", plan); err != nil {
		t.Fatal(err)
	}

	renderResponse := performJSON(t, handler, http.MethodPost, "/projects/"+project.ID+"/render", map[string]any{"output_preset": "social"}, "render-key", true, nil)
	if renderResponse.Code != http.StatusAccepted {
		t.Fatalf("render status = %d, body = %s", renderResponse.Code, renderResponse.Body.String())
	}
	var renderJobs jobsResponse
	decodeResponse(t, renderResponse, &renderJobs)
	duplicateRender := performJSON(t, handler, http.MethodPost, "/projects/"+project.ID+"/render", map[string]any{"output_preset": "social"}, "render-key", true, nil)
	var duplicateRenderJobs jobsResponse
	decodeResponse(t, duplicateRender, &duplicateRenderJobs)
	if renderJobs.JobID != duplicateRenderJobs.JobID {
		t.Fatal("idempotent render request created a duplicate job")
	}
	if len(publisher.Messages()) != 2 {
		t.Fatalf("published messages = %d, want analysis + render", len(publisher.Messages()))
	}
	assertSmallIdentifierOnlyMessage(t, publisher.Messages()[1])

	renderStatus := performJSON(t, handler, http.MethodPost, "/internal/jobs/"+renderJobs.JobID+"/status", map[string]any{
		"status":                "succeeded",
		"progress":              1,
		"artifact_s3_key":       "users/user-1/projects/" + project.ID + "/story.mp4",
		"artifact_content_type": "video/mp4",
	}, "", false, map[string]string{"X-FrameMio-Internal-Token": testInternalToken})
	if renderStatus.Code != http.StatusOK {
		t.Fatalf("render status = %d, body = %s", renderStatus.Code, renderStatus.Body.String())
	}
	var renderedJob models.ProcessingJob
	decodeResponse(t, renderStatus, &renderedJob)
	if renderedJob.ArtifactS3Key == "" || renderedJob.Status != models.JobStatusSucceeded {
		t.Fatalf("render artifact was not recorded: %+v", renderedJob)
	}

	regression := performJSON(t, handler, http.MethodPost, "/internal/jobs/"+renderJobs.JobID+"/status", map[string]any{
		"status": "running", "progress": 0.5,
	}, "", false, map[string]string{"X-FrameMio-Internal-Token": testInternalToken})
	if regression.Code != http.StatusConflict {
		t.Fatalf("terminal job regression status = %d", regression.Code)
	}

	storyResponse := performJSON(t, handler, http.MethodGet, "/projects/"+project.ID+"/story", nil, "", true, nil)
	if storyResponse.Code != http.StatusOK {
		t.Fatalf("story status = %d, body = %s", storyResponse.Code, storyResponse.Body.String())
	}
	var returnedPlan models.StoryPlan
	decodeResponse(t, storyResponse, &returnedPlan)
	if returnedPlan.ID != plan.ID {
		t.Fatalf("story plan id = %s, want %s", returnedPlan.ID, plan.ID)
	}
	finalStatus := performJSON(t, handler, http.MethodGet, "/projects/"+project.ID+"/status", nil, "", true, nil)
	var finalStatusBody struct {
		Project models.StoryProject `json:"project"`
	}
	decodeResponse(t, finalStatus, &finalStatusBody)
	if finalStatusBody.Project.Status != models.ProjectStatusReady {
		t.Fatalf("project status = %s, want ready", finalStatusBody.Project.Status)
	}
}

func TestMediaCompletionRetryDoesNotFinalizeObjectTwice(t *testing.T) {
	t.Parallel()
	memoryStore := store.NewMemoryStore()
	objects := &countingObjectStorage{base: storage.StaticObjectStorage{
		BaseURL: "https://uploads.example.test", PartSizeBytes: 5 * 1024 * 1024, MultipartThreshold: 10 * 1024 * 1024,
	}}
	handler := newTestServerWithStorage(memoryStore, queue.NewMemoryPublisher(), objects).Handler()

	upload := performJSON(t, handler, http.MethodPost, "/media/upload-session", map[string]any{
		"file_name": "photo.jpg", "content_type": "image/jpeg", "file_size_bytes": 100, "media_type": "photo",
	}, "retryable-upload", true, nil)
	if upload.Code != http.StatusCreated {
		t.Fatalf("upload status = %d, body = %s", upload.Code, upload.Body.String())
	}
	var created uploadSessionResponse
	decodeResponse(t, upload, &created)
	completionBody := map[string]any{"media_id": created.Media.ID, "upload_id": created.Session.ID, "s3_key": created.Session.S3Key, "media_type": "photo"}
	for attempt := 0; attempt < 2; attempt++ {
		completed := performJSON(t, handler, http.MethodPost, "/media", completionBody, "", true, nil)
		if completed.Code != http.StatusOK {
			t.Fatalf("completion attempt %d = %d, body = %s", attempt+1, completed.Code, completed.Body.String())
		}
	}
	if objects.CompletionCount() != 1 {
		t.Fatalf("object completion count = %d, want 1", objects.CompletionCount())
	}
}

func TestWorkerCallbacksPersistResultsIdempotently(t *testing.T) {
	t.Parallel()
	memoryStore := store.NewMemoryStore()
	now := time.Date(2026, time.August, 19, 10, 0, 0, 0, time.UTC)
	media := models.MediaAsset{ID: "media-worker", UserID: "user-1", Type: models.MediaTypePhoto, Status: models.MediaStatusReady, FileName: "moment.jpg", ContentType: "image/jpeg", FileSizeBytes: 100, S3Key: "users/user-1/media/media-worker", UploadID: "upload-worker", CreatedAt: now, UpdatedAt: now}
	session := models.UploadSession{ID: "upload-worker", UserID: "user-1", MediaID: media.ID, S3Key: media.S3Key, Strategy: models.UploadStrategySingle, CreatedAt: now}
	if err := memoryStore.CreateMediaUpload(context.Background(), media, session); err != nil {
		t.Fatal(err)
	}
	project := models.StoryProject{ID: "project-worker", UserID: "user-1", MediaIDs: []string{media.ID}, Status: models.ProjectStatusAnalyzing, CreatedAt: now, UpdatedAt: now}
	if _, _, err := memoryStore.CreateProject(context.Background(), project, "project-worker-key"); err != nil {
		t.Fatal(err)
	}
	job := models.ProcessingJob{ID: "job-worker", UserID: "user-1", ProjectID: project.ID, MediaID: media.ID, Type: models.JobTypeAnalysis, Status: models.JobStatusRunning, CreatedAt: now, UpdatedAt: now}
	if _, _, err := memoryStore.CreateJob(context.Background(), job, "job-worker-key"); err != nil {
		t.Fatal(err)
	}
	handler := newTestServer(memoryStore, queue.NewMemoryPublisher()).Handler()
	headers := map[string]string{"X-FrameMio-Internal-Token": testInternalToken}

	analysis := performJSON(t, handler, http.MethodPost, "/internal/jobs/job-worker/analysis", map[string]any{
		"labels": []string{"birthday", "friends"}, "people_count": 3, "quality_score": 0.91,
	}, "", false, headers)
	if analysis.Code != http.StatusOK {
		t.Fatalf("analysis callback = %d, body = %s", analysis.Code, analysis.Body.String())
	}

	storyPlanBody := map[string]any{
		"id": "plan-worker", "project_id": "ignored-client-project", "version": 1,
		"moments": []map[string]any{{"asset_id": media.ID, "duration_ms": 4000}},
		"output":  map[string]any{"aspect_ratio": "9:16", "max_duration_seconds": 60, "preset": "social"},
	}
	status := performJSON(t, handler, http.MethodGet, "/projects/"+project.ID+"/status", nil, "", true, nil)
	var statusBody struct {
		Project models.StoryProject    `json:"project"`
		Jobs    []models.ProcessingJob `json:"jobs"`
	}
	decodeResponse(t, status, &statusBody)
	if statusBody.Project.Status != models.ProjectStatusAnalyzing || len(statusBody.Jobs) != 2 {
		t.Fatalf("worker results did not queue story planning: %+v", statusBody)
	}
	var storyPlanJobID string
	for _, queuedJob := range statusBody.Jobs {
		if queuedJob.Type == models.JobTypeStoryPlan {
			storyPlanJobID = queuedJob.ID
		}
	}
	if storyPlanJobID == "" {
		t.Fatalf("missing queued story plan job: %+v", statusBody.Jobs)
	}

	for attempt := 0; attempt < 2; attempt++ {
		storyPlan := performJSON(t, handler, http.MethodPost, "/internal/jobs/"+storyPlanJobID+"/story-plan", storyPlanBody, "", false, headers)
		if storyPlan.Code != http.StatusOK {
			t.Fatalf("story plan callback attempt %d = %d, body = %s", attempt+1, storyPlan.Code, storyPlan.Body.String())
		}
	}

	story := performJSON(t, handler, http.MethodGet, "/projects/"+project.ID+"/story", nil, "", true, nil)
	var storedPlan models.StoryPlan
	decodeResponse(t, story, &storedPlan)
	if storedPlan.ID != "plan-worker" || storedPlan.ProjectID != project.ID {
		t.Fatalf("unexpected stored plan: %+v", storedPlan)
	}
	status = performJSON(t, handler, http.MethodGet, "/projects/"+project.ID+"/status", nil, "", true, nil)
	decodeResponse(t, status, &statusBody)
	if statusBody.Project.Status != models.ProjectStatusStoryReady || len(statusBody.Jobs) != 2 {
		t.Fatalf("worker results did not update status: %+v", statusBody)
	}
}

func TestIOSUploadContractCompatibility(t *testing.T) {
	t.Parallel()
	handler := newTestServer(store.NewMemoryStore(), queue.NewMemoryPublisher()).Handler()
	create := performJSON(t, handler, http.MethodPost, "/v1/media/upload-sessions", map[string]any{
		"idempotencyKey": "media-upload-original-asset-1",
		"metadata": map[string]any{
			"assetID": "asset-1", "mediaType": "photo", "contentType": "image/jpeg", "fileName": "IMG_0001.JPG",
			"fileSizeBytes": 100, "pixelWidth": 4032, "pixelHeight": 3024, "orientation": "landscape",
			"optimization": "original", "preferredPartSizeBytes": 8 * 1024 * 1024,
		},
	}, "", true, nil)
	if create.Code != http.StatusCreated {
		t.Fatalf("iOS create status = %d, body = %s", create.Code, create.Body.String())
	}
	var session iosUploadSessionResponse
	decodeResponse(t, create, &session)
	if session.UploadID == "" || session.MediaID == "" || len(session.Parts) != 1 || session.Parts[0].PresignedURL == "" || session.Parts[0].Status != "pending" {
		t.Fatalf("invalid iOS upload session: %+v", session)
	}

	notify := performJSON(t, handler, http.MethodPost, "/v1/media/uploads/complete", map[string]any{
		"mediaId": session.MediaID, "uploadId": session.UploadID, "s3Key": session.S3Key, "mediaType": "photo",
	}, "", true, nil)
	if notify.Code != http.StatusOK {
		t.Fatalf("iOS completion status = %d, body = %s", notify.Code, notify.Body.String())
	}
}

func TestIOSStoriesCompatibilityCreatesProjectAndFetchesStories(t *testing.T) {
	t.Parallel()

	memoryStore := store.NewMemoryStore()
	publisher := queue.NewMemoryPublisher()
	handler := newTestServer(memoryStore, publisher).Handler()

	uploadResponse := createReadyIOSMedia(t, handler, "asset-photo-1", models.MediaTypePhoto)
	storyResponse := performJSON(t, handler, http.MethodPost, "/v1/stories", map[string]any{
		"context": "Special moments",
		"assets": []map[string]any{
			{
				"id":           "asset-photo-1",
				"kind":         "image",
				"duration":     0,
				"pixelWidth":   3024,
				"pixelHeight":  4032,
				"creationDate": "2026-08-19T10:00:00Z",
			},
		},
		"analyses": []map[string]any{
			{"assetID": "asset-photo-1", "labels": []string{"beach", "friends"}, "peopleCount": 2, "confidence": 0.91},
		},
	}, "", true, nil)
	if storyResponse.Code != http.StatusCreated {
		t.Fatalf("create iOS story status = %d, body = %s", storyResponse.Code, storyResponse.Body.String())
	}
	var story iosStory
	decodeResponse(t, storyResponse, &story)
	if story.ID == "" || story.Context != "Special moments" || story.Status != "analyzing" || len(story.AssetIDs) != 1 || story.AssetIDs[0] != uploadResponse.MediaID {
		t.Fatalf("unexpected iOS story response: %+v", story)
	}

	listResponse := performJSON(t, handler, http.MethodGet, "/v1/stories", nil, "", true, nil)
	if listResponse.Code != http.StatusOK {
		t.Fatalf("list iOS stories status = %d, body = %s", listResponse.Code, listResponse.Body.String())
	}
	var stories []iosStory
	decodeResponse(t, listResponse, &stories)
	if len(stories) != 1 || stories[0].ID != story.ID {
		t.Fatalf("unexpected iOS story list: %+v", stories)
	}

	getResponse := performJSON(t, handler, http.MethodGet, "/v1/stories/"+story.ID, nil, "", true, nil)
	if getResponse.Code != http.StatusOK {
		t.Fatalf("get iOS story status = %d, body = %s", getResponse.Code, getResponse.Body.String())
	}
	var fetched iosStory
	decodeResponse(t, getResponse, &fetched)
	if fetched.ID != story.ID || fetched.Status != "analyzing" {
		t.Fatalf("unexpected fetched iOS story: %+v", fetched)
	}

	messages := publisher.Messages()
	if len(messages) != 1 || messages[0].JobType != models.JobTypeStoryPlan {
		t.Fatalf("expected only a story-plan queue message for photo-only story, got %+v", messages)
	}
}

func TestAuthenticationAndIdempotencyAreRequired(t *testing.T) {
	t.Parallel()
	server := newTestServer(store.NewMemoryStore(), queue.NewMemoryPublisher())
	handler := server.Handler()

	unauthorized := performJSON(t, handler, http.MethodPost, "/projects", map[string]any{"media_ids": []string{"media-1"}}, "project-key", false, nil)
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d", unauthorized.Code)
	}

	missingIdempotency := performJSON(t, handler, http.MethodPost, "/media/upload-session", map[string]any{
		"file_name": "photo.jpg", "content_type": "image/jpeg", "file_size_bytes": 100, "media_type": "photo",
	}, "", true, nil)
	if missingIdempotency.Code != http.StatusBadRequest {
		t.Fatalf("missing idempotency status = %d", missingIdempotency.Code)
	}
}

func TestHMACTokenAuthenticator(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 19, 10, 0, 0, 0, time.UTC)
	secret := []byte("a-secret-with-at-least-thirty-two-characters")
	payload, err := json.Marshal(tokenClaims{Subject: "user-123", Email: "person@example.com", ExpiresAt: now.Add(time.Hour).Unix()})
	if err != nil {
		t.Fatal(err)
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(encoded))
	token := encoded + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	request := httptest.NewRequest(http.MethodGet, "/media/media-1", nil)
	request.Header.Set("Authorization", "Bearer "+token)

	user, err := (HMACTokenAuthenticator{Secret: secret, Now: func() time.Time { return now }}).Authenticate(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if user.ID != "user-123" || user.Email != "person@example.com" {
		t.Fatalf("unexpected authenticated user: %+v", user)
	}
}

func newTestServer(memoryStore *store.MemoryStore, publisher *queue.MemoryPublisher) *Server {
	return newTestServerWithStorage(memoryStore, publisher, storage.StaticObjectStorage{
		BaseURL: "https://uploads.example.test", PartSizeBytes: 5 * 1024 * 1024, MultipartThreshold: 10 * 1024 * 1024,
	})
}

func newTestServerWithStorage(memoryStore *store.MemoryStore, publisher *queue.MemoryPublisher, objects storage.ObjectStorage) *Server {
	return NewServer(Server{
		Store:         memoryStore,
		ObjectStorage: objects,
		Publisher:     publisher,
		Authenticator: StaticTokenAuthenticator{Token: "test-token", UserID: "user-1", Email: "user@example.com"},
		InternalToken: testInternalToken,
		Logger:        slog.New(slog.NewJSONHandler(io.Discard, nil)),
		Now:           func() time.Time { return time.Date(2026, time.August, 19, 10, 0, 0, 0, time.UTC) },
	})
}

type countingObjectStorage struct {
	mu          sync.Mutex
	base        storage.StaticObjectStorage
	completions int
}

func (s *countingObjectStorage) AuthorizeUpload(ctx context.Context, request storage.UploadAuthorizationRequest) (models.UploadSession, error) {
	return s.base.AuthorizeUpload(ctx, request)
}

func (s *countingObjectStorage) CompleteUpload(ctx context.Context, session models.UploadSession, parts []models.CompletedUploadPart) (string, error) {
	s.mu.Lock()
	s.completions++
	s.mu.Unlock()
	return s.base.CompleteUpload(ctx, session, parts)
}

func (s *countingObjectStorage) AbortUpload(ctx context.Context, session models.UploadSession) error {
	return s.base.AbortUpload(ctx, session)
}

func (s *countingObjectStorage) ObjectExists(ctx context.Context, key string) (bool, error) {
	return s.base.ObjectExists(ctx, key)
}

func (s *countingObjectStorage) CompletionCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.completions
}

func performJSON(t *testing.T, handler http.Handler, method, path string, body any, idempotencyKey string, authenticated bool, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var requestBody io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		requestBody = bytes.NewReader(encoded)
	}
	request := httptest.NewRequest(method, path, requestBody)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if authenticated {
		request.Header.Set("Authorization", "Bearer test-token")
	}
	if idempotencyKey != "" {
		request.Header.Set("Idempotency-Key", idempotencyKey)
	}
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func decodeResponse(t *testing.T, response *httptest.ResponseRecorder, destination any) {
	t.Helper()
	if err := json.NewDecoder(response.Body).Decode(destination); err != nil {
		t.Fatalf("decode response: %v, body = %s", err, response.Body.String())
	}
}

func createReadyIOSMedia(t *testing.T, handler http.Handler, assetID string, mediaType models.MediaType) iosUploadSessionResponse {
	t.Helper()
	upload := performJSON(t, handler, http.MethodPost, "/v1/media/upload-sessions", map[string]any{
		"idempotencyKey": "media-upload-original-" + assetID,
		"metadata": map[string]any{
			"assetID":                assetID,
			"mediaType":              mediaType,
			"contentType":            "image/jpeg",
			"fileName":               assetID + ".jpg",
			"fileSizeBytes":          1_024,
			"pixelWidth":             3024,
			"pixelHeight":            4032,
			"orientation":            "portrait",
			"optimization":           "original",
			"preferredPartSizeBytes": 5 * 1024 * 1024,
			"creationDate":           "2026-08-19T10:00:00Z",
		},
	}, "", true, nil)
	if upload.Code != http.StatusCreated {
		t.Fatalf("create iOS upload status = %d, body = %s", upload.Code, upload.Body.String())
	}
	var uploadResponse iosUploadSessionResponse
	decodeResponse(t, upload, &uploadResponse)
	complete := performJSON(t, handler, http.MethodPost, "/v1/media/uploads/complete", map[string]any{
		"mediaId":   uploadResponse.MediaID,
		"uploadId":  uploadResponse.UploadID,
		"s3Key":     uploadResponse.S3Key,
		"mediaType": mediaType,
	}, "", true, nil)
	if complete.Code != http.StatusOK {
		t.Fatalf("complete iOS upload status = %d, body = %s", complete.Code, complete.Body.String())
	}
	return uploadResponse
}

func assertSmallIdentifierOnlyMessage(t *testing.T, message queue.JobMessage) {
	t.Helper()
	encoded, err := json.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) > 1024 {
		t.Fatalf("queue message is unexpectedly large: %d bytes", len(encoded))
	}
	lower := strings.ToLower(string(encoded))
	for _, forbidden := range []string{"video_bytes", "image_bytes", "transcript", "prompt", "base64"} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf("queue message contains forbidden field %q: %s", forbidden, encoded)
		}
	}
	if message.JobID == "" || message.ProjectID == "" || message.JobType == "" {
		t.Fatalf("queue message is missing identifiers: %+v", message)
	}
}
