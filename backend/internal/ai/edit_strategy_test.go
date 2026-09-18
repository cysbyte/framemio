package ai

import (
	"context"
	"testing"
	"time"

	"github.com/framemio/backend/internal/models"
)

func TestEditStrategiesGenerateRepresentativePlans(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		projectType models.ProjectType
		config      models.VideoProjectConfiguration
		wantAspect  string
		assert      func(*testing.T, models.StoryPlan)
	}{
		{
			name: "story", projectType: models.ProjectTypeStory, wantAspect: "9:16",
			assert: func(t *testing.T, plan models.StoryPlan) {
				t.Helper()
				if plan.Moments[0].MediaID != "media-morning" {
					t.Fatalf("story should preserve contextual order, got %+v", plan.Moments)
				}
				if plan.Moments[0].ScenePurpose != "" {
					t.Fatalf("story should not get promo scene purpose: %+v", plan.Moments[0])
				}
			},
		},
		{
			name: "promo", projectType: models.ProjectTypePromo, wantAspect: "16:9",
			config: models.VideoProjectConfiguration{Promo: &models.PromoProjectConfiguration{CallToAction: "Download now", Hook: "Create videos faster"}},
			assert: func(t *testing.T, plan models.StoryPlan) {
				t.Helper()
				if plan.Moments[0].ScenePurpose != models.PromoScenePurposeHook {
					t.Fatalf("promo should assign hook scene purpose, got %+v", plan.Moments[0])
				}
				if plan.CallToAction != "Download now" {
					t.Fatalf("promo CTA = %q", plan.CallToAction)
				}
			},
		},
		{
			name: "reel", projectType: models.ProjectTypeReel, wantAspect: "9:16",
			config: models.VideoProjectConfiguration{Reel: &models.ReelProjectConfiguration{Style: "dynamic", HookPreference: "Start with action"}},
			assert: func(t *testing.T, plan models.StoryPlan) {
				t.Helper()
				if plan.Moments[0].CropIntent != "fill" || plan.Moments[0].Order != 1 {
					t.Fatalf("reel should be ordered and fill-framed, got %+v", plan.Moments[0])
				}
				if plan.Moments[0].Typography == nil || plan.Moments[0].Typography.Title != "Start with action" {
					t.Fatalf("reel should receive hook typography, got %+v", plan.Moments[0].Typography)
				}
			},
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			plan, err := (&Planner{}).Plan(context.Background(), Request{
				ProjectID:            "project-" + test.name,
				Version:              1,
				ProjectType:          test.projectType,
				ProjectConfiguration: test.config,
				DesiredDuration:      12 * time.Second,
				StoryType:            models.StoryTypeCustom,
				SelectedMediaIDs:     []string{"media-morning", "media-product", "media-proof"},
				PhotoAnalyses: []models.PhotoAnalysis{
					{PhotoID: "media-morning", ProjectID: "project-" + test.name, Caption: "Morning context", StoryRelevance: 0.7, MemoryValue: 0.7, EmotionalValue: 0.7, VisualQuality: 0.8, TimestampMicros: 1},
					{PhotoID: "media-product", ProjectID: "project-" + test.name, Caption: "Product demo", StoryRelevance: 0.95, MemoryValue: 0.8, EmotionalValue: 0.7, VisualQuality: 0.95, TimestampMicros: 2},
					{PhotoID: "media-proof", ProjectID: "project-" + test.name, Caption: "Happy result", StoryRelevance: 0.82, MemoryValue: 0.8, EmotionalValue: 0.8, VisualQuality: 0.85, TimestampMicros: 3},
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			if plan.ProjectType != test.projectType || plan.Output.AspectRatio != test.wantAspect {
				t.Fatalf("plan type/aspect = %q/%q", plan.ProjectType, plan.Output.AspectRatio)
			}
			test.assert(t, plan)
		})
	}
}

func TestEditStrategyRejectsMissingAssetsThroughValidation(t *testing.T) {
	t.Parallel()

	request := storyPlannerProviderRequest()
	plan := storyPlannerProviderPlan(t)
	plan.Moments[0].MediaID = "missing-media"
	plan.Moments[0].AssetID = "missing-media"

	if err := validateStoryPlannerOutputMediaReferences(plan, request); err == nil {
		t.Fatal("expected missing media reference to be rejected")
	}
}

func storyPlannerProviderPlan(t *testing.T) models.StoryPlan {
	t.Helper()
	plan, err := (&Planner{}).Plan(context.Background(), storyPlannerProviderRequest())
	if err != nil {
		t.Fatal(err)
	}
	return plan
}
