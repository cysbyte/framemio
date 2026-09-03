package ai

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/framemio/backend/internal/models"
)

const (
	TypographyPromptVersion     = "typography-planner-v1"
	MaxTypographyTitleLength    = 40
	MaxTypographySubtitleLength = 100
	MaxTypographyReasonLength   = 240
	DefaultTypographyMaxWidth   = 0.72
	DefaultTypographySafeMargin = 0.08
	DefaultTypographyStartMS    = 350
	MaxTypographyPerStory       = 2
)

var (
	allowedTextOverlayTypes = map[models.TextOverlayType]struct{}{
		models.TextOverlayMomentTitle:   {},
		models.TextOverlayMomentCaption: {},
		models.TextOverlayLocation:      {},
		models.TextOverlayDate:          {},
		models.TextOverlayChapterTitle:  {},
		models.TextOverlayStoryIntro:    {},
		models.TextOverlayStoryOutro:    {},
	}
	allowedTypographyStyles = map[models.TypographyStyle]struct{}{
		models.TypographyStyleCinematic: {},
		models.TypographyStyleMinimal:   {},
		models.TypographyStyleEditorial: {},
		models.TypographyStyleMemory:    {},
		models.TypographyStyleLocation:  {},
	}
	allowedTextPlacements = map[models.TextPlacement]struct{}{
		models.TextPlacementTopLeft:      {},
		models.TextPlacementTopCenter:    {},
		models.TextPlacementTopRight:     {},
		models.TextPlacementCenterLeft:   {},
		models.TextPlacementCenter:       {},
		models.TextPlacementCenterRight:  {},
		models.TextPlacementBottomLeft:   {},
		models.TextPlacementBottomCenter: {},
		models.TextPlacementBottomRight:  {},
	}
	allowedTextAnimations = map[models.TextAnimation]struct{}{
		models.TextAnimationFade:        {},
		models.TextAnimationFadeUp:      {},
		models.TextAnimationFadeDown:    {},
		models.TextAnimationSubtleScale: {},
		models.TextAnimationNone:        {},
	}
	allowedTextSources = map[models.TextSource]struct{}{
		models.TextSourceAI:     {},
		models.TextSourceUser:   {},
		models.TextSourceSystem: {},
	}
)

type TypographyPlanner interface {
	Generate(ctx context.Context, input TypographyPlanningInput) (TypographyPlan, error)
}

type TypographyPlanningInput struct {
	ProjectID            string
	StoryPlanID          string
	StoryPlanVersion     int
	TypographyVersion    string
	StoryTitle           string
	StoryType            models.StoryType
	VisualStyle          models.VisualStyle
	NarrativeSummary     string
	UserInstructions     string
	Moments              []TypographyMomentContext
	PreviousMomentTitles []string
}

type TypographyMomentContext struct {
	MomentID   string
	AssetID    string
	MediaType  models.MediaType
	Narrative  string
	Caption    string
	Reason     string
	DurationMS int
	Score      float64
	Index      int
}

type TypographyPlan struct {
	Overlays map[string]models.TextOverlay
}

type typographyPlanDocument struct {
	Overlays []typographyOverlayDocument `json:"overlays"`
}

type typographyOverlayDocument struct {
	MomentID string `json:"moment_id"`
	models.TextOverlay
}

type DefaultTypographyPlanner struct{}

type typographyCandidate struct {
	context TypographyMomentContext
	title   string
	score   float64
}

func (DefaultTypographyPlanner) Generate(ctx context.Context, input TypographyPlanningInput) (TypographyPlan, error) {
	if err := ctx.Err(); err != nil {
		return TypographyPlan{}, err
	}
	candidates := make([]typographyCandidate, 0, len(input.Moments))
	for _, moment := range input.Moments {
		if moment.MediaType != models.MediaTypePhoto && moment.MediaType != models.MediaTypeLivePhoto {
			continue
		}
		title := typographyTitleFromMoment(moment)
		if title == "" || isGenericTypographyText(title) || moment.DurationMS < 1800 {
			continue
		}
		score := moment.Score
		if score == 0 {
			score = 0.58
		}
		if moment.Caption != "" || moment.Narrative != "" {
			score += 0.08
		}
		if score < 0.52 {
			continue
		}
		candidates = append(candidates, typographyCandidate{context: moment, title: title, score: score})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].score == candidates[j].score {
			return candidates[i].context.Index < candidates[j].context.Index
		}
		return candidates[i].score > candidates[j].score
	})
	plan := TypographyPlan{Overlays: make(map[string]models.TextOverlay)}
	for _, candidate := range candidates {
		if len(plan.Overlays) >= MaxTypographyPerStory {
			break
		}
		moment := candidate.context
		durationMS := clampInt(moment.DurationMS-650, 900, 3500)
		overlay := models.TextOverlay{
			Enabled:    true,
			Type:       models.TextOverlayMomentTitle,
			Title:      candidate.title,
			Subtitle:   typographySubtitleFromMoment(moment, candidate.title),
			Style:      typographyStyleForVisualStyle(input.VisualStyle),
			Placement:  typographyPlacementFor(moment),
			StartMS:    minInt(DefaultTypographyStartMS, maxInt(0, moment.DurationMS-900)),
			DurationMS: durationMS,
			MaxWidth:   DefaultTypographyMaxWidth,
			SafeArea: models.SafeArea{
				Top: DefaultTypographySafeMargin, Right: DefaultTypographySafeMargin, Bottom: DefaultTypographySafeMargin, Left: DefaultTypographySafeMargin,
			},
			Animation:  models.TextAnimationFadeUp,
			Source:     models.TextSourceAI,
			Reason:     truncateText("Generated from photo analysis and story context", MaxTypographyReasonLength),
			Confidence: roundFloat(clamp(candidate.score), 3),
		}
		normalized, err := NormalizeTextOverlay(overlay, moment.DurationMS)
		if err != nil {
			continue
		}
		plan.Overlays[moment.MomentID] = normalized
	}
	return plan, nil
}

func NormalizeTextOverlay(overlay models.TextOverlay, momentDurationMS int) (models.TextOverlay, error) {
	if !overlay.Enabled {
		return models.TextOverlay{Enabled: false}, nil
	}
	overlay.Title = cleanTypographyText(overlay.Title)
	overlay.Subtitle = cleanTypographyText(overlay.Subtitle)
	overlay.Reason = cleanTypographyText(overlay.Reason)
	if overlay.Title == "" && overlay.Subtitle == "" {
		return models.TextOverlay{}, errors.New("enabled typography requires title or subtitle")
	}
	if overlay.Type == "" {
		overlay.Type = models.TextOverlayMomentTitle
	}
	if _, ok := allowedTextOverlayTypes[overlay.Type]; !ok {
		return models.TextOverlay{}, fmt.Errorf("unsupported typography type %q", overlay.Type)
	}
	if overlay.Style == "" {
		overlay.Style = models.TypographyStyleCinematic
	}
	if _, ok := allowedTypographyStyles[overlay.Style]; !ok {
		return models.TextOverlay{}, fmt.Errorf("unsupported typography style %q", overlay.Style)
	}
	if overlay.Placement == "" {
		overlay.Placement = models.TextPlacementBottomLeft
	}
	if _, ok := allowedTextPlacements[overlay.Placement]; !ok {
		return models.TextOverlay{}, fmt.Errorf("unsupported typography placement %q", overlay.Placement)
	}
	if overlay.Animation == "" {
		overlay.Animation = models.TextAnimationFadeUp
	}
	if _, ok := allowedTextAnimations[overlay.Animation]; !ok {
		return models.TextOverlay{}, fmt.Errorf("unsupported typography animation %q", overlay.Animation)
	}
	if overlay.Source == "" {
		overlay.Source = models.TextSourceAI
	}
	if _, ok := allowedTextSources[overlay.Source]; !ok {
		return models.TextOverlay{}, fmt.Errorf("unsupported typography source %q", overlay.Source)
	}
	overlay.Title = truncateRunes(overlay.Title, MaxTypographyTitleLength)
	overlay.Subtitle = truncateRunes(overlay.Subtitle, MaxTypographySubtitleLength)
	overlay.Reason = truncateRunes(overlay.Reason, MaxTypographyReasonLength)
	if overlay.StartMS < 0 {
		overlay.StartMS = 0
	}
	if overlay.DurationMS <= 0 {
		overlay.DurationMS = maxInt(900, momentDurationMS-overlay.StartMS-250)
	}
	if momentDurationMS > 0 && overlay.StartMS+overlay.DurationMS > momentDurationMS {
		overlay.DurationMS = maxInt(0, momentDurationMS-overlay.StartMS)
	}
	if overlay.DurationMS < 250 {
		return models.TextOverlay{}, errors.New("typography duration is too short")
	}
	if overlay.MaxWidth <= 0 || overlay.MaxWidth > 1 {
		overlay.MaxWidth = DefaultTypographyMaxWidth
	}
	overlay.SafeArea = normalizeSafeArea(overlay.SafeArea)
	overlay.Confidence = clamp(overlay.Confidence)
	return overlay, nil
}

func ParseTypographyPlanJSON(data []byte, momentDurations map[string]int) (TypographyPlan, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var document typographyPlanDocument
	if err := decoder.Decode(&document); err != nil {
		return TypographyPlan{}, fmt.Errorf("decode typography plan json: %w", err)
	}
	if len(document.Overlays) == 0 {
		return TypographyPlan{Overlays: map[string]models.TextOverlay{}}, nil
	}
	plan := TypographyPlan{Overlays: make(map[string]models.TextOverlay, len(document.Overlays))}
	for index, item := range document.Overlays {
		momentID := strings.TrimSpace(item.MomentID)
		if momentID == "" {
			return TypographyPlan{}, fmt.Errorf("typography overlay %d missing moment_id", index)
		}
		durationMS, ok := momentDurations[momentID]
		if !ok {
			return TypographyPlan{}, fmt.Errorf("typography overlay %d references unknown moment %q", index, momentID)
		}
		if _, exists := plan.Overlays[momentID]; exists {
			return TypographyPlan{}, fmt.Errorf("duplicate typography overlay for moment %q", momentID)
		}
		normalized, err := NormalizeTextOverlay(item.TextOverlay, durationMS)
		if err != nil {
			return TypographyPlan{}, fmt.Errorf("typography overlay %d invalid: %w", index, err)
		}
		if normalized.Enabled {
			plan.Overlays[momentID] = normalized
		}
	}
	return plan, nil
}

func ValidTextOverlay(overlay models.TextOverlay, momentDurationMS int) error {
	_, err := NormalizeTextOverlay(overlay, momentDurationMS)
	return err
}

func typographyTitleFromMoment(moment TypographyMomentContext) string {
	text := firstNonEmpty(humanText(moment.Caption), humanText(moment.Narrative), humanText(moment.Reason))
	if text == "" {
		return ""
	}
	text = strings.Trim(strings.TrimSpace(text), ".,;:!?")
	words := strings.Fields(text)
	if len(words) == 0 {
		return ""
	}
	selected := make([]string, 0, 4)
	for _, word := range words {
		cleaned := strings.Trim(word, "\"'“”‘’.,;:!?()[]{}")
		if cleaned == "" || isMinorTitleWord(cleaned) {
			if len(selected) == 0 {
				continue
			}
		}
		selected = append(selected, cleaned)
		if len(selected) == 4 {
			break
		}
	}
	if len(selected) == 0 {
		return ""
	}
	return titleCase(truncateRunes(strings.Join(selected, " "), MaxTypographyTitleLength))
}

func typographySubtitleFromMoment(moment TypographyMomentContext, title string) string {
	text := firstNonEmpty(humanText(moment.Narrative), humanText(moment.Reason), humanText(moment.Caption))
	text = strings.Trim(strings.TrimSpace(text), ".,;:!?")
	if text == "" || strings.EqualFold(text, title) || strings.Contains(strings.ToLower(text), strings.ToLower(title)) {
		return ""
	}
	return truncateRunes(text, MaxTypographySubtitleLength)
}

func typographyStyleForVisualStyle(style models.VisualStyle) models.TypographyStyle {
	switch normalizeVisualStyle(style) {
	case models.VisualStyleMinimal:
		return models.TypographyStyleMinimal
	case models.VisualStyleElegant:
		return models.TypographyStyleEditorial
	case models.VisualStyleWarm, models.VisualStyleNatural, models.VisualStyleCalm, models.VisualStylePlayful:
		return models.TypographyStyleMemory
	default:
		return models.TypographyStyleCinematic
	}
}

func typographyPlacementFor(moment TypographyMomentContext) models.TextPlacement {
	placements := []models.TextPlacement{models.TextPlacementBottomLeft, models.TextPlacementBottomRight, models.TextPlacementTopLeft, models.TextPlacementTopRight}
	hash := sha256.Sum256([]byte(firstNonEmpty(moment.MomentID, moment.AssetID, moment.Caption)))
	return placements[int(hash[0])%len(placements)]
}

func normalizeSafeArea(area models.SafeArea) models.SafeArea {
	return models.SafeArea{
		Top:    normalizedMargin(area.Top),
		Right:  normalizedMargin(area.Right),
		Bottom: normalizedMargin(area.Bottom),
		Left:   normalizedMargin(area.Left),
	}
}

func normalizedMargin(value float64) float64 {
	if value <= 0 {
		return DefaultTypographySafeMargin
	}
	if value < 0.02 {
		value = 0.02
	}
	if value > 0.24 {
		value = 0.24
	}
	return roundFloat(value, 3)
}

func cleanTypographyText(value string) string {
	value = strings.TrimSpace(strings.Map(func(char rune) rune {
		if char == '\n' || char == '\r' || char == '\t' {
			return ' '
		}
		if unicode.IsControl(char) {
			return -1
		}
		return char
	}, value))
	return strings.Join(strings.Fields(value), " ")
}

func isGenericTypographyText(value string) bool {
	lower := strings.ToLower(strings.TrimSpace(value))
	if lower == "" {
		return true
	}
	generic := []string{"photo", "image", "moment", "memory", "uploaded image", "first uploaded image", "meaningful photo moment", "beautiful day", "good times"}
	for _, entry := range generic {
		if lower == entry || strings.Contains(lower, entry) {
			return true
		}
	}
	return false
}

func isMinorTitleWord(value string) bool {
	switch strings.ToLower(value) {
	case "a", "an", "the", "and", "or", "but", "in", "on", "at", "near", "by", "with", "from", "to", "of", "for":
		return true
	default:
		return false
	}
}

func titleCase(value string) string {
	words := strings.Fields(value)
	for index, word := range words {
		runes := []rune(strings.ToLower(word))
		if len(runes) == 0 {
			continue
		}
		runes[0] = unicode.ToUpper(runes[0])
		words[index] = string(runes)
	}
	return strings.Join(words, " ")
}

func truncateRunes(value string, limit int) string {
	if limit <= 0 || utf8.RuneCountInString(value) <= limit {
		return value
	}
	runes := []rune(value)
	if limit <= 1 {
		return string(runes[:limit])
	}
	return strings.TrimSpace(string(runes[:limit-1])) + "…"
}

func typographyPlanHash(plan TypographyPlan) string {
	keys := make([]string, 0, len(plan.Overlays))
	for key := range plan.Overlays {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	builder := strings.Builder{}
	for _, key := range keys {
		overlay := plan.Overlays[key]
		builder.WriteString(key)
		builder.WriteString(string(overlay.Type))
		builder.WriteString(overlay.Title)
		builder.WriteString(overlay.Subtitle)
		builder.WriteString(string(overlay.Style))
		builder.WriteString(string(overlay.Placement))
	}
	digest := sha256.Sum256([]byte(builder.String()))
	return hex.EncodeToString(digest[:])
}
