package storyvalidation

import (
	"context"
	"fmt"
	"math"
	"strings"

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
	if config.AllowedAudioSources == nil {
		config.AllowedAudioSources = DefaultConfig().AllowedAudioSources
	}
	if config.AudioSourcesRequiringMood == nil {
		config.AudioSourcesRequiringMood = DefaultConfig().AudioSourcesRequiringMood
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
	if config.MaxTitleLength == 0 {
		config.MaxTitleLength = DefaultConfig().MaxTitleLength
	}
	if config.MaxEndingLength == 0 {
		config.MaxEndingLength = DefaultConfig().MaxEndingLength
	}
	if config.MaxMusicMoodLength == 0 {
		config.MaxMusicMoodLength = DefaultConfig().MaxMusicMoodLength
	}
	if config.MaxUserInstructionsLength == 0 {
		config.MaxUserInstructionsLength = DefaultConfig().MaxUserInstructionsLength
	}
	if config.MaxCaptionLength == 0 {
		config.MaxCaptionLength = DefaultConfig().MaxCaptionLength
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
	if strings.TrimSpace(plan.MusicMood) == "" {
		problems.Add("music_mood", "required", "music mood is required")
	} else if len(plan.MusicMood) > config.MaxMusicMoodLength {
		problems.Add("music_mood", "too_long", fmt.Sprintf("music mood must be at most %d characters", config.MaxMusicMoodLength))
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
		if strings.TrimSpace(plan.Audio.Source) == "" {
			problems.Add("audio.source", "required", "audio source is required")
		} else if len(plan.Audio.Source) > config.MaxAudioSourceLength {
			problems.Add("audio.source", "too_long", fmt.Sprintf("audio source must be at most %d characters", config.MaxAudioSourceLength))
		} else if !v.allowedAudioSource(plan.Audio.Source) {
			problems.Add("audio.source", "unsupported_audio_source", "audio source is unsupported")
		}
		if strings.TrimSpace(plan.Audio.Mood) != "" && len(plan.Audio.Mood) > config.MaxMusicMoodLength {
			problems.Add("audio.mood", "too_long", fmt.Sprintf("audio mood must be at most %d characters", config.MaxMusicMoodLength))
		}
		if _, requiresMood := config.AudioSourcesRequiringMood[strings.ToLower(strings.TrimSpace(plan.Audio.Source))]; requiresMood && strings.TrimSpace(plan.Audio.Mood) == "" {
			problems.Add("audio.mood", "required", "audio mood is required for the selected source")
		}
		if plan.Audio.BackgroundVolume < 0 || plan.Audio.BackgroundVolume > 1 {
			problems.Add("audio.background_volume", "out_of_range", "background volume must be between 0 and 1")
		}
		if plan.Audio.OriginalVolume < 0 || plan.Audio.OriginalVolume > 1 {
			problems.Add("audio.original_volume", "out_of_range", "original volume must be between 0 and 1")
		}
		if plan.Audio.DuckedVolume < 0 || plan.Audio.DuckedVolume > 1 {
			problems.Add("audio.ducked_volume", "out_of_range", "ducked volume must be between 0 and 1")
		}
		if plan.Audio.BackgroundVolume > 0 && plan.Audio.DuckedVolume > plan.Audio.BackgroundVolume {
			problems.Add("audio.ducked_volume", "out_of_range", "ducked volume must not exceed background volume")
		}
		if plan.Audio.StartOffsetSeconds < 0 {
			problems.Add("audio.start_offset_seconds", "out_of_range", "music start offset must not be negative")
		}
		if plan.Audio.FadeInMS < 0 || plan.Audio.FadeOutMS < 0 {
			problems.Add("audio.fade", "out_of_range", "audio fade values must not be negative")
		}
		if strings.EqualFold(strings.TrimSpace(plan.Audio.Source), "licensed") && strings.TrimSpace(plan.Audio.TrackID) == "" && strings.TrimSpace(plan.Audio.BackgroundS3Key) == "" {
			problems.Add("audio.track_id", "required", "licensed music requires a track id or background storage key")
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
	if config.AllowedAudioSources == nil {
		config.AllowedAudioSources = defaults.AllowedAudioSources
	}
	if config.AudioSourcesRequiringMood == nil {
		config.AudioSourcesRequiringMood = defaults.AudioSourcesRequiringMood
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
	if config.MaxTitleLength == 0 {
		config.MaxTitleLength = defaults.MaxTitleLength
	}
	if config.MaxEndingLength == 0 {
		config.MaxEndingLength = defaults.MaxEndingLength
	}
	if config.MaxMusicMoodLength == 0 {
		config.MaxMusicMoodLength = defaults.MaxMusicMoodLength
	}
	if config.MaxUserInstructionsLength == 0 {
		config.MaxUserInstructionsLength = defaults.MaxUserInstructionsLength
	}
	if config.MaxCaptionLength == 0 {
		config.MaxCaptionLength = defaults.MaxCaptionLength
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

func (v *Validator) allowedAudioSource(source string) bool {
	_, ok := v.config().AllowedAudioSources[strings.ToLower(strings.TrimSpace(source))]
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
