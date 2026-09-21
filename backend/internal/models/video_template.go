package models

// VideoTemplateLayout describes the bounded visual recipe selected by a user.
// It is intentionally an editorial/layout enum, not renderer instructions.
type VideoTemplateLayout string

const (
	VideoTemplateLayoutFullFrame        VideoTemplateLayout = "full_frame"
	VideoTemplateLayoutPictureInPicture VideoTemplateLayout = "picture_in_picture"
	VideoTemplateLayoutSplitScreen      VideoTemplateLayout = "split_screen"
	VideoTemplateLayoutMontage          VideoTemplateLayout = "montage"
)

type VideoTemplatePlacement string

const (
	VideoTemplatePlacementTopLeft     VideoTemplatePlacement = "top_left"
	VideoTemplatePlacementTopRight    VideoTemplatePlacement = "top_right"
	VideoTemplatePlacementBottomLeft  VideoTemplatePlacement = "bottom_left"
	VideoTemplatePlacementBottomRight VideoTemplatePlacement = "bottom_right"
	VideoTemplatePlacementCenter      VideoTemplatePlacement = "center"
	VideoTemplatePlacementLeft        VideoTemplatePlacement = "left"
	VideoTemplatePlacementRight       VideoTemplatePlacement = "right"
)

type VideoTemplateSize string

const (
	VideoTemplateSizeSmall  VideoTemplateSize = "small"
	VideoTemplateSizeMedium VideoTemplateSize = "medium"
	VideoTemplateSizeLarge  VideoTemplateSize = "large"
	VideoTemplateSizeHalf   VideoTemplateSize = "half"
)

type VideoTemplateFrame string

const (
	VideoTemplateFrameNone   VideoTemplateFrame = "none"
	VideoTemplateFrameSubtle VideoTemplateFrame = "subtle"
	VideoTemplateFrameBold   VideoTemplateFrame = "bold"
)

type VideoTemplateAnimation string

const (
	VideoTemplateAnimationAuto        VideoTemplateAnimation = "auto"
	VideoTemplateAnimationNone        VideoTemplateAnimation = "none"
	VideoTemplateAnimationFade        VideoTemplateAnimation = "fade"
	VideoTemplateAnimationSubtleScale VideoTemplateAnimation = "subtle_scale"
	VideoTemplateAnimationScale       VideoTemplateAnimation = "scale"
	VideoTemplateAnimationMove        VideoTemplateAnimation = "move"
)

type DurationMode string

const (
	DurationModeAuto       DurationMode = "auto"
	DurationModeFixed      DurationMode = "fixed"
	DurationModeFullSource DurationMode = "full_source"
)

// VideoTemplateConfiguration is the resolved, user-editable configuration
// applied on top of a catalog template's defaults.
type VideoTemplateConfiguration struct {
	AspectRatio      string                 `json:"aspect_ratio,omitempty"`
	DurationSeconds  int                    `json:"duration_seconds,omitempty"`
	DurationMode     DurationMode           `json:"duration_mode,omitempty"`
	RemoveFiller     bool                   `json:"remove_filler_and_pauses,omitempty"`
	VisualStyle      VisualStyle            `json:"visual_style,omitempty"`
	CaptionsEnabled  *bool                  `json:"captions_enabled,omitempty"`
	CaptionStyle     string                 `json:"caption_style,omitempty"`
	SceneDetail      string                 `json:"scene_detail,omitempty"`
	Layout           VideoTemplateLayout    `json:"layout,omitempty"`
	OverlayPlacement VideoTemplatePlacement `json:"overlay_placement,omitempty"`
	OverlaySize      VideoTemplateSize      `json:"overlay_size,omitempty"`
	OverlayOpacity   float64                `json:"overlay_opacity,omitempty"`
	OverlayFrame     VideoTemplateFrame     `json:"overlay_frame,omitempty"`
	OverlayAnimation VideoTemplateAnimation `json:"overlay_animation,omitempty"`
}

type VideoTemplateMediaSlot struct {
	ID                string                   `json:"id"`
	Role              string                   `json:"role"`
	MediaTypes        []MediaType              `json:"media_types"`
	Required          bool                     `json:"required"`
	Presentation      VideoTemplateLayout      `json:"presentation"`
	AllowedPlacements []VideoTemplatePlacement `json:"allowed_placements,omitempty"`
	AllowedSizes      []VideoTemplateSize      `json:"allowed_sizes,omitempty"`
}

// VideoTemplate is backend-owned catalog metadata. DemoVideoPath points to a
// reviewed static MP4 and is never a runtime generation request.
type VideoTemplate struct {
	ID                    string                     `json:"template_id"`
	Version               int                        `json:"version"`
	Name                  string                     `json:"name"`
	Description           string                     `json:"description"`
	DemoVideoPath         string                     `json:"demo_video_path"`
	SupportedProjectTypes []ProjectType              `json:"supported_project_types"`
	Defaults              VideoTemplateConfiguration `json:"defaults"`
	EditableOptions       []string                   `json:"editable_options"`
	MediaSlots            []VideoTemplateMediaSlot   `json:"media_slots"`
	PlannerGuidance       string                     `json:"planner_guidance"`
	RendererCapabilities  []string                   `json:"renderer_capabilities"`
}
