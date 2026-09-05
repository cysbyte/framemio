package storyvalidation

import "github.com/framemio/backend/internal/models"

type Config struct {
	SchemaVersion               string
	SupportedAspectRatios       map[string]struct{}
	AllowedCaptionPresets       map[string]struct{}
	AllowedCaptionFonts         map[string]struct{}
	AllowedTransitions          map[string]struct{}
	AllowedCropIntents          map[string]struct{}
	AllowedCaptionPositions     map[string]struct{}
	AllowedTextOverlayTypes     map[models.TextOverlayType]struct{}
	AllowedTypographyStyles     map[models.TypographyStyle]struct{}
	AllowedTextPlacements       map[models.TextPlacement]struct{}
	AllowedTextAnimations       map[models.TextAnimation]struct{}
	AllowedTextSources          map[models.TextSource]struct{}
	MaxTitleLength              int
	MaxEndingLength             int
	MaxUserInstructionsLength   int
	MaxCaptionLength            int
	MaxTypographyTitleLength    int
	MaxTypographySubtitleLength int
	MaxTypographyReasonLength   int
	MaxTypographyLines          int
	MaxNarrativeLength          int
	MaxReasonLength             int
	MaxCropIntentLength         int
	MaxTransitionLength         int
	MaxBRollReasonLength        int
	MaxBRollAnchorTypeLength    int
	MaxPresetLength             int
	MaxAudioSourceLength        int
	MaxCaptionFontSize          int
	MaxCaptionStrokeWidth       int
	MaxCaptionBottomOffset      int
	MaxCaptionLines             int
	MaxCaptionLineCharacters    int
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
		AllowedTransitions: map[string]struct{}{
			"fade_in":   {},
			"fade":      {},
			"crossfade": {},
			"cut":       {},
		},
		AllowedCropIntents: map[string]struct{}{
			"fit":  {},
			"fill": {},
		},
		AllowedCaptionPositions: map[string]struct{}{
			"bottom": {},
			"middle": {},
			"top":    {},
		},
		AllowedTextOverlayTypes: map[models.TextOverlayType]struct{}{
			models.TextOverlayMomentTitle:   {},
			models.TextOverlayMomentCaption: {},
			models.TextOverlayLocation:      {},
			models.TextOverlayDate:          {},
			models.TextOverlayChapterTitle:  {},
			models.TextOverlayStoryIntro:    {},
			models.TextOverlayStoryOutro:    {},
		},
		AllowedTypographyStyles: map[models.TypographyStyle]struct{}{
			models.TypographyStyleCinematic: {},
			models.TypographyStyleMinimal:   {},
			models.TypographyStyleEditorial: {},
			models.TypographyStyleMemory:    {},
			models.TypographyStyleLocation:  {},
		},
		AllowedTextPlacements: map[models.TextPlacement]struct{}{
			models.TextPlacementTopLeft:      {},
			models.TextPlacementTopCenter:    {},
			models.TextPlacementTopRight:     {},
			models.TextPlacementCenterLeft:   {},
			models.TextPlacementCenter:       {},
			models.TextPlacementCenterRight:  {},
			models.TextPlacementBottomLeft:   {},
			models.TextPlacementBottomCenter: {},
			models.TextPlacementBottomRight:  {},
		},
		AllowedTextAnimations: map[models.TextAnimation]struct{}{
			models.TextAnimationFade:        {},
			models.TextAnimationFadeUp:      {},
			models.TextAnimationFadeDown:    {},
			models.TextAnimationSubtleScale: {},
			models.TextAnimationNone:        {},
		},
		AllowedTextSources: map[models.TextSource]struct{}{
			models.TextSourceAI:     {},
			models.TextSourceUser:   {},
			models.TextSourceSystem: {},
		},
		MaxTitleLength:              120,
		MaxEndingLength:             180,
		MaxUserInstructionsLength:   500,
		MaxCaptionLength:            220,
		MaxTypographyTitleLength:    40,
		MaxTypographySubtitleLength: 100,
		MaxTypographyReasonLength:   240,
		MaxTypographyLines:          3,
		MaxNarrativeLength:          240,
		MaxReasonLength:             240,
		MaxCropIntentLength:         32,
		MaxTransitionLength:         32,
		MaxBRollReasonLength:        120,
		MaxBRollAnchorTypeLength:    32,
		MaxPresetLength:             64,
		MaxAudioSourceLength:        32,
		MaxCaptionFontSize:          96,
		MaxCaptionStrokeWidth:       12,
		MaxCaptionBottomOffset:      480,
		MaxCaptionLines:             4,
		MaxCaptionLineCharacters:    48,
	}
}
