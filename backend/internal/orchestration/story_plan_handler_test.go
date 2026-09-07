package orchestration

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/framemio/backend/internal/ai"
	"github.com/framemio/backend/internal/models"
	"github.com/framemio/backend/internal/storage"
	"github.com/framemio/backend/internal/store"
	"github.com/framemio/backend/internal/storyvalidation"
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

func TestStoryPlanHandlerPersistsExternalPlannerOutput(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	memoryStore, project := storyRevisionHandlerFixture(t)
	now := project.CreatedAt
	planner := fixedStoryPlanner{plan: models.StoryPlan{
		SchemaVersion:       models.StoryPlanSchemaVersionV1,
		ID:                  "plan-external-valid",
		ProjectID:           project.ID,
		Version:             2,
		RequestedDurationMS: 4000,
		StoryType:           models.StoryTypeTrip,
		VisualStyle:         models.VisualStyleCinematic,
		TitleText:           "GPT Beach Story",
		EndingText:          "Until next time.",
		Moments: []models.StoryMoment{{
			MomentID:   "moment-photo-revision",
			AssetID:    "photo-revision",
			MediaID:    "photo-revision",
			MediaType:  models.MediaTypePhoto,
			Narrative:  "A beach memory opens the story.",
			Caption:    "Beach memory",
			DurationMS: 4000,
			CropIntent: "fit",
			Transition: "fade_in",
			Reason:     "Strong opening photo",
			Score:      0.9,
		}},
		Captions:        []models.StoryCaption{{MomentID: "moment-photo-revision", Text: "Beach memory", StartMS: 0, EndMS: 4000}},
		CaptionStyle:    &models.CaptionStyle{Preset: "cinematic_subtitle", FontFamily: "inter-bold", FontSize: 48, TextColor: "#FFFFFF", StrokeColor: "#000000", StrokeWidth: 4, BackgroundEnabled: true, BackgroundColor: "#000000", BackgroundOpacity: 0.32, Position: "bottom", BottomOffset: 150, MaxLines: 2, MaxCharactersPerLine: 32},
		Audio:           &models.StoryAudio{Source: "original", OriginalAudio: boolPtr(true), OriginalVolume: 0.4},
		Output:          models.StoryOutput{AspectRatio: "9:16", MaxDurationS: 4, Preset: "premium-memory"},
		TotalDurationMS: 4000,
		CreatedAt:       now,
	}}
	job := models.ProcessingJob{ID: "job-external-valid", UserID: project.UserID, ProjectID: project.ID, Type: models.JobTypeStoryPlan, Status: models.JobStatusRunning, Configuration: map[string]string{"story_plan_version": "2"}, CreatedAt: now, UpdatedAt: now}
	handler := StoryPlanHandler{Store: memoryStore, Planner: planner, Validator: storyvalidation.New(memoryStore, storage.StaticObjectStorage{}, storyvalidation.DefaultConfig()), Now: func() time.Time { return now }}
	if err := handler.HandleJob(ctx, job); err != nil {
		t.Fatal(err)
	}
	latest, err := memoryStore.GetStoryPlan(ctx, project.UserID, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if latest.ID != "plan-external-valid" || latest.Version != 2 || len(latest.Captions) != 1 {
		t.Fatalf("external planner output was not persisted: %+v", latest)
	}
}

func TestStoryPlanHandlerSanitizesExternalPlannerTransitionsBeforeValidation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	memoryStore := store.NewMemoryStore()
	now := time.Date(2026, time.September, 7, 15, 30, 0, 0, time.UTC)
	photos := []models.MediaAsset{
		{ID: "photo-transition-1", UserID: "user-1", Type: models.MediaTypePhoto, Status: models.MediaStatusReady, FileName: "IMG_0001.JPG", ContentType: "image/jpeg", FileSizeBytes: 100, S3Key: "source/photo-transition-1", UploadID: "upload-transition-1", CreatedAt: now, UpdatedAt: now},
		{ID: "photo-transition-2", UserID: "user-1", Type: models.MediaTypePhoto, Status: models.MediaStatusReady, FileName: "IMG_0002.JPG", ContentType: "image/jpeg", FileSizeBytes: 100, S3Key: "source/photo-transition-2", UploadID: "upload-transition-2", CreatedAt: now, UpdatedAt: now},
	}
	for _, photo := range photos {
		if err := memoryStore.CreateMediaUpload(ctx, photo, models.UploadSession{ID: photo.UploadID, UserID: photo.UserID, MediaID: photo.ID, S3Key: photo.S3Key, Strategy: models.UploadStrategySingle, CreatedAt: now}); err != nil {
			t.Fatal(err)
		}
	}
	project := models.StoryProject{ID: "project-transition", UserID: "user-1", Title: "Transition", MediaIDs: []string{photos[0].ID, photos[1].ID}, Status: models.ProjectStatusAnalyzing, CreatedAt: now, UpdatedAt: now}
	if _, _, err := memoryStore.CreateProject(ctx, project, "project-transition-key"); err != nil {
		t.Fatal(err)
	}
	planner := fixedStoryPlanner{plan: models.StoryPlan{
		SchemaVersion:       models.StoryPlanSchemaVersionV1,
		ID:                  "plan-transition",
		ProjectID:           project.ID,
		Version:             1,
		RequestedDurationMS: 4000,
		StoryType:           models.StoryTypeTrip,
		VisualStyle:         models.VisualStyleCinematic,
		TitleText:           "Unsupported Transitions",
		EndingText:          "Until next time.",
		Moments: []models.StoryMoment{
			{MomentID: "moment-transition-1", AssetID: photos[0].ID, MediaID: photos[0].ID, MediaType: models.MediaTypePhoto, Narrative: "Opening memory", DurationMS: 2000, CropIntent: "fit", Transition: "iris wipe", Reason: "AI-specific transition"},
			{MomentID: "moment-transition-2", AssetID: photos[1].ID, MediaID: photos[1].ID, MediaType: models.MediaTypePhoto, Narrative: "Second memory", DurationMS: 2000, CropIntent: "fit", Transition: "push-left", Reason: "AI-specific transition"},
		},
		Output:          models.StoryOutput{AspectRatio: "9:16", MaxDurationS: 4, Preset: "premium-memory"},
		TotalDurationMS: 4000,
		CreatedAt:       now,
	}}
	job := models.ProcessingJob{ID: "job-transition", UserID: project.UserID, ProjectID: project.ID, Type: models.JobTypeStoryPlan, Status: models.JobStatusRunning, CreatedAt: now, UpdatedAt: now}
	handler := StoryPlanHandler{Store: memoryStore, Planner: planner, Validator: storyvalidation.New(memoryStore, storage.StaticObjectStorage{}, storyvalidation.DefaultConfig()), Now: func() time.Time { return now }}
	if err := handler.HandleJob(ctx, job); err != nil {
		t.Fatal(err)
	}
	latest, err := memoryStore.GetStoryPlan(ctx, project.UserID, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := latest.Moments[0].Transition; got != "fade_in" {
		t.Fatalf("first unsupported transition = %q, want fade_in", got)
	}
	if got := latest.Moments[1].Transition; got != "cut" {
		t.Fatalf("later unsupported transition = %q, want cut", got)
	}
}

func TestStoryPlanHandlerRejectsExternalPlannerInvalidMedia(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	memoryStore, project := storyRevisionHandlerFixture(t)
	now := project.CreatedAt
	planner := fixedStoryPlanner{plan: models.StoryPlan{
		SchemaVersion:       models.StoryPlanSchemaVersionV1,
		ID:                  "plan-external-invalid",
		ProjectID:           project.ID,
		Version:             2,
		RequestedDurationMS: 1000,
		TitleText:           "Invalid",
		EndingText:          "Invalid",
		Moments:             []models.StoryMoment{{MomentID: "invented", AssetID: "invented-media", MediaID: "invented-media", MediaType: models.MediaTypePhoto, DurationMS: 1000, CropIntent: "fit", Transition: "fade_in"}},
		Output:              models.StoryOutput{AspectRatio: "9:16", MaxDurationS: 1, Preset: "premium-memory"},
		TotalDurationMS:     1000,
		CreatedAt:           now,
	}}
	job := models.ProcessingJob{ID: "job-external-invalid", UserID: project.UserID, ProjectID: project.ID, Type: models.JobTypeStoryPlan, Status: models.JobStatusRunning, Configuration: map[string]string{"story_plan_version": "2"}, CreatedAt: now, UpdatedAt: now}
	handler := StoryPlanHandler{Store: memoryStore, Planner: planner, Validator: storyvalidation.New(memoryStore, storage.StaticObjectStorage{}, storyvalidation.DefaultConfig()), Now: func() time.Time { return now }}
	if err := handler.HandleJob(ctx, job); err == nil {
		t.Fatal("expected invalid external planner media reference to fail validation")
	}
	latest, err := memoryStore.GetStoryPlan(ctx, project.UserID, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if latest.Version != 1 {
		t.Fatalf("invalid external plan should not become active, got version %d", latest.Version)
	}
	if _, err := memoryStore.GetStoryPlanVersion(ctx, project.UserID, project.ID, 2); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("invalid external plan should not persist v2, got %v", err)
	}
}

func TestStoryPlanRevisionFailureLeavesBasePlanActive(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	memoryStore, project := storyRevisionHandlerFixture(t)
	job := models.ProcessingJob{ID: "job-revision-fail", UserID: project.UserID, ProjectID: project.ID, Type: models.JobTypeStoryPlan, Status: models.JobStatusRunning, Configuration: map[string]string{"revision_mode": "ai_story_plan_revision", "base_story_plan_version": "1", "story_plan_version": "2", "user_instructions": "Make it shorter."}, CreatedAt: project.CreatedAt, UpdatedAt: project.CreatedAt}
	handler := StoryPlanHandler{Store: memoryStore, RevisionPlanner: failingRevisionPlanner{}, Now: func() time.Time { return project.CreatedAt }}
	if err := handler.HandleJob(ctx, job); err == nil {
		t.Fatal("expected revision planner failure")
	}
	latest, err := memoryStore.GetStoryPlan(ctx, project.UserID, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if latest.Version != 1 {
		t.Fatalf("base plan should remain active, got version %d", latest.Version)
	}
	if _, err := memoryStore.GetStoryPlanVersion(ctx, project.UserID, project.ID, 2); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("invalid revision should not persist v2, got %v", err)
	}
}

func TestStoryPlanRevisionValidationFailureLeavesBasePlanActive(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	memoryStore, project := storyRevisionHandlerFixture(t)
	job := models.ProcessingJob{ID: "job-revision-invalid", UserID: project.UserID, ProjectID: project.ID, Type: models.JobTypeStoryPlan, Status: models.JobStatusRunning, Configuration: map[string]string{"revision_mode": "ai_story_plan_revision", "base_story_plan_version": "1", "story_plan_version": "2", "user_instructions": "Remove everything."}, CreatedAt: project.CreatedAt, UpdatedAt: project.CreatedAt}
	handler := StoryPlanHandler{Store: memoryStore, RevisionPlanner: invalidRevisionPlanner{}, Validator: rejectingRevisionValidator{}, Now: func() time.Time { return project.CreatedAt }}
	if err := handler.HandleJob(ctx, job); err == nil {
		t.Fatal("expected validation failure")
	}
	latest, err := memoryStore.GetStoryPlan(ctx, project.UserID, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if latest.Version != 1 {
		t.Fatalf("base plan should remain active, got version %d", latest.Version)
	}
	if _, err := memoryStore.GetStoryPlanVersion(ctx, project.UserID, project.ID, 2); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("rejected revision should not persist v2, got %v", err)
	}
}

func TestStoryPlanRevisionAllowsAIFlexibleDurationUpToSixtySeconds(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	memoryStore, project := storyRevisionHandlerFixture(t)
	job := models.ProcessingJob{ID: "job-revision-flex-duration", UserID: project.UserID, ProjectID: project.ID, Type: models.JobTypeStoryPlan, Status: models.JobStatusRunning, Configuration: map[string]string{"revision_mode": "ai_story_plan_revision", "base_story_plan_version": "1", "story_plan_version": "2", "user_instructions": "Make each moment longer."}, CreatedAt: project.CreatedAt, UpdatedAt: project.CreatedAt}
	planner := &capturingRevisionPlanner{}
	handler := StoryPlanHandler{Store: memoryStore, RevisionPlanner: planner, Now: func() time.Time { return project.CreatedAt }}
	if err := handler.HandleJob(ctx, job); err != nil {
		t.Fatal(err)
	}
	if planner.input.Constraints.TargetDurationFixed {
		t.Fatal("expected revision duration to be AI-selected when no explicit target duration is set")
	}
	if planner.input.Constraints.MaxDurationMS != 60000 {
		t.Fatalf("max duration = %d, want 60000", planner.input.Constraints.MaxDurationMS)
	}
	plan, err := memoryStore.GetStoryPlan(ctx, project.UserID, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if plan.TotalDurationMS != 12000 || plan.Output.MaxDurationS != 12 || plan.RequestedDurationMS != 12000 {
		t.Fatalf("expected longer AI-selected duration to persist, got total=%d requested=%d max=%d", plan.TotalDurationMS, plan.RequestedDurationMS, plan.Output.MaxDurationS)
	}
}

func TestStoryPlanRevisionKeepsExplicitTargetDurationFixed(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	memoryStore, project := storyRevisionHandlerFixture(t)
	job := models.ProcessingJob{ID: "job-revision-fixed-duration", UserID: project.UserID, ProjectID: project.ID, Type: models.JobTypeStoryPlan, Status: models.JobStatusRunning, Configuration: map[string]string{"revision_mode": "ai_story_plan_revision", "base_story_plan_version": "1", "story_plan_version": "2", "target_duration_seconds": "15", "user_instructions": "Make this exactly the selected length."}, CreatedAt: project.CreatedAt, UpdatedAt: project.CreatedAt}
	planner := &capturingRevisionPlanner{}
	handler := StoryPlanHandler{Store: memoryStore, RevisionPlanner: planner, Now: func() time.Time { return project.CreatedAt }}
	if err := handler.HandleJob(ctx, job); err != nil {
		t.Fatal(err)
	}
	if !planner.input.Constraints.TargetDurationFixed {
		t.Fatal("expected explicit target duration to be fixed")
	}
	if planner.input.Constraints.MaxDurationMS != 15000 {
		t.Fatalf("max duration = %d, want 15000", planner.input.Constraints.MaxDurationMS)
	}
}

func storyRevisionHandlerFixture(t *testing.T) (*store.MemoryStore, models.StoryProject) {
	t.Helper()
	ctx := context.Background()
	memoryStore := store.NewMemoryStore()
	now := time.Date(2026, time.September, 5, 8, 0, 0, 0, time.UTC)
	photo := models.MediaAsset{ID: "photo-revision", UserID: "user-1", Type: models.MediaTypePhoto, Status: models.MediaStatusReady, FileName: "IMG_0001.JPG", ContentType: "image/jpeg", FileSizeBytes: 100, S3Key: "source/photo-revision", UploadID: "upload-photo-revision", CreatedAt: now, UpdatedAt: now}
	if err := memoryStore.CreateMediaUpload(ctx, photo, models.UploadSession{ID: photo.UploadID, UserID: photo.UserID, MediaID: photo.ID, S3Key: photo.S3Key, Strategy: models.UploadStrategySingle, CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	project := models.StoryProject{ID: "project-revision-handler", UserID: photo.UserID, Title: "Revision", MediaIDs: []string{photo.ID}, Status: models.ProjectStatusReady, CreatedAt: now, UpdatedAt: now}
	if _, _, err := memoryStore.CreateProject(ctx, project, "project-revision-handler"); err != nil {
		t.Fatal(err)
	}
	if err := memoryStore.SaveAnalysis(ctx, photo.UserID, models.MediaAnalysis{ID: "analysis-photo-revision", ProjectID: project.ID, MediaID: photo.ID, Labels: []string{"beach"}, QualityScore: 0.9, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := memoryStore.SaveStoryPlan(ctx, photo.UserID, models.StoryPlan{SchemaVersion: models.StoryPlanSchemaVersionV1, ID: "plan-revision-base", ProjectID: project.ID, Version: 1, RequestedDurationMS: 4000, TitleText: "Revision", EndingText: "The moments that stay with us.", Moments: []models.StoryMoment{{MomentID: "photo", AssetID: photo.ID, MediaID: photo.ID, MediaType: models.MediaTypePhoto, Narrative: "Beach", DurationMS: 4000, CropIntent: "fit", Transition: "fade_in"}}, Output: models.StoryOutput{AspectRatio: "9:16", MaxDurationS: 8, Preset: "premium-memory"}, TotalDurationMS: 4000, CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	return memoryStore, project
}

type failingRevisionPlanner struct{}

func (failingRevisionPlanner) Revise(context.Context, ai.StoryRevisionInput) (ai.StoryRevisionResult, error) {
	return ai.StoryRevisionResult{}, errors.New("provider unavailable")
}

type invalidRevisionPlanner struct{}

func (invalidRevisionPlanner) Revise(_ context.Context, input ai.StoryRevisionInput) (ai.StoryRevisionResult, error) {
	plan := input.CurrentStoryPlan
	plan.ID = "plan-invalid-revision"
	plan.Version = input.Story.NextVersion
	plan.Moments = []models.StoryMoment{{MomentID: "missing", AssetID: "missing-media", MediaID: "missing-media", MediaType: models.MediaTypePhoto, DurationMS: 1000}}
	plan.TotalDurationMS = 1000
	return ai.StoryRevisionResult{RevisionSummary: "Invalid revision", RevisedStoryPlan: plan}, nil
}

type rejectingRevisionValidator struct{}

func (rejectingRevisionValidator) Validate(context.Context, string, models.StoryPlan) error {
	return errors.New("invalid story plan")
}

type capturingRevisionPlanner struct {
	input ai.StoryRevisionInput
}

func (p *capturingRevisionPlanner) Revise(_ context.Context, input ai.StoryRevisionInput) (ai.StoryRevisionResult, error) {
	p.input = input
	plan := input.CurrentStoryPlan
	plan.ID = "plan-captured-revision"
	plan.ProjectID = input.Story.ID
	plan.Version = input.Story.NextVersion
	plan.RequestedDurationMS = 12000
	plan.Output.MaxDurationS = 12
	for index := range plan.Moments {
		plan.Moments[index].DurationMS = 12000 / len(plan.Moments)
		plan.Moments[index].MomentID = fmt.Sprintf("moment-%d", index+1)
	}
	plan.TotalDurationMS = 12000
	return ai.StoryRevisionResult{RevisionSummary: "Captured revision duration constraints", RevisedStoryPlan: plan}, nil
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
	large := models.StoryProject{MediaIDs: []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l", "m", "n", "o", "p", "q", "r", "s", "t"}}
	if got := defaultStoryDuration(large); got != 60*time.Second {
		t.Fatalf("large story duration = %s, want 60s", got)
	}
}

type fixedStoryPlanner struct {
	plan models.StoryPlan
}

func (planner fixedStoryPlanner) Plan(context.Context, ai.Request) (models.StoryPlan, error) {
	return planner.plan, nil
}

func (fixedStoryPlanner) Name() string { return "openai-compatible" }

func (fixedStoryPlanner) Model() string { return "gpt-test" }

func boolPtr(value bool) *bool { return &value }
