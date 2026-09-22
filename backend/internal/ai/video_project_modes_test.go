package ai

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/framemio/backend/internal/models"
)

func TestPlannerGeneratesEditPlansForEveryProjectType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		projectType models.ProjectType
		config      models.VideoProjectConfiguration
		aspectRatio string
	}{
		{
			name: "story", projectType: models.ProjectTypeStory,
			config:      models.VideoProjectConfiguration{Story: &models.StoryProjectConfiguration{EmotionalTone: "warm"}},
			aspectRatio: "9:16",
		},
		{
			name: "promo", projectType: models.ProjectTypePromo,
			config: models.VideoProjectConfiguration{Promo: &models.PromoProjectConfiguration{
				ProductInformation: "FrameMio app", CallToAction: "Try FrameMio today", Hook: "Your memories, made cinematic",
			}},
			aspectRatio: "16:9",
		},
		{
			name: "reel", projectType: models.ProjectTypeReel,
			config: models.VideoProjectConfiguration{Reel: &models.ReelProjectConfiguration{
				SocialPlatform: "instagram_reels", TargetDurationSecs: 15, Style: "dynamic", HookPreference: "question", AspectRatio: "1:1",
			}},
			aspectRatio: "9:16",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			plan, err := (&Planner{Now: func() time.Time {
				return time.Date(2026, time.September, 10, 10, 0, 0, 0, time.UTC)
			}}).Plan(context.Background(), Request{
				ProjectID:            "project-" + test.name,
				Version:              1,
				ProjectType:          test.projectType,
				ProjectConfiguration: test.config,
				DesiredDuration:      8 * time.Second,
				StoryType:            models.StoryTypeCustom,
				VisualStyle:          models.VisualStyleCinematic,
				SelectedMediaIDs:     []string{"photo-1"},
				PhotoAnalyses: []models.PhotoAnalysis{{
					ID: "analysis-photo-1", PhotoID: "photo-1", ProjectID: "project-" + test.name,
					AnalysisVersion: "photo-v1", Caption: "A meaningful product moment", Description: "A meaningful moment",
					MemoryValue: 0.9, EmotionalValue: 0.9, VisualQuality: 0.9, StoryRelevance: 0.9, Selected: true,
				}},
			})
			if err != nil {
				t.Fatal(err)
			}
			if plan.ProjectType != test.projectType {
				t.Fatalf("plan project type = %q, want %q", plan.ProjectType, test.projectType)
			}
			if plan.Output.AspectRatio != test.aspectRatio {
				t.Fatalf("plan aspect ratio = %q, want %q", plan.Output.AspectRatio, test.aspectRatio)
			}
			if test.projectType == models.ProjectTypePromo {
				if plan.CallToAction != "Try FrameMio today" || plan.EndingText != "Try FrameMio today" {
					t.Fatalf("promo CTA was not carried into the edit plan: %+v", plan)
				}
			}
			if err := ValidateStoryPlan(plan); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestStoryPlannerProviderInputCarriesProjectModeConfiguration(t *testing.T) {
	t.Parallel()

	request := Request{
		ProjectID:   "project-reel",
		Version:     2,
		ProjectType: models.ProjectTypeReel,
		ProjectConfiguration: models.VideoProjectConfiguration{
			Reel: &models.ReelProjectConfiguration{SocialPlatform: "youtube_shorts", TargetDurationSecs: 30, Style: "cinematic", AspectRatio: "9:16"},
		},
		DesiredDuration: 30 * time.Second,
		StoryType:       models.StoryTypeCustom,
		VisualStyle:     models.VisualStylePlayful,
	}
	request = normalizeRequest(request)
	input := storyPlannerInputFromRequest(request)
	if input.Project.ProjectType != models.ProjectTypeReel {
		t.Fatalf("provider input project type = %q", input.Project.ProjectType)
	}
	if input.Project.ModeConfiguration.Reel == nil || input.Project.ModeConfiguration.Reel.SocialPlatform != "youtube_shorts" {
		t.Fatalf("provider input omitted mode configuration: %+v", input.Project.ModeConfiguration)
	}
	if input.Project.ModeConfiguration.Reel.Style != "cinematic" || input.Project.RequestedOutput.AspectRatio != "9:16" {
		t.Fatalf("provider input did not normalize reel configuration: %+v output=%+v", input.Project.ModeConfiguration.Reel, input.Project.RequestedOutput)
	}
	data, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Fatal("provider input serialized to empty JSON")
	}
}

func TestPlannerNormalizesUnsupportedModeAspectRatio(t *testing.T) {
	t.Parallel()

	plan, err := (&Planner{}).Plan(context.Background(), Request{
		ProjectID:   "project-reel-invalid-ratio",
		Version:     1,
		ProjectType: models.ProjectTypeReel,
		ProjectConfiguration: models.VideoProjectConfiguration{
			Reel: &models.ReelProjectConfiguration{AspectRatio: "4:5"},
		},
		DesiredDuration:  8 * time.Second,
		StoryType:        models.StoryTypeCustom,
		VisualStyle:      models.VisualStyleCinematic,
		SelectedMediaIDs: []string{"photo-1"},
		PhotoAnalyses: []models.PhotoAnalysis{{
			ID: "analysis-photo-1", PhotoID: "photo-1", ProjectID: "project-reel-invalid-ratio",
			AnalysisVersion: "photo-v1", Caption: "A useful moment",
			MemoryValue: 0.9, EmotionalValue: 0.9, VisualQuality: 0.9, StoryRelevance: 0.9, Selected: true,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Output.AspectRatio != "9:16" {
		t.Fatalf("unsupported reel aspect ratio = %q, want default 9:16", plan.Output.AspectRatio)
	}
}

func TestPlannerKeepsModeCaptionDefaultsForUnknownPreset(t *testing.T) {
	t.Parallel()

	plan, err := (&Planner{}).Plan(context.Background(), Request{
		ProjectID:          "project-promo-caption-default",
		Version:            1,
		ProjectType:        models.ProjectTypePromo,
		DesiredDuration:    8 * time.Second,
		StoryType:          models.StoryTypeCustom,
		VisualStyle:        models.VisualStyleCinematic,
		CaptionStylePreset: "brand_special",
		SelectedMediaIDs:   []string{"photo-1"},
		PhotoAnalyses: []models.PhotoAnalysis{{
			ID: "analysis-photo-1", PhotoID: "photo-1", ProjectID: "project-promo-caption-default",
			AnalysisVersion: "photo-v1", Caption: "A useful moment",
			MemoryValue: 0.9, EmotionalValue: 0.9, VisualQuality: 0.9, StoryRelevance: 0.9, Selected: true,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.CaptionStyle == nil || plan.CaptionStyle.Preset != "clean_minimal" {
		t.Fatalf("promo caption style = %+v, want clean_minimal mode default", plan.CaptionStyle)
	}
	if plan.CaptionStyle.FontSize != 58 || plan.CaptionStyle.MaxLines != 1 || plan.CaptionStyle.MaxCharactersPerLine != 24 {
		t.Fatalf("promo caption sizing = %+v, want larger short-line captions", plan.CaptionStyle)
	}
}

func TestPlannerGeneratesPromoSceneWorkflowMetadata(t *testing.T) {
	t.Parallel()

	plan, err := (&Planner{}).Plan(context.Background(), Request{
		ProjectID:   "project-promo-scenes",
		Version:     1,
		ProjectType: models.ProjectTypePromo,
		ProjectConfiguration: models.VideoProjectConfiguration{
			Promo: &models.PromoProjectConfiguration{
				PromotionCategory:  "app",
				PromotionGoal:      "downloads",
				ProductInformation: "FrameMio app",
				Hook:               "Create ads from your phone",
				CTA:                &models.CTAConfiguration{Text: "Try it free", Style: "button", Placement: "bottom"},
				Brand:              models.BrandConfiguration{Name: "FrameMio", PrimaryColor: "#2244FF", SecondaryColor: "#11AA88", FontPreference: "Inter"},
			},
		},
		DesiredDuration:  15 * time.Second,
		StoryType:        models.StoryTypeCustom,
		VisualStyle:      models.VisualStyleCinematic,
		SelectedMediaIDs: []string{"photo-1", "photo-2", "photo-3"},
		PhotoAnalyses: []models.PhotoAnalysis{
			{ID: "analysis-photo-1", PhotoID: "photo-1", ProjectID: "project-promo-scenes", AnalysisVersion: "photo-v1", Caption: "App dashboard", MemoryValue: 0.9, EmotionalValue: 0.7, VisualQuality: 0.95, StoryRelevance: 0.95, Selected: true},
			{ID: "analysis-photo-2", PhotoID: "photo-2", ProjectID: "project-promo-scenes", AnalysisVersion: "photo-v1", Caption: "Fast workflow", MemoryValue: 0.8, EmotionalValue: 0.7, VisualQuality: 0.9, StoryRelevance: 0.9, Selected: true},
			{ID: "analysis-photo-3", PhotoID: "photo-3", ProjectID: "project-promo-scenes", AnalysisVersion: "photo-v1", Caption: "Finished promo", MemoryValue: 0.8, EmotionalValue: 0.7, VisualQuality: 0.88, StoryRelevance: 0.88, Selected: true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Moments) < 3 {
		t.Fatalf("expected promo scenes, got %d", len(plan.Moments))
	}
	if plan.Moments[0].Order != 1 || plan.Moments[0].ScenePurpose != models.PromoScenePurposeHook {
		t.Fatalf("first scene metadata = order %d purpose %q", plan.Moments[0].Order, plan.Moments[0].ScenePurpose)
	}
	if last := plan.Moments[len(plan.Moments)-1]; last.ScenePurpose != models.PromoScenePurposeCTA {
		t.Fatalf("last scene purpose = %q, want CTA", last.ScenePurpose)
	}
	if plan.CallToAction != "Try it free" || plan.EndingText != "Try it free" {
		t.Fatalf("CTA not carried into promo plan: call_to_action=%q ending=%q", plan.CallToAction, plan.EndingText)
	}
	if plan.Moments[0].Typography == nil || plan.Moments[0].Typography.Title != "Create ads from your phone" {
		t.Fatalf("hook typography not generated: %+v", plan.Moments[0].Typography)
	}
}

func TestPlannerGeneratesReelEditPlanDefaults(t *testing.T) {
	t.Parallel()

	plan, err := (&Planner{}).Plan(context.Background(), Request{
		ProjectID:   "project-reel-defaults",
		Version:     1,
		ProjectType: models.ProjectTypeReel,
		ProjectConfiguration: models.VideoProjectConfiguration{
			Reel: &models.ReelProjectConfiguration{
				SocialPlatform: "TikTok", TargetDurationSecs: 15, Style: "dynamic",
				HookPreference: "Start with the jump", CaptionStyle: "bold_social", AspectRatio: "16:9",
			},
		},
		DesiredDuration:     15 * time.Second,
		TargetDurationFixed: true,
		StoryType:           models.StoryTypeCustom,
		UserInstructions:    "Remove boring parts but keep context.",
		SelectedMediaIDs:    []string{"video-1", "video-2"},
		CaptionStylePreset:  "bold_social",
		VideoAnalyses: []models.VideoAnalysis{
			reelVideoAnalysis("video-1", "project-reel-defaults", "Opening jump with excited reaction", 0.96, []models.VideoTranscriptSegment{{StartMicros: 0, EndMicros: 1400000, Text: "Watch this part", Confidence: 0.93}}),
			reelVideoAnalysis("video-2", "project-reel-defaults", "Clear follow-up context", 0.82, nil),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.ProjectType != models.ProjectTypeReel || plan.Output.AspectRatio != "9:16" {
		t.Fatalf("reel plan project/aspect = %q/%q", plan.ProjectType, plan.Output.AspectRatio)
	}
	if len(plan.Moments) < 2 {
		t.Fatalf("expected multiple reel clips, got %d", len(plan.Moments))
	}
	if plan.Moments[0].Order != 1 || plan.Moments[0].CropIntent != "fill" {
		t.Fatalf("first reel clip metadata = order %d crop %q", plan.Moments[0].Order, plan.Moments[0].CropIntent)
	}
	if plan.Moments[0].Typography == nil || plan.Moments[0].Typography.Title != "Start with the jump" {
		t.Fatalf("reel hook typography not generated: %+v", plan.Moments[0].Typography)
	}
	if plan.CaptionStyle == nil || plan.CaptionStyle.Preset != "bold_social" || plan.CaptionStyle.BottomOffset < 120 {
		t.Fatalf("reel captions not mobile-safe: %+v", plan.CaptionStyle)
	}
	for _, moment := range plan.Moments {
		if moment.DurationMS > 4200 {
			t.Fatalf("reel clip duration = %d, want tight default", moment.DurationMS)
		}
	}
}

func reelVideoAnalysis(videoID, projectID, description string, relevance float64, transcript []models.VideoTranscriptSegment) models.VideoAnalysis {
	return models.VideoAnalysis{
		ID: "analysis-" + videoID, VideoID: videoID, ProjectID: projectID, AnalysisVersion: "video-v1",
		DurationMicros: 5_000_000,
		Transcript:     models.VideoTranscript{HasSpeech: len(transcript) > 0, Segments: transcript},
		Segments: []models.VideoSegment{{
			SegmentID: "segment-" + videoID, SceneID: "scene-" + videoID, StartMicros: 0, EndMicros: 5_000_000, Description: description,
			Importance: relevance, VisualQuality: 0.9, MemoryValue: 0.8, EmotionalValue: 0.82, StoryRelevance: relevance,
			Recommended: true, RecommendedUsage: "main",
		}},
		QualitySignals: models.VideoQualitySignals{OverallScore: 0.9},
	}
}
