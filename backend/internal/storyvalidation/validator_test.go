package storyvalidation

import (
	"context"
	"testing"
	"time"

	"github.com/framemio/backend/internal/models"
	"github.com/framemio/backend/internal/storage"
	"github.com/framemio/backend/internal/store"
)

type testObjectStorage struct {
	existing map[string]struct{}
}

func (s testObjectStorage) AuthorizeUpload(context.Context, storage.UploadAuthorizationRequest) (models.UploadSession, error) {
	return models.UploadSession{}, nil
}

func (s testObjectStorage) RefreshUpload(context.Context, models.UploadSession, storage.UploadRefreshRequest) (models.UploadSession, error) {
	return models.UploadSession{}, nil
}

func (s testObjectStorage) CompleteUpload(context.Context, models.UploadSession, []models.CompletedUploadPart) (string, error) {
	return "", nil
}

func (s testObjectStorage) PresignedDownloadURL(_ context.Context, key string, _ time.Duration) (string, error) {
	return "https://objects.test/" + key, nil
}

func (s testObjectStorage) AbortUpload(context.Context, models.UploadSession) error {
	return nil
}

func (s testObjectStorage) ObjectExists(_ context.Context, key string) (bool, error) {
	_, ok := s.existing[key]
	return ok, nil
}

func TestValidateStoryPlanAcceptsValidPlan(t *testing.T) {
	t.Parallel()
	validator, plan := validStoryPlanFixture(t)
	if err := validator.Validate(context.Background(), "user-1", plan); err != nil {
		t.Fatalf("validate valid plan: %v", err)
	}
}

func TestValidateStoryPlanRejectsInvalidInputs(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		mutate func(*models.StoryPlan)
		modify func(*Validator)
		field  string
		code   string
	}{
		{
			name: "missing asset",
			mutate: func(plan *models.StoryPlan) {
				plan.Moments[0].AssetID = "missing"
			},
			field: "moments[0].asset_id",
			code:  "missing_asset",
		},
		{
			name: "missing video analysis",
			mutate: func(plan *models.StoryPlan) {
				plan.Moments[0].VideoAnalysisVersion = "missing-analysis"
			},
			field: "moments[0].video_analysis_version",
			code:  "missing_analysis",
		},
		{
			name: "invalid video timestamps",
			mutate: func(plan *models.StoryPlan) {
				plan.Moments[0].EndMicros = 10_000_000
			},
			field: "moments[0].timestamps",
			code:  "out_of_range",
		},
		{
			name: "duplicate asset reference",
			mutate: func(plan *models.StoryPlan) {
				plan.Moments = append(plan.Moments, plan.Moments[0])
				plan.TotalDurationMS += plan.Moments[0].DurationMS
			},
			field: "moments[1].asset_id",
			code:  "duplicate_reference",
		},
		{
			name: "missing s3 object",
			modify: func(validator *Validator) {
				validator.ObjectStorage = testObjectStorage{existing: map[string]struct{}{}}
			},
			field: "moments[0].s3_key",
			code:  "missing_object",
		},
		{
			name: "unsupported aspect ratio",
			mutate: func(plan *models.StoryPlan) {
				plan.Output.AspectRatio = "2:1"
			},
			field: "output.aspect_ratio",
			code:  "unsupported_aspect_ratio",
		},
		{
			name: "title too long",
			mutate: func(plan *models.StoryPlan) {
				plan.TitleText = makeString("x", 121)
			},
			field: "title_text",
			code:  "too_long",
		},
		{
			name: "invalid audio source",
			mutate: func(plan *models.StoryPlan) {
				plan.Audio.Source = "unsupported"
			},
			field: "audio.source",
			code:  "unsupported_audio_source",
		},
		{
			name: "invalid schema",
			mutate: func(plan *models.StoryPlan) {
				plan.SchemaVersion = "story_plan_v2"
			},
			field: "schema_version",
			code:  "invalid_schema",
		},
		{
			name: "duration exceeds request",
			mutate: func(plan *models.StoryPlan) {
				plan.RequestedDurationMS = 2000
			},
			field: "total_duration_ms",
			code:  "out_of_range",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			validator, plan := validStoryPlanFixture(t)
			if tc.modify != nil {
				tc.modify(validator)
			}
			if tc.mutate != nil {
				tc.mutate(&plan)
			}
			err := validator.Validate(context.Background(), "user-1", plan)
			if err == nil {
				t.Fatalf("expected validation error")
			}
			validationErr, ok := AsValidationError(err)
			if !ok {
				t.Fatalf("unexpected error type: %T %v", err, err)
			}
			assertFieldError(t, validationErr, tc.field, tc.code)
		})
	}
}

func validStoryPlanFixture(t *testing.T) (*Validator, models.StoryPlan) {
	t.Helper()
	memoryStore := store.NewMemoryStore()
	now := time.Date(2026, time.August, 21, 12, 0, 0, 0, time.UTC)
	userID := "user-1"
	durationSeconds := 8.0
	video := models.MediaAsset{
		ID:              "video-1",
		UserID:          userID,
		Type:            models.MediaTypeVideo,
		Status:          models.MediaStatusReady,
		FileName:        "clip.mov",
		ContentType:     "video/quicktime",
		FileSizeBytes:   12_000,
		DurationSeconds: &durationSeconds,
		PixelWidth:      1920,
		PixelHeight:     1080,
		S3Key:           "users/user-1/media/video-1/clip.mov",
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := memoryStore.CreateMediaUpload(context.Background(), video, models.UploadSession{ID: "upload-1", UserID: userID, MediaID: video.ID, S3Key: video.S3Key, Strategy: models.UploadStrategySingle, CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	project := models.StoryProject{ID: "project-1", UserID: userID, MediaIDs: []string{video.ID}, Status: models.ProjectStatusStoryReady, CreatedAt: now, UpdatedAt: now}
	if _, _, err := memoryStore.CreateProject(context.Background(), project, "project-key"); err != nil {
		t.Fatal(err)
	}
	analysis := models.VideoAnalysis{
		ID:              "video-analysis-1",
		VideoID:         video.ID,
		ProjectID:       project.ID,
		AnalysisVersion: "video-analysis-v1",
		DurationMicros:  8_000_000,
		Transcript:      models.VideoTranscript{HasSpeech: true},
		QualitySignals:  models.VideoQualitySignals{OverallScore: 0.9},
		CreatedAt:       now,
	}
	if err := memoryStore.SaveVideoAnalysis(context.Background(), userID, analysis); err != nil {
		t.Fatal(err)
	}
	validator := New(memoryStore, testObjectStorage{existing: map[string]struct{}{video.S3Key: {}}}, DefaultConfig())
	plan := models.StoryPlan{
		SchemaVersion:       models.StoryPlanSchemaVersionV1,
		ID:                  "plan-1",
		ProjectID:           project.ID,
		Version:             1,
		RequestedDurationMS: 5000,
		StoryType:           models.StoryTypeToday,
		VisualStyle:         models.VisualStyleCinematic,
		TitleText:           "Your moments",
		EndingText:          "A story made from the moments that mattered most.",
		MusicMood:           "calm and reflective",
		Moments: []models.StoryMoment{{
			MomentID:             "moment-1",
			AssetID:              video.ID,
			MediaType:            models.MediaTypeVideo,
			VideoAnalysisVersion: analysis.AnalysisVersion,
			StartMicros:          1_000_000,
			EndMicros:            4_000_000,
			DurationMS:           3000,
			CropIntent:           "fill",
			Transition:           "cut",
			Narrative:            "Friends laughing by the water",
			Reason:               "warm laughter",
		}},
		Audio:           &models.StoryAudio{Source: "adaptive", Mood: "calm"},
		Output:          models.StoryOutput{AspectRatio: "9:16", MaxDurationS: 60, Preset: "premium-memory"},
		TotalDurationMS: 3000,
		CreatedAt:       now,
	}
	return validator, plan
}

func assertFieldError(t *testing.T, validationErr *ValidationError, field, code string) {
	t.Helper()
	for _, item := range validationErr.Errors {
		if item.Field == field && item.Code == code {
			return
		}
	}
	t.Fatalf("expected field error %s/%s, got %+v", field, code, validationErr.Errors)
}

func makeString(character string, length int) string {
	if length <= 0 {
		return ""
	}
	bytes := make([]byte, length)
	for index := range bytes {
		bytes[index] = character[0]
	}
	return string(bytes)
}
