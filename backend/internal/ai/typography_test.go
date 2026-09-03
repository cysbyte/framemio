package ai

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/framemio/backend/internal/models"
)

func TestNormalizeTextOverlayAppliesDefaultsAndLimits(t *testing.T) {
	t.Parallel()
	overlay, err := NormalizeTextOverlay(models.TextOverlay{
		Enabled:    true,
		Title:      "  Golden\nHour  ",
		Subtitle:   strings.Repeat("warm ", 40),
		Reason:     strings.Repeat("reason ", 80),
		StartMS:    -120,
		DurationMS: 9000,
		MaxWidth:   1.8,
		Confidence: 1.4,
	}, 4000)
	if err != nil {
		t.Fatal(err)
	}
	if overlay.Type != models.TextOverlayMomentTitle || overlay.Style != models.TypographyStyleCinematic || overlay.Placement != models.TextPlacementBottomLeft || overlay.Animation != models.TextAnimationFadeUp || overlay.Source != models.TextSourceAI {
		t.Fatalf("expected default enums, got %+v", overlay)
	}
	if overlay.Title != "Golden Hour" || strings.Contains(overlay.Subtitle, "\n") || len([]rune(overlay.Subtitle)) > MaxTypographySubtitleLength || len([]rune(overlay.Reason)) > MaxTypographyReasonLength {
		t.Fatalf("expected cleaned and truncated text, got %+v", overlay)
	}
	if overlay.StartMS != 0 || overlay.DurationMS != 4000 {
		t.Fatalf("expected timing clamped into owning moment, got start=%d duration=%d", overlay.StartMS, overlay.DurationMS)
	}
	if overlay.MaxWidth != DefaultTypographyMaxWidth || overlay.SafeArea.Top != DefaultTypographySafeMargin || overlay.Confidence != 1 {
		t.Fatalf("expected normalized layout/confidence, got %+v", overlay)
	}
}

func TestNormalizeTextOverlayRejectsInvalidModelOutput(t *testing.T) {
	t.Parallel()
	valid := models.TextOverlay{
		Enabled: true, Type: models.TextOverlayMomentTitle, Title: "Golden Hour", Style: models.TypographyStyleCinematic,
		Placement: models.TextPlacementBottomLeft, StartMS: 250, DurationMS: 2000, MaxWidth: 0.7,
		SafeArea: models.SafeArea{Top: 0.08, Right: 0.08, Bottom: 0.08, Left: 0.08}, Animation: models.TextAnimationFadeUp, Source: models.TextSourceAI,
	}
	cases := []struct {
		name   string
		mutate func(*models.TextOverlay)
	}{
		{name: "unsupported type", mutate: func(overlay *models.TextOverlay) { overlay.Type = "quote_block" }},
		{name: "unsupported style", mutate: func(overlay *models.TextOverlay) { overlay.Style = "neon" }},
		{name: "unsupported placement", mutate: func(overlay *models.TextOverlay) { overlay.Placement = "over_face" }},
		{name: "unsupported animation", mutate: func(overlay *models.TextOverlay) { overlay.Animation = "spin" }},
		{name: "unsupported source", mutate: func(overlay *models.TextOverlay) { overlay.Source = "remote_css" }},
		{name: "empty text", mutate: func(overlay *models.TextOverlay) { overlay.Title = ""; overlay.Subtitle = "" }},
		{name: "too short", mutate: func(overlay *models.TextOverlay) { overlay.DurationMS = 120 }},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			overlay := valid
			tc.mutate(&overlay)
			if _, err := NormalizeTextOverlay(overlay, 3000); err == nil {
				t.Fatal("expected invalid typography to be rejected")
			}
		})
	}
}

func TestParseTypographyPlanJSONValidatesStructuredOutput(t *testing.T) {
	t.Parallel()
	plan, err := ParseTypographyPlanJSON([]byte(`{
		"overlays": [{
			"moment_id": "photo-1",
			"enabled": true,
			"type": "moment_title",
			"title": "Golden Hour",
			"subtitle": "Somewhere between the sea and the sky.",
			"style": "cinematic",
			"placement": "bottom_left",
			"start_ms": 350,
			"duration_ms": 2200,
			"max_width": 0.72,
			"safe_area": {"top": 0.08, "right": 0.08, "bottom": 0.08, "left": 0.08},
			"animation": "fade_up",
			"source": "ai",
			"confidence": 0.91
		}]
	}`), map[string]int{"photo-1": 4000})
	if err != nil {
		t.Fatal(err)
	}
	if overlay := plan.Overlays["photo-1"]; overlay.Title != "Golden Hour" || overlay.DurationMS != 2200 || overlay.SafeArea.Left != 0.08 {
		t.Fatalf("unexpected parsed typography overlay: %+v", overlay)
	}
}

func TestParseTypographyPlanJSONRejectsMalformedOutput(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		data string
	}{
		{name: "malformed", data: `{`},
		{name: "unknown field", data: `{"overlays":[{"moment_id":"photo-1","enabled":true,"title":"Golden Hour","style":"cinematic","placement":"bottom_left","animation":"fade_up","source":"ai","unexpected":true}]}`},
		{name: "missing moment id", data: `{"overlays":[{"enabled":true,"title":"Golden Hour","style":"cinematic","placement":"bottom_left","animation":"fade_up","source":"ai"}]}`},
		{name: "unknown moment", data: `{"overlays":[{"moment_id":"missing","enabled":true,"title":"Golden Hour","style":"cinematic","placement":"bottom_left","animation":"fade_up","source":"ai"}]}`},
		{name: "unsupported enum", data: `{"overlays":[{"moment_id":"photo-1","enabled":true,"title":"Golden Hour","style":"neon","placement":"bottom_left","animation":"fade_up","source":"ai"}]}`},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := ParseTypographyPlanJSON([]byte(tc.data), map[string]int{"photo-1": 4000}); err == nil {
				t.Fatal("expected invalid structured output to be rejected")
			}
		})
	}
}

func TestDefaultTypographyPlannerChoosesOnlyMeaningfulPhotos(t *testing.T) {
	t.Parallel()
	plan, err := DefaultTypographyPlanner{}.Generate(context.Background(), TypographyPlanningInput{
		ProjectID: "project-typography", StoryPlanID: "story-plan-1", StoryPlanVersion: 1, TypographyVersion: TypographyPromptVersion,
		StoryTitle: "Weekend by the Sea", VisualStyle: models.VisualStyleMinimal,
		Moments: []TypographyMomentContext{
			{MomentID: "photo-1", AssetID: "photo-1", MediaType: models.MediaTypePhoto, Caption: "person walking by the ocean at sunset", DurationMS: 3500, Score: 0.91, Index: 0},
			{MomentID: "video-1", AssetID: "video-1", MediaType: models.MediaTypeVideo, Caption: "friends laughing", DurationMS: 4000, Score: 0.99, Index: 1},
			{MomentID: "photo-generic", AssetID: "photo-generic", MediaType: models.MediaTypePhoto, Caption: "beautiful day", DurationMS: 3500, Score: 0.95, Index: 2},
			{MomentID: "photo-2", AssetID: "photo-2", MediaType: models.MediaTypePhoto, Caption: "quiet dinner under candlelight", DurationMS: 3500, Score: 0.82, Index: 3},
			{MomentID: "photo-3", AssetID: "photo-3", MediaType: models.MediaTypePhoto, Caption: "morning light through curtains", DurationMS: 3500, Score: 0.8, Index: 4},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Overlays) != MaxTypographyPerStory {
		t.Fatalf("expected typography limit of %d, got %+v", MaxTypographyPerStory, plan.Overlays)
	}
	if _, ok := plan.Overlays["video-1"]; ok {
		t.Fatal("video moment should not receive photo typography")
	}
	if _, ok := plan.Overlays["photo-generic"]; ok {
		t.Fatal("generic caption should not receive typography")
	}
	if overlay := plan.Overlays["photo-1"]; overlay.Style != models.TypographyStyleMinimal || overlay.Title == "" || overlay.Source != models.TextSourceAI {
		t.Fatalf("expected minimal AI typography for best photo, got %+v", overlay)
	}
}

type failingTypographyPlanner struct{}

func (failingTypographyPlanner) Generate(context.Context, TypographyPlanningInput) (TypographyPlan, error) {
	return TypographyPlan{}, errors.New("typography provider unavailable")
}

func TestPlannerTypographyFailureFallsBackToPlainStory(t *testing.T) {
	t.Parallel()
	plan, err := (&Planner{TypographyPlanner: failingTypographyPlanner{}, Now: func() time.Time { return time.Date(2026, time.August, 25, 12, 0, 0, 0, time.UTC) }}).Plan(context.Background(), Request{
		ProjectID: "project-typography-fallback", DesiredDuration: 5 * time.Second, StoryType: models.StoryTypeCustom, VisualStyle: models.VisualStyleCinematic,
		PhotoAnalyses: []models.PhotoAnalysis{{ID: "analysis-1", PhotoID: "photo-1", ProjectID: "project-typography-fallback", AnalysisVersion: "photo-v1", Caption: "person walking by the ocean at sunset", MemoryValue: 0.9, EmotionalValue: 0.85, VisualQuality: 0.85, StoryRelevance: 0.9, Selected: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, moment := range plan.Moments {
		if moment.Typography != nil {
			t.Fatalf("typography failure should not attach stale overlays: %+v", plan.Moments)
		}
	}
}
