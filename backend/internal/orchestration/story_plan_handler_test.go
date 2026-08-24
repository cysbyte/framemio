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

func ptrFloat64(value float64) *float64 { return &value }
