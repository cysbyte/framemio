package ai

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/framemio/backend/internal/models"
)

type Request struct {
	ProjectID           string
	Version             int
	DesiredDuration     time.Duration
	TargetDurationFixed bool
	StoryType           models.StoryType
	VisualStyle         models.VisualStyle
	UserInstructions    string
	SelectedMediaIDs    []string
	PreferredMediaOrder []string
	ExcludedMediaIDs    []string
	ExcludedMomentIDs   []string
	SubtitlesEnabled    *bool
	CaptionStylePreset  string
	BRollPreference     string
	Timestamps          []time.Time
	Locations           []string
	PhotoAnalyses       []models.PhotoAnalysis
	VideoAnalyses       []models.VideoAnalysis
	MediaClusters       []models.MediaCluster
}

type Planner struct {
	Now               func() time.Time
	TypographyPlanner TypographyPlanner
	Logger            *slog.Logger
}

const maxVideoMomentDuration = 5 * time.Second
const maxStoryPlanCaptionLength = 220

type candidate struct {
	moment        models.StoryMoment
	captionCues   []captionCue
	sourceTime    int64
	score         float64
	repetition    string
	mediaID       string
	sceneID       string
	clusterID     string
	semanticTerms map[string]struct{}
	isVideo       bool
	isSelected    bool
	isEnding      bool
}

type captionCue struct {
	Text        string
	StartMicros int64
	EndMicros   int64
}

func (p *Planner) Plan(ctx context.Context, request Request) (models.StoryPlan, error) {
	if err := ctx.Err(); err != nil {
		return models.StoryPlan{}, err
	}
	if err := validateRequest(request); err != nil {
		return models.StoryPlan{}, err
	}
	now := p.now()
	selected := make(map[string]struct{}, len(request.SelectedMediaIDs))
	for _, mediaID := range request.SelectedMediaIDs {
		selected[mediaID] = struct{}{}
	}
	excludedMedia := make(map[string]struct{}, len(request.ExcludedMediaIDs))
	for _, mediaID := range request.ExcludedMediaIDs {
		if trimmed := strings.TrimSpace(mediaID); trimmed != "" {
			excludedMedia[trimmed] = struct{}{}
		}
	}
	excludedMoments := make(map[string]struct{}, len(request.ExcludedMomentIDs))
	for _, momentID := range request.ExcludedMomentIDs {
		if trimmed := strings.TrimSpace(momentID); trimmed != "" {
			excludedMoments[trimmed] = struct{}{}
		}
	}
	clusterByMedia := buildClusterIndex(request.MediaClusters)
	candidates := filterExcludedCandidates(buildCandidates(request, selected, clusterByMedia), excludedMedia, excludedMoments)
	applyInstructionPreferences(candidates, request.UserInstructions)
	if len(candidates) == 0 {
		return models.StoryPlan{}, errors.New("story planner requires at least one usable analysis")
	}
	chosen := selectCandidates(candidates, request.DesiredDuration)
	if len(chosen) == 0 {
		return models.StoryPlan{}, errors.New("story planner could not assemble a coherent story")
	}
	sortCandidatesForStory(chosen, request.PreferredMediaOrder)
	moments := make([]models.StoryMoment, 0, len(chosen))
	captions := make([]models.StoryCaption, 0, len(chosen))
	totalDuration := 0
	for index, entry := range chosen {
		entry.moment.MomentID = momentID(entry)
		if index > 0 {
			entry.moment.Transition = transitionFor(chosen[index-1], entry, request.VisualStyle)
		} else {
			entry.moment.Transition = "fade_in"
		}
		if entry.isVideo {
			entry.moment.CropIntent = "fill"
		} else {
			entry.moment.CropIntent = "fit"
		}
		if entry.moment.Caption == "" && shouldCaption(entry) {
			entry.moment.Caption = entry.moment.Narrative
		}
		moments = append(moments, entry.moment)
		totalDuration += entry.moment.DurationMS
		if len(entry.captionCues) > 0 {
			captions = append(captions, timedStoryCaptions(entry.moment, entry.captionCues, totalDuration-entry.moment.DurationMS)...)
		} else if !entry.isVideo && strings.TrimSpace(entry.moment.Caption) != "" {
			captions = append(captions, models.StoryCaption{
				MomentID: entry.moment.MomentID,
				Text:     entry.moment.Caption,
				StartMS:  totalDuration - entry.moment.DurationMS,
				EndMS:    totalDuration,
			})
		}
	}
	broll := planBRoll(request, moments, captions, candidates, totalDuration)
	if request.SubtitlesEnabled != nil && !*request.SubtitlesEnabled {
		captions = nil
	}
	if normalizedBRollPreference(request.BRollPreference) == "none" {
		broll = nil
	}
	storyType := normalizeStoryType(request.StoryType)
	visualStyle := normalizeVisualStyle(request.VisualStyle)
	titleText := titleFor(storyType, request, moments)
	endingText := endingFor(storyType, request, moments)
	captionStyle := captionStyleFor(storyType, visualStyle)
	if preset := strings.TrimSpace(request.CaptionStylePreset); preset != "" {
		captionStyle = captionStylePreset(preset, storyType, visualStyle)
	}
	plan := models.StoryPlan{
		SchemaVersion:       models.StoryPlanSchemaVersionV1,
		ID:                  planID(request, moments),
		ProjectID:           request.ProjectID,
		Version:             versionFor(request.Version),
		RequestedDurationMS: max(1, int(request.DesiredDuration.Milliseconds())),
		StoryType:           storyType,
		VisualStyle:         visualStyle,
		TitleText:           titleText,
		EndingText:          endingText,
		UserInstructions:    strings.TrimSpace(request.UserInstructions),
		Moments:             moments,
		BRoll:               broll,
		Captions:            captions,
		CaptionStyle:        captionStyle,
		Output: models.StoryOutput{
			AspectRatio:  "9:16",
			MaxDurationS: max(1, int(math.Ceil(request.DesiredDuration.Seconds()))),
			Preset:       "premium-memory",
		},
		TotalDurationMS: totalDuration,
		CreatedAt:       now,
	}
	EnforceSceneTransitions(&plan, request.VideoAnalyses, visualStyle)
	applyCaptionColorInstruction(&plan, request.UserInstructions)
	if err := ValidateStoryPlan(plan); err != nil {
		if p != nil && p.Logger != nil {
			brollDurationMS := 0
			for _, overlay := range plan.BRoll {
				brollDurationMS += overlay.DurationMS
			}
			p.Logger.Warn("story_planner_local_output_invalid",
				slog.String("project_id", request.ProjectID),
				slog.Int("story_plan_version", plan.Version),
				slog.Int("moment_count", len(plan.Moments)),
				slog.Int("broll_count", len(plan.BRoll)),
				slog.Int("broll_duration_ms", brollDurationMS),
				slog.Int("broll_duration_limit_ms", maxBRollDurationMS(plan.TotalDurationMS)),
				slog.String("broll_preference", normalizedBRollPreference(request.BRollPreference)),
				slog.Any("error", err),
			)
		}
		return models.StoryPlan{}, err
	}
	return plan, nil
}

func (p *Planner) applyTypography(ctx context.Context, request Request, moments []models.StoryMoment, titleText string) []models.StoryMoment {
	planner := TypographyPlanner(DefaultTypographyPlanner{})
	if p != nil && p.TypographyPlanner != nil {
		planner = p.TypographyPlanner
	}
	input := TypographyPlanningInput{
		ProjectID:         request.ProjectID,
		StoryPlanID:       planID(request, moments),
		StoryPlanVersion:  versionFor(request.Version),
		TypographyVersion: TypographyPromptVersion,
		StoryTitle:        titleText,
		StoryType:         normalizeStoryType(request.StoryType),
		VisualStyle:       normalizeVisualStyle(request.VisualStyle),
		NarrativeSummary:  titleText,
		UserInstructions:  strings.TrimSpace(request.UserInstructions),
		Moments:           typographyContexts(request, moments),
	}
	started := time.Now()
	p.logger().Info("typography_planning_started", slog.String("project_id", request.ProjectID), slog.Int("story_plan_version", input.StoryPlanVersion), slog.String("provider", typographyPlannerName(planner)), slog.String("model", typographyPlannerModel(planner)), slog.Int("moment_count", len(input.Moments)))
	typographyPlan, err := planner.Generate(ctx, input)
	if err != nil {
		p.logger().Warn("typography_planning_failed", slog.String("project_id", request.ProjectID), slog.Int("story_plan_version", input.StoryPlanVersion), slog.String("provider", typographyPlannerName(planner)), slog.String("model", typographyPlannerModel(planner)), slog.Int64("latency_ms", time.Since(started).Milliseconds()), slog.Any("error", err))
		return moments
	}
	if len(typographyPlan.Overlays) == 0 {
		p.logger().Info("typography_disabled", slog.String("project_id", request.ProjectID), slog.Int("story_plan_version", input.StoryPlanVersion), slog.String("provider", typographyPlannerName(planner)), slog.String("model", typographyPlannerModel(planner)), slog.Int64("latency_ms", time.Since(started).Milliseconds()))
		return moments
	}
	updated := append([]models.StoryMoment(nil), moments...)
	applied := 0
	for index := range updated {
		overlay, ok := typographyPlan.Overlays[updated[index].MomentID]
		if !ok {
			continue
		}
		normalized, err := NormalizeTextOverlay(overlay, updated[index].DurationMS)
		if err != nil {
			p.logger().Warn("typography_validation_failed", slog.String("project_id", request.ProjectID), slog.Int("story_plan_version", input.StoryPlanVersion), slog.String("moment_id", updated[index].MomentID), slog.Any("error", err))
			continue
		}
		updated[index].Typography = &normalized
		applied++
	}
	p.logger().Info("typography_planning_completed", slog.String("project_id", request.ProjectID), slog.Int("story_plan_version", input.StoryPlanVersion), slog.String("provider", typographyPlannerName(planner)), slog.String("model", typographyPlannerModel(planner)), slog.Int64("latency_ms", time.Since(started).Milliseconds()), slog.Int("overlay_count", applied), slog.String("plan_hash", typographyPlanHash(typographyPlan)))
	return updated
}

func (p *Planner) logger() *slog.Logger {
	if p != nil && p.Logger != nil {
		return p.Logger
	}
	return slog.Default()
}

type namedTypographyPlanner interface {
	Name() string
}

type modeledTypographyPlanner interface {
	Model() string
}

func typographyPlannerName(planner TypographyPlanner) string {
	if named, ok := planner.(namedTypographyPlanner); ok {
		if name := strings.TrimSpace(named.Name()); name != "" {
			return name
		}
	}
	return "unknown"
}

func typographyPlannerModel(planner TypographyPlanner) string {
	if modeled, ok := planner.(modeledTypographyPlanner); ok {
		if model := strings.TrimSpace(modeled.Model()); model != "" {
			return model
		}
	}
	return "unknown"
}

func typographyContexts(request Request, moments []models.StoryMoment) []TypographyMomentContext {
	photoByID := make(map[string]models.PhotoAnalysis, len(request.PhotoAnalyses))
	for _, photo := range request.PhotoAnalyses {
		photoByID[photo.PhotoID] = photo
	}
	clusters := buildClusterIndex(request.MediaClusters)
	contexts := make([]TypographyMomentContext, 0, len(moments))
	for index, moment := range moments {
		assetID := firstNonEmpty(moment.AssetID, moment.MediaID)
		context := TypographyMomentContext{
			MomentID: moment.MomentID, AssetID: assetID, MediaType: moment.MediaType,
			Narrative: moment.Narrative, Caption: moment.Caption, Reason: moment.Reason,
			DurationMS: moment.DurationMS, Score: moment.Score, Index: index,
		}
		if index > 0 {
			context.PreviousNarrative = firstNonEmpty(moments[index-1].Narrative, moments[index-1].Caption, moments[index-1].Reason)
		}
		if index+1 < len(moments) {
			context.NextNarrative = firstNonEmpty(moments[index+1].Narrative, moments[index+1].Caption, moments[index+1].Reason)
		}
		if photo, ok := photoByID[assetID]; ok {
			context.Location = strings.TrimSpace(photo.Location)
			if photo.TakenAt != nil {
				context.TakenAt = photo.TakenAt.UTC().Format(time.RFC3339)
			}
			context.Labels = boundedStrings(photo.Labels, 12)
			context.People = boundedStrings(photo.People, 8)
		}
		if cluster := clusterForMediaValue(clusters, assetID); cluster.ID != "" {
			context.ClusterLabel = strings.TrimSpace(firstNonEmpty(cluster.Label, cluster.Location))
		}
		contexts = append(contexts, context)
	}
	return contexts
}

func boundedStrings(values []string, limit int) []string {
	if limit <= 0 {
		return nil
	}
	result := make([]string, 0, minInt(len(values), limit))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		key := strings.ToLower(trimmed)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, truncateText(trimmed, 40))
		if len(result) >= limit {
			break
		}
	}
	return result
}

func captionStyleFor(storyType models.StoryType, visualStyle models.VisualStyle) *models.CaptionStyle {
	switch {
	case visualStyle == models.VisualStyleMinimal || visualStyle == models.VisualStyleElegant:
		return &models.CaptionStyle{
			Preset: "clean_minimal", FontFamily: "inter", FontSize: 42, TextColor: "#FFFFFF", StrokeColor: "#000000", StrokeWidth: 2,
			BackgroundEnabled: false, Position: "bottom", BottomOffset: 150, MaxLines: 2, MaxCharactersPerLine: 34,
		}
	case visualStyle == models.VisualStylePlayful || storyType == models.StoryTypeBirthday || storyType == models.StoryTypeFriends || storyType == models.StoryTypePet:
		return &models.CaptionStyle{
			Preset: "playful_pop", FontFamily: "inter-bold", FontSize: 56, TextColor: "#FFFFFF", StrokeColor: "#111111", StrokeWidth: 5,
			BackgroundEnabled: true, BackgroundColor: "#111111", BackgroundOpacity: 0.28, Position: "bottom", BottomOffset: 150, MaxLines: 2, MaxCharactersPerLine: 28,
		}
	case visualStyle == models.VisualStyleCinematic || visualStyle == models.VisualStyleWarm:
		return &models.CaptionStyle{
			Preset: "cinematic_subtitle", FontFamily: "inter-bold", FontSize: 48, TextColor: "#FFFFFF", StrokeColor: "#000000", StrokeWidth: 4,
			BackgroundEnabled: true, BackgroundColor: "#000000", BackgroundOpacity: 0.32, Position: "bottom", BottomOffset: 150, MaxLines: 2, MaxCharactersPerLine: 32,
		}
	default:
		return &models.CaptionStyle{
			Preset: "bold_social", FontFamily: "inter-bold", FontSize: 52, TextColor: "#FFFFFF", StrokeColor: "#000000", StrokeWidth: 4,
			BackgroundEnabled: true, BackgroundColor: "#000000", BackgroundOpacity: 0.32, Position: "bottom", BottomOffset: 150, MaxLines: 2, MaxCharactersPerLine: 32,
		}
	}
}

func captionStylePreset(preset string, storyType models.StoryType, visualStyle models.VisualStyle) *models.CaptionStyle {
	switch strings.ToLower(strings.TrimSpace(preset)) {
	case "clean_minimal":
		return &models.CaptionStyle{
			Preset: "clean_minimal", FontFamily: "inter", FontSize: 42, TextColor: "#FFFFFF", StrokeColor: "#000000", StrokeWidth: 2,
			BackgroundEnabled: false, Position: "bottom", BottomOffset: 150, MaxLines: 2, MaxCharactersPerLine: 34,
		}
	case "playful_pop":
		return &models.CaptionStyle{
			Preset: "playful_pop", FontFamily: "inter-bold", FontSize: 56, TextColor: "#FFFFFF", StrokeColor: "#111111", StrokeWidth: 5,
			BackgroundEnabled: true, BackgroundColor: "#111111", BackgroundOpacity: 0.28, Position: "bottom", BottomOffset: 150, MaxLines: 2, MaxCharactersPerLine: 28,
		}
	case "cinematic_subtitle":
		return &models.CaptionStyle{
			Preset: "cinematic_subtitle", FontFamily: "inter-bold", FontSize: 48, TextColor: "#FFFFFF", StrokeColor: "#000000", StrokeWidth: 4,
			BackgroundEnabled: true, BackgroundColor: "#000000", BackgroundOpacity: 0.32, Position: "bottom", BottomOffset: 150, MaxLines: 2, MaxCharactersPerLine: 32,
		}
	case "bold_social":
		return &models.CaptionStyle{
			Preset: "bold_social", FontFamily: "inter-bold", FontSize: 52, TextColor: "#FFFFFF", StrokeColor: "#000000", StrokeWidth: 4,
			BackgroundEnabled: true, BackgroundColor: "#000000", BackgroundOpacity: 0.32, Position: "bottom", BottomOffset: 150, MaxLines: 2, MaxCharactersPerLine: 32,
		}
	default:
		return captionStyleFor(storyType, visualStyle)
	}
}

func applyCaptionColorInstruction(plan *models.StoryPlan, instructions string) bool {
	color, ok := captionTextColorFromInstruction(instructions)
	if !ok {
		return false
	}
	if plan.CaptionStyle == nil {
		plan.CaptionStyle = captionStyleFor(plan.StoryType, plan.VisualStyle)
	}
	if plan.CaptionStyle == nil {
		plan.CaptionStyle = &models.CaptionStyle{Preset: "clean_minimal"}
	}
	plan.CaptionStyle.TextColor = color
	if strings.TrimSpace(plan.CaptionStyle.FontFamily) == "" {
		plan.CaptionStyle.FontFamily = "inter"
	}
	if strings.TrimSpace(plan.CaptionStyle.StrokeColor) == "" {
		plan.CaptionStyle.StrokeColor = "#000000"
	}
	if plan.CaptionStyle.FontSize <= 0 {
		plan.CaptionStyle.FontSize = 42
	}
	if strings.TrimSpace(plan.CaptionStyle.Position) == "" {
		plan.CaptionStyle.Position = "bottom"
	}
	return true
}

func normalizedBRollPreference(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "none", "main_moments", "main-moments", "main moments", "off", "disabled":
		return "none"
	case "more", "more_details", "more-details", "more details", "extra", "rich":
		return "more"
	default:
		return ""
	}
}

func captionTextColorFromInstruction(instructions string) (string, bool) {
	text := strings.ToLower(strings.TrimSpace(instructions))
	if text == "" || !captionColorInstructionTargetsText(text) {
		return "", false
	}
	if hex, ok := firstHexColor(text); ok {
		return strings.ToUpper(hex), true
	}
	colors := []struct {
		name string
		hex  string
	}{
		{"red", "#FF3B30"},
		{"white", "#FFFFFF"},
		{"black", "#000000"},
		{"yellow", "#FFE066"},
		{"gold", "#FFD60A"},
		{"orange", "#FF9500"},
		{"blue", "#4DA3FF"},
		{"green", "#34C759"},
		{"pink", "#FF2D55"},
		{"purple", "#AF52DE"},
	}
	for _, color := range colors {
		if containsInstructionWord(text, color.name) {
			return color.hex, true
		}
	}
	return "", false
}

func captionColorInstructionTargetsText(text string) bool {
	return containsInstructionWord(text, "subtitle") ||
		containsInstructionWord(text, "subtitles") ||
		containsInstructionWord(text, "caption") ||
		containsInstructionWord(text, "captions") ||
		containsInstructionWord(text, "text") ||
		strings.Contains(text, "text overlay") ||
		strings.Contains(text, "text overlays")
}

func firstHexColor(text string) (string, bool) {
	for index := 0; index+7 <= len(text); index++ {
		if text[index] != '#' {
			continue
		}
		candidate := text[index : index+7]
		valid := true
		for _, char := range candidate[1:] {
			if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
				valid = false
				break
			}
		}
		if valid {
			return candidate, true
		}
	}
	return "", false
}

func containsInstructionWord(text, word string) bool {
	start := strings.Index(text, word)
	for start >= 0 {
		beforeOK := start == 0 || !isInstructionWordChar(rune(text[start-1]))
		afterIndex := start + len(word)
		afterOK := afterIndex >= len(text) || !isInstructionWordChar(rune(text[afterIndex]))
		if beforeOK && afterOK {
			return true
		}
		next := strings.Index(text[start+len(word):], word)
		if next < 0 {
			return false
		}
		start += len(word) + next
	}
	return false
}

func isInstructionWordChar(char rune) bool {
	return (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') || char == '_'
}

func ValidateStoryPlan(plan models.StoryPlan) error {
	if strings.TrimSpace(plan.SchemaVersion) != models.StoryPlanSchemaVersionV1 {
		return fmt.Errorf("invalid story plan schema %q", plan.SchemaVersion)
	}
	if strings.TrimSpace(plan.ID) == "" || strings.TrimSpace(plan.ProjectID) == "" || plan.Version <= 0 || plan.RequestedDurationMS <= 0 {
		return errors.New("story plan requires identity, version, and requested duration")
	}
	if !validStoryType(plan.StoryType) {
		return fmt.Errorf("invalid story type %q", plan.StoryType)
	}
	if !validVisualStyle(plan.VisualStyle) {
		return fmt.Errorf("invalid visual style %q", plan.VisualStyle)
	}
	if strings.TrimSpace(plan.TitleText) == "" || strings.TrimSpace(plan.EndingText) == "" {
		return errors.New("story plan requires title and ending")
	}
	if strings.TrimSpace(plan.Output.AspectRatio) == "" || strings.TrimSpace(plan.Output.Preset) == "" || plan.Output.MaxDurationS <= 0 {
		return errors.New("story plan output is incomplete")
	}
	if len(plan.Moments) == 0 {
		return errors.New("story plan requires at least one moment")
	}
	seenMoments := make(map[string]struct{}, len(plan.Moments))
	momentIDs := make(map[string]struct{}, len(plan.Moments))
	durationMS := 0
	for index, moment := range plan.Moments {
		mediaID := moment.AssetID
		if mediaID == "" {
			mediaID = moment.MediaID
		}
		if strings.TrimSpace(mediaID) == "" || moment.DurationMS <= 0 {
			return fmt.Errorf("moment %d requires a media reference and positive duration", index)
		}
		if _, exists := seenMoments[momentKey(moment)]; exists {
			return fmt.Errorf("duplicate moment %s", momentKey(moment))
		}
		seenMoments[momentKey(moment)] = struct{}{}
		if strings.TrimSpace(moment.MomentID) != "" {
			momentIDs[moment.MomentID] = struct{}{}
		}
		if moment.MediaType == models.MediaTypeVideo {
			if strings.TrimSpace(moment.VideoAnalysisVersion) == "" || moment.StartMicros < 0 || moment.EndMicros <= moment.StartMicros {
				return fmt.Errorf("video moment %s is missing valid analysis references", mediaID)
			}
		}
		if moment.Typography != nil {
			if err := ValidTextOverlay(*moment.Typography, moment.DurationMS); err != nil {
				return fmt.Errorf("moment %s typography is invalid: %w", mediaID, err)
			}
		}
		if len(moment.Caption) > maxStoryPlanCaptionLength {
			return fmt.Errorf("moment %s caption must be at most %d characters", mediaID, maxStoryPlanCaptionLength)
		}
		if strings.TrimSpace(moment.CropIntent) == "" || strings.TrimSpace(moment.Transition) == "" {
			return fmt.Errorf("moment %s requires crop intent and transition", mediaID)
		}
		durationMS += moment.DurationMS
	}
	if plan.TotalDurationMS > 0 && durationMS != plan.TotalDurationMS {
		return fmt.Errorf("story plan duration mismatch: moments=%d total=%d", durationMS, plan.TotalDurationMS)
	}
	if err := validateBRollOverlays(plan, momentIDs, durationMS); err != nil {
		return err
	}
	if len(plan.Captions) > 0 {
		for _, caption := range plan.Captions {
			if strings.TrimSpace(caption.MomentID) == "" || strings.TrimSpace(caption.Text) == "" {
				return errors.New("captions require a moment reference and text")
			}
			if len(caption.Text) > maxStoryPlanCaptionLength {
				return fmt.Errorf("caption must be at most %d characters", maxStoryPlanCaptionLength)
			}
			if _, exists := momentIDs[caption.MomentID]; !exists {
				return fmt.Errorf("caption references unknown moment %s", caption.MomentID)
			}
		}
	}
	return nil
}

func validateBRollOverlays(plan models.StoryPlan, momentIDs map[string]struct{}, totalDurationMS int) error {
	seen := make(map[string]struct{}, len(plan.BRoll))
	usedDurationMS := 0
	for index, overlay := range plan.BRoll {
		assetID := strings.TrimSpace(firstNonEmpty(overlay.AssetID, overlay.MediaID))
		if assetID == "" || overlay.DurationMS <= 0 {
			return fmt.Errorf("broll %d requires a media reference and positive duration", index)
		}
		if overlay.StartMS < 0 || overlay.StartMS+overlay.DurationMS > totalDurationMS {
			return fmt.Errorf("broll %s timing is outside story duration", assetID)
		}
		if overlay.MediaType == models.MediaTypeVideo && (strings.TrimSpace(overlay.VideoAnalysisVersion) == "" || overlay.SourceStartMicros < 0 || overlay.SourceEndMicros <= overlay.SourceStartMicros) {
			return fmt.Errorf("video broll %s is missing valid source references", assetID)
		}
		if overlay.MediaType != models.MediaTypeVideo && (overlay.SourceStartMicros != 0 || overlay.SourceEndMicros != 0) {
			return fmt.Errorf("photo broll %s must not define source timestamps", assetID)
		}
		if overlay.Anchor != nil {
			if overlay.Anchor.StartMS < 0 || overlay.Anchor.EndMS <= overlay.Anchor.StartMS || overlay.Anchor.EndMS > totalDurationMS {
				return fmt.Errorf("broll %s anchor timing is invalid", assetID)
			}
			if strings.TrimSpace(overlay.Anchor.MomentID) != "" {
				if _, exists := momentIDs[overlay.Anchor.MomentID]; !exists {
					return fmt.Errorf("broll %s references unknown anchor moment %s", assetID, overlay.Anchor.MomentID)
				}
			}
		}
		key := fmt.Sprintf("%s:%s:%d:%d:%d:%d", assetID, overlay.VideoAnalysisVersion, overlay.SourceStartMicros, overlay.SourceEndMicros, overlay.StartMS, overlay.DurationMS)
		if _, exists := seen[key]; exists {
			return fmt.Errorf("duplicate broll reference %s", key)
		}
		seen[key] = struct{}{}
		usedDurationMS += overlay.DurationMS
	}
	if totalDurationMS > 0 && usedDurationMS > maxBRollDurationMS(totalDurationMS)+1 {
		return fmt.Errorf("broll duration exceeds story ratio")
	}
	return nil
}

func (p *Planner) now() time.Time {
	if p != nil && p.Now != nil {
		return p.Now().UTC()
	}
	return time.Now().UTC()
}

func versionFor(value int) int {
	if value > 0 {
		return value
	}
	return 1
}

func buildCandidates(request Request, selected map[string]struct{}, clusters map[string]models.MediaCluster) []candidate {
	candidates := make([]candidate, 0, len(request.PhotoAnalyses)+len(request.VideoAnalyses))
	for _, photo := range request.PhotoAnalyses {
		if photo.ProjectID != "" && photo.ProjectID != request.ProjectID {
			continue
		}
		mediaID := strings.TrimSpace(photo.PhotoID)
		if mediaID == "" {
			continue
		}
		clusterID := strings.TrimSpace(photo.ClusterID)
		if clusterID == "" {
			clusterID = clusterForMedia(clusters, mediaID)
		}
		timestamp := photo.TimestampMicros
		if timestamp == 0 && photo.TakenAt != nil {
			timestamp = photo.TakenAt.UTC().UnixMicro()
		}
		if timestamp == 0 {
			timestamp = requestTimestampFallback(request)
		}
		duration := photoDuration(request.StoryType, photo)
		caption := photoMomentTitle(photo)
		candidates = append(candidates, candidate{
			moment: models.StoryMoment{
				AssetID: mediaID, MediaID: mediaID, MediaType: models.MediaTypePhoto,
				Caption: caption, Narrative: caption, DurationMS: duration,
				Reason: photoReason(photo), Score: photoScore(photo, selected, clusterID),
			},
			sourceTime: timestamp, score: photoScore(photo, selected, clusterID),
			repetition: repetitionKey(mediaID, clusterID, timestamp, "photo"), mediaID: mediaID, clusterID: clusterID,
			semanticTerms: photoSemanticTerms(photo),
			isSelected:    hasMedia(selected, mediaID),
		})
	}
	for _, analysis := range request.VideoAnalyses {
		mediaID := strings.TrimSpace(analysis.VideoID)
		if mediaID == "" {
			continue
		}
		if analysis.ProjectID != "" && analysis.ProjectID != request.ProjectID && !hasMedia(selected, mediaID) {
			continue
		}
		mediaBaseTime := mediaBaseTimeMicros(clusters, mediaID)
		for _, segment := range analysis.Segments {
			if strings.TrimSpace(segment.SegmentID) == "" || strings.TrimSpace(segment.SceneID) == "" {
				continue
			}
			if excludedVideoUsage(segment.RecommendedUsage) {
				continue
			}
			if segment.EndMicros <= segment.StartMicros || segment.StartMicros < 0 || segment.EndMicros > analysis.DurationMicros {
				continue
			}
			clusterID := clusterForMedia(clusters, mediaID)
			score := videoScore(analysis, segment, selected, clusterID)
			endMicros := selectedVideoEndMicros(segment)
			caption := captionFromSegment(segment)
			reason := reasonFromSegment(segment)
			narrative := firstNonEmpty(caption, reason)
			moment := models.StoryMoment{
				AssetID: mediaID, MediaID: mediaID, MediaType: models.MediaTypeVideo,
				VideoAnalysisVersion: analysis.AnalysisVersion,
				StartMicros:          segment.StartMicros, EndMicros: endMicros,
				Narrative: narrative, Caption: caption,
				DurationMS: videoDuration(segment), Reason: reason, Score: score,
				CropIntent: cropIntentFor(analysis, segment), Transition: "cut",
			}
			if clusterID != "" {
				if moment.Reason != "" {
					moment.Reason = truncateText(moment.Reason+" · cluster:"+clusterID, 240)
				} else {
					moment.Reason = truncateText("cluster:"+clusterID, 240)
				}
			}
			segmentTime := midpoint(segment.StartMicros, segment.EndMicros)
			candidates = append(candidates, candidate{
				moment: moment, captionCues: transcriptCaptionCues(analysis.Transcript, segment.StartMicros, endMicros),
				sourceTime: sourceTimelineMicros(mediaBaseTime, segmentTime), score: score,
				repetition: repetitionKey(mediaID, clusterID, segmentTime, segment.SceneID),
				mediaID:    mediaID, sceneID: segment.SceneID, clusterID: clusterID, semanticTerms: videoSegmentSemanticTerms(segment), isVideo: true, isSelected: hasMedia(selected, mediaID),
			})
		}
	}
	return candidates
}

func mediaBaseTimeMicros(clusters map[string]models.MediaCluster, mediaID string) int64 {
	cluster := clusterForMediaValue(clusters, mediaID)
	if cluster.StartAt != nil {
		return cluster.StartAt.UTC().UnixMicro()
	}
	return 0
}

func sourceTimelineMicros(baseMicros, offsetMicros int64) int64 {
	if baseMicros <= 0 {
		return offsetMicros
	}
	return baseMicros + maxInt64(0, offsetMicros)
}

func clusterForMediaValue(clusters map[string]models.MediaCluster, mediaID string) models.MediaCluster {
	for _, cluster := range clusters {
		for _, entry := range cluster.MediaIDs {
			if entry == mediaID {
				return cluster
			}
		}
	}
	return models.MediaCluster{}
}

func filterExcludedCandidates(candidates []candidate, excludedMedia, excludedMoments map[string]struct{}) []candidate {
	if len(excludedMedia) == 0 && len(excludedMoments) == 0 {
		return candidates
	}
	filtered := make([]candidate, 0, len(candidates))
	for _, candidate := range candidates {
		if _, excluded := excludedMedia[candidate.mediaID]; excluded {
			continue
		}
		if _, excluded := excludedMoments[momentKey(candidate.moment)]; excluded {
			continue
		}
		filtered = append(filtered, candidate)
	}
	return filtered
}

func sortCandidatesForStory(candidates []candidate, preferredMediaOrder []string) {
	order := make(map[string]int, len(preferredMediaOrder))
	for index, mediaID := range preferredMediaOrder {
		mediaID = strings.TrimSpace(mediaID)
		if mediaID == "" {
			continue
		}
		if _, exists := order[mediaID]; !exists {
			order[mediaID] = index
		}
	}
	sort.SliceStable(candidates, func(left, right int) bool {
		leftOrder, leftPreferred := order[candidates[left].mediaID]
		rightOrder, rightPreferred := order[candidates[right].mediaID]
		if leftPreferred || rightPreferred {
			if leftPreferred != rightPreferred {
				return leftPreferred
			}
			if leftOrder != rightOrder {
				return leftOrder < rightOrder
			}
		}
		if candidates[left].sourceTime == candidates[right].sourceTime {
			return candidates[left].score > candidates[right].score
		}
		return candidates[left].sourceTime < candidates[right].sourceTime
	})
}

func applyInstructionPreferences(candidates []candidate, instructions string) {
	instructionTerms := semanticTermsFromText(instructions)
	if len(instructionTerms) == 0 {
		return
	}
	pacingMultiplier := instructionPacingMultiplier(instructions)
	for index := range candidates {
		match := semanticMatch(instructionTerms, candidates[index].semanticTerms)
		if match > 0 {
			boost := 0.08 + 0.22*match
			candidates[index].score = math.Min(1, candidates[index].score+boost)
			candidates[index].moment.Score = math.Min(1, candidates[index].moment.Score+boost)
		}
		if pacingMultiplier != 1 && candidates[index].moment.MediaType == models.MediaTypePhoto {
			candidates[index].moment.DurationMS = clampInt(int(math.Round(float64(candidates[index].moment.DurationMS)*pacingMultiplier)), 1800, 5200)
		}
	}
}

func instructionPacingMultiplier(instructions string) float64 {
	text := strings.ToLower(instructions)
	if strings.Contains(text, "slow") || strings.Contains(text, "slower") || strings.Contains(text, "calm") || strings.Contains(text, "gentle") || strings.Contains(text, "emotional") || strings.Contains(text, "meaningful") {
		return 1.18
	}
	if strings.Contains(text, "fast") || strings.Contains(text, "faster") || strings.Contains(text, "quick") || strings.Contains(text, "upbeat") || strings.Contains(text, "energetic") || strings.Contains(text, "fun") {
		return 0.88
	}
	return 1
}

func selectCandidates(candidates []candidate, desired time.Duration) []candidate {
	if len(candidates) == 0 {
		return nil
	}
	sort.Slice(candidates, func(left, right int) bool {
		if candidates[left].score == candidates[right].score {
			return candidates[left].sourceTime < candidates[right].sourceTime
		}
		return candidates[left].score > candidates[right].score
	})
	selected := make([]candidate, 0, minInt(len(candidates), max(1, int(math.Ceil(desired.Seconds()/4)))))
	seenKeys := make(map[string]struct{}, len(candidates))
	selectedPerMedia := make(map[string]int, len(candidates))
	targetMS := int(desired / time.Millisecond)
	if targetMS <= 0 {
		targetMS = 15000
	}
	totalMS := 0
	addCandidate := func(entry candidate, force bool) bool {
		if _, exists := seenKeys[entry.repetition]; exists {
			return false
		}
		if selectedPerMedia[entry.mediaID] >= 2 && !force {
			return false
		}
		if totalMS+entry.moment.DurationMS > targetMS && len(selected) > 0 {
			return false
		}
		selected = append(selected, entry)
		seenKeys[entry.repetition] = struct{}{}
		selectedPerMedia[entry.mediaID]++
		totalMS += entry.moment.DurationMS
		return true
	}
	for _, candidate := range bestCandidatePerMedia(candidates) {
		addCandidate(candidate, true)
	}
	for _, candidate := range candidates {
		if totalMS >= targetMS && len(selected) > 0 {
			break
		}
		addCandidate(candidate, false)
	}
	if len(selected) == 0 {
		selected = append(selected, candidates[0])
	}
	if totalMS > targetMS {
		selected = trimToDuration(selected, targetMS)
	}
	return selected
}

func bestCandidatePerMedia(candidates []candidate) []candidate {
	bestByMedia := make(map[string]candidate, len(candidates))
	for _, entry := range candidates {
		current, exists := bestByMedia[entry.mediaID]
		if !exists || entry.score > current.score || (entry.score == current.score && entry.sourceTime < current.sourceTime) {
			bestByMedia[entry.mediaID] = entry
		}
	}
	best := make([]candidate, 0, len(bestByMedia))
	for _, entry := range bestByMedia {
		best = append(best, entry)
	}
	sort.Slice(best, func(left, right int) bool {
		if best[left].isSelected != best[right].isSelected {
			return best[left].isSelected
		}
		if best[left].isVideo != best[right].isVideo {
			return !best[left].isVideo
		}
		if best[left].moment.DurationMS == best[right].moment.DurationMS {
			if best[left].score == best[right].score {
				return best[left].sourceTime < best[right].sourceTime
			}
			return best[left].score > best[right].score
		}
		return best[left].moment.DurationMS < best[right].moment.DurationMS
	})
	return best
}

func trimToDuration(candidates []candidate, targetMS int) []candidate {
	sorted := append([]candidate(nil), candidates...)
	sort.Slice(sorted, func(left, right int) bool {
		if sorted[left].score == sorted[right].score {
			return sorted[left].moment.DurationMS < sorted[right].moment.DurationMS
		}
		return sorted[left].score < sorted[right].score
	})
	total := 0
	for _, candidate := range sorted {
		total += candidate.moment.DurationMS
	}
	for total > targetMS && len(sorted) > 1 {
		removeIndex := firstRemovableCandidate(sorted)
		if removeIndex < 0 {
			break
		}
		removed := sorted[removeIndex]
		sorted = append(sorted[:removeIndex], sorted[removeIndex+1:]...)
		total -= removed.moment.DurationMS
	}
	sort.Slice(sorted, func(left, right int) bool {
		if sorted[left].sourceTime == sorted[right].sourceTime {
			return sorted[left].score > sorted[right].score
		}
		return sorted[left].sourceTime < sorted[right].sourceTime
	})
	return sorted
}

func firstRemovableCandidate(candidates []candidate) int {
	videoCount := 0
	photoCount := 0
	for _, entry := range candidates {
		if entry.isVideo {
			videoCount++
		} else {
			photoCount++
		}
	}
	for index, entry := range candidates {
		if entry.isVideo && videoCount == 1 && photoCount > 0 {
			continue
		}
		if !entry.isVideo && photoCount == 1 && videoCount > 0 {
			continue
		}
		return index
	}
	return -1
}

func validateRequest(request Request) error {
	if strings.TrimSpace(request.ProjectID) == "" || request.DesiredDuration <= 0 {
		return errors.New("story planner requires a project and duration")
	}
	if !validStoryType(request.StoryType) {
		return fmt.Errorf("unsupported story type %q", request.StoryType)
	}
	if request.VisualStyle != "" && !validVisualStyle(request.VisualStyle) {
		return fmt.Errorf("unsupported visual style %q", request.VisualStyle)
	}
	if len(request.PhotoAnalyses) == 0 && len(request.VideoAnalyses) == 0 {
		return errors.New("story planner requires photo or video analyses")
	}
	return nil
}

func validStoryType(value models.StoryType) bool {
	switch value {
	case models.StoryTypeToday, models.StoryTypeWeekend, models.StoryTypeTrip, models.StoryTypeBirthday,
		models.StoryTypeFamily, models.StoryTypeFriends, models.StoryTypePet, models.StoryTypeEvent, models.StoryTypeCustom:
		return true
	default:
		return false
	}
}

func validVisualStyle(value models.VisualStyle) bool {
	switch value {
	case "", models.VisualStyleNatural, models.VisualStyleCinematic, models.VisualStyleCalm, models.VisualStyleWarm, models.VisualStylePlayful, models.VisualStyleMinimal, models.VisualStyleElegant:
		return true
	default:
		return false
	}
}

func normalizeStoryType(value models.StoryType) models.StoryType {
	if validStoryType(value) {
		return value
	}
	return models.StoryTypeCustom
}

func normalizeVisualStyle(value models.VisualStyle) models.VisualStyle {
	if validVisualStyle(value) && value != "" {
		return value
	}
	return models.VisualStyleCinematic
}

func titleFor(storyType models.StoryType, request Request, moments []models.StoryMoment) string {
	if storyType == models.StoryTypeCustom || storyType == models.StoryTypeToday || storyType == models.StoryTypeWeekend {
		if title := titleFromMoments(request, moments); title != "" {
			return title
		}
	}
	switch storyType {
	case models.StoryTypeToday:
		return "Your Day"
	case models.StoryTypeWeekend:
		return "A Weekend to Remember"
	case models.StoryTypeTrip:
		return titleWithLocation("A Trip Worth Remembering", request)
	case models.StoryTypeBirthday:
		return "A Birthday Worth Holding Onto"
	case models.StoryTypeFamily:
		return "Family, Remembered Beautifully"
	case models.StoryTypeFriends:
		return "Moments With Friends"
	case models.StoryTypePet:
		return "Little Moments, Big Love"
	case models.StoryTypeEvent:
		return titleWithLocation("Event Highlights", request)
	case models.StoryTypeCustom:
		if trimmed := humanText(request.UserInstructions); trimmed != "" {
			return truncateText(trimmed, 42)
		}
		fallthrough
	default:
		return "My Story"
	}
}

func endingFor(storyType models.StoryType, request Request, moments []models.StoryMoment) string {
	switch storyType {
	case models.StoryTypeTrip:
		return "A place, a feeling, a story to return to."
	case models.StoryTypeBirthday:
		return "One more candle, one more memory."
	case models.StoryTypeFamily, models.StoryTypeFriends, models.StoryTypePet:
		return "The moments that stay with us."
	default:
		if len(moments) > 0 {
			return "A story made from the moments that mattered most."
		}
		return ""
	}
}

func titleFromMoments(request Request, moments []models.StoryMoment) string {
	if len(request.MediaClusters) == 1 {
		cluster := request.MediaClusters[0]
		if label := humanText(cluster.Label); label != "" {
			if location := humanText(cluster.Location); location != "" {
				return truncateText(toDisplayTitle(label+" in "+location), 42)
			}
			return truncateText(toDisplayTitle(label), 42)
		}
	}
	for _, moment := range moments {
		for _, value := range []string{moment.Caption, moment.Narrative, moment.Reason} {
			if text := humanText(value); text != "" {
				return truncateText(toDisplayTitle(text), 42)
			}
		}
	}
	if location := firstNonEmpty(request.Locations...); location != "" {
		return truncateText("Memories in "+location, 42)
	}
	return ""
}

func titleWithLocation(prefix string, request Request) string {
	if location := firstNonEmpty(request.Locations...); location != "" {
		return prefix + " in " + location
	}
	return prefix
}

func buildClusterIndex(clusters []models.MediaCluster) map[string]models.MediaCluster {
	index := make(map[string]models.MediaCluster, len(clusters))
	for _, cluster := range clusters {
		for _, mediaID := range cluster.MediaIDs {
			if _, exists := index[mediaID]; !exists {
				index[mediaID] = cluster
			}
		}
	}
	return index
}

func clusterForMedia(index map[string]models.MediaCluster, mediaID string) string {
	if cluster, ok := index[mediaID]; ok {
		return cluster.ID
	}
	return ""
}

func photoScore(photo models.PhotoAnalysis, selected map[string]struct{}, clusterID string) float64 {
	score := 0.35*clamp(photo.MemoryValue) + 0.25*clamp(photo.EmotionalValue) + 0.2*clamp(photo.StoryRelevance) + 0.15*clamp(photo.VisualQuality)
	if photo.Selected || hasMedia(selected, photo.PhotoID) {
		score += 0.15
	}
	if clusterID != "" {
		score += 0.05
	}
	return math.Min(1, score)
}

func excludedVideoUsage(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "exclude", "audio_only":
		return true
	default:
		return false
	}
}

func videoScore(analysis models.VideoAnalysis, segment models.VideoSegment, selected map[string]struct{}, clusterID string) float64 {
	score := 0.3*clamp(segment.Importance) + 0.2*clamp(segment.MemoryValue) + 0.2*clamp(segment.EmotionalValue) + 0.15*clamp(segment.StoryRelevance) + 0.1*clamp(segment.VisualQuality) + 0.05*clamp(analysis.QualitySignals.OverallScore)
	if segment.Recommended {
		score += 0.1
	}
	if hasMedia(selected, analysis.VideoID) {
		score += 0.15
	}
	if clusterID != "" {
		score += 0.05
	}
	return math.Min(1, score)
}

func photoDuration(storyType models.StoryType, photo models.PhotoAnalysis) int {
	base := 3000
	switch storyType {
	case models.StoryTypeTrip, models.StoryTypeEvent:
		base = 2600
	case models.StoryTypeBirthday, models.StoryTypeFriends, models.StoryTypePet:
		base = 3400
	}
	if photo.Caption != "" || photo.Description != "" {
		base += 200
	}
	if photo.VisualQuality < 0.4 {
		base -= 300
	}
	return clampInt(base, 2200, 4500)
}

func videoDuration(segment models.VideoSegment) int {
	duration := int((selectedVideoEndMicros(segment) - segment.StartMicros) / 1000)
	if duration < 1 {
		duration = 1
	}
	return duration
}

func selectedVideoEndMicros(segment models.VideoSegment) int64 {
	return min(segment.EndMicros, segment.StartMicros+maxVideoMomentDuration.Microseconds())
}

func captionFromSegment(segment models.VideoSegment) string {
	if text := humanText(segment.Description); text != "" && (segment.Recommended || segment.StoryRelevance >= 0.7) {
		return truncateText(text, maxStoryPlanCaptionLength)
	}
	for _, reason := range segment.Reasons {
		if text := humanText(reason); text != "" {
			return truncateText(text, maxStoryPlanCaptionLength)
		}
	}
	return ""
}

func captionFromTranscript(transcript models.VideoTranscript, startMicros, endMicros int64) string {
	if !transcript.HasSpeech || endMicros <= startMicros {
		return ""
	}
	parts := make([]string, 0, len(transcript.Segments))
	for _, segment := range transcript.Segments {
		if segment.EndMicros <= segment.StartMicros || segment.EndMicros <= startMicros || segment.StartMicros >= endMicros {
			continue
		}
		overlapStart := maxInt64(segment.StartMicros, startMicros)
		overlapEnd := minInt64(segment.EndMicros, endMicros)
		if overlapEnd-overlapStart < 250_000 {
			continue
		}
		if text := humanText(segment.Text); text != "" {
			parts = append(parts, text)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return truncateText(strings.Join(parts, " "), 120)
}

func transcriptCaptionCues(transcript models.VideoTranscript, startMicros, endMicros int64) []captionCue {
	if !transcript.HasSpeech || endMicros <= startMicros {
		return nil
	}
	cues := make([]captionCue, 0, len(transcript.Segments))
	for _, segment := range transcript.Segments {
		if segment.EndMicros <= segment.StartMicros || segment.EndMicros <= startMicros || segment.StartMicros >= endMicros {
			continue
		}
		overlapStart := maxInt64(segment.StartMicros, startMicros)
		overlapEnd := minInt64(segment.EndMicros, endMicros)
		if overlapEnd-overlapStart < 250_000 {
			continue
		}
		if text := humanText(segment.Text); text != "" {
			cues = append(cues, captionCue{Text: truncateText(text, 120), StartMicros: overlapStart, EndMicros: overlapEnd})
		}
	}
	return cues
}

func timedStoryCaptions(moment models.StoryMoment, cues []captionCue, timelineStartMS int) []models.StoryCaption {
	if strings.TrimSpace(moment.MomentID) == "" || moment.EndMicros <= moment.StartMicros || moment.DurationMS <= 0 {
		return nil
	}
	sourceDurationMS := float64(moment.EndMicros-moment.StartMicros) / 1000
	if sourceDurationMS <= 0 {
		return nil
	}
	scale := float64(moment.DurationMS) / sourceDurationMS
	captions := make([]models.StoryCaption, 0, len(cues))
	for _, cue := range cues {
		text := strings.TrimSpace(cue.Text)
		if text == "" || cue.EndMicros <= cue.StartMicros {
			continue
		}
		startMS := timelineStartMS + int(math.Round((float64(cue.StartMicros-moment.StartMicros)/1000)*scale))
		endMS := timelineStartMS + int(math.Round((float64(cue.EndMicros-moment.StartMicros)/1000)*scale))
		startMS = clampInt(startMS, timelineStartMS, timelineStartMS+moment.DurationMS)
		endMS = clampInt(endMS, startMS, timelineStartMS+moment.DurationMS)
		if endMS-startMS < 250 {
			continue
		}
		captions = append(captions, models.StoryCaption{MomentID: moment.MomentID, Text: text, StartMS: startMS, EndMS: endMS})
	}
	return captions
}

func reasonFromSegment(segment models.VideoSegment) string {
	parts := make([]string, 0, len(segment.Reasons))
	for _, reason := range segment.Reasons {
		if text := humanText(reason); text != "" {
			parts = append(parts, text)
		}
	}
	if len(parts) > 0 {
		return truncateText(strings.Join(parts, " · "), 240)
	}
	return truncateText(humanText(segment.Description), 240)
}

func photoMomentTitle(photo models.PhotoAnalysis) string {
	if text := humanText(photo.Caption); text != "" {
		return truncateText(text, maxStoryPlanCaptionLength)
	}
	if text := humanText(photo.Description); text != "" {
		return truncateText(text, maxStoryPlanCaptionLength)
	}
	if text := humanText(photo.Location); text != "" {
		return truncateText(text+" memory", maxStoryPlanCaptionLength)
	}
	return ""
}

func photoReason(photo models.PhotoAnalysis) string {
	if text := humanText(photo.Description); text != "" {
		return text
	}
	if text := humanText(photo.Caption); text != "" {
		return text
	}
	if text := humanText(photo.Location); text != "" {
		return text + " memory"
	}
	return ""
}

func cropIntentFor(analysis models.VideoAnalysis, segment models.VideoSegment) string {
	if analysis.QualitySignals.LikelyStatic || segment.VisualQuality >= 0.8 {
		return "fit"
	}
	return "fill"
}

func transitionFor(previous, current candidate, style models.VisualStyle) string {
	if previous.sceneID != "" && current.sceneID != "" && previous.sceneID != current.sceneID {
		if style == models.VisualStyleCalm || style == models.VisualStyleMinimal {
			return "crossfade"
		}
		return "fade"
	}
	if previous.mediaID == current.mediaID {
		return "cut"
	}
	if style == models.VisualStyleCalm || style == models.VisualStyleMinimal {
		return "crossfade"
	}
	return "fade"
}

func EnforceSceneTransitions(plan *models.StoryPlan, videoAnalyses []models.VideoAnalysis, style models.VisualStyle) {
	if plan == nil || len(plan.Moments) < 2 {
		return
	}
	videoScenes := videoSceneLookup(videoAnalyses)
	for index := 1; index < len(plan.Moments); index++ {
		previous := plan.Moments[index-1]
		current := &plan.Moments[index]
		previousMediaID := strings.TrimSpace(firstNonEmpty(previous.MediaID, previous.AssetID))
		currentMediaID := strings.TrimSpace(firstNonEmpty(current.MediaID, current.AssetID))
		if previousMediaID == "" || currentMediaID == "" || previousMediaID != currentMediaID {
			continue
		}
		previousSceneID := sceneIDForRange(videoScenes[previousMediaID], previous.StartMicros, previous.EndMicros)
		currentSceneID := sceneIDForRange(videoScenes[currentMediaID], current.StartMicros, current.EndMicros)
		if previousSceneID == "" || currentSceneID == "" || previousSceneID == currentSceneID {
			continue
		}
		current.Transition = sceneBoundaryTransition(style)
	}
}

func videoSceneLookup(analyses []models.VideoAnalysis) map[string][]models.VideoScene {
	result := make(map[string][]models.VideoScene, len(analyses))
	for _, analysis := range analyses {
		mediaID := strings.TrimSpace(analysis.VideoID)
		if mediaID == "" || len(analysis.Scenes) == 0 {
			continue
		}
		scenes := append([]models.VideoScene(nil), analysis.Scenes...)
		sort.SliceStable(scenes, func(left, right int) bool { return scenes[left].StartMicros < scenes[right].StartMicros })
		result[mediaID] = scenes
	}
	return result
}

func sceneIDForRange(scenes []models.VideoScene, startMicros, endMicros int64) string {
	if len(scenes) == 0 || endMicros <= startMicros {
		return ""
	}
	midpointMicros := midpoint(startMicros, endMicros)
	for _, scene := range scenes {
		if strings.TrimSpace(scene.SceneID) == "" {
			continue
		}
		if midpointMicros >= scene.StartMicros && midpointMicros < scene.EndMicros {
			return scene.SceneID
		}
	}
	return ""
}

func sceneBoundaryTransition(style models.VisualStyle) string {
	if style == models.VisualStyleCalm || style == models.VisualStyleMinimal {
		return "crossfade"
	}
	return "fade"
}

func shouldCaption(entry candidate) bool {
	return strings.TrimSpace(entry.moment.Caption) != "" && (entry.moment.MediaType == models.MediaTypePhoto || entry.moment.Score >= 0.75)
}

func planID(request Request, moments []models.StoryMoment) string {
	hash := sha256.New()
	for _, value := range []string{request.ProjectID, string(request.StoryType), string(request.VisualStyle), request.UserInstructions, fmt.Sprintf("%d", request.Version)} {
		_, _ = hash.Write([]byte(value))
	}
	for _, moment := range moments {
		_, _ = hash.Write([]byte(momentKey(moment)))
	}
	return "plan_" + hex.EncodeToString(hash.Sum(nil))[:24]
}

func momentID(entry candidate) string {
	return momentKey(entry.moment)
}

func momentKey(moment models.StoryMoment) string {
	mediaID := moment.AssetID
	if mediaID == "" {
		mediaID = moment.MediaID
	}
	return fmt.Sprintf("%s:%s:%d:%d", mediaID, moment.VideoAnalysisVersion, moment.StartMicros, moment.EndMicros)
}

func repetitionKey(mediaID, clusterID string, timestampMicros int64, suffix string) string {
	bucket := timestampMicros / (15 * int64(time.Minute/time.Microsecond))
	if clusterID != "" {
		return fmt.Sprintf("%s:%s:%s:%d", clusterID, mediaID, suffix, bucket)
	}
	return fmt.Sprintf("%s:%s:%d", mediaID, suffix, bucket)
}

func requestTimestampFallback(request Request) int64 {
	if len(request.Timestamps) > 0 {
		return request.Timestamps[0].UTC().UnixMicro()
	}
	return time.Now().UTC().UnixMicro()
}

func humanText(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || isGenericMediaText(trimmed) || isTechnicalMediaText(trimmed) {
		return ""
	}
	trimmed = strings.Split(trimmed, " · cluster:")[0]
	trimmed = strings.TrimSpace(trimmed)
	if trimmed == "" || isGenericMediaText(trimmed) || isTechnicalMediaText(trimmed) {
		return ""
	}
	return strings.Trim(trimmed, " .")
}

func isGenericMediaText(value string) bool {
	normalized := strings.ToLower(strings.TrimSpace(value))
	normalized = strings.TrimSuffix(normalized, ".")
	switch normalized {
	case "selected moments", "selected moments story", "special moments", "special moments story", "your moments", "your day in moments", "a memory story",
		"opening moment", "the next memory", "a favorite part",
		"photo moment", "video moment", "live photo moment", "a meaningful photo moment",
		"first uploaded image", "updated image", "uploaded image", "image", "photo", "video",
		"strong opening video moment", "strong middle video moment", "strong ending video moment":
		return true
	default:
		return strings.HasPrefix(normalized, "photo moment ") || strings.HasPrefix(normalized, "video moment ") || strings.HasPrefix(normalized, "moment ")
	}
}

func isTechnicalMediaText(value string) bool {
	normalized := strings.ToLower(strings.TrimSpace(value))
	normalized = strings.ReplaceAll(normalized, "-", "_")
	technicalTokens := []string{
		"transcript_segment_index",
		"transcript_segment_indexes",
		"transcript segment index",
		"transcript segment indexes",
		"frame_id",
		"frame_ids",
		"frame index",
		"frame indexes",
		"segment_id",
		"scene_id",
	}
	for _, token := range technicalTokens {
		if strings.Contains(normalized, token) {
			return true
		}
	}
	return false
}

func toDisplayTitle(value string) string {
	words := strings.Fields(value)
	if len(words) == 0 {
		return ""
	}
	if len(words) > 6 {
		words = words[:6]
	}
	minor := map[string]struct{}{
		"a": {}, "an": {}, "and": {}, "at": {}, "by": {}, "for": {}, "in": {}, "of": {}, "on": {}, "the": {}, "to": {}, "with": {},
	}
	for index, word := range words {
		lower := strings.ToLower(strings.Trim(word, " .,;:!?"))
		if lower == "" {
			continue
		}
		if _, isMinor := minor[lower]; isMinor && index != 0 {
			words[index] = lower
			continue
		}
		words[index] = strings.ToUpper(lower[:1]) + lower[1:]
	}
	return strings.Join(words, " ")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func hasMedia(set map[string]struct{}, mediaID string) bool {
	_, ok := set[mediaID]
	return ok
}

func truncateText(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if len(value) <= limit {
		return value
	}
	suffix := "…"
	if limit < len(suffix) {
		return strings.Repeat(".", limit)
	}
	if limit == len(suffix) {
		return suffix
	}
	var builder strings.Builder
	for _, char := range value {
		charText := string(char)
		if builder.Len()+len(charText)+len(suffix) > limit {
			break
		}
		builder.WriteString(charText)
	}
	return strings.TrimSpace(builder.String()) + suffix
}

func clamp(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}

func minInt64(left, right int64) int64 {
	if left < right {
		return left
	}
	return right
}

func maxInt64(left, right int64) int64 {
	if left > right {
		return left
	}
	return right
}

func clampInt(value, minValue, maxValue int) int {
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

func max(left, right int) int {
	if left > right {
		return left
	}
	return right
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func midpoint(start, end int64) int64 {
	return start + (end-start)/2
}
