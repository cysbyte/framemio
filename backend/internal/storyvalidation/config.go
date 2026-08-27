package storyvalidation

import "github.com/framemio/backend/internal/models"

type Config struct {
	SchemaVersion             string
	SupportedAspectRatios     map[string]struct{}
	AllowedAudioSources       map[string]struct{}
	AudioSourcesRequiringMood map[string]struct{}
	AllowedCaptionPresets     map[string]struct{}
	AllowedCaptionFonts       map[string]struct{}
	AllowedCaptionPositions   map[string]struct{}
	MaxTitleLength            int
	MaxEndingLength           int
	MaxMusicMoodLength        int
	MaxUserInstructionsLength int
	MaxCaptionLength          int
	MaxNarrativeLength        int
	MaxReasonLength           int
	MaxCropIntentLength       int
	MaxTransitionLength       int
	MaxPresetLength           int
	MaxAudioSourceLength      int
	MaxCaptionFontSize        int
	MaxCaptionStrokeWidth     int
	MaxCaptionBottomOffset    int
	MaxCaptionLines           int
	MaxCaptionLineCharacters  int
}

func DefaultConfig() Config {
	return Config{
		SchemaVersion: models.StoryPlanSchemaVersionV1,
		SupportedAspectRatios: map[string]struct{}{
			"9:16": {},
			"16:9": {},
			"1:1":  {},
			"4:5":  {},
		},
		AllowedAudioSources: map[string]struct{}{
			"adaptive": {},
			"none":     {},
			"original": {},
			"licensed": {},
			"custom":   {},
		},
		AudioSourcesRequiringMood: map[string]struct{}{
			"adaptive": {},
			"licensed": {},
			"custom":   {},
		},
		AllowedCaptionPresets: map[string]struct{}{
			"bold_social":        {},
			"cinematic_subtitle": {},
			"clean_minimal":      {},
			"playful_pop":        {},
		},
		AllowedCaptionFonts: map[string]struct{}{
			"inter":          {},
			"inter-bold":     {},
			"system":         {},
			"system-bold":    {},
			"noto-sans":      {},
			"noto-sans-bold": {},
		},
		AllowedCaptionPositions: map[string]struct{}{
			"bottom": {},
			"middle": {},
			"top":    {},
		},
		MaxTitleLength:            120,
		MaxEndingLength:           180,
		MaxMusicMoodLength:        80,
		MaxUserInstructionsLength: 500,
		MaxCaptionLength:          220,
		MaxNarrativeLength:        240,
		MaxReasonLength:           240,
		MaxCropIntentLength:       32,
		MaxTransitionLength:       32,
		MaxPresetLength:           64,
		MaxAudioSourceLength:      32,
		MaxCaptionFontSize:        96,
		MaxCaptionStrokeWidth:     12,
		MaxCaptionBottomOffset:    480,
		MaxCaptionLines:           4,
		MaxCaptionLineCharacters:  48,
	}
}
