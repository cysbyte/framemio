package templates

import (
	"fmt"
	"strings"

	"github.com/framemio/backend/internal/models"
)

var catalog = []models.VideoTemplate{
	{
		ID:                    "full-screen-cinematic-cutaways",
		Version:               1,
		Name:                  "Cinematic cutaways",
		Description:           "Let each supporting moment take the full frame.",
		DemoVideoPath:         "/videos/templates/full-screen-cinematic-cutaways.mp4",
		SupportedProjectTypes: []models.ProjectType{models.ProjectTypeStory, models.ProjectTypePromo, models.ProjectTypeReel},
		Defaults: models.VideoTemplateConfiguration{
			AspectRatio: "9:16", DurationSeconds: 30, VisualStyle: models.VisualStyleCinematic,
			CaptionsEnabled: boolPtr(true), CaptionStyle: "cinematic_subtitle", SceneDetail: "balanced", Layout: models.VideoTemplateLayoutFullFrame,
			OverlayPlacement: models.VideoTemplatePlacementCenter, OverlaySize: models.VideoTemplateSizeLarge,
			OverlayOpacity: 1, OverlayFrame: models.VideoTemplateFrameNone, OverlayAnimation: models.VideoTemplateAnimationFade,
		},
		EditableOptions:      []string{"duration_seconds", "aspect_ratio", "visual_style", "captions_enabled", "caption_style", "scene_detail"},
		MediaSlots:           []models.VideoTemplateMediaSlot{{ID: "cutaways", Role: "supporting_cutaways", MediaTypes: []models.MediaType{models.MediaTypePhoto, models.MediaTypeVideo}, Required: false, Presentation: models.VideoTemplateLayoutFullFrame}},
		PlannerGuidance:      "Use relevant supporting media as full-frame cutaways while preserving the primary story audio.",
		RendererCapabilities: []string{"full_frame_media", "fade"},
	},
	{
		ID:                    "photo-over-video",
		Version:               1,
		Name:                  "Photo over video",
		Description:           "Keep the video moving while a supporting photo appears above it.",
		DemoVideoPath:         "/videos/templates/photo-over-video.mp4",
		SupportedProjectTypes: []models.ProjectType{models.ProjectTypeStory, models.ProjectTypePromo, models.ProjectTypeReel},
		Defaults: models.VideoTemplateConfiguration{
			AspectRatio: "9:16", DurationSeconds: 30, VisualStyle: models.VisualStyleCinematic,
			CaptionsEnabled: boolPtr(true), CaptionStyle: "cinematic_subtitle", SceneDetail: "balanced",
			Layout: models.VideoTemplateLayoutPictureInPicture, OverlayPlacement: models.VideoTemplatePlacementBottomRight,
			OverlaySize: models.VideoTemplateSizeMedium, OverlayOpacity: 1, OverlayFrame: models.VideoTemplateFrameSubtle,
			OverlayAnimation: models.VideoTemplateAnimationFade,
		},
		EditableOptions: []string{"duration_seconds", "aspect_ratio", "visual_style", "captions_enabled", "caption_style", "scene_detail", "overlay_placement", "overlay_size", "overlay_opacity", "overlay_frame", "overlay_animation"},
		MediaSlots: []models.VideoTemplateMediaSlot{
			{ID: "base_video", Role: "primary_video", MediaTypes: []models.MediaType{models.MediaTypeVideo}, Required: true, Presentation: models.VideoTemplateLayoutFullFrame},
			{ID: "supporting_photo", Role: "supporting_photo", MediaTypes: []models.MediaType{models.MediaTypePhoto, models.MediaTypeLivePhoto}, Required: true, Presentation: models.VideoTemplateLayoutPictureInPicture, AllowedPlacements: []models.VideoTemplatePlacement{models.VideoTemplatePlacementTopLeft, models.VideoTemplatePlacementTopRight, models.VideoTemplatePlacementBottomLeft, models.VideoTemplatePlacementBottomRight, models.VideoTemplatePlacementCenter}, AllowedSizes: []models.VideoTemplateSize{models.VideoTemplateSizeSmall, models.VideoTemplateSizeMedium, models.VideoTemplateSizeLarge}},
		},
		PlannerGuidance:      "Prefer one strong base video and one semantically related supporting photo. The base video must continue playing underneath the photo.",
		RendererCapabilities: []string{"full_frame_media", "picture_in_picture", "placement", "bounded_size", "opacity", "frame", "fade", "subtle_scale"},
	},
	{
		ID:                    "picture-in-picture",
		Version:               1,
		Name:                  "Picture in picture",
		Description:           "Layer a secondary moment over the main video like a small memory window.",
		DemoVideoPath:         "/videos/templates/picture-in-picture.mp4",
		SupportedProjectTypes: []models.ProjectType{models.ProjectTypeStory, models.ProjectTypePromo, models.ProjectTypeReel},
		Defaults: models.VideoTemplateConfiguration{
			AspectRatio: "9:16", DurationSeconds: 30, VisualStyle: models.VisualStyleWarm,
			CaptionsEnabled: boolPtr(true), CaptionStyle: "clean_minimal", SceneDetail: "more",
			Layout: models.VideoTemplateLayoutPictureInPicture, OverlayPlacement: models.VideoTemplatePlacementTopRight,
			OverlaySize: models.VideoTemplateSizeSmall, OverlayOpacity: 1, OverlayFrame: models.VideoTemplateFrameSubtle,
			OverlayAnimation: models.VideoTemplateAnimationSubtleScale,
		},
		EditableOptions:      []string{"duration_seconds", "aspect_ratio", "visual_style", "captions_enabled", "caption_style", "scene_detail", "overlay_placement", "overlay_size", "overlay_opacity", "overlay_frame", "overlay_animation"},
		MediaSlots:           []models.VideoTemplateMediaSlot{{ID: "secondary_window", Role: "supporting_media", MediaTypes: []models.MediaType{models.MediaTypePhoto, models.MediaTypeVideo}, Required: true, Presentation: models.VideoTemplateLayoutPictureInPicture, AllowedPlacements: []models.VideoTemplatePlacement{models.VideoTemplatePlacementTopLeft, models.VideoTemplatePlacementTopRight, models.VideoTemplatePlacementBottomLeft, models.VideoTemplatePlacementBottomRight}, AllowedSizes: []models.VideoTemplateSize{models.VideoTemplateSizeSmall, models.VideoTemplateSizeMedium}}},
		PlannerGuidance:      "Use a visually distinct supporting moment in a small secondary window over the strongest base scene.",
		RendererCapabilities: []string{"full_frame_media", "picture_in_picture", "placement", "bounded_size", "opacity", "frame", "fade", "subtle_scale"},
	},
	{
		ID:                    "split-screen-memory",
		Version:               1,
		Name:                  "Split screen",
		Description:           "Show two related moments together in a balanced frame.",
		DemoVideoPath:         "/videos/templates/split-screen-memory.mp4",
		SupportedProjectTypes: []models.ProjectType{models.ProjectTypePromo, models.ProjectTypeReel},
		Defaults: models.VideoTemplateConfiguration{
			AspectRatio: "9:16", DurationSeconds: 30, VisualStyle: models.VisualStyleMinimal,
			CaptionsEnabled: boolPtr(false), CaptionStyle: "clean_minimal", SceneDetail: "balanced",
			Layout: models.VideoTemplateLayoutSplitScreen, OverlayPlacement: models.VideoTemplatePlacementRight,
			OverlaySize: models.VideoTemplateSizeHalf, OverlayOpacity: 1, OverlayFrame: models.VideoTemplateFrameNone,
			OverlayAnimation: models.VideoTemplateAnimationNone,
		},
		EditableOptions:      []string{"duration_seconds", "aspect_ratio", "visual_style", "captions_enabled", "caption_style", "scene_detail"},
		MediaSlots:           []models.VideoTemplateMediaSlot{{ID: "secondary_panel", Role: "paired_media", MediaTypes: []models.MediaType{models.MediaTypePhoto, models.MediaTypeVideo}, Required: true, Presentation: models.VideoTemplateLayoutSplitScreen, AllowedPlacements: []models.VideoTemplatePlacement{models.VideoTemplatePlacementLeft, models.VideoTemplatePlacementRight}, AllowedSizes: []models.VideoTemplateSize{models.VideoTemplateSizeHalf}}},
		PlannerGuidance:      "Pair two complementary moments and keep the secondary panel at half width.",
		RendererCapabilities: []string{"full_frame_media", "split_screen", "placement", "bounded_size"},
	},
	{
		ID:                    "talking-head-supporting-images",
		Version:               1,
		Name:                  "Talking head with images",
		Description:           "Keep the speaker present while relevant photos support what they say.",
		DemoVideoPath:         "/videos/templates/talking-head-supporting-images.mp4",
		SupportedProjectTypes: []models.ProjectType{models.ProjectTypePromo, models.ProjectTypeReel},
		Defaults: models.VideoTemplateConfiguration{
			AspectRatio: "9:16", DurationSeconds: 30, VisualStyle: models.VisualStyleMinimal,
			CaptionsEnabled: boolPtr(true), CaptionStyle: "bold_social", SceneDetail: "more",
			Layout: models.VideoTemplateLayoutPictureInPicture, OverlayPlacement: models.VideoTemplatePlacementBottomLeft,
			OverlaySize: models.VideoTemplateSizeMedium, OverlayOpacity: 1, OverlayFrame: models.VideoTemplateFrameSubtle,
			OverlayAnimation: models.VideoTemplateAnimationFade,
		},
		EditableOptions:      []string{"duration_seconds", "visual_style", "captions_enabled", "caption_style", "scene_detail", "overlay_placement", "overlay_size", "overlay_frame", "overlay_animation"},
		MediaSlots:           []models.VideoTemplateMediaSlot{{ID: "supporting_images", Role: "spoken_context_cutaways", MediaTypes: []models.MediaType{models.MediaTypePhoto, models.MediaTypeLivePhoto}, Required: false, Presentation: models.VideoTemplateLayoutPictureInPicture, AllowedPlacements: []models.VideoTemplatePlacement{models.VideoTemplatePlacementBottomLeft, models.VideoTemplatePlacementBottomRight}, AllowedSizes: []models.VideoTemplateSize{models.VideoTemplateSizeSmall, models.VideoTemplateSizeMedium}}},
		PlannerGuidance:      "Use transcript timing to place relevant supporting photos without obscuring the speaker or captions.",
		RendererCapabilities: []string{"full_frame_media", "picture_in_picture", "placement", "bounded_size", "frame", "fade"},
	},
	{
		ID:                    "fast-montage",
		Version:               1,
		Name:                  "Fast montage",
		Description:           "Build a lively sequence of short, high-energy moments.",
		DemoVideoPath:         "/videos/templates/fast-montage.mp4",
		SupportedProjectTypes: []models.ProjectType{models.ProjectTypePromo, models.ProjectTypeReel},
		Defaults: models.VideoTemplateConfiguration{
			AspectRatio: "9:16", DurationSeconds: 15, VisualStyle: models.VisualStylePlayful,
			CaptionsEnabled: boolPtr(true), CaptionStyle: "bold_social", SceneDetail: "more",
			Layout: models.VideoTemplateLayoutMontage, OverlayPlacement: models.VideoTemplatePlacementCenter,
			OverlaySize: models.VideoTemplateSizeLarge, OverlayOpacity: 1, OverlayFrame: models.VideoTemplateFrameNone,
			OverlayAnimation: models.VideoTemplateAnimationNone,
		},
		EditableOptions:      []string{"duration_seconds", "visual_style", "captions_enabled", "caption_style", "scene_detail"},
		MediaSlots:           []models.VideoTemplateMediaSlot{{ID: "montage_moments", Role: "rapid_sequence", MediaTypes: []models.MediaType{models.MediaTypePhoto, models.MediaTypeVideo}, Required: true, Presentation: models.VideoTemplateLayoutMontage}},
		PlannerGuidance:      "Favor several distinct high-quality moments with short durations and cut transitions.",
		RendererCapabilities: []string{"full_frame_media", "cut"},
	},
}

func Catalog() []models.VideoTemplate {
	result := make([]models.VideoTemplate, len(catalog))
	copy(result, catalog)
	return result
}

func CatalogForProjectType(projectType models.ProjectType) []models.VideoTemplate {
	result := make([]models.VideoTemplate, 0, len(catalog))
	for _, item := range catalog {
		if supportsProjectType(item, projectType) {
			result = append(result, item)
		}
	}
	return result
}

func Find(id string, version int, projectType models.ProjectType) (models.VideoTemplate, bool) {
	id = strings.TrimSpace(id)
	for _, item := range catalog {
		if item.ID != id || (version > 0 && item.Version != version) || !supportsProjectType(item, projectType) {
			continue
		}
		return item, true
	}
	return models.VideoTemplate{}, false
}

func Resolve(projectType models.ProjectType, id string, version int, supplied models.VideoTemplateConfiguration) (models.VideoTemplate, models.VideoTemplateConfiguration, error) {
	if strings.TrimSpace(id) == "" {
		return models.VideoTemplate{}, supplied, nil
	}
	template, ok := Find(id, version, projectType)
	if !ok {
		return models.VideoTemplate{}, models.VideoTemplateConfiguration{}, fmt.Errorf("unknown or unsupported video template %q", id)
	}
	if err := ValidateSuppliedConfiguration(template, supplied); err != nil {
		return models.VideoTemplate{}, models.VideoTemplateConfiguration{}, err
	}
	resolved := mergeSupported(template, supplied)
	if err := ValidateConfiguration(template, resolved); err != nil {
		return models.VideoTemplate{}, models.VideoTemplateConfiguration{}, err
	}
	return template, resolved, nil
}

// NormalizeCompatibleConfiguration preserves only values that a new template
// allows, resetting incompatible settings to the new template defaults.
func NormalizeCompatibleConfiguration(projectType models.ProjectType, id string, version int, previous models.VideoTemplateConfiguration) (models.VideoTemplate, models.VideoTemplateConfiguration, error) {
	template, ok := Find(id, version, projectType)
	if !ok {
		return models.VideoTemplate{}, models.VideoTemplateConfiguration{}, fmt.Errorf("unknown or unsupported video template %q", id)
	}
	resolved := mergeSupported(template, previous)
	if projectType == models.ProjectTypeReel {
		resolved.AspectRatio = "9:16"
	}
	if err := ValidateConfiguration(template, resolved); err != nil {
		return models.VideoTemplate{}, models.VideoTemplateConfiguration{}, err
	}
	return template, resolved, nil
}

func ValidateSuppliedConfiguration(template models.VideoTemplate, config models.VideoTemplateConfiguration) error {
	if config.AspectRatio != "" && !contains(template.EditableOptions, "aspect_ratio") && config.AspectRatio != template.Defaults.AspectRatio {
		return fmt.Errorf("template does not support aspect ratio")
	}
	if config.DurationSeconds > 0 && !contains(template.EditableOptions, "duration_seconds") && config.DurationSeconds != template.Defaults.DurationSeconds {
		return fmt.Errorf("template does not support duration")
	}
	if config.VisualStyle != "" && !contains(template.EditableOptions, "visual_style") && config.VisualStyle != template.Defaults.VisualStyle {
		return fmt.Errorf("template does not support visual style")
	}
	if config.CaptionsEnabled != nil && !contains(template.EditableOptions, "captions_enabled") && template.Defaults.CaptionsEnabled != nil && *config.CaptionsEnabled != *template.Defaults.CaptionsEnabled {
		return fmt.Errorf("template does not support caption preference")
	}
	if config.CaptionStyle != "" && !contains(template.EditableOptions, "caption_style") && config.CaptionStyle != template.Defaults.CaptionStyle {
		return fmt.Errorf("template does not support caption style")
	}
	if config.SceneDetail != "" && !contains(template.EditableOptions, "scene_detail") && config.SceneDetail != template.Defaults.SceneDetail {
		return fmt.Errorf("template does not support scene detail")
	}
	if config.Layout != "" && config.Layout != template.Defaults.Layout {
		return fmt.Errorf("template layout is not editable")
	}
	if config.OverlayPlacement != "" && !contains(template.EditableOptions, "overlay_placement") && config.OverlayPlacement != template.Defaults.OverlayPlacement {
		return fmt.Errorf("template does not support overlay placement")
	}
	if config.OverlaySize != "" && !contains(template.EditableOptions, "overlay_size") && config.OverlaySize != template.Defaults.OverlaySize {
		return fmt.Errorf("template does not support overlay size")
	}
	if config.OverlayOpacity > 0 && !contains(template.EditableOptions, "overlay_opacity") && config.OverlayOpacity != template.Defaults.OverlayOpacity {
		return fmt.Errorf("template does not support overlay opacity")
	}
	if config.OverlayFrame != "" && !contains(template.EditableOptions, "overlay_frame") && config.OverlayFrame != template.Defaults.OverlayFrame {
		return fmt.Errorf("template does not support overlay frame")
	}
	if config.OverlayAnimation != "" && !contains(template.EditableOptions, "overlay_animation") && config.OverlayAnimation != template.Defaults.OverlayAnimation {
		return fmt.Errorf("template does not support overlay animation")
	}
	return nil
}

func ValidateConfiguration(template models.VideoTemplate, config models.VideoTemplateConfiguration) error {
	if config.DurationSeconds < 5 || config.DurationSeconds > 120 {
		return fmt.Errorf("template duration must be between 5 and 120 seconds")
	}
	if config.AspectRatio != "9:16" && config.AspectRatio != "16:9" && config.AspectRatio != "1:1" {
		return fmt.Errorf("template aspect ratio %q is unsupported", config.AspectRatio)
	}
	if config.OverlayOpacity < 0.35 || config.OverlayOpacity > 1 {
		return fmt.Errorf("template overlay opacity must be between 0.35 and 1")
	}
	if !contains(template.EditableOptions, "overlay_placement") && config.OverlayPlacement != template.Defaults.OverlayPlacement {
		return fmt.Errorf("template does not support overlay placement")
	}
	if !contains(template.EditableOptions, "overlay_size") && config.OverlaySize != template.Defaults.OverlaySize {
		return fmt.Errorf("template does not support overlay size")
	}
	if !contains(template.EditableOptions, "overlay_opacity") && config.OverlayOpacity != template.Defaults.OverlayOpacity {
		return fmt.Errorf("template does not support overlay opacity")
	}
	if !contains(template.EditableOptions, "overlay_frame") && config.OverlayFrame != template.Defaults.OverlayFrame {
		return fmt.Errorf("template does not support overlay frame")
	}
	if !contains(template.EditableOptions, "overlay_animation") && config.OverlayAnimation != template.Defaults.OverlayAnimation {
		return fmt.Errorf("template does not support overlay animation")
	}
	if config.Layout != template.Defaults.Layout {
		return fmt.Errorf("template layout is not editable")
	}
	if config.OverlayPlacement == "" || config.OverlaySize == "" || config.OverlayFrame == "" || config.OverlayAnimation == "" {
		return fmt.Errorf("template overlay configuration is incomplete")
	}
	if !validPlacement(config.OverlayPlacement) || !validSize(config.OverlaySize) || !validFrame(config.OverlayFrame) || !validAnimation(config.OverlayAnimation) {
		return fmt.Errorf("template overlay configuration contains an unsupported value")
	}
	if len(template.MediaSlots) == 0 {
		return fmt.Errorf("template must define at least one media slot")
	}
	for _, slot := range template.MediaSlots {
		if slot.Presentation != models.VideoTemplateLayoutFullFrame && slot.Presentation != template.Defaults.Layout {
			return fmt.Errorf("template slot %q uses an unsupported presentation", slot.ID)
		}
	}
	return nil
}

func ApplyPlanDefaults(plan *models.StoryPlan, config models.VideoTemplateConfiguration) {
	if plan == nil || len(plan.BRoll) == 0 || config.Layout == "" {
		return
	}
	for index := range plan.BRoll {
		overlay := &plan.BRoll[index]
		if overlay.Presentation == "" {
			overlay.Presentation = config.Layout
		}
		if overlay.Placement == "" {
			overlay.Placement = config.OverlayPlacement
		}
		if overlay.Size == "" {
			overlay.Size = config.OverlaySize
		}
		if overlay.Opacity == 0 {
			overlay.Opacity = config.OverlayOpacity
		}
		if overlay.Frame == "" {
			overlay.Frame = config.OverlayFrame
		}
		if overlay.Animation == "" {
			overlay.Animation = config.OverlayAnimation
		}
	}
}

func ValidatePlan(plan models.StoryPlan) error {
	if strings.TrimSpace(plan.TemplateID) == "" {
		return nil
	}
	template, ok := Find(plan.TemplateID, plan.TemplateVersion, plan.ProjectType)
	if !ok {
		return fmt.Errorf("story plan references unknown or unsupported template %q", plan.TemplateID)
	}
	if err := ValidateConfiguration(template, plan.TemplateConfiguration); err != nil {
		return fmt.Errorf("template configuration is invalid: %w", err)
	}
	if plan.Output.AspectRatio != plan.TemplateConfiguration.AspectRatio {
		return fmt.Errorf("story plan aspect ratio does not match template configuration")
	}
	for index, overlay := range plan.BRoll {
		presentation := overlay.Presentation
		if presentation == "" {
			presentation = models.VideoTemplateLayoutFullFrame
		}
		if presentation != template.Defaults.Layout && presentation != models.VideoTemplateLayoutFullFrame {
			return fmt.Errorf("b-roll %d uses unsupported template presentation %q", index, presentation)
		}
		if presentation != models.VideoTemplateLayoutFullFrame && !templateSupportsPresentation(template, presentation) {
			return fmt.Errorf("b-roll %d uses a presentation not supported by template %q", index, template.ID)
		}
		if overlay.Opacity < 0 || overlay.Opacity > 1 {
			return fmt.Errorf("b-roll %d opacity is outside 0..1", index)
		}
		if overlay.Placement != "" && !validPlacement(overlay.Placement) {
			return fmt.Errorf("b-roll %d placement %q is unsupported", index, overlay.Placement)
		}
		if overlay.Size != "" && !validSize(overlay.Size) {
			return fmt.Errorf("b-roll %d size %q is unsupported", index, overlay.Size)
		}
		if overlay.Placement != "" && !templateSupportsPlacement(template, presentation, overlay.Placement) {
			return fmt.Errorf("b-roll %d placement %q is not allowed by template %q", index, overlay.Placement, template.ID)
		}
		if overlay.Size != "" && !templateSupportsSize(template, presentation, overlay.Size) {
			return fmt.Errorf("b-roll %d size %q is not allowed by template %q", index, overlay.Size, template.ID)
		}
		if overlay.Frame != "" && !validFrame(overlay.Frame) {
			return fmt.Errorf("b-roll %d frame %q is unsupported", index, overlay.Frame)
		}
		if overlay.Animation != "" && !validAnimation(overlay.Animation) {
			return fmt.Errorf("b-roll %d animation %q is unsupported", index, overlay.Animation)
		}
	}
	return nil
}

func NormalizeProjectConfiguration(projectType models.ProjectType, configuration models.VideoProjectConfiguration) (models.VideoProjectConfiguration, error) {
	if strings.TrimSpace(configuration.TemplateID) == "" {
		return models.NormalizeVideoProjectConfiguration(projectType, configuration), nil
	}
	template, resolved, err := Resolve(projectType, configuration.TemplateID, configuration.TemplateVersion, configuration.TemplateConfiguration)
	if err != nil {
		return models.VideoProjectConfiguration{}, err
	}
	configuration.TemplateID = template.ID
	configuration.TemplateConfiguration = resolved
	configuration.TemplateVersion = template.Version
	configuration.Composition.AspectRatio = resolved.AspectRatio
	if projectType == models.ProjectTypeReel {
		configuration.Composition.AspectRatio = "9:16"
		configuration.TemplateConfiguration.AspectRatio = "9:16"
	}
	return models.NormalizeVideoProjectConfiguration(projectType, configuration), nil
}

func templateVersion(id string, version int, projectType models.ProjectType) int {
	template, ok := Find(id, version, projectType)
	if !ok {
		return version
	}
	return template.Version
}

func mergeSupported(template models.VideoTemplate, supplied models.VideoTemplateConfiguration) models.VideoTemplateConfiguration {
	defaults := template.Defaults
	resolved := defaults
	if contains(template.EditableOptions, "aspect_ratio") && supplied.AspectRatio != "" {
		resolved.AspectRatio = supplied.AspectRatio
	}
	if contains(template.EditableOptions, "duration_seconds") && supplied.DurationSeconds > 0 {
		resolved.DurationSeconds = supplied.DurationSeconds
	}
	if contains(template.EditableOptions, "visual_style") && supplied.VisualStyle != "" {
		resolved.VisualStyle = supplied.VisualStyle
	}
	if contains(template.EditableOptions, "captions_enabled") && supplied.CaptionsEnabled != nil {
		resolved.CaptionsEnabled = supplied.CaptionsEnabled
	}
	if contains(template.EditableOptions, "caption_style") && supplied.CaptionStyle != "" {
		resolved.CaptionStyle = supplied.CaptionStyle
	}
	if contains(template.EditableOptions, "scene_detail") && supplied.SceneDetail != "" {
		resolved.SceneDetail = supplied.SceneDetail
	}
	if contains(template.EditableOptions, "overlay_placement") && supplied.OverlayPlacement != "" {
		resolved.OverlayPlacement = supplied.OverlayPlacement
	}
	if contains(template.EditableOptions, "overlay_size") && supplied.OverlaySize != "" {
		resolved.OverlaySize = supplied.OverlaySize
	}
	if contains(template.EditableOptions, "overlay_opacity") && supplied.OverlayOpacity > 0 {
		resolved.OverlayOpacity = supplied.OverlayOpacity
	}
	if contains(template.EditableOptions, "overlay_frame") && supplied.OverlayFrame != "" {
		resolved.OverlayFrame = supplied.OverlayFrame
	}
	if contains(template.EditableOptions, "overlay_animation") && supplied.OverlayAnimation != "" {
		resolved.OverlayAnimation = supplied.OverlayAnimation
	}
	return resolved
}

func supportsProjectType(template models.VideoTemplate, projectType models.ProjectType) bool {
	for _, supported := range template.SupportedProjectTypes {
		if supported == projectType {
			return true
		}
	}
	return false
}

func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

func validPlacement(value models.VideoTemplatePlacement) bool {
	switch value {
	case models.VideoTemplatePlacementTopLeft, models.VideoTemplatePlacementTopRight, models.VideoTemplatePlacementBottomLeft, models.VideoTemplatePlacementBottomRight, models.VideoTemplatePlacementCenter, models.VideoTemplatePlacementLeft, models.VideoTemplatePlacementRight:
		return true
	default:
		return false
	}
}

func validSize(value models.VideoTemplateSize) bool {
	switch value {
	case models.VideoTemplateSizeSmall, models.VideoTemplateSizeMedium, models.VideoTemplateSizeLarge, models.VideoTemplateSizeHalf:
		return true
	default:
		return false
	}
}

func validFrame(value models.VideoTemplateFrame) bool {
	switch value {
	case models.VideoTemplateFrameNone, models.VideoTemplateFrameSubtle, models.VideoTemplateFrameBold:
		return true
	default:
		return false
	}
}

func validAnimation(value models.VideoTemplateAnimation) bool {
	switch value {
	case models.VideoTemplateAnimationNone, models.VideoTemplateAnimationFade, models.VideoTemplateAnimationSubtleScale:
		return true
	default:
		return false
	}
}

func templateSupportsPresentation(template models.VideoTemplate, presentation models.VideoTemplateLayout) bool {
	for _, slot := range template.MediaSlots {
		if slot.Presentation == presentation {
			return true
		}
	}
	return false
}

func templateSupportsPlacement(template models.VideoTemplate, presentation models.VideoTemplateLayout, placement models.VideoTemplatePlacement) bool {
	hasPresentationSlot := false
	for _, slot := range template.MediaSlots {
		if slot.Presentation != presentation {
			continue
		}
		hasPresentationSlot = true
		if len(slot.AllowedPlacements) == 0 || containsPlacement(slot.AllowedPlacements, placement) {
			return true
		}
	}
	return !hasPresentationSlot
}

func templateSupportsSize(template models.VideoTemplate, presentation models.VideoTemplateLayout, size models.VideoTemplateSize) bool {
	hasPresentationSlot := false
	for _, slot := range template.MediaSlots {
		if slot.Presentation != presentation {
			continue
		}
		hasPresentationSlot = true
		if len(slot.AllowedSizes) == 0 || containsSize(slot.AllowedSizes, size) {
			return true
		}
	}
	return !hasPresentationSlot
}

func containsPlacement(values []models.VideoTemplatePlacement, value models.VideoTemplatePlacement) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

func containsSize(values []models.VideoTemplateSize, value models.VideoTemplateSize) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

func boolPtr(value bool) *bool {
	return &value
}
