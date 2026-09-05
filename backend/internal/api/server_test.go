package api

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
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
		RequestedDurationMS: 3000,
		StoryType:           models.StoryTypeToday,
		VisualStyle:         models.VisualStyleCinematic,
		TitleText:           "Your moments",
		EndingText:          "A story made from the moments that mattered most.",
		Moments: []models.StoryMoment{{
			MomentID:             "moment-1",
			AssetID:              uploadResponse.Media.ID,
			MediaType:            models.MediaTypeVideo,
			VideoAnalysisVersion: videoAnalysis.AnalysisVersion,
			StartMicros:          1_000_000,
			EndMicros:            4_000_000,
			DurationMS:           3000,
			CropIntent:           "fill",
			Transition:           "cut",
		}},
		Output:          models.StoryOutput{AspectRatio: "9:16", MaxDurationS: 60, Preset: "social"},
		TotalDurationMS: 3000,
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

	for attempt := 0; attempt < 2; attempt++ {
		artifactStatus := performJSON(t, handler, http.MethodPost, "/internal/jobs/job-worker/status", map[string]any{
			"status": "COMPLETED", "progress": 1, "artifact_s3_key": "users/user-1/media/media-worker", "artifact_content_type": "image/jpeg",
		}, "", false, headers)
		if artifactStatus.Code != http.StatusOK {
			t.Fatalf("artifact status callback attempt %d = %d, body = %s", attempt+1, artifactStatus.Code, artifactStatus.Body.String())
		}
	}

	storyPlanBody := map[string]any{
		"schema_version":        models.StoryPlanSchemaVersionV1,
		"id":                    "plan-worker",
		"project_id":            "ignored-client-project",
		"version":               1,
		"requested_duration_ms": 4000,
		"title_text":            "Worker story",
		"ending_text":           "The moments that stay with us.",
		"moments":               []map[string]any{{"moment_id": "worker-moment", "asset_id": media.ID, "media_type": "photo", "duration_ms": 4000}},
		"output":                map[string]any{"aspect_ratio": "9:16", "max_duration_seconds": 60, "preset": "social"},
		"total_duration_ms":     4000,
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
	// The repeated story-plan callback above must still leave exactly one
	// validation and one render job: advancing is idempotent.
	if statusBody.Project.Status != models.ProjectStatusRendering || len(statusBody.Jobs) != 4 {
		t.Fatalf("worker results did not update status: %+v", statusBody)
	}
	if countType(statusBody.Jobs, models.JobTypeStoryValidate) != 1 || countType(statusBody.Jobs, models.JobTypeVideoRender) != 1 {
		t.Fatalf("story plan completion should queue one validation and one render, got %+v", statusBody.Jobs)
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
	if session.Completed {
		t.Fatalf("a fresh session must not be reported completed: %+v", session)
	}

	notify := performJSON(t, handler, http.MethodPost, "/v1/media/uploads/complete", map[string]any{
		"mediaId": session.MediaID, "uploadId": session.UploadID, "s3Key": session.S3Key, "mediaType": "photo",
	}, "", true, nil)
	if notify.Code != http.StatusOK {
		t.Fatalf("iOS completion status = %d, body = %s", notify.Code, notify.Body.String())
	}

	// Re-requesting a session for bytes already in S3 must report completion.
	// Presigned URLs are never refreshed for a completed session, so describing
	// its parts as pending work sends the client into an unbreakable retry loop
	// against dead URLs.
	replay := performJSON(t, handler, http.MethodPost, "/v1/media/upload-sessions", map[string]any{
		"idempotencyKey": "media-upload-original-asset-1",
		"metadata": map[string]any{
			"assetID": "asset-1", "mediaType": "photo", "contentType": "image/jpeg", "fileName": "IMG_0001.JPG",
			"fileSizeBytes": 100, "pixelWidth": 4032, "pixelHeight": 3024, "orientation": "landscape",
			"optimization": "original", "preferredPartSizeBytes": 8 * 1024 * 1024,
		},
	}, "", true, nil)
	if replay.Code != http.StatusOK {
		t.Fatalf("replay status = %d, body = %s", replay.Code, replay.Body.String())
	}
	var replayed iosUploadSessionResponse
	decodeResponse(t, replay, &replayed)
	if replayed.UploadID != session.UploadID {
		t.Fatalf("replay returned a different session: %s want %s", replayed.UploadID, session.UploadID)
	}
	if !replayed.Completed {
		t.Fatalf("completed session must report completed: %+v", replayed)
	}
	for _, part := range replayed.Parts {
		if part.Status != "completed" {
			t.Fatalf("completed session part %d status = %s, want completed", part.PartNumber, part.Status)
		}
	}
}

func TestIOSUploadCompletionReturnsConflictForSizeMismatch(t *testing.T) {
	t.Parallel()
	memoryStore := store.NewMemoryStore()
	handler := newTestServerWithStorage(memoryStore, queue.NewMemoryPublisher(), &sizeMismatchObjectStorage{base: storage.StaticObjectStorage{
		BaseURL: "https://uploads.example.test", PartSizeBytes: 5 * 1024 * 1024, MultipartThreshold: 10 * 1024 * 1024,
	}}).Handler()

	upload := performJSON(t, handler, http.MethodPost, "/v1/media/upload-sessions", map[string]any{
		"idempotencyKey": "media-upload-original-stale-size",
		"metadata": map[string]any{
			"assetID": "stale-size", "mediaType": "photo", "contentType": "image/jpeg", "fileName": "IMG_0001.JPG",
			"fileSizeBytes": 100, "pixelWidth": 4032, "pixelHeight": 3024, "orientation": "landscape",
			"optimization": "original", "preferredPartSizeBytes": 8 * 1024 * 1024,
		},
	}, "", true, nil)
	if upload.Code != http.StatusCreated {
		t.Fatalf("create iOS upload status = %d, body = %s", upload.Code, upload.Body.String())
	}
	var session iosUploadSessionResponse
	decodeResponse(t, upload, &session)

	complete := performJSON(t, handler, http.MethodPost, "/v1/media/uploads/complete", map[string]any{
		"mediaId": session.MediaID, "uploadId": session.UploadID, "s3Key": session.S3Key, "mediaType": "photo",
	}, "", true, nil)
	if complete.Code != http.StatusConflict {
		t.Fatalf("size mismatch completion status = %d, body = %s", complete.Code, complete.Body.String())
	}
	if !strings.Contains(complete.Body.String(), "stale_upload_size") {
		t.Fatalf("expected stale_upload_size response, body = %s", complete.Body.String())
	}
}

func TestIOSUploadSessionDetectsMutableSignedHeaders(t *testing.T) {
	t.Parallel()
	unsafe := models.UploadSession{Parts: []models.UploadPart{{UploadURL: "https://s3.example.test/object?X-Amz-SignedHeaders=content-length%3Bcontent-type%3Bhost"}}}
	if !iosUploadSessionUsesMutableSignedHeaders(unsafe) {
		t.Fatal("expected old signed content headers to require refresh")
	}

	safe := models.UploadSession{Parts: []models.UploadPart{{UploadURL: "https://s3.example.test/object?X-Amz-SignedHeaders=host"}}}
	if iosUploadSessionUsesMutableSignedHeaders(safe) {
		t.Fatal("host-only signed headers should not require refresh")
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
	if len(messages) != 1 || messages[0].JobType != models.JobTypeStoryPlan || messages[0].MediaID != "" {
		t.Fatalf("expected immediate story plan queue message for photo-only story, got %+v", messages)
	}
}

func TestIOSStoryIncludesBackendJobProgress(t *testing.T) {
	t.Parallel()

	memoryStore := store.NewMemoryStore()
	publisher := queue.NewMemoryPublisher()
	handler := newTestServer(memoryStore, publisher).Handler()

	uploadResponse := createReadyIOSMedia(t, handler, "asset-progress-video", models.MediaTypeVideo)
	storyResponse := performJSON(t, handler, http.MethodPost, "/v1/stories", map[string]any{
		"context": "Weekend clips",
		"assets": []map[string]any{{
			"id":          "asset-progress-video",
			"kind":        "video",
			"duration":    12,
			"pixelWidth":  1920,
			"pixelHeight": 1080,
		}},
	}, "", true, nil)
	if storyResponse.Code != http.StatusCreated {
		t.Fatalf("create iOS story status = %d, body = %s", storyResponse.Code, storyResponse.Body.String())
	}
	var story iosStory
	decodeResponse(t, storyResponse, &story)

	now := time.Date(2026, time.August, 19, 10, 1, 0, 0, time.UTC)
	if _, _, err := memoryStore.CreateJob(context.Background(), models.ProcessingJob{
		ID:        "job-progress-understand",
		UserID:    "user-1",
		ProjectID: story.ID,
		MediaID:   uploadResponse.MediaID,
		Type:      models.JobTypeVideoUnderstand,
		Status:    models.JobStatusRunning,
		Progress:  0.5,
		CreatedAt: now,
		UpdatedAt: now,
	}, "job-progress-understand"); err != nil {
		t.Fatal(err)
	}

	getResponse := performJSON(t, handler, http.MethodGet, "/v1/stories/"+story.ID, nil, "", true, nil)
	if getResponse.Code != http.StatusOK {
		t.Fatalf("get iOS story status = %d, body = %s", getResponse.Code, getResponse.Body.String())
	}
	var fetched iosStory
	decodeResponse(t, getResponse, &fetched)
	if fetched.ProgressTitle != "Finding the best parts..." {
		t.Fatalf("progress title = %q", fetched.ProgressTitle)
	}
	if !strings.Contains(fetched.ProgressDetail, "Scanning scenes") {
		t.Fatalf("progress detail = %q", fetched.ProgressDetail)
	}
	if fetched.ProgressValue == nil || *fetched.ProgressValue < 0.61 || *fetched.ProgressValue > 0.62 {
		t.Fatalf("progress value = %v", fetched.ProgressValue)
	}
}

func TestIOSStoriesCompatibilityDeletesStory(t *testing.T) {
	t.Parallel()

	memoryStore := store.NewMemoryStore()
	publisher := queue.NewMemoryPublisher()
	handler := newTestServer(memoryStore, publisher).Handler()

	createReadyIOSMedia(t, handler, "asset-delete-photo", models.MediaTypePhoto)
	storyResponse := performJSON(t, handler, http.MethodPost, "/v1/stories", map[string]any{
		"context": "Special moments",
		"assets": []map[string]any{{
			"id":          "asset-delete-photo",
			"kind":        "image",
			"pixelWidth":  3024,
			"pixelHeight": 4032,
		}},
	}, "", true, nil)
	if storyResponse.Code != http.StatusCreated {
		t.Fatalf("create iOS story status = %d, body = %s", storyResponse.Code, storyResponse.Body.String())
	}
	var story iosStory
	decodeResponse(t, storyResponse, &story)

	deleteResponse := performJSON(t, handler, http.MethodDelete, "/v1/stories/"+story.ID, nil, "", true, nil)
	if deleteResponse.Code != http.StatusOK {
		t.Fatalf("delete iOS story status = %d, body = %s", deleteResponse.Code, deleteResponse.Body.String())
	}

	getResponse := performJSON(t, handler, http.MethodGet, "/v1/stories/"+story.ID, nil, "", true, nil)
	if getResponse.Code != http.StatusNotFound {
		t.Fatalf("get deleted iOS story status = %d, body = %s", getResponse.Code, getResponse.Body.String())
	}

	listResponse := performJSON(t, handler, http.MethodGet, "/v1/stories", nil, "", true, nil)
	if listResponse.Code != http.StatusOK {
		t.Fatalf("list iOS stories status = %d, body = %s", listResponse.Code, listResponse.Body.String())
	}
	var stories []iosStory
	decodeResponse(t, listResponse, &stories)
	if len(stories) != 0 {
		t.Fatalf("deleted story still listed: %+v", stories)
	}
}

func TestIOSStoryEditQueuesStoryPlanRevisionWithoutReanalysis(t *testing.T) {
	t.Parallel()

	memoryStore := store.NewMemoryStore()
	publisher := queue.NewMemoryPublisher()
	server := newTestServer(memoryStore, publisher)
	handler := server.Handler()

	first := createReadyIOSMedia(t, handler, "asset-edit-photo-1", models.MediaTypePhoto)
	second := createReadyIOSMedia(t, handler, "asset-edit-photo-2", models.MediaTypePhoto)
	storyResponse := performJSON(t, handler, http.MethodPost, "/v1/stories", map[string]any{
		"context": "Special moments",
		"assets": []map[string]any{
			{"id": "asset-edit-photo-1", "kind": "image", "pixelWidth": 3024, "pixelHeight": 4032},
			{"id": "asset-edit-photo-2", "kind": "image", "pixelWidth": 3024, "pixelHeight": 4032},
		},
	}, "", true, nil)
	if storyResponse.Code != http.StatusCreated {
		t.Fatalf("create iOS story status = %d, body = %s", storyResponse.Code, storyResponse.Body.String())
	}
	var story iosStory
	decodeResponse(t, storyResponse, &story)
	completeProjectPhotoPreprocessJobs(t, server, memoryStore, story.ID)

	now := time.Date(2026, time.August, 19, 10, 0, 0, 0, time.UTC)
	if err := memoryStore.SaveStoryPlan(context.Background(), "user-1", models.StoryPlan{
		SchemaVersion:       models.StoryPlanSchemaVersionV1,
		ID:                  "plan-existing",
		ProjectID:           story.ID,
		Version:             1,
		RequestedDurationMS: 8000,
		StoryType:           models.StoryTypeCustom,
		VisualStyle:         models.VisualStyleNatural,
		TitleText:           "Special moments",
		EndingText:          "The moments that stay with us.",
		Moments: []models.StoryMoment{
			{MomentID: "breakfast", AssetID: first.MediaID, MediaID: first.MediaID, MediaType: models.MediaTypePhoto, Narrative: "Breakfast", DurationMS: 4000, CropIntent: "fit", Transition: "fade_in"},
			{MomentID: "lake", AssetID: second.MediaID, MediaID: second.MediaID, MediaType: models.MediaTypePhoto, Narrative: "Lake", DurationMS: 4000, CropIntent: "fit", Transition: "crossfade"},
		},
		Output:          models.StoryOutput{AspectRatio: "9:16", MaxDurationS: 8, Preset: "premium-memory"},
		TotalDurationMS: 8000,
		CreatedAt:       now,
	}); err != nil {
		t.Fatal(err)
	}

	editResponse := performJSON(t, handler, http.MethodPost, "/v1/stories/"+story.ID+"/versions", map[string]any{
		"baseVersion":      1,
		"removeMomentIds":  []string{"breakfast"},
		"style":            "cinematic",
		"subtitlesEnabled": false,
		"subtitleStyle":    "minimal",
		"targetDuration":   15,
		"instruction":      "Make it quieter.",
	}, "edit-key", true, nil)
	if editResponse.Code != http.StatusAccepted {
		t.Fatalf("edit iOS story status = %d, body = %s", editResponse.Code, editResponse.Body.String())
	}
	var edited iosStory
	decodeResponse(t, editResponse, &edited)
	if edited.Status != "analyzing" {
		t.Fatalf("expected analyzing response, got %+v", edited)
	}

	messages := publisher.Messages()
	last := messages[len(messages)-1]
	if last.JobType != models.JobTypeStoryPlan || last.MediaID != "" {
		t.Fatalf("edit should queue only a project-level story plan job, got %+v", last)
	}
	if last.Configuration["story_plan_version"] != "2" || last.Configuration["visual_style"] != "cinematic" || last.Configuration["subtitles_enabled"] != "false" || last.Configuration["caption_style_preset"] != "clean_minimal" {
		t.Fatalf("unexpected edit config: %+v", last.Configuration)
	}
	if strings.Contains(last.Configuration["selected_media_ids"], first.MediaID) || !strings.Contains(last.Configuration["selected_media_ids"], second.MediaID) {
		t.Fatalf("selected media did not reflect removed moment: %+v", last.Configuration)
	}
}

func TestIOSStoryRevisionRejectsStaleBaseVersion(t *testing.T) {
	t.Parallel()

	memoryStore := store.NewMemoryStore()
	publisher := queue.NewMemoryPublisher()
	server := newTestServer(memoryStore, publisher)
	handler := server.Handler()
	first := createReadyIOSMedia(t, handler, "asset-stale-photo-1", models.MediaTypePhoto)
	second := createReadyIOSMedia(t, handler, "asset-stale-photo-2", models.MediaTypePhoto)
	now := time.Date(2026, time.August, 19, 10, 0, 0, 0, time.UTC)
	project := models.StoryProject{ID: "project-stale-revision", UserID: "user-1", Title: "Stale edit", Context: "Special moments", MediaIDs: []string{first.MediaID, second.MediaID}, Status: models.ProjectStatusReady, CreatedAt: now, UpdatedAt: now}
	if _, _, err := memoryStore.CreateProject(context.Background(), project, "project-stale-revision"); err != nil {
		t.Fatal(err)
	}
	for version := 1; version <= 2; version++ {
		if err := memoryStore.SaveStoryPlan(context.Background(), "user-1", models.StoryPlan{SchemaVersion: models.StoryPlanSchemaVersionV1, ID: fmt.Sprintf("plan-stale-%d", version), ProjectID: project.ID, Version: version, Moments: []models.StoryMoment{{MomentID: "first", AssetID: first.MediaID, MediaID: first.MediaID, MediaType: models.MediaTypePhoto, Narrative: "Beach", DurationMS: 3000}, {MomentID: "second", AssetID: second.MediaID, MediaID: second.MediaID, MediaType: models.MediaTypePhoto, Narrative: "Dinner", DurationMS: 3000}}, Output: models.StoryOutput{AspectRatio: "9:16", MaxDurationS: 8, Preset: "premium-memory"}, TotalDurationMS: 6000, CreatedAt: now.Add(time.Duration(version) * time.Minute)}); err != nil {
			t.Fatal(err)
		}
	}

	response := performJSON(t, handler, http.MethodPost, "/v1/stories/"+project.ID+"/revisions", map[string]any{"baseVersion": 1, "instruction": "Make it shorter."}, "stale-edit-key", true, nil)
	if response.Code != http.StatusConflict {
		t.Fatalf("stale revision status = %d, body = %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "stale_story_plan_version") {
		t.Fatalf("expected stale_story_plan_version, body = %s", response.Body.String())
	}
}

func TestIOSStoryRevisionDuplicateRequestDoesNotDoubleReserveCredits(t *testing.T) {
	t.Parallel()

	memoryStore := store.NewMemoryStore()
	publisher := queue.NewMemoryPublisher()
	server := newTestServer(memoryStore, publisher)
	handler := server.Handler()
	first := createReadyIOSMedia(t, handler, "asset-duplicate-revision-1", models.MediaTypePhoto)
	second := createReadyIOSMedia(t, handler, "asset-duplicate-revision-2", models.MediaTypePhoto)
	now := time.Date(2026, time.August, 19, 10, 0, 0, 0, time.UTC)
	project := models.StoryProject{ID: "project-duplicate-revision", UserID: "user-1", Title: "Duplicate edit", Context: "Special moments", MediaIDs: []string{first.MediaID, second.MediaID}, Status: models.ProjectStatusReady, CreatedAt: now, UpdatedAt: now}
	if _, _, err := memoryStore.CreateProject(context.Background(), project, "project-duplicate-revision"); err != nil {
		t.Fatal(err)
	}
	if err := memoryStore.SaveStoryPlan(context.Background(), "user-1", models.StoryPlan{SchemaVersion: models.StoryPlanSchemaVersionV1, ID: "plan-duplicate-1", ProjectID: project.ID, Version: 1, Moments: []models.StoryMoment{{MomentID: "first", AssetID: first.MediaID, MediaID: first.MediaID, MediaType: models.MediaTypePhoto, Narrative: "Beach", DurationMS: 3000}, {MomentID: "second", AssetID: second.MediaID, MediaID: second.MediaID, MediaType: models.MediaTypePhoto, Narrative: "Dinner", DurationMS: 3000}}, Output: models.StoryOutput{AspectRatio: "9:16", MaxDurationS: 8, Preset: "premium-memory"}, TotalDurationMS: 6000, CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	body := map[string]any{"baseVersion": 1, "instruction": "Remove the dinner photos."}
	firstResponse := performJSON(t, handler, http.MethodPost, "/v1/stories/"+project.ID+"/revisions", body, "duplicate-edit-key", true, nil)
	if firstResponse.Code != http.StatusAccepted {
		t.Fatalf("first revision status = %d, body = %s", firstResponse.Code, firstResponse.Body.String())
	}
	secondResponse := performJSON(t, handler, http.MethodPost, "/v1/stories/"+project.ID+"/revisions", body, "duplicate-edit-key", true, nil)
	if secondResponse.Code != http.StatusAccepted {
		t.Fatalf("duplicate revision status = %d, body = %s", secondResponse.Code, secondResponse.Body.String())
	}
	balance, err := memoryStore.GetCreditBalance(context.Background(), "user-1")
	if err != nil {
		t.Fatal(err)
	}
	if balance.Available != 40 || balance.Reserved != 60 {
		t.Fatalf("duplicate should reserve once for regenerate+AI edit, got %+v", balance)
	}
	if got := len(publisher.Messages()); got != 1 {
		t.Fatalf("duplicate should publish one job message, got %d", got)
	}
}

func TestIOSStoryEditQueuesPreferredMediaOrder(t *testing.T) {
	t.Parallel()

	memoryStore := store.NewMemoryStore()
	publisher := queue.NewMemoryPublisher()
	server := newTestServer(memoryStore, publisher)
	handler := server.Handler()

	first := createReadyIOSMedia(t, handler, "asset-order-photo-1", models.MediaTypePhoto)
	second := createReadyIOSMedia(t, handler, "asset-order-photo-2", models.MediaTypePhoto)
	storyResponse := performJSON(t, handler, http.MethodPost, "/v1/stories", map[string]any{
		"context": "Special moments",
		"assets": []map[string]any{
			{"id": "asset-order-photo-1", "kind": "image", "pixelWidth": 3024, "pixelHeight": 4032},
			{"id": "asset-order-photo-2", "kind": "image", "pixelWidth": 3024, "pixelHeight": 4032},
		},
	}, "", true, nil)
	if storyResponse.Code != http.StatusCreated {
		t.Fatalf("create iOS story status = %d, body = %s", storyResponse.Code, storyResponse.Body.String())
	}
	var story iosStory
	decodeResponse(t, storyResponse, &story)
	completeProjectPhotoPreprocessJobs(t, server, memoryStore, story.ID)

	if err := memoryStore.SaveStoryPlan(context.Background(), "user-1", models.StoryPlan{
		SchemaVersion:       models.StoryPlanSchemaVersionV1,
		ID:                  "plan-order-existing",
		ProjectID:           story.ID,
		Version:             1,
		RequestedDurationMS: 8000,
		StoryType:           models.StoryTypeCustom,
		VisualStyle:         models.VisualStyleNatural,
		TitleText:           "Special moments",
		EndingText:          "The moments that stay with us.",
		Moments: []models.StoryMoment{
			{MomentID: "first", AssetID: first.MediaID, MediaID: first.MediaID, MediaType: models.MediaTypePhoto, Narrative: "First", DurationMS: 4000, CropIntent: "fit", Transition: "fade_in"},
			{MomentID: "second", AssetID: second.MediaID, MediaID: second.MediaID, MediaType: models.MediaTypePhoto, Narrative: "Second", DurationMS: 4000, CropIntent: "fit", Transition: "crossfade"},
		},
		Output:          models.StoryOutput{AspectRatio: "9:16", MaxDurationS: 8, Preset: "premium-memory"},
		TotalDurationMS: 8000,
		CreatedAt:       time.Date(2026, time.August, 29, 10, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatal(err)
	}

	editResponse := performJSON(t, handler, http.MethodPost, "/v1/stories/"+story.ID+"/versions", map[string]any{
		"orderedMediaIds": []string{second.MediaID, first.MediaID},
	}, "edit-order-key", true, nil)
	if editResponse.Code != http.StatusAccepted {
		t.Fatalf("edit iOS story status = %d, body = %s", editResponse.Code, editResponse.Body.String())
	}

	messages := publisher.Messages()
	last := messages[len(messages)-1]
	if last.JobType != models.JobTypeStoryPlan {
		t.Fatalf("reorder should queue a story plan revision, got %+v", last)
	}
	if got, want := last.Configuration["preferred_media_order"], second.MediaID+","+first.MediaID; got != want {
		t.Fatalf("preferred media order not preserved: got %q want %q config=%+v", got, want, last.Configuration)
	}
}

func TestIOSStoryEditRejectsForeignMediaReference(t *testing.T) {
	t.Parallel()

	memoryStore := store.NewMemoryStore()
	publisher := queue.NewMemoryPublisher()
	handler := newTestServer(memoryStore, publisher).Handler()

	createReadyIOSMedia(t, handler, "asset-edit-owned", models.MediaTypePhoto)
	storyResponse := performJSON(t, handler, http.MethodPost, "/v1/stories", map[string]any{
		"context": "Special moments",
		"assets":  []map[string]any{{"id": "asset-edit-owned", "kind": "image", "pixelWidth": 3024, "pixelHeight": 4032}},
	}, "", true, nil)
	if storyResponse.Code != http.StatusCreated {
		t.Fatalf("create iOS story status = %d, body = %s", storyResponse.Code, storyResponse.Body.String())
	}
	var story iosStory
	decodeResponse(t, storyResponse, &story)

	editResponse := performJSON(t, handler, http.MethodPost, "/v1/stories/"+story.ID+"/versions", map[string]any{
		"addMediaIds": []string{"media-not-in-this-project"},
	}, "edit-key", true, nil)
	if editResponse.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid media reference to be rejected, got %d body=%s", editResponse.Code, editResponse.Body.String())
	}
}

func TestIOSStoryEditAddMomentReservesAddAndRegenerateCredits(t *testing.T) {
	t.Parallel()

	memoryStore := store.NewMemoryStore()
	publisher := queue.NewMemoryPublisher()
	handler := newTestServer(memoryStore, publisher).Handler()
	now := time.Date(2026, time.August, 19, 10, 0, 0, 0, time.UTC)

	original := createReadyIOSMedia(t, handler, "asset-edit-charge-original", models.MediaTypePhoto)
	project := models.StoryProject{
		ID:        "project-edit-charge",
		UserID:    "user-1",
		Title:     "Charged edit",
		Context:   "Special moments",
		MediaIDs:  []string{original.MediaID},
		Status:    models.ProjectStatusReady,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if _, _, err := memoryStore.CreateProject(context.Background(), project, "project-edit-charge"); err != nil {
		t.Fatal(err)
	}
	if err := memoryStore.SaveStoryPlan(context.Background(), "user-1", models.StoryPlan{
		SchemaVersion: models.StoryPlanSchemaVersionV1,
		ID:            "plan-edit-charge",
		ProjectID:     project.ID,
		Version:       1,
		Moments: []models.StoryMoment{{
			MomentID:   "moment-original",
			AssetID:    original.MediaID,
			MediaID:    original.MediaID,
			MediaType:  models.MediaTypePhoto,
			Narrative:  "Original",
			DurationMS: 4000,
		}},
		Output:          models.StoryOutput{AspectRatio: "9:16", MaxDurationS: 8, Preset: "premium-memory"},
		TotalDurationMS: 4000,
		CreatedAt:       now,
	}); err != nil {
		t.Fatal(err)
	}

	added := createReadyIOSMedia(t, handler, "asset-edit-charge-added", models.MediaTypePhoto)
	editResponse := performJSON(t, handler, http.MethodPost, "/v1/stories/"+project.ID+"/versions", map[string]any{
		"addMediaIds": []string{added.MediaID},
		"analyses": []map[string]any{{
			"assetID":     added.MediaID,
			"labels":      []string{"sunset"},
			"peopleCount": 1,
			"confidence":  0.9,
		}},
	}, "edit-charge-add-moment", true, nil)
	if editResponse.Code != http.StatusAccepted {
		t.Fatalf("edit iOS story status = %d, body = %s", editResponse.Code, editResponse.Body.String())
	}
	stored, err := memoryStore.GetProject(context.Background(), "user-1", project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.CreditOperationKey != "REGENERATE_STORY,ADD_MOMENT" {
		t.Fatalf("credit operation key = %q", stored.CreditOperationKey)
	}
	balance, err := memoryStore.GetCreditBalance(context.Background(), "user-1")
	if err != nil {
		t.Fatal(err)
	}
	if balance.Available != 40 || balance.Reserved != 60 {
		t.Fatalf("expected 60 credits reserved for add+regenerate from initial 100, got %+v", balance)
	}
	if _, err := memoryStore.GetCreditReservationByReference(context.Background(), "user-1", models.CreditActionRegenerateStory, "project", project.ID+":v2"); err != nil {
		t.Fatalf("missing regenerate reservation: %v", err)
	}
	if _, err := memoryStore.GetCreditReservationByReference(context.Background(), "user-1", models.CreditActionAddMoment, "project", project.ID+":v2"); err != nil {
		t.Fatalf("missing add moment reservation: %v", err)
	}
}

func TestIOSStoryEditAddExistingProjectMomentStillReservesAddCredits(t *testing.T) {
	t.Parallel()

	memoryStore := store.NewMemoryStore()
	publisher := queue.NewMemoryPublisher()
	handler := newTestServer(memoryStore, publisher).Handler()
	now := time.Date(2026, time.August, 19, 10, 0, 0, 0, time.UTC)

	original := createReadyIOSMedia(t, handler, "asset-edit-charge-existing-original", models.MediaTypePhoto)
	added := createReadyIOSMedia(t, handler, "asset-edit-charge-existing-added", models.MediaTypePhoto)
	project := models.StoryProject{
		ID:        "project-edit-charge-existing",
		UserID:    "user-1",
		Title:     "Charged edit",
		Context:   "Special moments",
		MediaIDs:  []string{original.MediaID, added.MediaID},
		Status:    models.ProjectStatusReady,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if _, _, err := memoryStore.CreateProject(context.Background(), project, "project-edit-charge-existing"); err != nil {
		t.Fatal(err)
	}
	if err := memoryStore.SaveStoryPlan(context.Background(), "user-1", models.StoryPlan{
		SchemaVersion: models.StoryPlanSchemaVersionV1,
		ID:            "plan-edit-charge-existing",
		ProjectID:     project.ID,
		Version:       1,
		Moments: []models.StoryMoment{{
			MomentID:   "moment-original",
			AssetID:    original.MediaID,
			MediaID:    original.MediaID,
			MediaType:  models.MediaTypePhoto,
			Narrative:  "Original",
			DurationMS: 4000,
		}},
		Output:          models.StoryOutput{AspectRatio: "9:16", MaxDurationS: 8, Preset: "premium-memory"},
		TotalDurationMS: 4000,
		CreatedAt:       now,
	}); err != nil {
		t.Fatal(err)
	}

	editResponse := performJSON(t, handler, http.MethodPost, "/v1/stories/"+project.ID+"/versions", map[string]any{
		"addMediaIds": []string{added.MediaID},
		"analyses": []map[string]any{{
			"assetID":     added.MediaID,
			"labels":      []string{"sunset"},
			"peopleCount": 1,
			"confidence":  0.9,
		}},
	}, "edit-charge-add-existing-moment", true, nil)
	if editResponse.Code != http.StatusAccepted {
		t.Fatalf("edit iOS story status = %d, body = %s", editResponse.Code, editResponse.Body.String())
	}
	balance, err := memoryStore.GetCreditBalance(context.Background(), "user-1")
	if err != nil {
		t.Fatal(err)
	}
	if balance.Available != 40 || balance.Reserved != 60 {
		t.Fatalf("expected 60 credits reserved for add+regenerate from initial 100, got %+v", balance)
	}
}

func TestIOSStoryEditAddsUploadedPhotoOutsideOriginalStory(t *testing.T) {
	t.Parallel()

	memoryStore := store.NewMemoryStore()
	publisher := queue.NewMemoryPublisher()
	handler := newTestServer(memoryStore, publisher).Handler()

	original := createReadyIOSMedia(t, handler, "asset-edit-original", models.MediaTypePhoto)
	storyResponse := performJSON(t, handler, http.MethodPost, "/v1/stories", map[string]any{
		"context": "Special moments",
		"assets":  []map[string]any{{"id": "asset-edit-original", "kind": "image", "pixelWidth": 3024, "pixelHeight": 4032}},
	}, "", true, nil)
	if storyResponse.Code != http.StatusCreated {
		t.Fatalf("create iOS story status = %d, body = %s", storyResponse.Code, storyResponse.Body.String())
	}
	var story iosStory
	decodeResponse(t, storyResponse, &story)

	newPhoto := createReadyIOSMedia(t, handler, "asset-edit-new-photo", models.MediaTypePhoto)
	editResponse := performJSON(t, handler, http.MethodPost, "/v1/stories/"+story.ID+"/versions", map[string]any{
		"addMediaIds": []string{newPhoto.MediaID},
		"analyses": []map[string]any{{
			"assetID":     newPhoto.MediaID,
			"labels":      []string{"sunset", "lake"},
			"peopleCount": 1,
			"confidence":  0.88,
		}},
	}, "edit-add-photo", true, nil)
	if editResponse.Code != http.StatusAccepted {
		t.Fatalf("edit iOS story status = %d, body = %s", editResponse.Code, editResponse.Body.String())
	}
	project, err := memoryStore.GetProject(context.Background(), "user-1", story.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(project.MediaIDs, original.MediaID) || !slices.Contains(project.MediaIDs, newPhoto.MediaID) {
		t.Fatalf("project media was not extended safely: %+v", project.MediaIDs)
	}
	messages := publisher.Messages()
	last := messages[len(messages)-1]
	if last.JobType != models.JobTypeStoryPlan || last.MediaID != "" {
		t.Fatalf("new analyzed photo should publish a story plan revision without photo preprocess, got %+v", last)
	}
	jobs, err := memoryStore.ListProjectJobs(context.Background(), "user-1", story.ID)
	if err != nil {
		t.Fatal(err)
	}
	if countType(jobs, models.JobTypeStoryPlan) != 2 {
		t.Fatalf("expected initial story plan plus one revision, got %+v", jobs)
	}
	deferred := latestJobOfType(t, jobs, models.JobTypeStoryPlan)
	if !strings.Contains(deferred.Configuration["selected_media_ids"], original.MediaID) || !strings.Contains(deferred.Configuration["selected_media_ids"], newPhoto.MediaID) {
		t.Fatalf("selected media should include original and new photo: %+v", deferred.Configuration)
	}
}

func TestIOSStoryEditDefersStoryPlanUntilNewVideoAnalysis(t *testing.T) {
	t.Parallel()

	memoryStore := store.NewMemoryStore()
	publisher := queue.NewMemoryPublisher()
	server := newTestServer(memoryStore, publisher)
	handler := server.Handler()

	createReadyIOSMedia(t, handler, "asset-edit-video-original", models.MediaTypePhoto)
	storyResponse := performJSON(t, handler, http.MethodPost, "/v1/stories", map[string]any{
		"context": "Special moments",
		"assets":  []map[string]any{{"id": "asset-edit-video-original", "kind": "image", "pixelWidth": 3024, "pixelHeight": 4032}},
	}, "", true, nil)
	if storyResponse.Code != http.StatusCreated {
		t.Fatalf("create iOS story status = %d, body = %s", storyResponse.Code, storyResponse.Body.String())
	}
	var story iosStory
	decodeResponse(t, storyResponse, &story)
	completeProjectPhotoPreprocessJobs(t, server, memoryStore, story.ID)

	newVideo := createReadyIOSMedia(t, handler, "asset-edit-new-video", models.MediaTypeVideo)
	editResponse := performJSON(t, handler, http.MethodPost, "/v1/stories/"+story.ID+"/versions", map[string]any{
		"addMediaIds": []string{newVideo.MediaID},
	}, "edit-add-video", true, nil)
	if editResponse.Code != http.StatusAccepted {
		t.Fatalf("edit iOS story status = %d, body = %s", editResponse.Code, editResponse.Body.String())
	}
	if last := publisher.Messages()[len(publisher.Messages())-1]; last.JobType != models.JobTypeMediaPreprocess || last.MediaID != newVideo.MediaID {
		t.Fatalf("new video should queue only missing media analysis first, got %+v", last)
	}
	jobs, err := memoryStore.ListProjectJobs(context.Background(), "user-1", story.ID)
	if err != nil {
		t.Fatal(err)
	}
	if countType(jobs, models.JobTypeStoryPlan) != 2 {
		t.Fatalf("expected initial story plan plus deferred revision job, got %+v", jobs)
	}

	preprocess := latestPublishedJob(t, publisher, models.JobTypeMediaPreprocess)
	if _, err := server.Orchestrator.UpdateJobStatus(context.Background(), preprocess.JobID, models.JobStatusCompleted, 1, "", "", "", ""); err != nil {
		t.Fatal(err)
	}
	transcribe := latestPublishedJob(t, publisher, models.JobTypeTranscribe)
	if _, err := server.Orchestrator.UpdateJobStatus(context.Background(), transcribe.JobID, models.JobStatusCompleted, 1, "", "", "", ""); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.August, 19, 10, 0, 0, 0, time.UTC)
	if err := memoryStore.SaveVideoAnalysis(context.Background(), "user-1", models.VideoAnalysis{
		ID:                 "video-analysis-edit",
		VideoID:            newVideo.MediaID,
		ProjectID:          story.ID,
		AnalysisVersion:    "test-v1",
		DurationMicros:     30_000_000,
		DetectedActivities: []string{"laughing"},
		QualitySignals:     models.VideoQualitySignals{OverallScore: 0.9},
		Segments: []models.VideoSegment{{
			SegmentID:      "segment-edit-laughing",
			StartMicros:    8_000_000,
			EndMicros:      12_000_000,
			Description:    "Family laughing",
			Importance:     0.9,
			VisualQuality:  0.9,
			MemoryValue:    0.9,
			StoryRelevance: 0.9,
			Recommended:    true,
		}},
		CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	understand := latestPublishedJob(t, publisher, models.JobTypeVideoUnderstand)
	before := len(publisher.Messages())
	if _, err := server.Orchestrator.UpdateJobStatus(context.Background(), understand.JobID, models.JobStatusCompleted, 1, "", "", "", ""); err != nil {
		t.Fatal(err)
	}
	after := publisher.Messages()
	if len(after) <= before || after[len(after)-1].JobType != models.JobTypeStoryPlan {
		t.Fatalf("deferred story revision was not published after video analysis: before=%d after=%+v", before, after)
	}
}

func TestIOSStoryVideoMomentsUseRepresentativeFrameThumbnail(t *testing.T) {
	t.Parallel()

	memoryStore := store.NewMemoryStore()
	publisher := queue.NewMemoryPublisher()
	handler := newTestServer(memoryStore, publisher).Handler()

	video := createReadyIOSMedia(t, handler, "asset-video-thumbnail", models.MediaTypeVideo)
	storyResponse := performJSON(t, handler, http.MethodPost, "/v1/stories", map[string]any{
		"context": "Special moments",
		"assets":  []map[string]any{{"id": "asset-video-thumbnail", "kind": "video", "pixelWidth": 1920, "pixelHeight": 1080, "duration": 12}},
	}, "", true, nil)
	if storyResponse.Code != http.StatusCreated {
		t.Fatalf("create iOS story status = %d, body = %s", storyResponse.Code, storyResponse.Body.String())
	}
	var story iosStory
	decodeResponse(t, storyResponse, &story)

	now := time.Date(2026, time.August, 19, 10, 0, 0, 0, time.UTC)
	frameKey := "users/user-1/video-analysis/" + video.MediaID + "/frame-2.jpg"
	if err := memoryStore.SaveVideoAnalysis(context.Background(), "user-1", models.VideoAnalysis{
		ID:              "video-analysis-thumbnail",
		VideoID:         video.MediaID,
		ProjectID:       story.ID,
		AnalysisVersion: "thumb-v1",
		DurationMicros:  12_000_000,
		Scenes: []models.VideoScene{{
			SceneID:     "scene-video-thumbnail",
			StartMicros: 1_000_000,
			EndMicros:   5_000_000,
			RepresentativeFrames: []models.Frame{
				{FrameID: "frame-1", TimestampMicros: 500_000, S3Key: "users/user-1/video-analysis/" + video.MediaID + "/frame-1.jpg"},
				{FrameID: "frame-2", TimestampMicros: 2_000_000, S3Key: frameKey},
			},
		}},
		QualitySignals: models.VideoQualitySignals{OverallScore: 0.9},
		CreatedAt:      now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := memoryStore.SaveStoryPlan(context.Background(), "user-1", models.StoryPlan{
		SchemaVersion: models.StoryPlanSchemaVersionV1,
		ID:            "plan-video-thumbnail",
		ProjectID:     story.ID,
		Version:       1,
		TitleText:     "Video thumbnail story",
		Moments: []models.StoryMoment{{
			MomentID:             "moment-video-thumbnail",
			AssetID:              video.MediaID,
			MediaID:              video.MediaID,
			MediaType:            models.MediaTypeVideo,
			VideoAnalysisVersion: "thumb-v1",
			StartMicros:          1_500_000,
			EndMicros:            4_000_000,
			Narrative:            "Video moment",
			DurationMS:           2500,
		}},
		Output:          models.StoryOutput{AspectRatio: "9:16", MaxDurationS: 12, Preset: "social"},
		TotalDurationMS: 2500,
		CreatedAt:       now,
	}); err != nil {
		t.Fatal(err)
	}

	getResponse := performJSON(t, handler, http.MethodGet, "/v1/stories/"+story.ID, nil, "", true, nil)
	if getResponse.Code != http.StatusOK {
		t.Fatalf("get iOS story status = %d, body = %s", getResponse.Code, getResponse.Body.String())
	}
	var updated iosStory
	decodeResponse(t, getResponse, &updated)
	if len(updated.Moments) != 1 {
		t.Fatalf("expected one story moment, got %+v", updated.Moments)
	}
	if updated.Moments[0].ThumbnailURL == nil || !strings.Contains(*updated.Moments[0].ThumbnailURL, frameKey) {
		t.Fatalf("video moment thumbnail should use representative frame %q, got %+v", frameKey, updated.Moments[0].ThumbnailURL)
	}
}

func TestIOSStoriesCompatibilityResolvesByteScopedUploadKeys(t *testing.T) {
	t.Parallel()

	memoryStore := store.NewMemoryStore()
	publisher := queue.NewMemoryPublisher()
	handler := newTestServer(memoryStore, publisher).Handler()

	assetID := "asset-byte-scoped"
	uploadResponse := createReadyIOSMediaWithIdempotency(t, handler, assetID, models.MediaTypePhoto, 2_048, "media-upload-original-"+assetID+"-bytes-2048")
	storyResponse := performJSON(t, handler, http.MethodPost, "/v1/stories", map[string]any{
		"context": "Physical iPhone upload",
		"assets": []map[string]any{
			{
				"id":           assetID,
				"kind":         "image",
				"duration":     0,
				"pixelWidth":   3024,
				"pixelHeight":  4032,
				"creationDate": "2026-08-19T10:00:00Z",
			},
		},
	}, "", true, nil)
	if storyResponse.Code != http.StatusCreated {
		t.Fatalf("create iOS story with byte-scoped upload status = %d, body = %s", storyResponse.Code, storyResponse.Body.String())
	}
	var story iosStory
	decodeResponse(t, storyResponse, &story)
	if len(story.AssetIDs) != 1 || story.AssetIDs[0] != uploadResponse.MediaID {
		t.Fatalf("unexpected byte-scoped iOS story response: %+v", story)
	}
}

func TestIOSStoryCreationSanitizesAnalysisTextForJSONB(t *testing.T) {
	t.Parallel()

	memoryStore := store.NewMemoryStore()
	publisher := queue.NewMemoryPublisher()
	handler := newTestServer(memoryStore, publisher).Handler()

	uploadResponse := createReadyIOSMedia(t, handler, "asset-photo-nul", models.MediaTypePhoto)
	storyResponse := performJSON(t, handler, http.MethodPost, "/v1/stories", map[string]any{
		"context": "Special\x00 moments",
		"assets": []map[string]any{
			{
				"id":           "asset-photo-nul",
				"kind":         "image",
				"duration":     0,
				"pixelWidth":   3024,
				"pixelHeight":  4032,
				"creationDate": "2026-08-19T10:00:00Z",
			},
		},
		"analyses": []map[string]any{
			{"assetID": "asset-photo-nul", "labels": []string{"be\x00ach", "\x00"}, "peopleCount": 2, "confidence": 0.91},
		},
	}, "", true, nil)
	if storyResponse.Code != http.StatusCreated {
		t.Fatalf("create iOS story status = %d, body = %s", storyResponse.Code, storyResponse.Body.String())
	}
	var story iosStory
	decodeResponse(t, storyResponse, &story)
	if strings.Contains(story.Context, "\x00") {
		t.Fatalf("story context contains NUL: %q", story.Context)
	}
	analyses, _, err := memoryStore.ListProjectAnalyses(context.Background(), "user-1", story.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(analyses) != 1 || analyses[0].MediaID != uploadResponse.MediaID {
		t.Fatalf("unexpected stored analyses: %+v", analyses)
	}
	if len(analyses[0].Labels) != 1 || analyses[0].Labels[0] != "beach" || strings.Contains(analyses[0].Labels[0], "\x00") {
		t.Fatalf("analysis labels were not sanitized: %+v", analyses[0].Labels)
	}
}

func TestGetIOSStoryReconcilesCompletedVideoPreprocess(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	memoryStore := store.NewMemoryStore()
	publisher := queue.NewMemoryPublisher()
	handler := newTestServer(memoryStore, publisher).Handler()
	createReadyIOSMediaInStore(t, memoryStore, "asset-video-1", models.MediaTypeVideo, 1_024)
	now := time.Date(2026, time.August, 19, 10, 0, 0, 0, time.UTC)
	project := models.StoryProject{
		ID:        "project-stalled-ios",
		UserID:    "user-1",
		Title:     "Special moments story",
		Context:   "Special moments",
		MediaIDs:  []string{"media-asset-video-1"},
		Status:    models.ProjectStatusAnalyzing,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if _, _, err := memoryStore.CreateProject(ctx, project, "stalled-ios-project"); err != nil {
		t.Fatal(err)
	}
	completed := models.ProcessingJob{
		ID:            "job-completed-preprocess",
		UserID:        project.UserID,
		ProjectID:     project.ID,
		MediaID:       "media-asset-video-1",
		Type:          models.JobTypeMediaPreprocess,
		Status:        models.JobStatusCompleted,
		Progress:      1,
		MaxAttempts:   3,
		Configuration: map[string]string{"pipeline_version": "async-pipeline-v1", "job_type": string(models.JobTypeMediaPreprocess)},
		CreatedAt:     now,
		UpdatedAt:     now,
		CompletedAt:   &now,
	}
	if _, _, err := memoryStore.CreateJob(ctx, completed, "completed-preprocess"); err != nil {
		t.Fatal(err)
	}

	response := performJSON(t, handler, http.MethodGet, "/v1/stories/"+project.ID, nil, "", true, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("get iOS story status = %d, body = %s", response.Code, response.Body.String())
	}
	if countMessageType(publisher.Messages(), models.JobTypeTranscribe) != 1 {
		t.Fatalf("expected stalled completed preprocess to enqueue transcribe, got %+v", publisher.Messages())
	}
	jobs, err := memoryStore.ListProjectJobs(ctx, project.UserID, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if countType(jobs, models.JobTypeTranscribe) != 1 {
		t.Fatalf("expected exactly one transcribe job after reconciliation, got %+v", jobs)
	}
}

func TestIOSStoriesCompatibilityRejectsStoriesOverTotalUploadLimit(t *testing.T) {
	t.Parallel()

	memoryStore := store.NewMemoryStore()
	publisher := queue.NewMemoryPublisher()
	server := newTestServer(memoryStore, publisher)
	handler := server.Handler()

	createReadyIOSMediaInStore(t, memoryStore, "asset-video-1", models.MediaTypeVideo, 600_000_000)
	createReadyIOSMediaInStore(t, memoryStore, "asset-video-2", models.MediaTypeVideo, 400_000_001)
	storyResponse := performJSON(t, handler, http.MethodPost, "/v1/stories", map[string]any{
		"context": "Too many clips",
		"assets": []map[string]any{
			{"id": "asset-video-1", "kind": "video", "duration": 60, "pixelWidth": 1920, "pixelHeight": 1080},
			{"id": "asset-video-2", "kind": "video", "duration": 60, "pixelWidth": 1920, "pixelHeight": 1080},
		},
	}, "", true, nil)

	if storyResponse.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("create oversized iOS story status = %d, body = %s", storyResponse.Code, storyResponse.Body.String())
	}
	if !strings.Contains(storyResponse.Body.String(), "story_too_large") {
		t.Fatalf("expected story_too_large response, got %s", storyResponse.Body.String())
	}
	if messages := publisher.Messages(); len(messages) != 0 {
		t.Fatalf("expected no queued jobs for rejected story, got %+v", messages)
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

func TestAnonymousAuthIssuesUsableToken(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 19, 10, 0, 0, 0, time.UTC)
	secret := []byte("a-secret-with-at-least-thirty-two-characters")
	handler := NewServer(Server{
		Store:         store.NewMemoryStore(),
		Authenticator: HMACTokenAuthenticator{Secret: secret, Now: func() time.Time { return now }},
		InternalToken: testInternalToken,
		Logger:        slog.New(slog.NewJSONHandler(io.Discard, nil)),
		Now:           func() time.Time { return now },
	}).Handler()

	issued := performJSON(t, handler, http.MethodPost, "/v1/auth/anonymous", map[string]any{"installation_id": "device-1"}, "", false, nil)
	if issued.Code != http.StatusOK {
		t.Fatalf("anonymous auth status = %d, body = %s", issued.Code, issued.Body.String())
	}
	var token authTokenResponse
	decodeResponse(t, issued, &token)
	if token.AccessToken == "" || token.RefreshToken == "" || token.RefreshExpiresAt == nil || token.User.ID != "ios-anon-device-1" {
		t.Fatalf("unexpected token response: %+v", token)
	}
	if !token.ExpiresAt.Equal(now.Add(anonymousAccessTokenTTL)) || !token.RefreshExpiresAt.Equal(now.Add(anonymousRefreshTokenTTL)) {
		t.Fatalf("unexpected token expiry: %+v", token)
	}

	stories := performJSON(t, handler, http.MethodGet, "/v1/stories", nil, "", false, map[string]string{"Authorization": "Bearer " + token.AccessToken})
	if stories.Code != http.StatusOK {
		t.Fatalf("list stories status = %d, body = %s", stories.Code, stories.Body.String())
	}

	refreshed := performJSON(t, handler, http.MethodPost, "/v1/auth/refresh", map[string]any{"refresh_token": token.RefreshToken}, "", false, nil)
	if refreshed.Code != http.StatusOK {
		t.Fatalf("refresh status = %d, body = %s", refreshed.Code, refreshed.Body.String())
	}
	var refreshedToken authTokenResponse
	decodeResponse(t, refreshed, &refreshedToken)
	if refreshedToken.AccessToken == "" || refreshedToken.RefreshToken == "" || refreshedToken.User.ID != token.User.ID {
		t.Fatalf("unexpected refreshed token response: %+v", refreshedToken)
	}

	refreshAsAccess := performJSON(t, handler, http.MethodGet, "/v1/stories", nil, "", false, map[string]string{"Authorization": "Bearer " + token.RefreshToken})
	if refreshAsAccess.Code != http.StatusUnauthorized {
		t.Fatalf("refresh token must not authorize protected routes: %d body = %s", refreshAsAccess.Code, refreshAsAccess.Body.String())
	}
}

func TestHMACTokenAuthenticatorRejectsExpiredAccessToken(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 19, 10, 0, 0, 0, time.UTC)
	secret := []byte("a-secret-with-at-least-thirty-two-characters")
	authenticator := HMACTokenAuthenticator{Secret: secret, Now: func() time.Time { return now }}
	token, err := authenticator.IssueToken(models.User{ID: "user-expired"}, now.Add(-time.Second))
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/v1/stories", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	if _, err := authenticator.Authenticate(context.Background(), request); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("expected expired token to be rejected, got %v", err)
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

type sizeMismatchObjectStorage struct {
	base storage.StaticObjectStorage
}

func (s *sizeMismatchObjectStorage) AuthorizeUpload(ctx context.Context, request storage.UploadAuthorizationRequest) (models.UploadSession, error) {
	return s.base.AuthorizeUpload(ctx, request)
}

func (s *sizeMismatchObjectStorage) RefreshUpload(ctx context.Context, session models.UploadSession, request storage.UploadRefreshRequest) (models.UploadSession, error) {
	return s.base.RefreshUpload(ctx, session, request)
}

func (s *sizeMismatchObjectStorage) CompleteUpload(context.Context, models.UploadSession, []models.CompletedUploadPart) (string, error) {
	return "", storage.ErrUploadSizeMismatch
}

func (s *sizeMismatchObjectStorage) AbortUpload(ctx context.Context, session models.UploadSession) error {
	return s.base.AbortUpload(ctx, session)
}

func (s *sizeMismatchObjectStorage) ObjectExists(ctx context.Context, key string) (bool, error) {
	return s.base.ObjectExists(ctx, key)
}

func (s *sizeMismatchObjectStorage) PutObject(ctx context.Context, key, contentType string, data []byte) error {
	return s.base.PutObject(ctx, key, contentType, data)
}

func (s *sizeMismatchObjectStorage) PresignedDownloadURL(ctx context.Context, key string, expiry time.Duration) (string, error) {
	return s.base.PresignedDownloadURL(ctx, key, expiry)
}

type countingObjectStorage struct {
	mu          sync.Mutex
	base        storage.StaticObjectStorage
	completions int
}

func (s *countingObjectStorage) AuthorizeUpload(ctx context.Context, request storage.UploadAuthorizationRequest) (models.UploadSession, error) {
	return s.base.AuthorizeUpload(ctx, request)
}

func (s *countingObjectStorage) RefreshUpload(ctx context.Context, session models.UploadSession, request storage.UploadRefreshRequest) (models.UploadSession, error) {
	return s.base.RefreshUpload(ctx, session, request)
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

func (s *countingObjectStorage) PutObject(ctx context.Context, key, contentType string, data []byte) error {
	return s.base.PutObject(ctx, key, contentType, data)
}

func (s *countingObjectStorage) PresignedDownloadURL(ctx context.Context, key string, expiry time.Duration) (string, error) {
	return s.base.PresignedDownloadURL(ctx, key, expiry)
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

func createReadyIOSMediaInStore(t *testing.T, memoryStore *store.MemoryStore, assetID string, mediaType models.MediaType, fileSizeBytes int64) {
	t.Helper()
	now := time.Date(2026, time.August, 19, 10, 0, 0, 0, time.UTC)
	mediaID := "media-" + assetID
	uploadID := "upload-" + assetID
	s3Key := "users/user-1/media/" + mediaID
	media := models.MediaAsset{
		ID:            mediaID,
		UserID:        "user-1",
		Type:          mediaType,
		Status:        models.MediaStatusReady,
		FileName:      assetID,
		ContentType:   "application/octet-stream",
		FileSizeBytes: fileSizeBytes,
		PixelWidth:    1920,
		PixelHeight:   1080,
		S3Key:         s3Key,
		UploadID:      uploadID,
		ETag:          "etag-" + assetID,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	session := models.UploadSession{
		ID:             uploadID,
		UserID:         "user-1",
		MediaID:        mediaID,
		S3Key:          s3Key,
		Strategy:       models.UploadStrategySingle,
		PartSizeBytes:  5 * 1024 * 1024,
		IdempotencyKey: "media-upload-original-" + assetID,
		CreatedAt:      now,
		CompletedAt:    &now,
	}
	if err := memoryStore.CreateMediaUpload(context.Background(), media, session); err != nil {
		t.Fatal(err)
	}
}

func createReadyIOSMedia(t *testing.T, handler http.Handler, assetID string, mediaType models.MediaType) iosUploadSessionResponse {
	t.Helper()
	return createReadyIOSMediaWithSize(t, handler, assetID, mediaType, 1_024)
}

func createReadyIOSMediaWithSize(t *testing.T, handler http.Handler, assetID string, mediaType models.MediaType, fileSizeBytes int64) iosUploadSessionResponse {
	t.Helper()
	return createReadyIOSMediaWithIdempotency(t, handler, assetID, mediaType, fileSizeBytes, "media-upload-original-"+assetID)
}

func createReadyIOSMediaWithIdempotency(t *testing.T, handler http.Handler, assetID string, mediaType models.MediaType, fileSizeBytes int64, idempotencyKey string) iosUploadSessionResponse {
	t.Helper()
	contentType := "image/jpeg"
	fileExtension := ".jpg"
	if mediaType == models.MediaTypeVideo {
		contentType = "video/quicktime"
		fileExtension = ".mov"
	}
	upload := performJSON(t, handler, http.MethodPost, "/v1/media/upload-sessions", map[string]any{
		"idempotencyKey": idempotencyKey,
		"metadata": map[string]any{
			"assetID":                assetID,
			"mediaType":              mediaType,
			"contentType":            contentType,
			"fileName":               assetID + fileExtension,
			"fileSizeBytes":          fileSizeBytes,
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

func TestIOSStoryPresentationSuppressesGenericNames(t *testing.T) {
	t.Parallel()
	moments := []iosStoryMoment{
		{ID: "one", MediaID: "photo-1", Type: "photo", Title: "Opening moment", Description: "Family breakfast by the window", Selected: true},
		{ID: "two", MediaID: "video-1", Type: "video", Title: "Everyone laughing near the lake", Selected: true},
	}
	if got := iosDisplayStoryTitle("Special moments story", moments, "Special moments"); got != "Family breakfast by the window" {
		t.Fatalf("story title = %q", got)
	}
	if got := iosDisplayStoryTitle("Selected moments", moments, "Selected moments"); got != "Family breakfast by the window" {
		t.Fatalf("selected moments story title = %q", got)
	}
	if got := iosMomentTitle(models.StoryMoment{Caption: "Video moment", Narrative: "Everyone laughing near the lake"}, string(models.MediaTypeVideo), 0); got != "Everyone laughing near the lake" {
		t.Fatalf("moment title = %q", got)
	}
	if got := fallbackIOSMomentTitle(string(models.MediaTypePhoto), 0); got == "Photo moment 1" || got == "Video moment 1" {
		t.Fatalf("generic fallback survived: %q", got)
	}
	technicalReason := "Selected from available frame evidence while external AI understanding is disabled."
	if got := iosMomentTitle(models.StoryMoment{Reason: technicalReason}, string(models.MediaTypeVideo), 0); got != "Opening moment" {
		t.Fatalf("technical fallback reason leaked into moment title: %q", got)
	}
	plan := models.StoryPlan{
		UserInstructions: technicalReason,
		Moments:          []models.StoryMoment{{Reason: technicalReason}, {Caption: "Everyone laughing by the lake"}},
	}
	if got := iosStoryDescription(plan); got != "Everyone laughing by the lake" {
		t.Fatalf("story description = %q", got)
	}
	promptPlan := models.StoryPlan{
		UserInstructions: "Make it slower and more emotional, focus on the sunset, lake, and people laughing",
		Moments:          []models.StoryMoment{{Narrative: "A warm lakeside picnic with people laughing at sunset"}},
	}
	if got := iosStoryDescription(promptPlan); strings.Contains(strings.ToLower(got), "make it slower") || got != "A warm lakeside picnic with people laughing at sunset" {
		t.Fatalf("story prompt leaked into description: %q", got)
	}
	pollutedPlan := models.StoryPlan{
		Moments: []models.StoryMoment{{
			Caption:   "Look at the ducks by the lake This is my favorite part",
			Narrative: "Look at the ducks by the lake This is my favorite part",
			Reason:    "Family laughing near the lake",
		}},
		Captions: []models.StoryCaption{
			{MomentID: "video-talk", Text: "Look at the ducks by the lake"},
			{MomentID: "video-talk", Text: "This is my favorite part"},
		},
	}
	if got := iosStoryDescription(pollutedPlan); got != "Family laughing near the lake" {
		t.Fatalf("subtitle text leaked into story description: %q", got)
	}
	if got := iosMomentTitleIgnoringSubtitles(pollutedPlan.Moments[0], string(models.MediaTypeVideo), 0, iosCaptionTextMatchers(pollutedPlan.Captions)); got != "Family laughing near the lake" {
		t.Fatalf("subtitle text leaked into moment title: %q", got)
	}
	metadataPollutedPlan := models.StoryPlan{Moments: []models.StoryMoment{{
		Narrative: "Strong memory moment: transcript_segment_indexes 0 and 1 reference the strawberry spill and how funny it was.",
		Reason:    "Family laughing about the strawberry spill by the lake",
	}}}
	if got := iosStoryDescription(metadataPollutedPlan); got != "Family laughing about the strawberry spill by the lake" {
		t.Fatalf("technical grounding metadata leaked into story description: %q", got)
	}
	observationalPlan := models.StoryPlan{Moments: []models.StoryMoment{{
		Narrative: "A woman and a young child sit on a red checkered picnic blanket by a lake, watching ducks on the water.",
	}}}
	observationalDescription := iosStoryDescription(observationalPlan)
	if strings.Contains(strings.ToLower(observationalDescription), "a woman") || strings.Contains(strings.ToLower(observationalDescription), "young child") {
		t.Fatalf("observational people labels leaked into user-facing description: %q", observationalDescription)
	}
	if !strings.Contains(observationalDescription, "A warm family memory by the lake") || !strings.Contains(observationalDescription, "watching ducks together") {
		t.Fatalf("description should be memory-centered, got %q", observationalDescription)
	}
	longMoment := strings.Repeat("lake picnic with friends ", 16)
	longPlan := models.StoryPlan{Moments: []models.StoryMoment{{Narrative: longMoment}}}
	if got := iosStoryDescription(longPlan); len(got) <= 120 || len(got) > iosStoryDescriptionMaxLength || !strings.HasSuffix(got, "…") {
		t.Fatalf("story description = %q, want concise truncated text within max length", got)
	}
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

func TestPublicMusicRoutesAreUnavailableInV1(t *testing.T) {
	t.Parallel()
	handler := newTestServer(store.NewMemoryStore(), queue.NewMemoryPublisher()).Handler()
	response := performJSON(t, handler, http.MethodGet, "/v1/music", nil, "", true, nil)
	if response.Code != http.StatusNotFound && response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("V1 should not expose public music APIs, got status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestGrantPurchasedCreditsAddsPersistentBalance(t *testing.T) {
	t.Parallel()
	memoryStore := store.NewMemoryStore()
	server := newTestServer(memoryStore, queue.NewMemoryPublisher())
	handler := server.Handler()

	packs := performJSON(t, handler, http.MethodGet, "/v1/credit-packs", nil, "", true, nil)
	if packs.Code != http.StatusOK {
		t.Fatalf("credit packs status = %d, body = %s", packs.Code, packs.Body.String())
	}

	first := performJSON(t, handler, http.MethodPost, "/v1/me/credits/purchased", map[string]any{
		"pack_id":     "medium",
		"purchase_id": "credit-purchase-123",
	}, "", true, nil)
	if first.Code != http.StatusOK {
		t.Fatalf("first purchased grant status = %d, body = %s", first.Code, first.Body.String())
	}
	var firstBody struct {
		Balance models.CreditBalance `json:"balance"`
	}
	decodeResponse(t, first, &firstBody)
	wantPurchasedCredits := server.Credits.Policy.InitialGrant + 300
	if firstBody.Balance.PurchasedCredits != wantPurchasedCredits {
		t.Fatalf("purchased credits = %d, want %d", firstBody.Balance.PurchasedCredits, wantPurchasedCredits)
	}

	second := performJSON(t, handler, http.MethodPost, "/v1/me/credits/purchased", map[string]any{
		"pack_id":     "medium",
		"purchase_id": "credit-purchase-123",
	}, "", true, nil)
	if second.Code != http.StatusOK {
		t.Fatalf("second purchased grant status = %d, body = %s", second.Code, second.Body.String())
	}
	var secondBody struct {
		Balance models.CreditBalance `json:"balance"`
	}
	decodeResponse(t, second, &secondBody)
	if secondBody.Balance.PurchasedCredits != wantPurchasedCredits {
		t.Fatalf("duplicate purchased grant changed balance: %+v", secondBody.Balance)
	}
}

func TestCreditBalanceExposesSubscriptionCyclePolicy(t *testing.T) {
	t.Parallel()
	memoryStore := store.NewMemoryStore()
	server := newTestServer(memoryStore, queue.NewMemoryPublisher())
	handler := server.Handler()

	response := performJSON(t, handler, http.MethodGet, "/v1/me/credits", nil, "", true, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("credit balance status = %d, body = %s", response.Code, response.Body.String())
	}
	var balance models.CreditBalance
	decodeResponse(t, response, &balance)
	if balance.MonthlySubscriptionCycleCredits != server.Credits.Policy.MonthlySubscriptionCycleCredits {
		t.Fatalf("monthly credits = %d, want %d", balance.MonthlySubscriptionCycleCredits, server.Credits.Policy.MonthlySubscriptionCycleCredits)
	}
	if balance.YearlySubscriptionCycleCredits != server.Credits.Policy.YearlySubscriptionCycleCredits {
		t.Fatalf("yearly credits = %d, want %d", balance.YearlySubscriptionCycleCredits, server.Credits.Policy.YearlySubscriptionCycleCredits)
	}
}

func TestGrantSubscriptionCycleCreditsIsIdempotent(t *testing.T) {
	t.Parallel()
	memoryStore := store.NewMemoryStore()
	server := newTestServer(memoryStore, queue.NewMemoryPublisher())
	handler := server.Handler()

	first := performJSON(t, handler, http.MethodPost, "/v1/me/credits/subscription-cycle", map[string]any{
		"plan":        "monthly",
		"purchase_id": "purchase-123",
	}, "", true, nil)
	if first.Code != http.StatusOK {
		t.Fatalf("first grant status = %d, body = %s", first.Code, first.Body.String())
	}
	var grant models.CreditGrant
	decodeResponse(t, first, &grant)
	if grant.Amount != server.Credits.Policy.MonthlySubscriptionCycleCredits {
		t.Fatalf("grant amount = %d, want %d", grant.Amount, server.Credits.Policy.MonthlySubscriptionCycleCredits)
	}

	second := performJSON(t, handler, http.MethodPost, "/v1/me/credits/subscription-cycle", map[string]any{
		"plan":        "monthly",
		"purchase_id": "purchase-123",
	}, "", true, nil)
	if second.Code != http.StatusOK {
		t.Fatalf("second grant status = %d, body = %s", second.Code, second.Body.String())
	}
	var duplicateGrant models.CreditGrant
	decodeResponse(t, second, &duplicateGrant)
	if duplicateGrant.ID != grant.ID {
		t.Fatal("duplicate grant created a second record")
	}

	balanceResponse := performJSON(t, handler, http.MethodGet, "/v1/me/credits", nil, "", true, nil)
	if balanceResponse.Code != http.StatusOK {
		t.Fatalf("balance status = %d, body = %s", balanceResponse.Code, balanceResponse.Body.String())
	}
	var balance models.CreditBalance
	decodeResponse(t, balanceResponse, &balance)
	if balance.SubscriptionCredits != server.Credits.Policy.MonthlySubscriptionCycleCredits {
		t.Fatalf("subscription credits = %d, want %d", balance.SubscriptionCredits, server.Credits.Policy.MonthlySubscriptionCycleCredits)
	}
}

func latestJobOfType(t *testing.T, jobs []models.ProcessingJob, jobType models.JobType) models.ProcessingJob {
	t.Helper()
	for index := len(jobs) - 1; index >= 0; index-- {
		if jobs[index].Type == jobType {
			return jobs[index]
		}
	}
	t.Fatalf("missing job type %s in %+v", jobType, jobs)
	return models.ProcessingJob{}
}

func completeProjectPhotoPreprocessJobs(t *testing.T, server *Server, memoryStore *store.MemoryStore, projectID string) {
	t.Helper()
}

func TestGrantPromptTranscriptionToken(t *testing.T) {
	t.Parallel()
	memoryStore := store.NewMemoryStore()
	publisher := queue.NewMemoryPublisher()
	server := newTestServer(memoryStore, publisher)
	server.PromptTranscriber = &mockPromptTokenTranscriber{
		mockPromptTranscriber: mockPromptTranscriber{transcript: "unused"},
		token: PromptTranscriptionToken{
			AccessToken:  "temporary-token",
			ExpiresIn:    60,
			WebsocketURL: "wss://api.deepgram.com/v1/listen",
			Model:        "nova-3",
			Language:     "en",
		},
	}
	handler := server.Handler()

	response := performJSON(t, handler, http.MethodPost, "/v1/transcriptions/token", nil, "", true, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("token status = %d, body = %s", response.Code, response.Body.String())
	}
	var token PromptTranscriptionToken
	decodeResponse(t, response, &token)
	if token.AccessToken != "temporary-token" || token.Model != "nova-3" || token.ExpiresIn != 60 {
		t.Fatalf("token = %+v", token)
	}
}

func TestGrantPromptTranscriptionTokenForbidden(t *testing.T) {
	t.Parallel()
	memoryStore := store.NewMemoryStore()
	publisher := queue.NewMemoryPublisher()
	server := newTestServer(memoryStore, publisher)
	server.PromptTranscriber = &mockPromptTokenTranscriber{
		mockPromptTranscriber: mockPromptTranscriber{transcript: "unused"},
		err:                   deepgramGrantError{StatusCode: http.StatusForbidden, Body: "forbidden"},
	}
	handler := server.Handler()

	response := performJSON(t, handler, http.MethodPost, "/v1/transcriptions/token", nil, "", true, nil)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("token status = %d, body = %s", response.Code, response.Body.String())
	}
	var payload map[string]string
	decodeResponse(t, response, &payload)
	if payload["error"] != "transcription_token_forbidden" {
		t.Fatalf("error = %q, body = %s", payload["error"], response.Body.String())
	}
}

type mockPromptTokenTranscriber struct {
	mockPromptTranscriber
	token PromptTranscriptionToken
	err   error
}

func (m *mockPromptTokenTranscriber) GrantPromptToken(_ context.Context, ttlSeconds int) (PromptTranscriptionToken, error) {
	if ttlSeconds != 60 {
		return PromptTranscriptionToken{}, errors.New("unexpected ttl")
	}
	if m.err != nil {
		return PromptTranscriptionToken{}, m.err
	}
	return m.token, nil
}

func TestTranscribePromptAudio(t *testing.T) {
	t.Parallel()
	memoryStore := store.NewMemoryStore()
	publisher := queue.NewMemoryPublisher()
	server := newTestServer(memoryStore, publisher)
	transcriber := &mockPromptTranscriber{transcript: "Make this slower and focus on the sunset."}
	server.PromptTranscriber = transcriber
	handler := server.Handler()

	request := httptest.NewRequest(http.MethodPost, "/v1/transcriptions", strings.NewReader("audio-bytes"))
	request.Header.Set("Authorization", "Bearer test-token")
	request.Header.Set("Content-Type", "audio/mp4")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("transcription status = %d, body = %s", response.Code, response.Body.String())
	}
	var body struct {
		Transcript string `json:"transcript"`
	}
	decodeResponse(t, response, &body)
	if body.Transcript != transcriber.transcript {
		t.Fatalf("transcript = %q, want %q", body.Transcript, transcriber.transcript)
	}
	if string(transcriber.audio) != "audio-bytes" || transcriber.contentType != "audio/mp4" {
		t.Fatalf("transcriber input = %q %q", string(transcriber.audio), transcriber.contentType)
	}
}

func TestTranscribePromptAudioRejectsNonAudio(t *testing.T) {
	t.Parallel()
	memoryStore := store.NewMemoryStore()
	publisher := queue.NewMemoryPublisher()
	server := newTestServer(memoryStore, publisher)
	server.PromptTranscriber = &mockPromptTranscriber{transcript: "unused"}
	handler := server.Handler()

	request := httptest.NewRequest(http.MethodPost, "/v1/transcriptions", strings.NewReader("not-audio"))
	request.Header.Set("Authorization", "Bearer test-token")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("transcription status = %d, body = %s", response.Code, response.Body.String())
	}
}

type mockPromptTranscriber struct {
	transcript    string
	audio         []byte
	contentType   string
	transcribeErr error
}

func (m *mockPromptTranscriber) TranscribePrompt(_ context.Context, audio []byte, contentType string) (string, error) {
	m.audio = append([]byte(nil), audio...)
	m.contentType = contentType
	if m.transcribeErr != nil {
		return "", m.transcribeErr
	}
	return m.transcript, nil
}
