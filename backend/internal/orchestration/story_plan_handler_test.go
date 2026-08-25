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
	if !hasStoryMoment(plan.Moments, photo.ID, models.MediaTypePhoto) {
		t.Fatalf("expected mixed story plan to retain selected photo moment, got %+v", plan.Moments)
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

func TestMediaClustersGroupsCoherentMixedMedia(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 25, 8, 30, 0, 0, time.UTC)
	photo1Time := now
	photo2Time := now.Add(45 * time.Minute)
	videoTime := now.Add(70 * time.Minute)
	project := models.StoryProject{ID: "project-cluster", UserID: "user-1", MediaIDs: []string{"photo-1", "photo-2", "video-1"}, CreatedAt: now}
	mediaByID := map[string]models.MediaAsset{
		"photo-1": {ID: "photo-1", CreationDate: &photo1Time},
		"photo-2": {ID: "photo-2", CreationDate: &photo2Time},
		"video-1": {ID: "video-1", CreationDate: &videoTime},
	}
	clusters := mediaClusters(project, mediaByID, []models.PhotoAnalysis{
		{PhotoID: "photo-1", Location: "Lake", Labels: []string{"family"}},
		{PhotoID: "photo-2", Location: "Lake", Labels: []string{"family", "park"}},
	}, []models.VideoAnalysis{{VideoID: "video-1", DetectedLocations: []string{"Lake"}, DetectedActivities: []string{"family"}}})
	if len(clusters) != 1 || len(clusters[0].MediaIDs) != 3 {
		t.Fatalf("expected coherent mixed media to form one cluster, got %+v", clusters)
	}
}

func TestMediaClustersSeparatesUnrelatedMedia(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 25, 8, 0, 0, 0, time.UTC)
	coffeeTime := now
	catTime := now.Add(3 * time.Hour)
	towerTime := now.Add(7 * time.Hour)
	gameTime := now.Add(10 * time.Hour)
	project := models.StoryProject{ID: "project-unrelated", UserID: "user-1", MediaIDs: []string{"coffee", "cat", "tower", "game"}, CreatedAt: now}
	mediaByID := map[string]models.MediaAsset{
		"coffee": {ID: "coffee", CreationDate: &coffeeTime},
		"cat":    {ID: "cat", CreationDate: &catTime},
		"tower":  {ID: "tower", CreationDate: &towerTime},
		"game":   {ID: "game", CreationDate: &gameTime},
	}
	clusters := mediaClusters(project, mediaByID, []models.PhotoAnalysis{
		{PhotoID: "coffee", Labels: []string{"coffee"}},
		{PhotoID: "cat", Labels: []string{"cat"}},
		{PhotoID: "tower", Location: "Tokyo Tower", Labels: []string{"landmark"}},
	}, []models.VideoAnalysis{{VideoID: "game", DetectedActivities: []string{"football"}}})
	if len(clusters) < 2 {
		t.Fatalf("expected unrelated media to remain separated, got %+v", clusters)
	}
}

func TestDefaultStoryDurationAdaptsToMemorySetSize(t *testing.T) {
	t.Parallel()
	if got := defaultStoryDuration(models.StoryProject{MediaIDs: []string{"a", "b", "c", "d"}}); got != 16*time.Second {
		t.Fatalf("4-media duration = %s, want 16s", got)
	}
	large := models.StoryProject{MediaIDs: []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"}}
	if got := defaultStoryDuration(large); got != 30*time.Second {
		t.Fatalf("large story duration = %s, want 30s", got)
	}
}
