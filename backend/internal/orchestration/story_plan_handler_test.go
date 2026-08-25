package orchestration

import (
	"context"
	"testing"
	"time"

	"github.com/framemio/backend/internal/models"
	"github.com/framemio/backend/internal/store"
)

func TestStoryPlanHandlerUsesPersistedVideoAnalysis(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	memoryStore := store.NewMemoryStore()
	now := time.Date(2026, time.August, 21, 10, 0, 0, 0, time.UTC)
	video := models.MediaAsset{
		ID:              "video-1",
		UserID:          "user-1",
		Type:            models.MediaTypeVideo,
		Status:          models.MediaStatusReady,
		FileName:        "IMG_0216.MOV",
		ContentType:     "video/quicktime",
		FileSizeBytes:   100,
		S3Key:           "source/video-1",
		UploadID:        "upload-1",
		CreationDate:    &now,
		DurationSeconds: ptrFloat64(12),
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := memoryStore.CreateMediaUpload(ctx, video, models.UploadSession{ID: video.UploadID, UserID: video.UserID, MediaID: video.ID, S3Key: video.S3Key, Strategy: models.UploadStrategySingle, CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	project := models.StoryProject{ID: "project-1", UserID: video.UserID, Title: "Weekend", MediaIDs: []string{video.ID}, Status: models.ProjectStatusAnalyzing, CreatedAt: now, UpdatedAt: now}
	if _, _, err := memoryStore.CreateProject(ctx, project, "project-key"); err != nil {
		t.Fatal(err)
	}
	if err := memoryStore.SaveVideoAnalysis(ctx, video.UserID, models.VideoAnalysis{
		ID:              "analysis-video-1",
		VideoID:         video.ID,
		ProjectID:       project.ID,
		AnalysisVersion: "video-analysis-v1",
		DurationMicros:  12_000_000,
		Segments: []models.VideoSegment{{
			SegmentID:      "segment-1",
			SceneID:        "scene-1",
			StartMicros:    1_000_000,
			EndMicros:      4_000_000,
			Description:    "A joyful wave at the camera",
			Importance:     0.95,
			MemoryValue:    0.9,
			EmotionalValue: 0.9,
			VisualQuality:  0.85,
			StoryRelevance: 0.95,
			Recommended:    true,
			Reasons:        []string{"clear emotional highlight"},
		}},
		QualitySignals: models.VideoQualitySignals{OverallScore: 0.9},
		CreatedAt:      now,
	}); err != nil {
		t.Fatal(err)
	}
	job := models.ProcessingJob{ID: "job-story-plan", UserID: video.UserID, ProjectID: project.ID, Type: models.JobTypeStoryPlan, Status: models.JobStatusRunning, CreatedAt: now, UpdatedAt: now}

	handler := StoryPlanHandler{Store: memoryStore, Now: func() time.Time { return now }}
	if err := handler.HandleJob(ctx, job); err != nil {
		t.Fatal(err)
	}
	plan, err := memoryStore.GetStoryPlan(ctx, video.UserID, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Moments) != 1 {
		t.Fatalf("expected one planned video moment, got %+v", plan.Moments)
	}
	moment := plan.Moments[0]
	if moment.AssetID != video.ID || moment.MediaType != models.MediaTypeVideo {
		t.Fatalf("story plan did not reference video media: %+v", moment)
	}
	if moment.VideoAnalysisVersion != "video-analysis-v1" || moment.StartMicros != 1_000_000 || moment.EndMicros != 4_000_000 {
		t.Fatalf("story plan lost video analysis/range contract: %+v", moment)
	}
}

func TestStoryPlanHandlerReusesVideoAnalysisForSelectedVideo(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	memoryStore := store.NewMemoryStore()
	now := time.Date(2026, time.August, 25, 15, 0, 0, 0, time.UTC)
	video := models.MediaAsset{
		ID:              "video-1",
		UserID:          "user-1",
		Type:            models.MediaTypeVideo,
		Status:          models.MediaStatusReady,
		FileName:        "IMG_0216.MOV",
		ContentType:     "video/quicktime",
		FileSizeBytes:   100,
		S3Key:           "source/video-1",
		UploadID:        "upload-video-1",
		DurationSeconds: ptrFloat64(12),
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	photo := models.MediaAsset{
		ID:            "photo-1",
		UserID:        "user-1",
		Type:          models.MediaTypePhoto,
		Status:        models.MediaStatusReady,
		FileName:      "IMG_0001.JPG",
		ContentType:   "image/jpeg",
		FileSizeBytes: 100,
		S3Key:         "source/photo-1",
		UploadID:      "upload-photo-1",
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	for _, media := range []models.MediaAsset{video, photo} {
		if err := memoryStore.CreateMediaUpload(ctx, media, models.UploadSession{ID: media.UploadID, UserID: media.UserID, MediaID: media.ID, S3Key: media.S3Key, Strategy: models.UploadStrategySingle, CreatedAt: now}); err != nil {
			t.Fatal(err)
		}
	}
	originalProject := models.StoryProject{ID: "project-original", UserID: video.UserID, Title: "Original", MediaIDs: []string{video.ID}, Status: models.ProjectStatusReady, CreatedAt: now, UpdatedAt: now}
	if _, _, err := memoryStore.CreateProject(ctx, originalProject, "project-original-key"); err != nil {
		t.Fatal(err)
	}
	newProject := models.StoryProject{ID: "project-new", UserID: video.UserID, Title: "New", MediaIDs: []string{video.ID, photo.ID}, Status: models.ProjectStatusAnalyzing, CreatedAt: now, UpdatedAt: now}
	if _, _, err := memoryStore.CreateProject(ctx, newProject, "project-new-key"); err != nil {
		t.Fatal(err)
	}
	if err := memoryStore.SaveAnalysis(ctx, photo.UserID, models.MediaAnalysis{ID: "analysis-photo-1", MediaID: photo.ID, ProjectID: newProject.ID, QualityScore: 0.5, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := memoryStore.SaveVideoAnalysis(ctx, video.UserID, models.VideoAnalysis{
		ID:              "analysis-video-1",
		VideoID:         video.ID,
		ProjectID:       originalProject.ID,
		AnalysisVersion: "video-analysis-v1",
		DurationMicros:  12_000_000,
		Segments: []models.VideoSegment{{
			SegmentID:      "segment-1",
			SceneID:        "scene-1",
			StartMicros:    2_000_000,
			EndMicros:      7_000_000,
			Description:    "A joyful video moment",
			Importance:     0.9,
			MemoryValue:    0.9,
			EmotionalValue: 0.9,
			VisualQuality:  0.85,
			StoryRelevance: 0.9,
			Recommended:    true,
		}},
		QualitySignals: models.VideoQualitySignals{OverallScore: 0.9},
		CreatedAt:      now,
	}); err != nil {
		t.Fatal(err)
	}

	job := models.ProcessingJob{ID: "job-story-plan", UserID: video.UserID, ProjectID: newProject.ID, Type: models.JobTypeStoryPlan, Status: models.JobStatusRunning, CreatedAt: now, UpdatedAt: now}
	handler := StoryPlanHandler{Store: memoryStore, Now: func() time.Time { return now }}
	if err := handler.HandleJob(ctx, job); err != nil {
		t.Fatal(err)
	}
	plan, err := memoryStore.GetStoryPlan(ctx, video.UserID, newProject.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !hasStoryMoment(plan.Moments, video.ID, models.MediaTypeVideo) {
		t.Fatalf("expected reused selected video analysis to produce a video moment, got %+v", plan.Moments)
	}
}

func hasStoryMoment(moments []models.StoryMoment, mediaID string, mediaType models.MediaType) bool {
	for _, moment := range moments {
		if moment.AssetID == mediaID && moment.MediaType == mediaType {
			return true
		}
	}
	return false
}

func ptrFloat64(value float64) *float64 { return &value }
