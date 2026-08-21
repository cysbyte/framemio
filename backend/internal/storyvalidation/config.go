package storyvalidation

import "github.com/framemio/backend/internal/models"

type Config struct {
	SchemaVersion             string
	SupportedAspectRatios     map[string]struct{}
	AllowedAudioSources       map[string]struct{}
	AudioSourcesRequiringMood map[string]struct{}
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
			"licensed": {},
			"custom":   {},
		},
		AudioSourcesRequiringMood: map[string]struct{}{
			"adaptive": {},
			"licensed": {},
			"custom":   {},
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
	}
}
