package storyvalidation

import (
	"context"
	"testing"
	"time"

	"github.com/framemio/backend/internal/models"
	"github.com/framemio/backend/internal/storage"
	"github.com/framemio/backend/internal/store"
	"github.com/framemio/backend/internal/templates"
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

func (s testObjectStorage) PutObject(context.Context, string, string, []byte) error {
	return nil
}

func (s testObjectStorage) DeleteObject(context.Context, string) error {
	return nil
}

func TestValidateStoryPlanAcceptsValidPlan(t *testing.T) {
	t.Parallel()
	validator, plan := validStoryPlanFixture(t)
	if err := validator.Validate(context.Background(), "user-1", plan); err != nil {
		t.Fatalf("validate valid plan: %v", err)
	}
}

func TestValidateStoryPlanAcceptsValidBRoll(t *testing.T) {
	t.Parallel()
	validator, plan := validStoryPlanFixture(t)
	plan.BRoll = []models.StoryBRoll{{
		BrollID:              "broll-1",
		AssetID:              "video-1",
		MediaType:            models.MediaTypeVideo,
		VideoAnalysisVersion: "video-analysis-v1",
		SourceStartMicros:    4_000_000,
		SourceEndMicros:      5_000_000,
		StartMS:              1000,
		DurationMS:           1000,
		Anchor:               &models.StoryBRollAnchor{Type: "transcript", MomentID: "moment-1", StartMS: 1000, EndMS: 2000},
		CropIntent:           "fill",
		Reason:               "semantic_match",
		Confidence:           0.91,
		Score:                0.88,
	}}
	if err := validator.Validate(context.Background(), "user-1", plan); err != nil {
		t.Fatal(err)
	}
}

func TestValidateStoryPlanAcceptsTemplateOverlay(t *testing.T) {
	t.Parallel()
	validator, plan := validStoryPlanFixture(t)
	template, resolved, err := templates.Resolve(models.ProjectTypeStory, "photo-over-video", 1, models.VideoTemplateConfiguration{})
	if err != nil {
		t.Fatal(err)
	}
	plan.TemplateID = template.ID
	plan.TemplateVersion = template.Version
	plan.TemplateConfiguration = resolved
	plan.ProjectType = models.ProjectTypeStory
	plan.BRoll = []models.StoryBRoll{{
		BrollID:      "broll-photo-overlay",
		AssetID:      "video-1",
		MediaType:    models.MediaTypeVideo,
		StartMS:      1000,
		DurationMS:   1000,
		Presentation: models.VideoTemplateLayoutPictureInPicture,
		Placement:    models.VideoTemplatePlacementBottomRight,
		Size:         models.VideoTemplateSizeMedium,
		Opacity:      1,
		Frame:        models.VideoTemplateFrameSubtle,
		Animation:    models.VideoTemplateAnimationFade,
		CropIntent:   "fill",
	}}
	plan.BRoll[0].VideoAnalysisVersion = "video-analysis-v1"
	plan.BRoll[0].SourceStartMicros = 4_000_000
	plan.BRoll[0].SourceEndMicros = 5_000_000

	if err := validator.Validate(context.Background(), "user-1", plan); err != nil {
		t.Fatalf("validate template overlay: %v", err)
	}
}

func TestValidateStoryPlanRejectsUnsupportedTemplateOverlay(t *testing.T) {
	t.Parallel()
	validator, plan := validStoryPlanFixture(t)
	template, resolved, err := templates.Resolve(models.ProjectTypeStory, "photo-over-video", 1, models.VideoTemplateConfiguration{})
	if err != nil {
		t.Fatal(err)
	}
	plan.TemplateID = template.ID
	plan.TemplateVersion = template.Version
	plan.TemplateConfiguration = resolved
	plan.ProjectType = models.ProjectTypeStory
	plan.BRoll = []models.StoryBRoll{{
		BrollID:      "broll-photo-overlay",
		AssetID:      "video-1",
		MediaType:    models.MediaTypeVideo,
		StartMS:      1000,
		DurationMS:   1000,
		Presentation: models.VideoTemplateLayoutSplitScreen,
		Placement:    models.VideoTemplatePlacementLeft,
		Size:         models.VideoTemplateSizeHalf,
		Opacity:      1,
		CropIntent:   "fill",
	}}
	plan.BRoll[0].VideoAnalysisVersion = "video-analysis-v1"
	plan.BRoll[0].SourceStartMicros = 4_000_000
	plan.BRoll[0].SourceEndMicros = 5_000_000

	err = validator.Validate(context.Background(), "user-1", plan)
	if err == nil {
		t.Fatal("expected validation error")
	}
	validationErr, ok := AsValidationError(err)
	if !ok {
		t.Fatalf("unexpected error type: %T %v", err, err)
	}
	assertFieldError(t, validationErr, "template", "invalid_template")
}

func TestValidateStoryPlanAcceptsMultipleSegmentsFromSameVideo(t *testing.T) {
	t.Parallel()
	validator, plan := validStoryPlanFixture(t)
	second := plan.Moments[0]
	second.MomentID = "moment-2"
	second.StartMicros = 4_000_000
	second.EndMicros = 7_000_000
	plan.Moments = append(plan.Moments, second)
	plan.TotalDurationMS += second.DurationMS
	plan.RequestedDurationMS = plan.TotalDurationMS

	if err := validator.Validate(context.Background(), "user-1", plan); err != nil {
		t.Fatalf("validate multiple segments from same video: %v", err)
	}
}

func TestValidateStoryPlanAcceptsMultipleTimedCaptionsForSameMoment(t *testing.T) {
	t.Parallel()
	validator, plan := validStoryPlanFixture(t)
	plan.Captions = []models.StoryCaption{
		{MomentID: "moment-1", Text: "Look at the lake", StartMS: 0, EndMS: 1200},
		{MomentID: "moment-1", Text: "This is beautiful", StartMS: 1200, EndMS: 2600},
	}

	if err := validator.Validate(context.Background(), "user-1", plan); err != nil {
		t.Fatalf("validate multiple timed captions for same moment: %v", err)
	}
}

func TestValidateStoryPlanAcceptsCaptionStyle(t *testing.T) {
	t.Parallel()
	validator, plan := validStoryPlanFixture(t)
	plan.CaptionStyle = &models.CaptionStyle{
		Preset: "cinematic_subtitle", Presentation: "word_by_word", FontFamily: "inter-bold", FontSize: 48, TextColor: "#FFFFFF", HighlightColor: "#FFD60A", StrokeColor: "#000000", StrokeWidth: 4,
		BackgroundEnabled: true, BackgroundColor: "#000000", BackgroundOpacity: 0.32, Position: "bottom", BottomOffset: 150, MaxLines: 2, MaxCharactersPerLine: 32,
	}

	if err := validator.Validate(context.Background(), "user-1", plan); err != nil {
		t.Fatalf("validate caption style: %v", err)
	}
}

func TestValidateStoryPlanRejectsInvalidCaptionPresentation(t *testing.T) {
	t.Parallel()
	validator, plan := validStoryPlanFixture(t)
	plan.CaptionStyle = &models.CaptionStyle{Preset: "bold_social", Presentation: "raw_css_animation", TextColor: "#FFFFFF"}

	err := validator.Validate(context.Background(), "user-1", plan)
	validationErr, ok := AsValidationError(err)
	if !ok {
		t.Fatalf("expected validation error, got %v", err)
	}
	assertFieldError(t, validationErr, "caption_style.presentation", "unsupported_caption_presentation")
}

func TestValidateStoryPlanAcceptsTypography(t *testing.T) {
	t.Parallel()
	validator, plan := validStoryPlanFixture(t)
	plan.Moments[0].Typography = &models.TextOverlay{
		Enabled: true, Type: models.TextOverlayMomentTitle, Title: "Golden Hour", Subtitle: "Somewhere between the sea and the sky.",
		Style: models.TypographyStyleCinematic, Placement: models.TextPlacementBottomLeft, StartMS: 350, DurationMS: 2200, MaxWidth: 0.72,
		SafeArea: models.SafeArea{Top: 0.08, Right: 0.08, Bottom: 0.08, Left: 0.08}, Animation: models.TextAnimationFadeUp, Source: models.TextSourceAI, Reason: "low-detail lower-left area", Confidence: 0.91,
	}

	if err := validator.Validate(context.Background(), "user-1", plan); err != nil {
		t.Fatalf("validate typography: %v", err)
	}
}

func TestValidatePromoEditPlanAcceptsSceneMetadata(t *testing.T) {
	t.Parallel()
	validator, plan := validStoryPlanFixture(t)
	plan.ProjectType = models.ProjectTypePromo
	plan.CallToAction = "Try it free"
	plan.Moments[0].Order = 1
	plan.Moments[0].ScenePurpose = models.PromoScenePurposeHook
	plan.Moments[0].SceneText = &models.PromoSceneText{Headline: "Launch faster", Supporting: "AI-assisted product videos", CTA: "Try it free"}
	plan.Moments[0].Voiceover = &models.SceneVoiceover{Text: "Create a polished promo in minutes.", StartMS: 0, DurationMS: 1800}
	plan.Moments[0].Typography = &models.TextOverlay{
		Enabled: true, Type: models.TextOverlayPromoHeadline, Title: "Launch faster", Subtitle: "AI-assisted videos",
		Style: models.TypographyStyleEditorial, Placement: models.TextPlacementBottomLeft, StartMS: 250, DurationMS: 1800, MaxWidth: 0.78,
		SafeArea: models.SafeArea{Top: 0.08, Right: 0.08, Bottom: 0.08, Left: 0.08}, Animation: models.TextAnimationFadeUp, Source: models.TextSourceAI, Confidence: 0.9,
	}

	if err := validator.Validate(context.Background(), "user-1", plan); err != nil {
		t.Fatalf("validate promo plan: %v", err)
	}
}

func TestValidatePromoEditPlanRejectsInvalidSceneMetadata(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		mutate func(*models.StoryMoment)
		field  string
		code   string
	}{
		{name: "invalid purpose", mutate: func(moment *models.StoryMoment) { moment.ScenePurpose = "random" }, field: "moments[0].scene_purpose", code: "unsupported_scene_purpose"},
		{name: "headline too long", mutate: func(moment *models.StoryMoment) {
			moment.SceneText = &models.PromoSceneText{Headline: makeString("A", 81)}
		}, field: "moments[0].scene_text.headline", code: "too_long"},
		{name: "voiceover outside scene", mutate: func(moment *models.StoryMoment) {
			moment.Voiceover = &models.SceneVoiceover{Text: "Read this", StartMS: 2500, DurationMS: 800}
		}, field: "moments[0].voiceover.timing", code: "invalid_timing"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			validator, plan := validStoryPlanFixture(t)
			plan.ProjectType = models.ProjectTypePromo
			tc.mutate(&plan.Moments[0])
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

func TestValidatePromoConfigurationChecksBrandAndCTA(t *testing.T) {
	t.Parallel()
	valid := models.PromoProjectConfiguration{
		PromotionCategory:  "app",
		PromotionGoal:      "downloads",
		ProductInformation: "FrameMio",
		CallToAction:       "Try it free",
		Brand:              models.BrandConfiguration{Name: "FrameMio", PrimaryColor: "#2244FF", SecondaryColor: "#11AA88", FontPreference: "Inter"},
		CTA:                &models.CTAConfiguration{Text: "Try it free", Style: "button", Placement: "bottom"},
	}
	if err := ValidatePromoConfiguration(valid); err != nil {
		t.Fatalf("valid promo configuration rejected: %v", err)
	}

	invalid := valid
	invalid.Brand.PrimaryColor = "blue"
	invalid.CTA = &models.CTAConfiguration{Text: "Try it free", Style: "flashy", URL: makeString("u", 241)}
	err := ValidatePromoConfiguration(invalid)
	if err == nil {
		t.Fatalf("expected validation error")
	}
	validationErr, ok := AsValidationError(err)
	if !ok {
		t.Fatalf("unexpected error type: %T %v", err, err)
	}
	assertFieldError(t, validationErr, "configuration.promo.brand.primary_color", "invalid_color")
	assertFieldError(t, validationErr, "configuration.promo.cta.style", "unsupported_cta_style")
	assertFieldError(t, validationErr, "configuration.promo.cta.url", "too_long")
}

func TestValidateStoryPlanRejectsInvalidTypography(t *testing.T) {
	t.Parallel()
	valid := models.TextOverlay{
		Enabled: true, Type: models.TextOverlayMomentTitle, Title: "Golden Hour", Style: models.TypographyStyleCinematic,
		Placement: models.TextPlacementBottomLeft, StartMS: 350, DurationMS: 2200, MaxWidth: 0.72,
		SafeArea: models.SafeArea{Top: 0.08, Right: 0.08, Bottom: 0.08, Left: 0.08}, Animation: models.TextAnimationFadeUp, Source: models.TextSourceAI, Confidence: 0.91,
	}
	cases := []struct {
		name   string
		mutate func(*models.TextOverlay)
		field  string
		code   string
	}{
		{name: "invalid style", mutate: func(overlay *models.TextOverlay) { overlay.Style = "neon" }, field: "moments[0].typography.style", code: "unsupported_style"},
		{name: "invalid placement", mutate: func(overlay *models.TextOverlay) { overlay.Placement = "over_face" }, field: "moments[0].typography.placement", code: "unsupported_placement"},
		{name: "title too long", mutate: func(overlay *models.TextOverlay) { overlay.Title = makeString("A", 41) }, field: "moments[0].typography.title", code: "too_long"},
		{name: "subtitle too long", mutate: func(overlay *models.TextOverlay) { overlay.Subtitle = makeString("B", 101) }, field: "moments[0].typography.subtitle", code: "too_long"},
		{name: "timing outside moment", mutate: func(overlay *models.TextOverlay) { overlay.StartMS = 2500; overlay.DurationMS = 900 }, field: "moments[0].typography.timing", code: "invalid_timing"},
		{name: "unsafe margin", mutate: func(overlay *models.TextOverlay) { overlay.SafeArea.Left = 0.8 }, field: "moments[0].typography.safe_area", code: "out_of_range"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			validator, plan := validStoryPlanFixture(t)
			overlay := valid
			tc.mutate(&overlay)
			plan.Moments[0].Typography = &overlay
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
			name: "video duration does not match selected range",
			mutate: func(plan *models.StoryPlan) {
				plan.Moments[0].DurationMS = 4500
				plan.TotalDurationMS = 4500
				plan.RequestedDurationMS = 4500
			},
			field: "moments[0].duration_ms",
			code:  "range_mismatch",
		},
		{
			name: "duplicate exact moment reference",
			mutate: func(plan *models.StoryPlan) {
				plan.Moments = append(plan.Moments, plan.Moments[0])
				plan.TotalDurationMS += plan.Moments[0].DurationMS
			},
			field: "moments[1].asset_id",
			code:  "duplicate_reference",
		},
		{
			name: "media outside project",
			mutate: func(plan *models.StoryPlan) {
				plan.Moments[0].AssetID = "photo-outside"
				plan.Moments[0].MediaType = models.MediaTypePhoto
				plan.Moments[0].VideoAnalysisVersion = ""
				plan.Moments[0].StartMicros = 0
				plan.Moments[0].EndMicros = 0
			},
			field: "moments[0].asset_id",
			code:  "not_in_project",
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
			name: "background music audio source not supported in V1",
			mutate: func(plan *models.StoryPlan) {
				plan.Audio = &models.StoryAudio{Source: "licensed"}
			},
			field: "audio.source",
			code:  "unsupported_audio_source",
		},
		{
			name: "invalid caption style",
			mutate: func(plan *models.StoryPlan) {
				plan.CaptionStyle = &models.CaptionStyle{Preset: "raw-ffmpeg", FontFamily: "../../font.ttf", TextColor: "white", FontSize: 240}
			},
			field: "caption_style.preset",
			code:  "unsupported_caption_preset",
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
		{
			name: "broll source segment out of range",
			mutate: func(plan *models.StoryPlan) {
				plan.BRoll = []models.StoryBRoll{{AssetID: "video-1", MediaType: models.MediaTypeVideo, VideoAnalysisVersion: "video-analysis-v1", SourceStartMicros: 7_500_000, SourceEndMicros: 9_000_000, StartMS: 500, DurationMS: 1000, Anchor: &models.StoryBRollAnchor{Type: "moment", MomentID: "moment-1", StartMS: 500, EndMS: 1500}}}
			},
			field: "broll[0].source_segment",
			code:  "out_of_range",
		},
		{
			name: "broll anchor unknown moment",
			mutate: func(plan *models.StoryPlan) {
				plan.BRoll = []models.StoryBRoll{{AssetID: "video-1", MediaType: models.MediaTypeVideo, VideoAnalysisVersion: "video-analysis-v1", SourceStartMicros: 4_000_000, SourceEndMicros: 5_000_000, StartMS: 500, DurationMS: 1000, Anchor: &models.StoryBRollAnchor{Type: "moment", MomentID: "missing", StartMS: 500, EndMS: 1500}}}
			},
			field: "broll[0].anchor.moment_id",
			code:  "unknown_moment",
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
	outsidePhoto := models.MediaAsset{
		ID:            "photo-outside",
		UserID:        userID,
		Type:          models.MediaTypePhoto,
		Status:        models.MediaStatusReady,
		FileName:      "outside.jpg",
		ContentType:   "image/jpeg",
		FileSizeBytes: 4_000,
		PixelWidth:    1200,
		PixelHeight:   1600,
		S3Key:         "users/user-1/media/photo-outside/outside.jpg",
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	for _, media := range []models.MediaAsset{video, outsidePhoto} {
		if err := memoryStore.CreateMediaUpload(context.Background(), media, models.UploadSession{ID: "upload-" + media.ID, UserID: userID, MediaID: media.ID, S3Key: media.S3Key, Strategy: models.UploadStrategySingle, CreatedAt: now}); err != nil {
			t.Fatal(err)
		}
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
	validator := New(memoryStore, testObjectStorage{existing: map[string]struct{}{video.S3Key: {}, outsidePhoto.S3Key: {}}}, DefaultConfig())
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
