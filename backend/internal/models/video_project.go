package models

import (
	"encoding/json"
	"strings"
	"time"
)

// ProjectType identifies the editorial intent of a VideoProject. The media,
// analysis, edit-plan, and render pipelines remain shared across all types.
type ProjectType string

const (
	ProjectTypeStory ProjectType = "story"
	ProjectTypePromo ProjectType = "promo"
	ProjectTypeReel  ProjectType = "reel"
)

func NormalizeProjectType(value ProjectType) (ProjectType, bool) {
	switch ProjectType(strings.ToLower(strings.TrimSpace(string(value)))) {
	case "", ProjectTypeStory:
		return ProjectTypeStory, true
	case ProjectTypePromo:
		return ProjectTypePromo, true
	case ProjectTypeReel:
		return ProjectTypeReel, true
	default:
		return "", false
	}
}

func NormalizeVideoProjectConfiguration(projectType ProjectType, configuration VideoProjectConfiguration) VideoProjectConfiguration {
	switch projectType {
	case ProjectTypeReel:
		configuration.Reel = NormalizeReelProjectConfiguration(configuration.Reel)
		configuration.Composition.AspectRatio = "9:16"
	case ProjectTypePromo:
		if strings.TrimSpace(configuration.Composition.AspectRatio) == "" {
			configuration.Composition.AspectRatio = "16:9"
		}
	default:
		if strings.TrimSpace(configuration.Composition.AspectRatio) == "" {
			configuration.Composition.AspectRatio = "9:16"
		}
	}
	return configuration
}

func NormalizeReelProjectConfiguration(configuration *ReelProjectConfiguration) *ReelProjectConfiguration {
	if configuration == nil {
		configuration = &ReelProjectConfiguration{}
	}
	normalized := *configuration
	normalized.SocialPlatform = normalizeReelPlatform(normalized.SocialPlatform)
	normalized.TargetDurationSecs = normalizeReelDuration(normalized.TargetDurationSecs)
	normalized.Style = normalizeReelStyle(normalized.Style)
	normalized.HookPreference = strings.TrimSpace(normalized.HookPreference)
	normalized.CaptionStyle = normalizeReelCaptionStyle(normalized.CaptionStyle)
	normalized.AspectRatio = "9:16"
	return &normalized
}

func normalizeReelPlatform(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	normalized = strings.ReplaceAll(normalized, "-", "_")
	normalized = strings.ReplaceAll(normalized, " ", "_")
	switch normalized {
	case "instagram", "instagram_reels", "reels", "ig", "ig_reels":
		return "instagram"
	case "tiktok", "tik_tok":
		return "tiktok"
	case "youtube_shorts", "youtube", "shorts", "yt_shorts":
		return "youtube_shorts"
	case "other", "general", "vertical":
		return "other"
	default:
		return "instagram"
	}
}

func normalizeReelDuration(seconds int) int {
	switch seconds {
	case 15, 30, 60:
		return seconds
	default:
		return 0
	}
}

func normalizeReelStyle(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	normalized = strings.ReplaceAll(normalized, "_", "-")
	normalized = strings.ReplaceAll(normalized, " ", "-")
	switch normalized {
	case "dynamic", "cinematic", "clean", "talking-head", "auto":
		return normalized
	default:
		return "auto"
	}
}

func normalizeReelCaptionStyle(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	normalized = strings.ReplaceAll(normalized, "-", "_")
	normalized = strings.ReplaceAll(normalized, " ", "_")
	switch normalized {
	case "bold_social", "cinematic_subtitle", "clean_minimal", "playful_pop":
		return normalized
	default:
		return "bold_social"
	}
}

type StoryProjectConfiguration struct {
	StorytellingStyle    string `json:"storytelling_style,omitempty"`
	EmotionalTone        string `json:"emotional_tone,omitempty"`
	MomentSelection      string `json:"moment_selection,omitempty"`
	NarrativePreferences string `json:"narrative_preferences,omitempty"`
}

type PromoProjectConfiguration struct {
	PromotionCategory   string             `json:"promotion_category,omitempty"`
	PromotionGoal       string             `json:"promotion_goal,omitempty"`
	ProductInformation  string             `json:"product_information,omitempty"`
	CallToAction        string             `json:"call_to_action,omitempty"`
	CTA                 *CTAConfiguration  `json:"cta,omitempty"`
	Hook                string             `json:"hook,omitempty"`
	TargetAudience      string             `json:"target_audience,omitempty"`
	DesiredDurationSecs int                `json:"desired_duration_seconds,omitempty"`
	Brand               BrandConfiguration `json:"brand,omitempty"`
}

type ReelProjectConfiguration struct {
	SocialPlatform     string `json:"social_platform,omitempty"`
	TargetDurationSecs int    `json:"target_duration_seconds,omitempty"`
	Style              string `json:"style,omitempty"`
	HookPreference     string `json:"hook_preference,omitempty"`
	CaptionStyle       string `json:"caption_style,omitempty"`
	AspectRatio        string `json:"aspect_ratio,omitempty"`
}

type BrandConfiguration struct {
	Name           string `json:"name,omitempty"`
	PrimaryColor   string `json:"primary_color,omitempty"`
	SecondaryColor string `json:"secondary_color,omitempty"`
	LogoAssetID    string `json:"logo_asset_id,omitempty"`
	Voice          string `json:"voice,omitempty"`
	FontPreference string `json:"font_preference,omitempty"`
}

type CTAConfiguration struct {
	Text      string `json:"text,omitempty"`
	Style     string `json:"style,omitempty"`
	URL       string `json:"url,omitempty"`
	Placement string `json:"placement,omitempty"`
}

type CompositionConfiguration struct {
	AspectRatio string `json:"aspect_ratio,omitempty"`
	FPS         int    `json:"fps,omitempty"`
	Preset      string `json:"preset,omitempty"`
}

type RenderConfiguration struct {
	OutputPreset string `json:"output_preset,omitempty"`
	Quality      string `json:"quality,omitempty"`
}

type VideoProjectConfiguration struct {
	AIInstructions        string                     `json:"ai_instructions,omitempty"`
	Story                 *StoryProjectConfiguration `json:"story,omitempty"`
	Promo                 *PromoProjectConfiguration `json:"promo,omitempty"`
	Reel                  *ReelProjectConfiguration  `json:"reel,omitempty"`
	Composition           CompositionConfiguration   `json:"composition,omitempty"`
	Render                RenderConfiguration        `json:"render,omitempty"`
	TemplateID            string                     `json:"template_id,omitempty"`
	TemplateVersion       int                        `json:"template_version,omitempty"`
	TemplateConfiguration VideoTemplateConfiguration `json:"template_configuration,omitempty"`
}

// VideoProject is the shared durable project record for every creation mode.
// MediaIDs are the existing asset relationship and StoryPlan remains the
// versioned editorial artifact referenced by the render pipeline.
type VideoProject struct {
	ID                 string                    `json:"id"`
	UserID             string                    `json:"user_id"`
	Type               ProjectType               `json:"type"`
	Title              string                    `json:"title"`
	Context            string                    `json:"context"`
	MediaIDs           []string                  `json:"media_ids"`
	Configuration      VideoProjectConfiguration `json:"configuration,omitempty"`
	CreditOperationKey string                    `json:"credit_operation_key,omitempty"`
	Status             ProjectStatus             `json:"status"`
	CreatedAt          time.Time                 `json:"created_at"`
	UpdatedAt          time.Time                 `json:"updated_at"`
}

// StoryProject is retained as a source-compatible name for existing callers.
type StoryProject = VideoProject

func (project VideoProject) Normalized() VideoProject {
	normalized := project
	projectType, valid := NormalizeProjectType(normalized.Type)
	if !valid {
		normalized.Type = ProjectTypeStory
	} else {
		normalized.Type = projectType
	}
	normalized.Configuration = NormalizeVideoProjectConfiguration(normalized.Type, normalized.Configuration)
	return normalized
}

func (project VideoProject) MarshalJSON() ([]byte, error) {
	type alias VideoProject
	normalized := project.Normalized()
	return json.Marshal(alias(normalized))
}

func (project *VideoProject) UnmarshalJSON(data []byte) error {
	type alias VideoProject
	var decoded alias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*project = VideoProject(decoded).Normalized()
	return nil
}
