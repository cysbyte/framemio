package ai

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
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
	StoryType           models.StoryType
	VisualStyle         models.VisualStyle
	UserInstructions    string
	SelectedMediaIDs    []string
	PreferredMediaOrder []string
	ExcludedMediaIDs    []string
	ExcludedMomentIDs   []string
	SubtitlesEnabled    *bool
	CaptionStylePreset  string
	Timestamps          []time.Time
	Locations           []string
	PhotoAnalyses       []models.PhotoAnalysis
	VideoAnalyses       []models.VideoAnalysis
	MediaClusters       []models.MediaCluster
}

type Planner struct {
	Now func() time.Time
}

const maxVideoMomentDuration = 5 * time.Second

type candidate struct {
	moment      models.StoryMoment
	captionCues []captionCue
	sourceTime  int64
	score       float64
	repetition  string
	mediaID     string
	clusterID   string
	isVideo     bool
	isSelected  bool
	isEnding    bool
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
	if request.SubtitlesEnabled != nil && !*request.SubtitlesEnabled {
		captions = nil
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
	if err := ValidateStoryPlan(plan); err != nil {
		return models.StoryPlan{}, err
	}
	return plan, nil
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
		if moment.MediaType == models.MediaTypeVideo {
			if strings.TrimSpace(moment.VideoAnalysisVersion) == "" || moment.StartMicros < 0 || moment.EndMicros <= moment.StartMicros {
				return fmt.Errorf("video moment %s is missing valid analysis references", mediaID)
			}
		}
		if strings.TrimSpace(moment.CropIntent) == "" || strings.TrimSpace(moment.Transition) == "" {
			return fmt.Errorf("moment %s requires crop intent and transition", mediaID)
		}
		durationMS += moment.DurationMS
	}
	if plan.TotalDurationMS > 0 && durationMS != plan.TotalDurationMS {
		return fmt.Errorf("story plan duration mismatch: moments=%d total=%d", durationMS, plan.TotalDurationMS)
	}
	if len(plan.Captions) > 0 {
		momentIDs := make(map[string]struct{}, len(plan.Moments))
		for _, moment := range plan.Moments {
			momentIDs[moment.MomentID] = struct{}{}
		}
		for _, caption := range plan.Captions {
			if strings.TrimSpace(caption.MomentID) == "" || strings.TrimSpace(caption.Text) == "" {
				return errors.New("captions require a moment reference and text")
			}
			if _, exists := momentIDs[caption.MomentID]; !exists {
				return fmt.Errorf("caption references unknown moment %s", caption.MomentID)
			}
		}
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
			isSelected: hasMedia(selected, mediaID),
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
				mediaID:    mediaID, clusterID: clusterID, isVideo: true, isSelected: hasMedia(selected, mediaID),
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
		return "Your Story"
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
		return text
	}
	for _, reason := range segment.Reasons {
		if text := humanText(reason); text != "" {
			return text
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
		return text
	}
	if text := humanText(photo.Description); text != "" {
		return text
	}
	if text := humanText(photo.Location); text != "" {
		return text + " memory"
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
	if previous.mediaID == current.mediaID {
		return "soft_cut"
	}
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
	case "special moments", "special moments story", "your moments", "your day in moments", "a memory story",
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
