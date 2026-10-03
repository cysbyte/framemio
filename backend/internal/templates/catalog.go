package templates

import (
	"fmt"
	"strings"

	"github.com/framemio/backend/internal/models"
)

var catalog = []models.VideoTemplate{
	fullFrameTemplate("full-screen-cinematic-cutaways", models.ProjectTypeStory, "Cinematic cutaways", "Let each supporting moment take the full frame.", "/videos/templates/full-screen-cinematic-cutaways.mp4", 30, models.VisualStyleCinematic, true, "bold_social", "balanced", "Use relevant supporting media as full-frame cutaways while preserving the primary story audio."),
	photoOverVideoTemplate(),
	pipTemplate("picture-in-picture", models.ProjectTypeStory, "Picture in picture", "Layer a secondary moment over the main video like a small memory window.", "/videos/templates/picture-in-picture.mp4", 30, models.VisualStyleWarm, true, "clean_minimal", "more", models.VideoTemplatePlacementTopRight, models.VideoTemplateSizeSmall, models.VideoTemplateAnimationSubtleScale, "Use a visually distinct supporting moment in a small secondary window over the strongest base scene."),
	fullFrameTemplate("warm-memory-recap", models.ProjectTypeStory, "Warm memory recap", "A gentle story with soft pacing and warm full-frame moments.", "/videos/templates/warm-memory-recap.mp4", 30, models.VisualStyleWarm, true, "clean_minimal", "balanced", "Favor emotionally connected moments, warm crops, and gentle full-frame cutaways."),
	pipTemplate("caption-led-memory", models.ProjectTypeStory, "Caption-led memory", "A readable story where captions carry the emotional thread.", "/videos/templates/caption-led-memory.mp4", 30, models.VisualStyleMinimal, true, "bold_social", "more", models.VideoTemplatePlacementBottomLeft, models.VideoTemplateSizeMedium, models.VideoTemplateAnimationFade, "Use overlays sparingly and keep captions readable while selecting supporting images that reinforce the narration."),
	montageTemplate("quick-family-recap", models.ProjectTypeStory, "Quick family recap", "A short upbeat memory built from several quick moments.", "/videos/templates/quick-family-recap.mp4", 15, models.VisualStylePlayful, true, "bold_social", "more", "Favor a concise sequence of distinct family or everyday moments with quick cuts."),

	fullFrameTemplate("product-spotlight", models.ProjectTypePromo, "Product spotlight", "A clean promo that keeps the product or offer full frame.", "/videos/templates/product-spotlight.mp4", 30, models.VisualStyleElegant, true, "clean_minimal", "balanced", "Lead with the strongest product or offer visuals and use full-frame supporting scenes."),
	pipTemplate("app-demo-overlay", models.ProjectTypePromo, "App demo overlay", "Show a supporting screen or image above a continuing product video.", "/videos/templates/app-demo-overlay.mp4", 30, models.VisualStyleMinimal, true, "clean_minimal", "more", models.VideoTemplatePlacementBottomRight, models.VideoTemplateSizeMedium, models.VideoTemplateAnimationFade, "Use a clear base video and overlay related product screenshots or proof points without blocking the main action."),
	pipTemplate("testimonial-with-images", models.ProjectTypePromo, "Testimonial with images", "Keep the speaker or main clip present while proof images appear.", "/videos/templates/testimonial-with-images.mp4", 30, models.VisualStyleMinimal, true, "bold_social", "more", models.VideoTemplatePlacementBottomLeft, models.VideoTemplateSizeMedium, models.VideoTemplateAnimationFade, "Use transcript or prompt context to place supporting proof images while keeping the subject visible."),
	splitTemplate("split-screen-offer", models.ProjectTypePromo, "Split-screen offer", "Compare two product moments in a balanced promotional frame.", "/videos/templates/split-screen-offer.mp4", 30, models.VisualStyleMinimal, false, "clean_minimal", "balanced", "Pair complementary product or offer visuals and keep the secondary panel at half width."),
	montageTemplate("fast-social-ad", models.ProjectTypePromo, "Fast social ad", "A high-energy promo sequence for quick attention.", "/videos/templates/fast-social-ad.mp4", 15, models.VisualStylePlayful, true, "bold_social", "more", "Favor several distinct promotional moments with short durations and direct visual pacing."),
	fullFrameTemplate("premium-brand-story", models.ProjectTypePromo, "Premium brand story", "A polished brand edit with calm full-frame visuals.", "/videos/templates/premium-brand-story.mp4", 30, models.VisualStyleElegant, true, "cinematic_subtitle", "balanced", "Use elegant full-frame scenes, slower pacing, and clear brand-oriented visual progression."),

	montageTemplate("regular-reel", models.ProjectTypeReel, "Regular reel", "Play selected moments one by one as a clean full-screen reel.", "/videos/templates/regular-reel.mp4", 15, models.VisualStyleWarm, false, "bold_social", "balanced", "Use selected moments in order as full-screen scenes. Keep each clip clear, simple, and sequential without captions."),
	montageTemplate("dynamic-transition-reel", models.ProjectTypeReel, "Dynamic transitions", "Move between scenes with zoom, blur, and sliding motion.", "/videos/templates/dynamic-transition-reel.mp4", 15, models.VisualStylePlayful, false, "bold_social", "more", "Use selected moments as full-screen scenes with energetic zoom, blur, and horizontal slide transitions between clips. Keep it local-render friendly and avoid captions."),
	montageTemplate("fast-montage", models.ProjectTypeReel, "Four-panel reveal", "Reveal four moving moments one by one, then close them in reverse order.", "/videos/templates/fast-montage.mp4", 15, models.VisualStylePlayful, false, "bold_social", "more", "Use four distinct moments in a two-by-two layout. Reveal panels in reading order over a black opening, keep all panels visible, then hide them in reverse order before ending on black."),
	montageTemplate("vertical-trio-reel", models.ProjectTypeReel, "Vertical trio", "Stack three moving scenes vertically in one reel.", "/videos/templates/vertical-trio-reel.mp4", 15, models.VisualStylePlayful, false, "bold_social", "more", "Use three distinct video moments stacked vertically as top, middle, and bottom scenes. Keep all three scenes moving together for the full reel without captions."),
	montageTemplate("vertical-quad-reel", models.ProjectTypeReel, "Vertical quad", "Stack four moving scenes vertically in one reel.", "/videos/templates/vertical-quad-reel.mp4", 15, models.VisualStylePlayful, false, "bold_social", "more", "Use four distinct video moments stacked vertically as top, upper-middle, lower-middle, and bottom scenes. Keep all four scenes moving together for the full reel without captions."),
	pipTemplate("reel-picture-in-picture", models.ProjectTypeReel, "Reel picture-in-picture", "Layer a secondary moment over the main vertical clip.", "/videos/templates/reel-picture-in-picture.mp4", 15, models.VisualStyleCinematic, true, "bold_social", "more", models.VideoTemplatePlacementTopRight, models.VideoTemplateSizeSmall, models.VideoTemplateAnimationSubtleScale, "Use a strong vertical base clip and a visually distinct supporting moment as a compact overlay."),
	splitTemplate("before-after-split", models.ProjectTypeReel, "Before/after split", "Show two related moments side by side for comparison.", "/videos/templates/before-after-split.mp4", 15, models.VisualStyleMinimal, true, "bold_social", "balanced", "Pair before/after or contrast moments and maintain a clear half-frame comparison."),
	fullFrameTemplate("cinematic-reel-cutaways", models.ProjectTypeReel, "Cinematic reel cutaways", "A vertical reel with full-frame cinematic inserts.", "/videos/templates/cinematic-reel-cutaways.mp4", 30, models.VisualStyleCinematic, true, "cinematic_subtitle", "balanced", "Use punchy full-frame cutaways while maintaining short-form pacing."),
	pipTemplate("talking-head-reel", models.ProjectTypeReel, "Talking-head reel", "Keep the speaker present while supporting visuals appear.", "/videos/templates/talking-head-reel.mp4", 30, models.VisualStyleMinimal, true, "bold_social", "more", models.VideoTemplatePlacementBottomRight, models.VideoTemplateSizeMedium, models.VideoTemplateAnimationFade, "Use transcript timing to place supporting visuals without obscuring the speaker or captions."),
	montageTemplate("travel-highlight-reel", models.ProjectTypeReel, "Travel highlight reel", "A bright vertical recap built from quick place and people moments.", "/videos/templates/travel-highlight-reel.mp4", 15, models.VisualStyleWarm, true, "bold_social", "more", "Favor varied travel or activity clips with quick pacing and readable captions."),
}

func fullFrameTemplate(id string, projectType models.ProjectType, name, description, demoPath string, duration int, style models.VisualStyle, captions bool, captionStyle, sceneDetail, guidance string) models.VideoTemplate {
	return baseTemplate(id, projectType, name, description, demoPath, duration, style, captions, captionStyle, sceneDetail, models.VideoTemplateLayoutFullFrame, models.VideoTemplatePlacementCenter, models.VideoTemplateSizeLarge, models.VideoTemplateFrameNone, models.VideoTemplateAnimationFade, []string{"duration_seconds", "duration_mode", "aspect_ratio", "visual_style", "captions_enabled", "caption_style", "scene_detail"}, []models.VideoTemplateMediaSlot{{ID: "cutaways", Role: "supporting_cutaways", MediaTypes: []models.MediaType{models.MediaTypePhoto, models.MediaTypeVideo}, Required: false, Presentation: models.VideoTemplateLayoutFullFrame}}, guidance, []string{"full_frame_media", "fade", "duration_mode_full_source"})
}

func pipTemplate(id string, projectType models.ProjectType, name, description, demoPath string, duration int, style models.VisualStyle, captions bool, captionStyle, sceneDetail string, placement models.VideoTemplatePlacement, size models.VideoTemplateSize, animation models.VideoTemplateAnimation, guidance string) models.VideoTemplate {
	return baseTemplate(id, projectType, name, description, demoPath, duration, style, captions, captionStyle, sceneDetail, models.VideoTemplateLayoutPictureInPicture, placement, size, models.VideoTemplateFrameSubtle, animation, []string{"duration_seconds", "duration_mode", "aspect_ratio", "visual_style", "captions_enabled", "caption_style", "scene_detail", "overlay_placement", "overlay_size", "overlay_opacity", "overlay_frame", "overlay_animation"}, []models.VideoTemplateMediaSlot{
		{ID: "base_video", Role: "primary_video", MediaTypes: []models.MediaType{models.MediaTypeVideo}, Required: true, Presentation: models.VideoTemplateLayoutFullFrame},
		{ID: "overlay_media", Role: "supporting_media", MediaTypes: []models.MediaType{models.MediaTypePhoto, models.MediaTypeLivePhoto, models.MediaTypeVideo}, Required: true, Presentation: models.VideoTemplateLayoutPictureInPicture, AllowedPlacements: []models.VideoTemplatePlacement{models.VideoTemplatePlacementTopLeft, models.VideoTemplatePlacementTopRight, models.VideoTemplatePlacementBottomLeft, models.VideoTemplatePlacementBottomRight, models.VideoTemplatePlacementCenter}, AllowedSizes: []models.VideoTemplateSize{models.VideoTemplateSizeSmall, models.VideoTemplateSizeMedium, models.VideoTemplateSizeLarge}},
	}, guidance, []string{"full_frame_media", "picture_in_picture", "placement", "bounded_size", "opacity", "frame", "fade", "subtle_scale", "duration_mode_full_source"})
}

func photoOverVideoTemplate() models.VideoTemplate {
	template := pipTemplate(
		"photo-over-video",
		models.ProjectTypeStory,
		"Photo over video",
		"Keep the video moving while a supporting photo appears above it.",
		"/videos/templates/photo-over-video.mp4",
		30,
		models.VisualStyleCinematic,
		true,
		"bold_social",
		"balanced",
		models.VideoTemplatePlacementBottomRight,
		models.VideoTemplateSizeMedium,
		models.VideoTemplateAnimationAuto,
		"Prefer one strong base video and one semantically related supporting photo. The base video must continue playing underneath the photo. Choose the supporting photo animation per overlay based on the image's narrative role and pacing.",
	)
	template.RendererCapabilities = append(template.RendererCapabilities, "scale", "move")
	return template
}

func splitTemplate(id string, projectType models.ProjectType, name, description, demoPath string, duration int, style models.VisualStyle, captions bool, captionStyle, sceneDetail, guidance string) models.VideoTemplate {
	return baseTemplate(id, projectType, name, description, demoPath, duration, style, captions, captionStyle, sceneDetail, models.VideoTemplateLayoutSplitScreen, models.VideoTemplatePlacementRight, models.VideoTemplateSizeHalf, models.VideoTemplateFrameNone, models.VideoTemplateAnimationNone, []string{"duration_seconds", "aspect_ratio", "visual_style", "captions_enabled", "caption_style", "scene_detail"}, []models.VideoTemplateMediaSlot{{ID: "secondary_panel", Role: "paired_media", MediaTypes: []models.MediaType{models.MediaTypePhoto, models.MediaTypeVideo}, Required: true, Presentation: models.VideoTemplateLayoutSplitScreen, AllowedPlacements: []models.VideoTemplatePlacement{models.VideoTemplatePlacementLeft, models.VideoTemplatePlacementRight}, AllowedSizes: []models.VideoTemplateSize{models.VideoTemplateSizeHalf}}}, guidance, []string{"full_frame_media", "split_screen", "placement", "bounded_size"})
}

func montageTemplate(id string, projectType models.ProjectType, name, description, demoPath string, duration int, style models.VisualStyle, captions bool, captionStyle, sceneDetail, guidance string) models.VideoTemplate {
	return baseTemplate(id, projectType, name, description, demoPath, duration, style, captions, captionStyle, sceneDetail, models.VideoTemplateLayoutMontage, models.VideoTemplatePlacementCenter, models.VideoTemplateSizeLarge, models.VideoTemplateFrameNone, models.VideoTemplateAnimationNone, []string{"duration_seconds", "visual_style", "captions_enabled", "caption_style", "scene_detail"}, []models.VideoTemplateMediaSlot{{ID: "montage_moments", Role: "rapid_sequence", MediaTypes: []models.MediaType{models.MediaTypePhoto, models.MediaTypeVideo}, Required: true, Presentation: models.VideoTemplateLayoutMontage}}, guidance, []string{"full_frame_media", "cut"})
}

func baseTemplate(id string, projectType models.ProjectType, name, description, demoPath string, duration int, style models.VisualStyle, captions bool, captionStyle, sceneDetail string, layout models.VideoTemplateLayout, placement models.VideoTemplatePlacement, size models.VideoTemplateSize, frame models.VideoTemplateFrame, animation models.VideoTemplateAnimation, editable []string, slots []models.VideoTemplateMediaSlot, guidance string, capabilities []string) models.VideoTemplate {
	return models.VideoTemplate{
		ID: id, Version: 1, Name: name, Description: description, DemoVideoPath: demoPath, SupportedProjectTypes: []models.ProjectType{projectType},
		Defaults: models.VideoTemplateConfiguration{
			AspectRatio: "9:16", DurationSeconds: duration, DurationMode: models.DurationModeFixed, VisualStyle: style, CaptionsEnabled: boolPtr(captions), CaptionStyle: captionStyle, SceneDetail: sceneDetail,
			Layout: layout, OverlayPlacement: placement, OverlaySize: size, OverlayOpacity: 1, OverlayFrame: frame, OverlayAnimation: animation,
		},
		EditableOptions: editable, MediaSlots: slots, PlannerGuidance: guidance, RendererCapabilities: capabilities,
	}
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
	if config.DurationMode != "" && (!contains(template.EditableOptions, "duration_mode") || (config.DurationMode == models.DurationModeFullSource && !contains(template.RendererCapabilities, "duration_mode_full_source"))) && config.DurationMode != template.Defaults.DurationMode {
		return fmt.Errorf("template does not support full length")
	}
	if config.RemoveFiller && config.DurationMode != models.DurationModeFullSource {
		return fmt.Errorf("remove filler words and pauses requires full length")
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
	config.DurationMode = models.NormalizeDurationMode(config.DurationMode)
	if config.DurationMode == models.DurationModeFixed && (config.DurationSeconds < 5 || config.DurationSeconds > 120) {
		return fmt.Errorf("template duration must be between 5 and 120 seconds")
	}
	if config.DurationMode == models.DurationModeAuto && config.DurationSeconds < 0 {
		return fmt.Errorf("template duration is invalid")
	}
	if config.DurationMode == models.DurationModeFullSource {
		if !contains(template.EditableOptions, "duration_mode") || !contains(template.RendererCapabilities, "duration_mode_full_source") {
			return fmt.Errorf("template does not support full length")
		}
		if config.DurationSeconds != 0 {
			return fmt.Errorf("full length must not set a fixed duration")
		}
	} else if config.RemoveFiller {
		return fmt.Errorf("remove filler words and pauses requires full length")
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
	if !validPlacement(config.OverlayPlacement) || !validSize(config.OverlaySize) || !validFrame(config.OverlayFrame) || !validConfigurationAnimation(template, config.OverlayAnimation) {
		return fmt.Errorf("template overlay configuration contains an unsupported value")
	}
	if (config.OverlayAnimation == models.VideoTemplateAnimationScale || config.OverlayAnimation == models.VideoTemplateAnimationMove) && !contains(template.RendererCapabilities, string(config.OverlayAnimation)) {
		return fmt.Errorf("template does not support overlay animation %q", config.OverlayAnimation)
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
	template, hasTemplate := Find(plan.TemplateID, plan.TemplateVersion, plan.ProjectType)
	for index := range plan.BRoll {
		overlay := &plan.BRoll[index]
		presentationChanged := false
		defaultPresentation := config.Layout
		if config.Layout == models.VideoTemplateLayoutMontage {
			defaultPresentation = models.VideoTemplateLayoutFullFrame
		}
		if overlay.Presentation == "" || (config.Layout == models.VideoTemplateLayoutMontage && overlay.Presentation == models.VideoTemplateLayoutMontage) || (hasTemplate && !planPresentationAllowedForTemplate(template, overlay.Presentation)) {
			overlay.Presentation = defaultPresentation
			presentationChanged = true
		}
		if overlay.Placement == "" || presentationChanged || (hasTemplate && !templateSupportsPlacement(template, overlay.Presentation, overlay.Placement)) {
			overlay.Placement = config.OverlayPlacement
		}
		if overlay.Size == "" || presentationChanged || (hasTemplate && !templateSupportsSize(template, overlay.Presentation, overlay.Size)) {
			overlay.Size = config.OverlaySize
		}
		if overlay.Opacity == 0 {
			overlay.Opacity = config.OverlayOpacity
		}
		if overlay.Frame == "" || presentationChanged {
			overlay.Frame = config.OverlayFrame
		}
		if overlay.Animation == "" || presentationChanged || (hasTemplate && overlay.Animation != models.VideoTemplateAnimationNone && !contains(template.RendererCapabilities, string(overlay.Animation))) {
			if config.OverlayAnimation == models.VideoTemplateAnimationAuto {
				overlay.Animation = models.VideoTemplateAnimationFade
			} else {
				overlay.Animation = config.OverlayAnimation
			}
		}
	}
}

func planPresentationAllowedForTemplate(template models.VideoTemplate, presentation models.VideoTemplateLayout) bool {
	if presentation == "" || presentation == template.Defaults.Layout || presentation == models.VideoTemplateLayoutFullFrame {
		return true
	}
	return presentation != models.VideoTemplateLayoutFullFrame && templateSupportsPresentation(template, presentation)
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
		if overlay.Animation != "" && overlay.Animation != models.VideoTemplateAnimationNone && !contains(template.RendererCapabilities, string(overlay.Animation)) {
			return fmt.Errorf("b-roll %d animation %q is not supported by template %q", index, overlay.Animation, template.ID)
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
		resolved.DurationMode = models.DurationModeFixed
	}
	if contains(template.EditableOptions, "duration_mode") && supplied.DurationMode != "" {
		mode := models.NormalizeDurationMode(supplied.DurationMode)
		if mode == models.DurationModeFullSource && contains(template.RendererCapabilities, "duration_mode_full_source") {
			resolved.DurationMode = mode
			resolved.DurationSeconds = 0
			resolved.RemoveFiller = supplied.RemoveFiller
		} else if mode == models.DurationModeAuto {
			resolved.DurationMode = mode
			resolved.DurationSeconds = 0
			resolved.RemoveFiller = false
		} else if mode == models.DurationModeFixed && resolved.DurationSeconds > 0 {
			resolved.DurationMode = mode
			resolved.RemoveFiller = false
		}
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
	case models.VideoTemplateAnimationNone, models.VideoTemplateAnimationFade, models.VideoTemplateAnimationSubtleScale, models.VideoTemplateAnimationScale, models.VideoTemplateAnimationMove:
		return true
	default:
		return false
	}
}

func validConfigurationAnimation(template models.VideoTemplate, value models.VideoTemplateAnimation) bool {
	if value == models.VideoTemplateAnimationAuto {
		return template.ID == "photo-over-video"
	}
	return validAnimation(value)
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
