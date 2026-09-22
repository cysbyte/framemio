package orchestration

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/framemio/backend/internal/ai"
	"github.com/framemio/backend/internal/models"
	"github.com/framemio/backend/internal/storage"
	"github.com/framemio/backend/internal/store"
	"github.com/framemio/backend/internal/storyvalidation"
	"github.com/framemio/backend/internal/templates"
)

func TestBRollScoresReportsIdentityAndUnsetScoreClearly(t *testing.T) {
	t.Parallel()
	got := brollScores([]models.StoryBRoll{
		{AssetID: "media-a", BrollID: "broll-a", Score: 0.72, Confidence: 0.81},
		{MediaID: "media-b", Confidence: 0.64},
	})
	if !strings.Contains(got, "broll-a:media-a:start=0:duration=0:score=0.720:confidence=0.810") {
		t.Fatalf("expected scored B-roll details, got %q", got)
	}
	if !strings.Contains(got, "broll_index_1:media-b:start=0:duration=0:score=unset:confidence=0.640") {
		t.Fatalf("expected missing score to be explicit, got %q", got)
	}
}

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

func TestStoryPlanHandlerMapsTemplateDefaultsToPlannerRequest(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	memoryStore := store.NewMemoryStore()
	now := time.Date(2026, time.September, 19, 12, 0, 0, 0, time.UTC)
	photo := models.MediaAsset{ID: "photo-template-request", UserID: "user-1", Type: models.MediaTypePhoto, Status: models.MediaStatusReady, FileName: "photo.jpg", ContentType: "image/jpeg", FileSizeBytes: 100, S3Key: "source/photo-template-request", UploadID: "upload-photo-template-request", CreatedAt: now, UpdatedAt: now}
	if err := memoryStore.CreateMediaUpload(ctx, photo, models.UploadSession{ID: photo.UploadID, UserID: photo.UserID, MediaID: photo.ID, S3Key: photo.S3Key, Strategy: models.UploadStrategySingle, CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	template, resolved, err := templates.Resolve(models.ProjectTypeStory, "picture-in-picture", 1, models.VideoTemplateConfiguration{DurationSeconds: 45})
	if err != nil {
		t.Fatal(err)
	}
	project := models.StoryProject{
		ID: "project-template-request", UserID: photo.UserID, Type: models.ProjectTypeStory, Title: "Template", MediaIDs: []string{photo.ID}, Status: models.ProjectStatusAnalyzing, CreatedAt: now, UpdatedAt: now,
		Configuration: models.VideoProjectConfiguration{TemplateID: template.ID, TemplateVersion: template.Version, TemplateConfiguration: resolved},
	}
	if _, _, err := memoryStore.CreateProject(ctx, project, "project-template-request-key"); err != nil {
		t.Fatal(err)
	}
	if err := memoryStore.SaveAnalysis(ctx, photo.UserID, models.MediaAnalysis{ID: "analysis-photo-template-request", MediaID: photo.ID, ProjectID: project.ID, QualityScore: 0.8, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	planner := &capturingStoryPlanner{}
	job := models.ProcessingJob{ID: "job-template-request", UserID: photo.UserID, ProjectID: project.ID, Type: models.JobTypeStoryPlan, Status: models.JobStatusRunning, CreatedAt: now, UpdatedAt: now}
	handler := StoryPlanHandler{Store: memoryStore, Planner: planner, Now: func() time.Time { return now }}
	if err := handler.HandleJob(ctx, job); err != nil {
		t.Fatal(err)
	}
	if planner.input.TemplateID != template.ID || planner.input.TemplateVersion != template.Version {
		t.Fatalf("template identity not mapped to planner request: %+v", planner.input)
	}
	if planner.input.DesiredDuration != 45*time.Second || !planner.input.TargetDurationFixed || planner.input.VisualStyle != models.VisualStyleWarm || planner.input.CaptionStylePreset != "clean_minimal" || planner.input.BRollPreference != "more" {
		t.Fatalf("template defaults not reflected in planner request: %+v", planner.input)
	}
}

func TestStoryPlanHandlerEnforcesTemplateIdentityFromExternalPlanner(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	memoryStore, project := storyRevisionHandlerFixture(t)
	now := project.CreatedAt
	template, resolved, err := templates.Resolve(models.ProjectTypeStory, "photo-over-video", 1, models.VideoTemplateConfiguration{OverlayPlacement: models.VideoTemplatePlacementTopLeft})
	if err != nil {
		t.Fatal(err)
	}
	project.Configuration.TemplateID = template.ID
	project.Configuration.TemplateVersion = template.Version
	project.Configuration.TemplateConfiguration = resolved
	if _, err := memoryStore.UpdateProject(ctx, project); err != nil {
		t.Fatal(err)
	}
	planner := fixedStoryPlanner{plan: models.StoryPlan{
		SchemaVersion:         models.StoryPlanSchemaVersionV1,
		ID:                    "plan-template-enforced",
		ProjectID:             project.ID,
		ProjectType:           project.Type,
		Version:               2,
		TemplateID:            "invented-template",
		TemplateVersion:       9,
		TemplateConfiguration: models.VideoTemplateConfiguration{AspectRatio: "16:9"},
		RequestedDurationMS:   4000,
		StoryType:             models.StoryTypeTrip,
		VisualStyle:           models.VisualStyleCinematic,
		TitleText:             "Template story",
		EndingText:            "Done",
		Moments: []models.StoryMoment{{
			MomentID: "moment-photo-revision", AssetID: "photo-revision", MediaID: "photo-revision", MediaType: models.MediaTypePhoto,
			Narrative: "A beach memory opens the story.", Caption: "Beach memory", DurationMS: 4000, CropIntent: "fit", Transition: "fade_in",
		}},
		BRoll: []models.StoryBRoll{{
			BrollID: "broll-photo-revision", AssetID: "photo-revision", MediaID: "photo-revision", MediaType: models.MediaTypePhoto,
			StartMS: 1000, DurationMS: 1000, CropIntent: "fit", Confidence: 0.8, Score: 0.7,
		}},
		Output:          models.StoryOutput{AspectRatio: "16:9", MaxDurationS: 4, Preset: "premium-memory"},
		TotalDurationMS: 4000,
		CreatedAt:       now,
	}}
	job := models.ProcessingJob{ID: "job-template-enforced", UserID: project.UserID, ProjectID: project.ID, Type: models.JobTypeStoryPlan, Status: models.JobStatusRunning, Configuration: map[string]string{"story_plan_version": "2"}, CreatedAt: now, UpdatedAt: now}
	handler := StoryPlanHandler{Store: memoryStore, Planner: planner, Validator: storyvalidation.New(memoryStore, storage.StaticObjectStorage{}, storyvalidation.DefaultConfig()), Now: func() time.Time { return now }}
	if err := handler.HandleJob(ctx, job); err != nil {
		t.Fatal(err)
	}
	latest, err := memoryStore.GetStoryPlan(ctx, project.UserID, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if latest.TemplateID != template.ID || latest.TemplateVersion != template.Version || latest.TemplateConfiguration.OverlayPlacement != models.VideoTemplatePlacementTopLeft {
		t.Fatalf("template identity/configuration not enforced: %+v", latest)
	}
	if latest.Output.AspectRatio != "9:16" {
		t.Fatalf("template aspect ratio not enforced: %+v", latest.Output)
	}
	if len(latest.BRoll) != 1 || latest.BRoll[0].Presentation != models.VideoTemplateLayoutPictureInPicture || latest.BRoll[0].Placement != models.VideoTemplatePlacementTopLeft {
		t.Fatalf("template overlay defaults not applied: %+v", latest.BRoll)
	}
}

func TestStoryPlanHandlerSanitizesExternalPlannerRendererValuesBeforeValidation(t *testing.T) {
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
			{MomentID: "moment-transition-1", AssetID: photos[0].ID, MediaID: photos[0].ID, MediaType: models.MediaTypePhoto, Narrative: "Opening memory", DurationMS: 2000, CropIntent: "contain", Transition: "iris wipe", Reason: "AI-specific transition", Typography: &models.TextOverlay{Enabled: true, Type: models.TextOverlayMomentTitle, Title: "Opening", Style: models.TypographyStyleMinimal, Placement: models.TextPlacementBottomCenter, StartMS: 0, DurationMS: 1000, MaxWidth: 0.8, Animation: models.TextAnimationFade, Source: models.TextSourceAI}},
			{MomentID: "moment-transition-2", AssetID: photos[1].ID, MediaID: photos[1].ID, MediaType: models.MediaTypePhoto, Narrative: "Second memory", DurationMS: 2000, CropIntent: "center crop", Transition: "push-left", Reason: "AI-specific transition"},
		},
		Captions: []models.StoryCaption{
			{MomentID: "moment-transition-1", Text: "Opening memory", StartMS: -100, EndMS: 0},
			{MomentID: "unknown-moment", Text: "Unknown moment", StartMS: 0, EndMS: 1000},
		},
		CaptionStyle:    &models.CaptionStyle{Preset: "cinematic", FontFamily: "Inter Bold", FontSize: 240, TextColor: "white", StrokeColor: "black", StrokeWidth: 40, BackgroundEnabled: true, BackgroundColor: "black", BackgroundOpacity: 2, Position: "center", BottomOffset: 999, MaxLines: 9, MaxCharactersPerLine: 99},
		Audio:           &models.StoryAudio{Source: "camera audio", OriginalAudio: boolPtr(true), OriginalVolume: 2.2},
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
	if latest.Moments[0].CropIntent != "fit" || latest.Moments[1].CropIntent != "fill" {
		t.Fatalf("unsupported crop intents were not sanitized: %+v", latest.Moments)
	}
	if latest.Moments[0].Typography == nil {
		t.Fatal("external story planner typography should be sanitized and preserved")
	}
	if latest.Moments[0].Typography.Type != models.TextOverlayMomentTitle || latest.Moments[0].Typography.Title != "Opening" || latest.Moments[0].Typography.Placement != models.TextPlacementBottomCenter {
		t.Fatalf("external story planner typography was not sanitized and preserved: %+v", latest.Moments[0].Typography)
	}
	if len(latest.Captions) != 1 || latest.Captions[0].StartMS != 0 || latest.Captions[0].EndMS != 2000 {
		t.Fatalf("invalid captions were not repaired/dropped: %+v", latest.Captions)
	}
	if latest.CaptionStyle == nil || latest.CaptionStyle.Preset != "cinematic_subtitle" || latest.CaptionStyle.FontFamily != "inter-bold" || latest.CaptionStyle.TextColor != "#FFFFFF" || latest.CaptionStyle.Position != "middle" || latest.CaptionStyle.StrokeWidth != 12 || latest.CaptionStyle.BackgroundOpacity != 1 {
		t.Fatalf("caption style aliases were not sanitized: %+v", latest.CaptionStyle)
	}
	if latest.Audio == nil || latest.Audio.Source != "original" || latest.Audio.OriginalVolume != 1 {
		t.Fatalf("audio aliases were not sanitized: %+v", latest.Audio)
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

func TestStoryPlanRevisionAppliesStructuredEditPreferencesWithPrompt(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	memoryStore, project := storyRevisionHandlerFixture(t)
	job := models.ProcessingJob{
		ID:        "job-revision-preferences",
		UserID:    project.UserID,
		ProjectID: project.ID,
		Type:      models.JobTypeStoryPlan,
		Status:    models.JobStatusRunning,
		Configuration: map[string]string{
			"revision_mode":           "ai_story_plan_revision",
			"base_story_plan_version": "1",
			"story_plan_version":      "2",
			"user_instructions":       "Make it more focused on the beach.",
			"visual_style":            string(models.VisualStylePlayful),
			"subtitles_enabled":       "true",
			"caption_style_preset":    "clean_minimal",
			"broll_preference":        "none",
		},
		CreatedAt: project.CreatedAt,
		UpdatedAt: project.CreatedAt,
	}
	planner := &capturingRevisionPlanner{}
	handler := StoryPlanHandler{Store: memoryStore, RevisionPlanner: planner, Now: func() time.Time { return project.CreatedAt }}
	if err := handler.HandleJob(ctx, job); err != nil {
		t.Fatal(err)
	}
	if planner.input.Constraints.RequestedVisualStyle != models.VisualStylePlayful {
		t.Fatalf("requested visual style = %q, want %q", planner.input.Constraints.RequestedVisualStyle, models.VisualStylePlayful)
	}
	if planner.input.Story.VisualStyle != models.VisualStylePlayful {
		t.Fatalf("story visual style = %q, want %q", planner.input.Story.VisualStyle, models.VisualStylePlayful)
	}
	if planner.input.Constraints.SubtitlesEnabled == nil || !*planner.input.Constraints.SubtitlesEnabled {
		t.Fatalf("subtitles enabled = %v, want true", planner.input.Constraints.SubtitlesEnabled)
	}
	if planner.input.Constraints.CaptionStylePreset != "clean_minimal" {
		t.Fatalf("caption style preset = %q, want clean_minimal", planner.input.Constraints.CaptionStylePreset)
	}
	if planner.input.Constraints.BRollPreference != "none" {
		t.Fatalf("b-roll preference = %q, want none", planner.input.Constraints.BRollPreference)
	}
	plan, err := memoryStore.GetStoryPlan(ctx, project.UserID, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if plan.VisualStyle != models.VisualStylePlayful {
		t.Fatalf("persisted visual style = %q, want %q", plan.VisualStyle, models.VisualStylePlayful)
	}
	if plan.CaptionStyle == nil || plan.CaptionStyle.Preset != "clean_minimal" {
		t.Fatalf("persisted caption style = %+v, want clean_minimal", plan.CaptionStyle)
	}
	if len(plan.Captions) == 0 {
		t.Fatal("expected subtitles-enabled revision to have generated captions")
	}
	if len(plan.BRoll) != 0 {
		t.Fatalf("expected main-moments revision to remove B-roll, got %+v", plan.BRoll)
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
	if got := defaultStoryDuration(models.StoryProject{MediaIDs: []string{"a", "b", "c", "d"}}, nil); got != 16*time.Second {
		t.Fatalf("4-media duration = %s, want 16s", got)
	}
	large := models.StoryProject{MediaIDs: []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l", "m", "n", "o", "p", "q", "r", "s", "t"}}
	if got := defaultStoryDuration(large, nil); got != 60*time.Second {
		t.Fatalf("large story duration = %s, want 60s", got)
	}
}

func TestDefaultStoryDurationUsesVideoLengthForLongUploads(t *testing.T) {
	t.Parallel()
	project := models.StoryProject{MediaIDs: []string{"video-short", "video-long", "photo"}}
	mediaByID := map[string]models.MediaAsset{
		"video-short": {ID: "video-short", Type: models.MediaTypeVideo, DurationSeconds: ptrFloat64(72)},
		"video-long":  {ID: "video-long", Type: models.MediaTypeVideo, DurationSeconds: ptrFloat64(240)},
		"photo":       {ID: "photo", Type: models.MediaTypePhoto},
	}
	got := defaultStoryDuration(project, mediaByID)
	if got != 60*time.Second {
		t.Fatalf("long-upload duration = %s, want 60s", got)
	}
}

func TestDefaultStoryDurationUses60SecondsWhenVideosExceedTwoMinutes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		videoSeconds float64
		expected     time.Duration
	}{
		{name: "exactly two minutes", videoSeconds: 120, expected: 30 * time.Second},
		{name: "just over two minutes", videoSeconds: 120.1, expected: 60 * time.Second},
		{name: "ten minutes", videoSeconds: 10 * 60, expected: 60 * time.Second},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			project := models.StoryProject{MediaIDs: []string{"video"}}
			mediaByID := map[string]models.MediaAsset{
				"video": {ID: "video", Type: models.MediaTypeVideo, DurationSeconds: ptrFloat64(test.videoSeconds)},
			}
			if got := defaultStoryDuration(project, mediaByID); got != test.expected {
				t.Fatalf("video duration %.1fs produced %s, want %s", test.videoSeconds, got, test.expected)
			}
		})
	}
}

func TestStoryPlanRequestUsesAutomaticLongVideoDuration(t *testing.T) {
	t.Parallel()
	project := models.StoryProject{
		ID:       "project-long-video-auto",
		MediaIDs: []string{"video-a", "video-b"},
	}
	mediaByID := map[string]models.MediaAsset{
		"video-a": {ID: "video-a", Type: models.MediaTypeVideo, DurationSeconds: ptrFloat64(240)},
		"video-b": {ID: "video-b", Type: models.MediaTypeVideo, DurationSeconds: ptrFloat64(360)},
	}
	request := (StoryPlanHandler{}).storyPlanRequest(
		project,
		map[string]string{"duration_mode": "auto"},
		mediaByID,
		nil,
		nil,
		nil,
	)
	if request.DurationMode != models.DurationModeAuto || request.TargetDurationFixed {
		t.Fatalf("expected automatic duration mode, got mode=%q fixed=%v", request.DurationMode, request.TargetDurationFixed)
	}
	if request.DesiredDuration != 60*time.Second {
		t.Fatalf("automatic long-video duration = %s, want 60s", request.DesiredDuration)
	}
}

func TestStoryPlanRequestPreservesExplicit30SecondDuration(t *testing.T) {
	t.Parallel()
	project := models.StoryProject{
		ID:       "project-long-video-fixed",
		MediaIDs: []string{"video"},
	}
	mediaByID := map[string]models.MediaAsset{
		"video": {ID: "video", Type: models.MediaTypeVideo, DurationSeconds: ptrFloat64(10 * 60)},
	}
	request := (StoryPlanHandler{}).storyPlanRequest(
		project,
		map[string]string{
			"duration_mode":           "fixed",
			"target_duration_seconds": "30",
		},
		mediaByID,
		nil,
		nil,
		nil,
	)
	if request.DesiredDuration != 30*time.Second || !request.TargetDurationFixed {
		t.Fatalf("explicit duration was changed: duration=%s fixed=%v", request.DesiredDuration, request.TargetDurationFixed)
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

type capturingStoryPlanner struct {
	input ai.Request
}

func (planner *capturingStoryPlanner) Plan(_ context.Context, input ai.Request) (models.StoryPlan, error) {
	planner.input = input
	return models.StoryPlan{
		SchemaVersion:       models.StoryPlanSchemaVersionV1,
		ID:                  "plan-capturing-template",
		ProjectID:           input.ProjectID,
		ProjectType:         input.ProjectType,
		Version:             1,
		RequestedDurationMS: int(input.DesiredDuration.Milliseconds()),
		StoryType:           input.StoryType,
		VisualStyle:         input.VisualStyle,
		TitleText:           "Template",
		EndingText:          "Done",
		Moments:             []models.StoryMoment{{MomentID: "moment-template", AssetID: input.SelectedMediaIDs[0], MediaID: input.SelectedMediaIDs[0], MediaType: models.MediaTypePhoto, DurationMS: 3000, CropIntent: "fit", Transition: "fade_in"}},
		Output:              models.StoryOutput{AspectRatio: input.TemplateConfiguration.AspectRatio, MaxDurationS: 45, Preset: "premium-memory"},
		TotalDurationMS:     3000,
		CreatedAt:           time.Now().UTC(),
	}, nil
}

func (capturingStoryPlanner) Name() string { return "capturing" }

func boolPtr(value bool) *bool { return &value }
