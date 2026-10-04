package templates

import (
	"strings"
	"testing"

	"github.com/framemio/backend/internal/models"
)

func TestCatalogContainsVersionedInitialTemplates(t *testing.T) {
	t.Parallel()
	expected := map[string]struct{}{
		"full-screen-cinematic-cutaways": {},
		"photo-over-video":               {},
		"picture-in-picture":             {},
		"warm-memory-recap":              {},
		"caption-led-memory":             {},
		"quick-family-recap":             {},
		"product-spotlight":              {},
		"app-demo-overlay":               {},
		"testimonial-with-images":        {},
		"split-screen-offer":             {},
		"fast-social-ad":                 {},
		"premium-brand-story":            {},
		"regular-reel":                   {},
		"dynamic-transition-reel":        {},
		"brush-paint-reel":               {},
		"fast-montage":                   {},
		"vertical-trio-reel":             {},
		"vertical-quad-reel":             {},
		"reel-picture-in-picture":        {},
		"before-after-split":             {},
		"cinematic-reel-cutaways":        {},
		"talking-head-reel":              {},
		"travel-highlight-reel":          {},
	}
	for _, template := range Catalog() {
		delete(expected, template.ID)
		if template.Version <= 0 {
			t.Fatalf("template %q has invalid version %d", template.ID, template.Version)
		}
		if strings.TrimSpace(template.DemoVideoPath) == "" || !strings.HasPrefix(template.DemoVideoPath, "/videos/templates/") || !strings.HasSuffix(template.DemoVideoPath, ".mp4") {
			t.Fatalf("template %q has invalid demo path %q", template.ID, template.DemoVideoPath)
		}
		if len(template.MediaSlots) == 0 || len(template.RendererCapabilities) == 0 || strings.TrimSpace(template.PlannerGuidance) == "" {
			t.Fatalf("template %q is missing catalog contract fields: %+v", template.ID, template)
		}
	}
	if len(expected) != 0 {
		t.Fatalf("missing initial templates: %+v", expected)
	}
}

func TestCatalogHasExpectedTemplateCountsPerProjectType(t *testing.T) {
	t.Parallel()
	tests := map[models.ProjectType]int{
		models.ProjectTypeStory: 6,
		models.ProjectTypePromo: 6,
		models.ProjectTypeReel:  11,
	}
	for projectType, expectedCount := range tests {
		templates := CatalogForProjectType(projectType)
		if len(templates) != expectedCount {
			t.Fatalf("%s templates = %d, want %d: %+v", projectType, len(templates), expectedCount, templates)
		}
	}
	if _, ok := Find("photo-over-video", 1, models.ProjectTypePromo); ok {
		t.Fatal("promo catalog should not include story photo-over-video template")
	}
	if _, ok := Find("product-spotlight", 1, models.ProjectTypeStory); ok {
		t.Fatal("story catalog should not include promo product-spotlight template")
	}
	if _, ok := Find("fast-montage", 1, models.ProjectTypePromo); ok {
		t.Fatal("promo catalog should not include reel fast-montage template")
	}
}

func TestResolveAppliesDefaultsAndSupportedOverrides(t *testing.T) {
	t.Parallel()
	captions := false
	template, resolved, err := Resolve(models.ProjectTypeStory, "photo-over-video", 1, models.VideoTemplateConfiguration{
		DurationSeconds:  45,
		CaptionsEnabled:  &captions,
		OverlayPlacement: models.VideoTemplatePlacementTopLeft,
		OverlaySize:      models.VideoTemplateSizeLarge,
		OverlayOpacity:   0.65,
	})
	if err != nil {
		t.Fatal(err)
	}
	if template.ID != "photo-over-video" || template.Version != 1 {
		t.Fatalf("unexpected template identity: %+v", template)
	}
	if resolved.DurationSeconds != 45 || resolved.CaptionsEnabled == nil || *resolved.CaptionsEnabled || resolved.OverlayPlacement != models.VideoTemplatePlacementTopLeft || resolved.OverlaySize != models.VideoTemplateSizeLarge || resolved.OverlayOpacity != 0.65 {
		t.Fatalf("supported overrides were not preserved: %+v", resolved)
	}
	if resolved.Layout != models.VideoTemplateLayoutPictureInPicture || resolved.AspectRatio != "9:16" {
		t.Fatalf("defaults were not applied: %+v", resolved)
	}
	if resolved.CaptionStyle != "bold_social" {
		t.Fatalf("story template should default to social captions, got %q", resolved.CaptionStyle)
	}
}

func TestPhotoOverVideoAllowsAutomaticAndExplicitOverlayAnimations(t *testing.T) {
	t.Parallel()

	_, automatic, err := Resolve(models.ProjectTypeStory, "photo-over-video", 1, models.VideoTemplateConfiguration{
		OverlayAnimation: models.VideoTemplateAnimationAuto,
	})
	if err != nil {
		t.Fatal(err)
	}
	if automatic.OverlayAnimation != models.VideoTemplateAnimationAuto {
		t.Fatalf("automatic animation was not preserved: %+v", automatic)
	}

	_, explicit, err := Resolve(models.ProjectTypeStory, "photo-over-video", 1, models.VideoTemplateConfiguration{
		OverlayAnimation: models.VideoTemplateAnimationMove,
	})
	if err != nil {
		t.Fatal(err)
	}
	if explicit.OverlayAnimation != models.VideoTemplateAnimationMove {
		t.Fatalf("explicit animation was not preserved: %+v", explicit)
	}
}

func TestApplyPlanDefaultsFallsBackWhenAutomaticAnimationIsUnresolved(t *testing.T) {
	t.Parallel()

	_, configuration, err := Resolve(models.ProjectTypeStory, "photo-over-video", 1, models.VideoTemplateConfiguration{})
	if err != nil {
		t.Fatal(err)
	}
	configuration.OverlayAnimation = models.VideoTemplateAnimationAuto
	plan := models.StoryPlan{
		TemplateID:            "photo-over-video",
		TemplateVersion:       1,
		TemplateConfiguration: configuration,
		BRoll:                 []models.StoryBRoll{{AssetID: "photo-1"}},
	}
	ApplyPlanDefaults(&plan, configuration)
	if plan.BRoll[0].Animation != models.VideoTemplateAnimationFade {
		t.Fatalf("unresolved automatic animation should fall back to fade: %+v", plan.BRoll[0])
	}
}

func TestApplyPlanDefaultsNormalizesUnsupportedBRollForTemplate(t *testing.T) {
	t.Parallel()

	_, configuration, err := Resolve(models.ProjectTypeReel, "fast-montage", 1, models.VideoTemplateConfiguration{})
	if err != nil {
		t.Fatal(err)
	}
	plan := models.StoryPlan{
		TemplateID:            "fast-montage",
		TemplateVersion:       1,
		ProjectType:           models.ProjectTypeReel,
		TemplateConfiguration: configuration,
		Output:                models.StoryOutput{AspectRatio: configuration.AspectRatio},
		BRoll: []models.StoryBRoll{{
			AssetID:      "photo-1",
			Presentation: models.VideoTemplateLayoutPictureInPicture,
			Placement:    models.VideoTemplatePlacementBottomRight,
			Size:         models.VideoTemplateSizeSmall,
			Animation:    models.VideoTemplateAnimationSubtleScale,
		}},
	}

	ApplyPlanDefaults(&plan, configuration)

	overlay := plan.BRoll[0]
	if overlay.Presentation != models.VideoTemplateLayoutFullFrame ||
		overlay.Placement != models.VideoTemplatePlacementCenter ||
		overlay.Size != models.VideoTemplateSizeLarge ||
		overlay.Animation != models.VideoTemplateAnimationNone {
		t.Fatalf("expected unsupported B-roll settings to be normalized to full-frame montage defaults: %+v", overlay)
	}
	if err := ValidatePlan(plan); err != nil {
		t.Fatalf("expected normalized template plan to validate: %v", err)
	}
}

func TestResolveAllowsFullSourceOnlyForSupportedTemplates(t *testing.T) {
	t.Parallel()
	_, resolved, err := Resolve(models.ProjectTypeReel, "talking-head-reel", 1, models.VideoTemplateConfiguration{
		DurationMode: models.DurationModeFullSource,
		RemoveFiller: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.DurationMode != models.DurationModeFullSource || resolved.DurationSeconds != 0 || !resolved.RemoveFiller {
		t.Fatalf("full-source settings were not preserved: %+v", resolved)
	}
	_, _, err = Resolve(models.ProjectTypeReel, "fast-montage", 1, models.VideoTemplateConfiguration{
		DurationMode: models.DurationModeFullSource,
	})
	if err == nil || !strings.Contains(err.Error(), "full length") {
		t.Fatalf("expected unsupported full length rejection, got %v", err)
	}
	_, _, err = Resolve(models.ProjectTypeStory, "photo-over-video", 1, models.VideoTemplateConfiguration{
		RemoveFiller: true,
	})
	if err == nil || !strings.Contains(err.Error(), "requires full length") {
		t.Fatalf("expected cleanup rejection outside full-source, got %v", err)
	}
}

func TestResolveRejectsUnsupportedOverrides(t *testing.T) {
	t.Parallel()
	_, _, err := Resolve(models.ProjectTypePromo, "fast-social-ad", 1, models.VideoTemplateConfiguration{
		OverlayPlacement: models.VideoTemplatePlacementBottomRight,
	})
	if err == nil || !strings.Contains(err.Error(), "overlay placement") {
		t.Fatalf("expected unsupported overlay placement rejection, got %v", err)
	}
}

func TestNormalizeCompatibleConfigurationPreservesOnlyEditableSettings(t *testing.T) {
	t.Parallel()
	captions := false
	_, resolved, err := NormalizeCompatibleConfiguration(models.ProjectTypePromo, "fast-social-ad", 1, models.VideoTemplateConfiguration{
		AspectRatio:      "16:9",
		DurationSeconds:  30,
		VisualStyle:      models.VisualStyleMinimal,
		CaptionsEnabled:  &captions,
		CaptionStyle:     "clean_minimal",
		SceneDetail:      "balanced",
		OverlayPlacement: models.VideoTemplatePlacementBottomRight,
		OverlaySize:      models.VideoTemplateSizeSmall,
		OverlayFrame:     models.VideoTemplateFrameSubtle,
		OverlayAnimation: models.VideoTemplateAnimationFade,
		OverlayOpacity:   0.5,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.DurationSeconds != 30 || resolved.VisualStyle != models.VisualStyleMinimal || resolved.CaptionsEnabled == nil || *resolved.CaptionsEnabled || resolved.CaptionStyle != "clean_minimal" || resolved.SceneDetail != "balanced" {
		t.Fatalf("compatible settings were not preserved: %+v", resolved)
	}
	if resolved.AspectRatio != "9:16" || resolved.OverlayPlacement != models.VideoTemplatePlacementCenter || resolved.OverlaySize != models.VideoTemplateSizeLarge || resolved.OverlayFrame != models.VideoTemplateFrameNone || resolved.OverlayAnimation != models.VideoTemplateAnimationNone || resolved.OverlayOpacity != 1 {
		t.Fatalf("incompatible settings were not reset to defaults: %+v", resolved)
	}
}

func TestUnsupportedProjectTypeTemplateIsRejected(t *testing.T) {
	t.Parallel()
	if _, _, err := Resolve(models.ProjectTypeStory, "split-screen-offer", 1, models.VideoTemplateConfiguration{}); err == nil {
		t.Fatal("expected story project to reject promo split-screen template")
	}
	if _, _, err := Resolve(models.ProjectTypePromo, "photo-over-video", 1, models.VideoTemplateConfiguration{}); err == nil {
		t.Fatal("expected promo project to reject story photo-over-video template")
	}
}
