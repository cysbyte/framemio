package storyvalidation

import (
	"context"
	"fmt"
	"math"
	"strings"
	"unicode/utf8"

	"github.com/framemio/backend/internal/models"
	"github.com/framemio/backend/internal/storage"
	"github.com/framemio/backend/internal/store"
)

type Service interface {
	Validate(ctx context.Context, userID string, plan models.StoryPlan) error
}

type Validator struct {
	Store         store.Store
	ObjectStorage storage.ObjectStorage
	Config        Config
}

var _ Service = (*Validator)(nil)

func New(store store.Store, objectStorage storage.ObjectStorage, config Config) *Validator {
	if config.SchemaVersion == "" {
		config = DefaultConfig()
	}
	if config.SupportedAspectRatios == nil {
		config.SupportedAspectRatios = DefaultConfig().SupportedAspectRatios
	}
	if config.AllowedCaptionPresets == nil {
		config.AllowedCaptionPresets = DefaultConfig().AllowedCaptionPresets
	}
	if config.AllowedCaptionFonts == nil {
		config.AllowedCaptionFonts = DefaultConfig().AllowedCaptionFonts
	}
	if config.AllowedCaptionPositions == nil {
		config.AllowedCaptionPositions = DefaultConfig().AllowedCaptionPositions
	}
	if config.AllowedTextOverlayTypes == nil {
		config.AllowedTextOverlayTypes = DefaultConfig().AllowedTextOverlayTypes
	}
	if config.AllowedTypographyStyles == nil {
		config.AllowedTypographyStyles = DefaultConfig().AllowedTypographyStyles
	}
	if config.AllowedTextPlacements == nil {
		config.AllowedTextPlacements = DefaultConfig().AllowedTextPlacements
	}
	if config.AllowedTextAnimations == nil {
		config.AllowedTextAnimations = DefaultConfig().AllowedTextAnimations
	}
	if config.AllowedTextSources == nil {
		config.AllowedTextSources = DefaultConfig().AllowedTextSources
	}
	if config.MaxTitleLength == 0 {
		config.MaxTitleLength = DefaultConfig().MaxTitleLength
	}
	if config.MaxEndingLength == 0 {
		config.MaxEndingLength = DefaultConfig().MaxEndingLength
	}
	if config.MaxUserInstructionsLength == 0 {
		config.MaxUserInstructionsLength = DefaultConfig().MaxUserInstructionsLength
	}
	if config.MaxCaptionLength == 0 {
		config.MaxCaptionLength = DefaultConfig().MaxCaptionLength
	}
	if config.MaxTypographyTitleLength == 0 {
		config.MaxTypographyTitleLength = DefaultConfig().MaxTypographyTitleLength
	}
	if config.MaxTypographySubtitleLength == 0 {
		config.MaxTypographySubtitleLength = DefaultConfig().MaxTypographySubtitleLength
	}
	if config.MaxTypographyReasonLength == 0 {
		config.MaxTypographyReasonLength = DefaultConfig().MaxTypographyReasonLength
	}
	if config.MaxTypographyLines == 0 {
		config.MaxTypographyLines = DefaultConfig().MaxTypographyLines
	}
	if config.MaxNarrativeLength == 0 {
		config.MaxNarrativeLength = DefaultConfig().MaxNarrativeLength
	}
	if config.MaxReasonLength == 0 {
		config.MaxReasonLength = DefaultConfig().MaxReasonLength
	}
	if config.MaxCropIntentLength == 0 {
		config.MaxCropIntentLength = DefaultConfig().MaxCropIntentLength
	}
	if config.MaxTransitionLength == 0 {
		config.MaxTransitionLength = DefaultConfig().MaxTransitionLength
	}
	if config.MaxBRollReasonLength == 0 {
		config.MaxBRollReasonLength = DefaultConfig().MaxBRollReasonLength
	}
	if config.MaxBRollAnchorTypeLength == 0 {
		config.MaxBRollAnchorTypeLength = DefaultConfig().MaxBRollAnchorTypeLength
	}
	if config.MaxPresetLength == 0 {
		config.MaxPresetLength = DefaultConfig().MaxPresetLength
	}
	if config.MaxAudioSourceLength == 0 {
		config.MaxAudioSourceLength = DefaultConfig().MaxAudioSourceLength
	}
	if config.MaxCaptionFontSize == 0 {
		config.MaxCaptionFontSize = DefaultConfig().MaxCaptionFontSize
	}
	if config.MaxCaptionStrokeWidth == 0 {
		config.MaxCaptionStrokeWidth = DefaultConfig().MaxCaptionStrokeWidth
	}
	if config.MaxCaptionBottomOffset == 0 {
		config.MaxCaptionBottomOffset = DefaultConfig().MaxCaptionBottomOffset
	}
	if config.MaxCaptionLines == 0 {
		config.MaxCaptionLines = DefaultConfig().MaxCaptionLines
	}
	if config.MaxCaptionLineCharacters == 0 {
		config.MaxCaptionLineCharacters = DefaultConfig().MaxCaptionLineCharacters
	}
	return &Validator{Store: store, ObjectStorage: objectStorage, Config: config}
}

func (v *Validator) Validate(ctx context.Context, userID string, plan models.StoryPlan) error {
	if v == nil || v.Store == nil || v.ObjectStorage == nil {
		return fmt.Errorf("story plan validator is not configured")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	problems := &ValidationError{}
	config := v.config()

	if strings.TrimSpace(plan.SchemaVersion) != config.SchemaVersion {
		problems.Add("schema_version", "invalid_schema", "story plan schema version is unsupported")
	}
	if strings.TrimSpace(plan.ID) == "" {
		problems.Add("id", "required", "story plan id is required")
	}
	if strings.TrimSpace(plan.ProjectID) == "" {
		problems.Add("project_id", "required", "story plan project id is required")
	}
	if plan.Version <= 0 {
		problems.Add("version", "invalid_version", "story plan version must be positive")
	}
	if strings.TrimSpace(plan.TitleText) == "" {
		problems.Add("title_text", "required", "title text is required")
	} else if len(plan.TitleText) > config.MaxTitleLength {
		problems.Add("title_text", "too_long", fmt.Sprintf("title text must be at most %d characters", config.MaxTitleLength))
	}
	if strings.TrimSpace(plan.EndingText) == "" {
		problems.Add("ending_text", "required", "ending text is required")
	} else if len(plan.EndingText) > config.MaxEndingLength {
		problems.Add("ending_text", "too_long", fmt.Sprintf("ending text must be at most %d characters", config.MaxEndingLength))
	}
	if len(plan.UserInstructions) > config.MaxUserInstructionsLength {
		problems.Add("user_instructions", "too_long", fmt.Sprintf("user instructions must be at most %d characters", config.MaxUserInstructionsLength))
	}
	if strings.TrimSpace(plan.Output.AspectRatio) == "" {
		problems.Add("output.aspect_ratio", "required", "output aspect ratio is required")
	} else if !v.supportedAspectRatio(plan.Output.AspectRatio) {
		problems.Add("output.aspect_ratio", "unsupported_aspect_ratio", "output aspect ratio is unsupported")
	}
	if strings.TrimSpace(plan.Output.Preset) == "" {
		problems.Add("output.preset", "required", "output preset is required")
	} else if len(plan.Output.Preset) > config.MaxPresetLength {
		problems.Add("output.preset", "too_long", fmt.Sprintf("output preset must be at most %d characters", config.MaxPresetLength))
	}
	if plan.Output.MaxDurationS <= 0 {
		problems.Add("output.max_duration_seconds", "invalid_duration", "output max duration must be positive")
	}
	if plan.RequestedDurationMS <= 0 {
		problems.Add("requested_duration_ms", "invalid_duration", "requested duration must be positive")
	}
	if len(plan.Moments) == 0 {
		problems.Add("moments", "required", "story plan requires at least one moment")
	}

	projectMedia := make(map[string]struct{})
	if strings.TrimSpace(plan.ProjectID) != "" {
		project, err := v.Store.GetProject(ctx, userID, plan.ProjectID)
		if err != nil {
			problems.Add("project_id", "missing_project", "story plan project does not exist")
		} else {
			projectMedia = make(map[string]struct{}, len(project.MediaIDs))
			for _, mediaID := range project.MediaIDs {
				projectMedia[mediaID] = struct{}{}
			}
		}
	}

	momentIDs := make(map[string]struct{}, len(plan.Moments))
	momentRefs := make(map[string]struct{}, len(plan.Moments))
	sumDurationMS := 0
	for index, moment := range plan.Moments {
		field := fmt.Sprintf("moments[%d]", index)
		assetID := strings.TrimSpace(firstNonEmpty(moment.AssetID, moment.MediaID))
		if assetID == "" {
			problems.Add(field+".asset_id", "required", "moment asset id is required")
			continue
		}
		if len(projectMedia) > 0 {
			if _, belongs := projectMedia[assetID]; !belongs {
				problems.Add(field+".asset_id", "not_in_project", "story plan references media outside the project")
			}
		}
		referenceKey := momentReferenceKey(assetID, moment)
		if _, exists := momentRefs[referenceKey]; exists {
			problems.Add(field+".asset_id", "duplicate_reference", "story plan contains a duplicate moment reference")
		} else {
			momentRefs[referenceKey] = struct{}{}
		}
		if strings.TrimSpace(moment.MomentID) != "" {
			if _, exists := momentIDs[moment.MomentID]; exists {
				problems.Add(field+".moment_id", "duplicate_reference", "moment ids must be unique")
			} else {
				momentIDs[moment.MomentID] = struct{}{}
			}
		}
		if moment.DurationMS <= 0 {
			problems.Add(field+".duration_ms", "invalid_duration", "moment duration must be positive")
		}
		if len(moment.CropIntent) > config.MaxCropIntentLength {
			problems.Add(field+".crop_intent", "too_long", fmt.Sprintf("crop intent must be at most %d characters", config.MaxCropIntentLength))
		}
		if len(moment.Transition) > config.MaxTransitionLength {
			problems.Add(field+".transition", "too_long", fmt.Sprintf("transition must be at most %d characters", config.MaxTransitionLength))
		}
		if len(moment.Narrative) > config.MaxNarrativeLength {
			problems.Add(field+".narrative", "too_long", fmt.Sprintf("narrative must be at most %d characters", config.MaxNarrativeLength))
		}
		if len(moment.Caption) > config.MaxCaptionLength {
			problems.Add(field+".caption", "too_long", fmt.Sprintf("caption must be at most %d characters", config.MaxCaptionLength))
		}
		if len(moment.Reason) > config.MaxReasonLength {
			problems.Add(field+".reason", "too_long", fmt.Sprintf("reason must be at most %d characters", config.MaxReasonLength))
		}
		if moment.Typography != nil {
			validateTypography(field+".typography", *moment.Typography, moment.DurationMS, config, problems)
		}
		media, err := v.Store.GetMedia(ctx, userID, assetID)
		if err != nil {
			problems.Add(field+".asset_id", "missing_asset", "story plan references a media asset that does not exist")
			continue
		}
		if moment.MediaType != "" && moment.MediaType != media.Type {
			problems.Add(field+".media_type", "mismatch", "moment media type does not match the referenced asset")
		}
		switch media.Type {
		case models.MediaTypeVideo:
			if media.DurationSeconds == nil || *media.DurationSeconds <= 0 {
				problems.Add(field+".duration_ms", "invalid_source_duration", "video asset is missing a valid source duration")
			} else {
				sourceDurationMicros := int64(math.Round(*media.DurationSeconds * 1_000_000))
				validRange := moment.StartMicros >= 0 && moment.EndMicros > moment.StartMicros && moment.EndMicros <= sourceDurationMicros
				if !validRange {
					problems.Add(field+".timestamps", "out_of_range", "video moment timestamps must be within the source duration")
				} else if !durationMatchesVideoRange(moment.DurationMS, moment.StartMicros, moment.EndMicros) {
					problems.Add(field+".duration_ms", "range_mismatch", "video moment duration must match the selected source range")
				}
			}
			if strings.TrimSpace(moment.VideoAnalysisVersion) == "" {
				problems.Add(field+".video_analysis_version", "required", "video analysis version is required for video moments")
			} else if _, err := v.Store.GetVideoAnalysis(ctx, userID, media.ID, moment.VideoAnalysisVersion); err != nil {
				problems.Add(field+".video_analysis_version", "missing_analysis", "video analysis version does not exist for the referenced asset")
			}
		case models.MediaTypePhoto, models.MediaTypeLivePhoto:
			if moment.StartMicros != 0 || moment.EndMicros != 0 {
				problems.Add(field+".timestamps", "invalid_timestamps", "photo moments must not define timestamps")
			}
		default:
			problems.Add(field+".media_type", "unsupported_media_type", "story plan references an unsupported media type")
		}
		if strings.TrimSpace(media.S3Key) == "" {
			problems.Add(field+".s3_key", "missing_object", "referenced media is missing its S3 object key")
		} else if exists, err := v.ObjectStorage.ObjectExists(ctx, media.S3Key); err != nil {
			problems.Add(field+".s3_key", "missing_object", "unable to verify referenced media object")
		} else if !exists {
			problems.Add(field+".s3_key", "missing_object", "referenced media object does not exist")
		}
		sumDurationMS += moment.DurationMS
	}

	if plan.TotalDurationMS > 0 && sumDurationMS != plan.TotalDurationMS {
		problems.Add("total_duration_ms", "mismatch", "total duration must equal the sum of moment durations")
	}
	if plan.RequestedDurationMS > 0 && sumDurationMS > plan.RequestedDurationMS {
		problems.Add("total_duration_ms", "out_of_range", "total duration exceeds the requested duration")
	}
	validateBRoll(ctx, v, userID, plan, projectMedia, momentIDs, sumDurationMS, problems)

	if len(plan.Captions) > 0 {
		seenCaptions := make(map[string]struct{}, len(plan.Captions))
		for index, caption := range plan.Captions {
			field := fmt.Sprintf("captions[%d]", index)
			if strings.TrimSpace(caption.MomentID) == "" {
				problems.Add(field+".moment_id", "required", "caption moment id is required")
			} else if _, exists := momentIDs[caption.MomentID]; !exists {
				problems.Add(field+".moment_id", "unknown_moment", "caption references an unknown moment")
			}
			captionKey := fmt.Sprintf("%s:%d:%d:%s", strings.TrimSpace(caption.MomentID), caption.StartMS, caption.EndMS, strings.TrimSpace(caption.Text))
			if _, duplicate := seenCaptions[captionKey]; duplicate {
				problems.Add(field, "duplicate_reference", "duplicate caption timing and text")
			} else {
				seenCaptions[captionKey] = struct{}{}
			}
			if strings.TrimSpace(caption.Text) == "" {
				problems.Add(field+".text", "required", "caption text is required")
			} else if len(caption.Text) > config.MaxCaptionLength {
				problems.Add(field+".text", "too_long", fmt.Sprintf("caption text must be at most %d characters", config.MaxCaptionLength))
			}
			if caption.StartMS < 0 || caption.EndMS <= caption.StartMS {
				problems.Add(field+".timing", "invalid_timing", "caption timing must be increasing")
			}
		}
	}

	if plan.CaptionStyle != nil {
		style := plan.CaptionStyle
		if strings.TrimSpace(style.Preset) != "" && !v.allowedCaptionPreset(style.Preset) {
			problems.Add("caption_style.preset", "unsupported_caption_preset", "caption style preset is unsupported")
		}
		if strings.TrimSpace(style.FontFamily) != "" && !v.allowedCaptionFont(style.FontFamily) {
			problems.Add("caption_style.font_family", "unsupported_caption_font", "caption font is unsupported")
		}
		if style.FontSize < 0 || style.FontSize > config.MaxCaptionFontSize {
			problems.Add("caption_style.font_size", "out_of_range", fmt.Sprintf("caption font size must be between 0 and %d", config.MaxCaptionFontSize))
		}
		if strings.TrimSpace(style.TextColor) != "" && !isHexColor(style.TextColor) {
			problems.Add("caption_style.text_color", "invalid_color", "caption text color must be a #RRGGBB color")
		}
		if strings.TrimSpace(style.StrokeColor) != "" && !isHexColor(style.StrokeColor) {
			problems.Add("caption_style.stroke_color", "invalid_color", "caption stroke color must be a #RRGGBB color")
		}
		if style.StrokeWidth < 0 || style.StrokeWidth > config.MaxCaptionStrokeWidth {
			problems.Add("caption_style.stroke_width", "out_of_range", fmt.Sprintf("caption stroke width must be between 0 and %d", config.MaxCaptionStrokeWidth))
		}
		if strings.TrimSpace(style.BackgroundColor) != "" && !isHexColor(style.BackgroundColor) {
			problems.Add("caption_style.background_color", "invalid_color", "caption background color must be a #RRGGBB color")
		}
		if style.BackgroundOpacity < 0 || style.BackgroundOpacity > 1 {
			problems.Add("caption_style.background_opacity", "out_of_range", "caption background opacity must be between 0 and 1")
		}
		if strings.TrimSpace(style.Position) != "" && !v.allowedCaptionPosition(style.Position) {
			problems.Add("caption_style.position", "unsupported_caption_position", "caption position is unsupported")
		}
		if style.BottomOffset < 0 || style.BottomOffset > config.MaxCaptionBottomOffset {
			problems.Add("caption_style.bottom_offset", "out_of_range", fmt.Sprintf("caption bottom offset must be between 0 and %d", config.MaxCaptionBottomOffset))
		}
		if style.MaxLines < 0 || style.MaxLines > config.MaxCaptionLines {
			problems.Add("caption_style.max_lines", "out_of_range", fmt.Sprintf("caption max lines must be between 0 and %d", config.MaxCaptionLines))
		}
		if style.MaxCharactersPerLine < 0 || style.MaxCharactersPerLine > config.MaxCaptionLineCharacters {
			problems.Add("caption_style.max_characters_per_line", "out_of_range", fmt.Sprintf("caption line length must be between 0 and %d", config.MaxCaptionLineCharacters))
		}
	}

	if plan.Audio != nil {
		if source := strings.TrimSpace(plan.Audio.Source); source != "" && !strings.EqualFold(source, "original") && !strings.EqualFold(source, "none") {
			problems.Add("audio.source", "unsupported_audio_source", "audio source is unsupported in V1")
		}
		if plan.Audio.OriginalVolume < 0 || plan.Audio.OriginalVolume > 1 {
			problems.Add("audio.original_volume", "out_of_range", "original volume must be between 0 and 1")
		}
	}

	if problems.HasErrors() {
		return problems
	}
	return nil
}

func (v *Validator) config() Config {
	if v == nil {
		return DefaultConfig()
	}
	config := v.Config
	defaults := DefaultConfig()
	if config.SchemaVersion == "" {
		config.SchemaVersion = defaults.SchemaVersion
	}
	if config.SupportedAspectRatios == nil {
		config.SupportedAspectRatios = defaults.SupportedAspectRatios
	}
	if config.AllowedCaptionPresets == nil {
		config.AllowedCaptionPresets = defaults.AllowedCaptionPresets
	}
	if config.AllowedCaptionFonts == nil {
		config.AllowedCaptionFonts = defaults.AllowedCaptionFonts
	}
	if config.AllowedCaptionPositions == nil {
		config.AllowedCaptionPositions = defaults.AllowedCaptionPositions
	}
	if config.AllowedTextOverlayTypes == nil {
		config.AllowedTextOverlayTypes = defaults.AllowedTextOverlayTypes
	}
	if config.AllowedTypographyStyles == nil {
		config.AllowedTypographyStyles = defaults.AllowedTypographyStyles
	}
	if config.AllowedTextPlacements == nil {
		config.AllowedTextPlacements = defaults.AllowedTextPlacements
	}
	if config.AllowedTextAnimations == nil {
		config.AllowedTextAnimations = defaults.AllowedTextAnimations
	}
	if config.AllowedTextSources == nil {
		config.AllowedTextSources = defaults.AllowedTextSources
	}
	if config.MaxTitleLength == 0 {
		config.MaxTitleLength = defaults.MaxTitleLength
	}
	if config.MaxEndingLength == 0 {
		config.MaxEndingLength = defaults.MaxEndingLength
	}
	if config.MaxUserInstructionsLength == 0 {
		config.MaxUserInstructionsLength = defaults.MaxUserInstructionsLength
	}
	if config.MaxCaptionLength == 0 {
		config.MaxCaptionLength = defaults.MaxCaptionLength
	}
	if config.MaxTypographyTitleLength == 0 {
		config.MaxTypographyTitleLength = defaults.MaxTypographyTitleLength
	}
	if config.MaxTypographySubtitleLength == 0 {
		config.MaxTypographySubtitleLength = defaults.MaxTypographySubtitleLength
	}
	if config.MaxTypographyReasonLength == 0 {
		config.MaxTypographyReasonLength = defaults.MaxTypographyReasonLength
	}
	if config.MaxTypographyLines == 0 {
		config.MaxTypographyLines = defaults.MaxTypographyLines
	}
	if config.MaxNarrativeLength == 0 {
		config.MaxNarrativeLength = defaults.MaxNarrativeLength
	}
	if config.MaxReasonLength == 0 {
		config.MaxReasonLength = defaults.MaxReasonLength
	}
	if config.MaxCropIntentLength == 0 {
		config.MaxCropIntentLength = defaults.MaxCropIntentLength
	}
	if config.MaxTransitionLength == 0 {
		config.MaxTransitionLength = defaults.MaxTransitionLength
	}
	if config.MaxBRollReasonLength == 0 {
		config.MaxBRollReasonLength = defaults.MaxBRollReasonLength
	}
	if config.MaxBRollAnchorTypeLength == 0 {
		config.MaxBRollAnchorTypeLength = defaults.MaxBRollAnchorTypeLength
	}
	if config.MaxPresetLength == 0 {
		config.MaxPresetLength = defaults.MaxPresetLength
	}
	if config.MaxAudioSourceLength == 0 {
		config.MaxAudioSourceLength = defaults.MaxAudioSourceLength
	}
	if config.MaxCaptionFontSize == 0 {
		config.MaxCaptionFontSize = defaults.MaxCaptionFontSize
	}
	if config.MaxCaptionStrokeWidth == 0 {
		config.MaxCaptionStrokeWidth = defaults.MaxCaptionStrokeWidth
	}
	if config.MaxCaptionBottomOffset == 0 {
		config.MaxCaptionBottomOffset = defaults.MaxCaptionBottomOffset
	}
	if config.MaxCaptionLines == 0 {
		config.MaxCaptionLines = defaults.MaxCaptionLines
	}
	if config.MaxCaptionLineCharacters == 0 {
		config.MaxCaptionLineCharacters = defaults.MaxCaptionLineCharacters
	}
	return config
}

func (v *Validator) supportedAspectRatio(aspectRatio string) bool {
	_, ok := v.config().SupportedAspectRatios[strings.TrimSpace(aspectRatio)]
	return ok
}

func (v *Validator) allowedCaptionPreset(preset string) bool {
	_, ok := v.config().AllowedCaptionPresets[strings.ToLower(strings.TrimSpace(preset))]
	return ok
}

func (v *Validator) allowedCaptionFont(font string) bool {
	_, ok := v.config().AllowedCaptionFonts[strings.ToLower(strings.TrimSpace(font))]
	return ok
}

func (v *Validator) allowedCaptionPosition(position string) bool {
	_, ok := v.config().AllowedCaptionPositions[strings.ToLower(strings.TrimSpace(position))]
	return ok
}

func validateTypography(field string, overlay models.TextOverlay, momentDurationMS int, config Config, problems *ValidationError) {
	if !overlay.Enabled {
		return
	}
	if strings.TrimSpace(overlay.Title) == "" && strings.TrimSpace(overlay.Subtitle) == "" {
		problems.Add(field+".text", "required", "enabled typography requires title or subtitle")
	}
	if overlay.Type == "" {
		problems.Add(field+".type", "required", "typography type is required")
	} else if _, ok := config.AllowedTextOverlayTypes[overlay.Type]; !ok {
		problems.Add(field+".type", "unsupported_type", "typography type is unsupported")
	}
	if overlay.Style == "" {
		problems.Add(field+".style", "required", "typography style is required")
	} else if _, ok := config.AllowedTypographyStyles[overlay.Style]; !ok {
		problems.Add(field+".style", "unsupported_style", "typography style is unsupported")
	}
	if overlay.Placement == "" {
		problems.Add(field+".placement", "required", "typography placement is required")
	} else if _, ok := config.AllowedTextPlacements[overlay.Placement]; !ok {
		problems.Add(field+".placement", "unsupported_placement", "typography placement is unsupported")
	}
	if overlay.Animation == "" {
		problems.Add(field+".animation", "required", "typography animation is required")
	} else if _, ok := config.AllowedTextAnimations[overlay.Animation]; !ok {
		problems.Add(field+".animation", "unsupported_animation", "typography animation is unsupported")
	}
	if overlay.Source == "" {
		problems.Add(field+".source", "required", "typography source is required")
	} else if _, ok := config.AllowedTextSources[overlay.Source]; !ok {
		problems.Add(field+".source", "unsupported_source", "typography source is unsupported")
	}
	if utf8.RuneCountInString(overlay.Title) > config.MaxTypographyTitleLength {
		problems.Add(field+".title", "too_long", fmt.Sprintf("typography title must be at most %d characters", config.MaxTypographyTitleLength))
	}
	if utf8.RuneCountInString(overlay.Subtitle) > config.MaxTypographySubtitleLength {
		problems.Add(field+".subtitle", "too_long", fmt.Sprintf("typography subtitle must be at most %d characters", config.MaxTypographySubtitleLength))
	}
	if utf8.RuneCountInString(overlay.Reason) > config.MaxTypographyReasonLength {
		problems.Add(field+".reason", "too_long", fmt.Sprintf("typography reason must be at most %d characters", config.MaxTypographyReasonLength))
	}
	if typographyLineCount(overlay) > config.MaxTypographyLines {
		problems.Add(field+".text", "too_many_lines", fmt.Sprintf("typography must fit within %d lines", config.MaxTypographyLines))
	}
	if overlay.StartMS < 0 || overlay.DurationMS <= 0 || overlay.StartMS+overlay.DurationMS > momentDurationMS {
		problems.Add(field+".timing", "invalid_timing", "typography timing must fit within the owning moment")
	}
	if overlay.MaxWidth < 0 || overlay.MaxWidth > 1 {
		problems.Add(field+".max_width", "out_of_range", "typography max width must be between 0 and 1")
	}
	if overlay.Confidence < 0 || overlay.Confidence > 1 {
		problems.Add(field+".confidence", "out_of_range", "typography confidence must be between 0 and 1")
	}
	validateSafeArea(field+".safe_area", overlay.SafeArea, problems)
}

func typographyLineCount(overlay models.TextOverlay) int {
	lines := 0
	if strings.TrimSpace(overlay.Title) != "" {
		lines++
	}
	if strings.TrimSpace(overlay.Subtitle) != "" {
		lines += 2
	}
	return lines
}

func validateSafeArea(field string, area models.SafeArea, problems *ValidationError) {
	if !validNormalizedMargin(area.Top) || !validNormalizedMargin(area.Right) || !validNormalizedMargin(area.Bottom) || !validNormalizedMargin(area.Left) {
		problems.Add(field, "out_of_range", "typography safe-area margins must be normalized values between 0 and 0.5")
	}
}

func validNormalizedMargin(value float64) bool {
	return value >= 0 && value <= 0.5
}

func validateBRoll(ctx context.Context, v *Validator, userID string, plan models.StoryPlan, projectMedia map[string]struct{}, momentIDs map[string]struct{}, totalDurationMS int, problems *ValidationError) {
	if len(plan.BRoll) == 0 {
		return
	}
	config := v.config()
	seen := make(map[string]struct{}, len(plan.BRoll))
	brollDurationMS := 0
	for index, overlay := range plan.BRoll {
		field := fmt.Sprintf("broll[%d]", index)
		assetID := strings.TrimSpace(firstNonEmpty(overlay.AssetID, overlay.MediaID))
		if assetID == "" {
			problems.Add(field+".asset_id", "required", "b-roll asset id is required")
			continue
		}
		if len(projectMedia) > 0 {
			if _, belongs := projectMedia[assetID]; !belongs {
				problems.Add(field+".asset_id", "not_in_project", "b-roll references media outside the project")
			}
		}
		if overlay.StartMS < 0 || overlay.DurationMS <= 0 || overlay.StartMS+overlay.DurationMS > totalDurationMS {
			problems.Add(field+".timing", "invalid_timing", "b-roll timing must fit within story duration")
		}
		if len(overlay.CropIntent) > config.MaxCropIntentLength {
			problems.Add(field+".crop_intent", "too_long", fmt.Sprintf("crop intent must be at most %d characters", config.MaxCropIntentLength))
		}
		if len(overlay.Reason) > config.MaxBRollReasonLength {
			problems.Add(field+".reason", "too_long", fmt.Sprintf("b-roll reason must be at most %d characters", config.MaxBRollReasonLength))
		}
		if overlay.Confidence < 0 || overlay.Confidence > 1 || overlay.Score < 0 || overlay.Score > 1 {
			problems.Add(field+".score", "out_of_range", "b-roll confidence and score must be between 0 and 1")
		}
		key := fmt.Sprintf("%s:%s:%d:%d:%d:%d", assetID, overlay.VideoAnalysisVersion, overlay.SourceStartMicros, overlay.SourceEndMicros, overlay.StartMS, overlay.DurationMS)
		if _, exists := seen[key]; exists {
			problems.Add(field+".asset_id", "duplicate_reference", "story plan contains a duplicate b-roll reference")
		} else {
			seen[key] = struct{}{}
		}
		media, err := v.Store.GetMedia(ctx, userID, assetID)
		if err != nil {
			problems.Add(field+".asset_id", "missing_asset", "b-roll references a media asset that does not exist")
			continue
		}
		if overlay.MediaType != "" && overlay.MediaType != media.Type {
			problems.Add(field+".media_type", "mismatch", "b-roll media type does not match the referenced asset")
		}
		switch media.Type {
		case models.MediaTypeVideo:
			if media.DurationSeconds == nil || *media.DurationSeconds <= 0 {
				problems.Add(field+".duration_ms", "invalid_source_duration", "video b-roll asset is missing a valid source duration")
			} else {
				sourceDurationMicros := int64(math.Round(*media.DurationSeconds * 1_000_000))
				if overlay.SourceStartMicros < 0 || overlay.SourceEndMicros <= overlay.SourceStartMicros || overlay.SourceEndMicros > sourceDurationMicros {
					problems.Add(field+".source_segment", "out_of_range", "video b-roll source segment must be within the source duration")
				}
				if overlay.DurationMS > 0 && overlay.SourceEndMicros > overlay.SourceStartMicros && !durationMatchesVideoRange(overlay.DurationMS, overlay.SourceStartMicros, overlay.SourceEndMicros) && overlay.DurationMS > int((overlay.SourceEndMicros-overlay.SourceStartMicros)/1000) {
					problems.Add(field+".duration_ms", "range_mismatch", "video b-roll duration must not exceed its source segment")
				}
			}
			if strings.TrimSpace(overlay.VideoAnalysisVersion) == "" {
				problems.Add(field+".video_analysis_version", "required", "video analysis version is required for video b-roll")
			} else if _, err := v.Store.GetVideoAnalysis(ctx, userID, media.ID, overlay.VideoAnalysisVersion); err != nil {
				problems.Add(field+".video_analysis_version", "missing_analysis", "video analysis version does not exist for the referenced b-roll asset")
			}
		case models.MediaTypePhoto, models.MediaTypeLivePhoto:
			if overlay.SourceStartMicros != 0 || overlay.SourceEndMicros != 0 {
				problems.Add(field+".source_segment", "invalid_timestamps", "photo b-roll must not define source timestamps")
			}
		default:
			problems.Add(field+".media_type", "unsupported_media_type", "b-roll references an unsupported media type")
		}
		if strings.TrimSpace(media.S3Key) == "" {
			problems.Add(field+".s3_key", "missing_object", "referenced b-roll media is missing its S3 object key")
		} else if exists, err := v.ObjectStorage.ObjectExists(ctx, media.S3Key); err != nil {
			problems.Add(field+".s3_key", "missing_object", "unable to verify referenced b-roll media object")
		} else if !exists {
			problems.Add(field+".s3_key", "missing_object", "referenced b-roll media object does not exist")
		}
		if overlay.Anchor != nil {
			if len(overlay.Anchor.Type) > config.MaxBRollAnchorTypeLength {
				problems.Add(field+".anchor.type", "too_long", fmt.Sprintf("b-roll anchor type must be at most %d characters", config.MaxBRollAnchorTypeLength))
			}
			if strings.TrimSpace(overlay.Anchor.MomentID) != "" {
				if _, exists := momentIDs[overlay.Anchor.MomentID]; !exists {
					problems.Add(field+".anchor.moment_id", "unknown_moment", "b-roll anchor references an unknown moment")
				}
			}
			if overlay.Anchor.StartMS < 0 || overlay.Anchor.EndMS <= overlay.Anchor.StartMS || overlay.Anchor.EndMS > totalDurationMS {
				problems.Add(field+".anchor.timing", "invalid_timing", "b-roll anchor timing must fit within story duration")
			}
		}
		brollDurationMS += overlay.DurationMS
	}
	if totalDurationMS > 0 && float64(brollDurationMS) > float64(totalDurationMS)*0.40+1 {
		problems.Add("broll", "too_much_broll", "b-roll must not dominate the story duration")
	}
}

func isHexColor(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) != 7 || value[0] != '#' {
		return false
	}
	for _, char := range value[1:] {
		if !((char >= '0' && char <= '9') || (char >= 'a' && char <= 'f') || (char >= 'A' && char <= 'F')) {
			return false
		}
	}
	return true
}

func momentReferenceKey(assetID string, moment models.StoryMoment) string {
	if moment.MediaType == models.MediaTypeVideo || moment.StartMicros != 0 || moment.EndMicros != 0 {
		return fmt.Sprintf("video:%s:%s:%d:%d", assetID, moment.VideoAnalysisVersion, moment.StartMicros, moment.EndMicros)
	}
	return fmt.Sprintf("photo:%s:%s", assetID, moment.MomentID)
}

func durationMatchesVideoRange(durationMS int, startMicros, endMicros int64) bool {
	expectedMS := int((endMicros - startMicros) / 1000)
	return absInt(durationMS-expectedMS) <= 1
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
