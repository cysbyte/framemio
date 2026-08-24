package api

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/framemio/backend/internal/ai"
	"github.com/framemio/backend/internal/models"
	"github.com/framemio/backend/internal/queue"
	"github.com/framemio/backend/internal/store"
)

func TestEndToEndMemoryVideoPipelineReusesCompletedStages(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	memoryStore := store.NewMemoryStore()
	publisher := queue.NewMemoryPublisher()
	server := newTestServer(memoryStore, publisher)
	handler := server.Handler()

	const userID = "user-1"
	now := time.Date(2026, time.August, 21, 10, 30, 0, 0, time.UTC)

	photo := models.MediaAsset{
		ID: "photo-1", UserID: userID, Type: models.MediaTypePhoto, Status: models.MediaStatusReady,
		FileName: "IMG_0001.JPG", ContentType: "image/jpeg", FileSizeBytes: 1024, PixelWidth: 4032, PixelHeight: 3024,
		S3Key: "users/user-1/media/photo-1/IMG_0001.JPG", CreatedAt: now, UpdatedAt: now,
	}
	video := models.MediaAsset{
		ID: "video-1", UserID: userID, Type: models.MediaTypeVideo, Status: models.MediaStatusReady,
		FileName: "VID_0001.MOV", ContentType: "video/quicktime", FileSizeBytes: 10 * 1024 * 1024, PixelWidth: 1920, PixelHeight: 1080,
		Orientation: "landscape", S3Key: "users/user-1/media/video-1/VID_0001.MOV", CreatedAt: now, UpdatedAt: now,
	}
	duration := 8.0
	video.DurationSeconds = &duration
	sessionPhoto := models.UploadSession{ID: "upload-photo-1", UserID: userID, MediaID: photo.ID, S3Key: photo.S3Key, Strategy: models.UploadStrategySingle, CreatedAt: now}
	sessionVideo := models.UploadSession{ID: "upload-video-1", UserID: userID, MediaID: video.ID, S3Key: video.S3Key, Strategy: models.UploadStrategySingle, CreatedAt: now}
	if err := memoryStore.CreateMediaUpload(ctx, photo, sessionPhoto); err != nil {
		t.Fatal(err)
	}
	if err := memoryStore.CreateMediaUpload(ctx, video, sessionVideo); err != nil {
		t.Fatal(err)
	}

	project := models.StoryProject{ID: "project-1", UserID: userID, Title: "Weekend memories", Context: "weekend", MediaIDs: []string{photo.ID, video.ID}, Status: models.ProjectStatusDraft, CreatedAt: now, UpdatedAt: now}
	createdProject, _, err := memoryStore.CreateProject(ctx, project, "project-key")
	if err != nil {
		t.Fatal(err)
	}

	analysisResponse := performJSON(t, handler, http.MethodPost, "/projects/"+createdProject.ID+"/analyze", nil, "analysis-key", true, nil)
	if analysisResponse.Code != http.StatusAccepted {
		t.Fatalf("analysis status = %d, body = %s", analysisResponse.Code, analysisResponse.Body.String())
	}
	var analysisJobs jobsResponse
	decodeResponse(t, analysisResponse, &analysisJobs)
	if len(analysisJobs.Jobs) != 2 {
		t.Fatalf("analysis jobs = %+v", analysisJobs)
	}

	var photoAnalysisJobs []string
	var videoPreprocessJobID string
	for _, job := range analysisJobs.Jobs {
		if job.MediaID == photo.ID {
			photoAnalysisJobs = append(photoAnalysisJobs, job.ID)
		}
		if job.MediaID == video.ID && job.Type == models.JobTypeMediaPreprocess {
			videoPreprocessJobID = job.ID
		}
	}
	if len(photoAnalysisJobs) != 1 || videoPreprocessJobID == "" {
		t.Fatalf("unexpected initial jobs: %+v", analysisJobs.Jobs)
	}

	photoAnalysisRecord := models.MediaAnalysis{ID: "analysis-photo-1", ProjectID: createdProject.ID, MediaID: photo.ID, Labels: []string{"friends"}, PeopleCount: 3, QualityScore: 0.91, CreatedAt: now, UpdatedAt: now}
	photoAnalysis := models.PhotoAnalysis{ID: "analysis-photo-1", PhotoID: photo.ID, ProjectID: createdProject.ID, AnalysisVersion: "photo-analysis-v1", TimestampMicros: 1_000_000, Location: "home", People: []string{"friends"}, Labels: []string{"weekend", "friends"}, Caption: "A warm moment", Description: "Friends together", MemoryValue: 0.9, EmotionalValue: 0.88, VisualQuality: 0.76, StoryRelevance: 0.92, Selected: true, CreatedAt: now}
	if err := memoryStore.SaveAnalysis(ctx, userID, photoAnalysisRecord); err != nil {
		t.Fatal(err)
	}
	if _, err := server.Orchestrator.UpdateJobStatus(ctx, photoAnalysisJobs[0], models.JobStatusCompleted, 1, "", "", "", ""); err != nil {
		t.Fatal(err)
	}

	if _, err := server.Orchestrator.UpdateJobStatus(ctx, videoPreprocessJobID, models.JobStatusCompleted, 1, "", "", "", ""); err != nil {
		t.Fatal(err)
	}
	videoTranscriptJob := latestPublishedJob(t, publisher, models.JobTypeTranscribe)
	if videoTranscriptJob.MediaID != video.ID {
		t.Fatalf("transcribe job media id = %s, want %s", videoTranscriptJob.MediaID, video.ID)
	}

	videoAnalysisVersion := "video-analysis-v1"
	videoAnalysis := models.VideoAnalysis{
		ID: "video-analysis-1", VideoID: video.ID, ProjectID: createdProject.ID, AnalysisVersion: videoAnalysisVersion,
		DurationMicros: 8_000_000, Transcript: models.VideoTranscript{HasSpeech: true, Segments: []models.VideoTranscriptSegment{{StartMicros: 1_000_000, EndMicros: 2_500_000, Text: "We made it."}}},
		Scenes:         []models.VideoScene{{SceneID: "scene-1", StartMicros: 0, EndMicros: 8_000_000, RepresentativeFrames: []models.Frame{{FrameID: "frame-1", TimestampMicros: 1_500_000, S3Key: "users/user-1/video-analysis/video-1/frame-1.jpg"}}}},
		Segments:       []models.VideoSegment{{SegmentID: "segment-1", SceneID: "scene-1", StartMicros: 1_000_000, EndMicros: 4_000_000, Description: "Friends laughing", Importance: 0.96, VisualQuality: 0.54, MemoryValue: 0.95, EmotionalValue: 0.97, StoryRelevance: 0.94, Recommended: true, Reasons: []string{"warm laughter"}, RecommendedUsage: "story"}},
		DetectedPeople: []string{"friends"}, DetectedActivities: []string{"laughing"}, DetectedLocations: []string{"home"},
		QualitySignals: models.VideoQualitySignals{OverallScore: 0.78, SceneChangesPM: 1.2, AverageSharpness: 0.72, AverageBrightness: 0.66}, CreatedAt: now,
	}
	if err := memoryStore.SaveVideoAnalysis(ctx, userID, videoAnalysis); err != nil {
		t.Fatal(err)
	}
	if _, err := server.Orchestrator.UpdateJobStatus(ctx, videoTranscriptJob.JobID, models.JobStatusCompleted, 1, "", "", "", ""); err != nil {
		t.Fatal(err)
	}
	videoUnderstandJob := latestPublishedJob(t, publisher, models.JobTypeVideoUnderstand)
	if videoUnderstandJob.MediaID != video.ID {
		t.Fatalf("video-understand media id = %s, want %s", videoUnderstandJob.MediaID, video.ID)
	}
	if _, err := server.Orchestrator.UpdateJobStatus(ctx, videoUnderstandJob.JobID, models.JobStatusCompleted, 1, "", "", "", ""); err != nil {
		t.Fatal(err)
	}

	projectStatus := performJSON(t, handler, http.MethodGet, "/projects/"+createdProject.ID+"/status", nil, "", true, nil)
	if projectStatus.Code != http.StatusOK {
		t.Fatalf("project status = %d, body = %s", projectStatus.Code, projectStatus.Body.String())
	}
	var statusBody struct {
		Project models.StoryProject    `json:"project"`
		Jobs    []models.ProcessingJob `json:"jobs"`
	}
	decodeResponse(t, projectStatus, &statusBody)
	if statusBody.Project.Status != models.ProjectStatusAnalyzing {
		t.Fatalf("project status = %s, want analyzing before story plan callback", statusBody.Project.Status)
	}
	if countType(statusBody.Jobs, models.JobTypeStoryPlan) != 1 {
		t.Fatalf("expected story plan job to be queued, got %+v", statusBody.Jobs)
	}

	planner := &ai.Planner{Now: func() time.Time { return now }}
	plan, err := planner.Plan(ctx, ai.Request{
		ProjectID:        createdProject.ID,
		Version:          1,
		DesiredDuration:  14 * time.Second,
		StoryType:        models.StoryTypeWeekend,
		VisualStyle:      models.VisualStyleCinematic,
		SelectedMediaIDs: []string{photo.ID, video.ID},
		PhotoAnalyses:    []models.PhotoAnalysis{photoAnalysis},
		VideoAnalyses:    []models.VideoAnalysis{videoAnalysis},
		MediaClusters:    []models.MediaCluster{{ID: "cluster-1", ProjectID: createdProject.ID, MediaIDs: []string{photo.ID, video.ID}, Label: "weekend"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := ai.ValidateStoryPlan(plan); err != nil {
		t.Fatal(err)
	}

	storyPlanJob := latestPublishedJob(t, publisher, models.JobTypeStoryPlan)
	if storyPlanJob.MediaID != "" {
		t.Fatalf("story plan job media id = %s, want empty", storyPlanJob.MediaID)
	}
	storyPlanResponse := performJSON(t, handler, http.MethodPost, "/internal/jobs/"+storyPlanJob.JobID+"/story-plan", plan, "", false, map[string]string{"X-FrameMio-Internal-Token": testInternalToken})
	if storyPlanResponse.Code != http.StatusOK {
		t.Fatalf("story plan callback = %d, body = %s", storyPlanResponse.Code, storyPlanResponse.Body.String())
	}

	validatedStatus := performJSON(t, handler, http.MethodGet, "/projects/"+createdProject.ID+"/status", nil, "", true, nil)
	decodeResponse(t, validatedStatus, &statusBody)
	if statusBody.Project.Status != models.ProjectStatusStoryReady {
		t.Fatalf("project status = %s, want story_ready", statusBody.Project.Status)
	}
	if countType(statusBody.Jobs, models.JobTypeStoryValidate) != 0 {
		t.Fatalf("validation job should not be queued until rendering, got %+v", statusBody.Jobs)
	}

	renderResponse := performJSON(t, handler, http.MethodPost, "/projects/"+createdProject.ID+"/render", map[string]any{"output_preset": "social"}, "render-key", true, nil)
	if renderResponse.Code != http.StatusAccepted {
		t.Fatalf("render status = %d, body = %s", renderResponse.Code, renderResponse.Body.String())
	}
	var renderJobs jobsResponse
	decodeResponse(t, renderResponse, &renderJobs)
	duplicateRender := performJSON(t, handler, http.MethodPost, "/projects/"+createdProject.ID+"/render", map[string]any{"output_preset": "social"}, "render-key", true, nil)
	var duplicateRenderJobs jobsResponse
	decodeResponse(t, duplicateRender, &duplicateRenderJobs)
	if renderJobs.JobID != duplicateRenderJobs.JobID {
		t.Fatal("idempotent render request created a duplicate job")
	}
	renderingStatus := performJSON(t, handler, http.MethodGet, "/projects/"+createdProject.ID+"/status", nil, "", true, nil)
	decodeResponse(t, renderingStatus, &statusBody)
	if statusBody.Project.Status != models.ProjectStatusRendering {
		t.Fatalf("project status = %s, want rendering after render request", statusBody.Project.Status)
	}
	if countType(statusBody.Jobs, models.JobTypeStoryValidate) != 1 || countType(statusBody.Jobs, models.JobTypeVideoRender) != 1 {
		t.Fatalf("expected validation and render jobs to be queued, got %+v", statusBody.Jobs)
	}
	if countMessageType(publisher.Messages(), models.JobTypeVideoUnderstand) != 1 {
		t.Fatalf("video understanding reran unexpectedly: %+v", publisher.Messages())
	}
	if countMessageType(publisher.Messages(), models.JobTypeVideoRender) != 1 {
		t.Fatalf("render requested duplicate render work unexpectedly: %+v", publisher.Messages())
	}

	renderJob, err := memoryStore.GetJob(ctx, userID, renderJobs.JobID)
	if err != nil {
		t.Fatal(err)
	}
	if renderJob.Type != models.JobTypeVideoRender {
		t.Fatalf("render job type = %s, want render", renderJob.Type)
	}
	videoRenderJob := latestPublishedJob(t, publisher, models.JobTypeVideoRender)
	if videoRenderJob.JobType != models.JobTypeVideoRender {
		t.Fatalf("latest job = %+v", videoRenderJob)
	}
	renderInputResponse := performJSON(t, handler, http.MethodGet, "/internal/jobs/"+videoRenderJob.JobID+"/render-input", nil, "", false, map[string]string{"X-FrameMio-Internal-Token": testInternalToken})
	if renderInputResponse.Code != http.StatusOK {
		t.Fatalf("render input status = %d, body = %s", renderInputResponse.Code, renderInputResponse.Body.String())
	}
	var renderInput struct {
		JobID       string `json:"job_id"`
		ProjectID   string `json:"project_id"`
		OutputS3Key string `json:"output_s3_key"`
		StoryPlan   struct {
			ID      string `json:"id"`
			Version int    `json:"version"`
		} `json:"story_plan"`
		MediaAssets []struct {
			ID              string  `json:"id"`
			S3Key           string  `json:"s3_key"`
			DurationSeconds float64 `json:"duration_seconds"`
		} `json:"media_assets"`
	}
	decodeResponse(t, renderInputResponse, &renderInput)
	if renderInput.JobID != videoRenderJob.JobID || renderInput.ProjectID != createdProject.ID || renderInput.StoryPlan.ID != plan.ID {
		t.Fatalf("unexpected render input: %+v", renderInput)
	}
	if len(renderInput.MediaAssets) != 2 || renderInput.MediaAssets[1].S3Key == "" {
		t.Fatalf("render input media assets missing source references: %+v", renderInput.MediaAssets)
	}
	if renderInput.OutputS3Key == "" || renderInput.OutputS3Key != renderJob.Configuration["output_s3_key"] {
		t.Fatalf("render output key = %q, job config = %+v", renderInput.OutputS3Key, renderJob.Configuration)
	}
	if _, err := server.Orchestrator.UpdateJobStatus(ctx, videoRenderJob.JobID, models.JobStatusCompleted, 1, "", "", renderInput.OutputS3Key, "video/mp4"); err != nil {
		t.Fatal(err)
	}

	finalStatus := performJSON(t, handler, http.MethodGet, "/projects/"+createdProject.ID+"/status", nil, "", true, nil)
	decodeResponse(t, finalStatus, &statusBody)
	if statusBody.Project.Status != models.ProjectStatusReady {
		t.Fatalf("final project status = %s, want ready", statusBody.Project.Status)
	}

	if _, err := memoryStore.GetVideoAnalysis(ctx, userID, video.ID, videoAnalysisVersion); err != nil {
		t.Fatal(err)
	}
	if _, err := memoryStore.GetStoryPlan(ctx, userID, createdProject.ID); err != nil {
		t.Fatal(err)
	}
}

func countType(jobs []models.ProcessingJob, jobType models.JobType) int {
	count := 0
	for _, job := range jobs {
		if job.Type == jobType {
			count++
		}
	}
	return count
}

func countMessageType(messages []queue.JobMessage, jobType models.JobType) int {
	count := 0
	for _, message := range messages {
		if message.JobType == jobType {
			count++
		}
	}
	return count
}

func latestPublishedJob(t *testing.T, publisher *queue.MemoryPublisher, jobType models.JobType) queue.JobMessage {
	t.Helper()
	messages := publisher.Messages()
	for index := len(messages) - 1; index >= 0; index-- {
		if messages[index].JobType == jobType {
			return messages[index]
		}
	}
	t.Fatalf("missing job type %s in %+v", jobType, messages)
	return queue.JobMessage{}
}
