package ai

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"sort"
	"strings"
	"unicode"

	"github.com/framemio/backend/internal/models"
)

const (
	maxBRollRatio              = 0.35
	minBRollMatchScore         = 0.54
	photoBRollDurationMS       = 1800
	videoBRollDurationMS       = 2200
	minBRollDurationMS         = 900
	minRemainingARollVisibleMS = 500
)

type brollCandidate struct {
	assetID              string
	mediaType            models.MediaType
	videoAnalysisVersion string
	sourceStartMicros    int64
	sourceEndMicros      int64
	clusterID            string
	sourceTime           int64
	semanticTerms        map[string]struct{}
	visualQuality        float64
	storyImportance      float64
	supportingValue      float64
	aestheticScore       float64
	speechConflict       float64
	baseScore            float64
	referenceKey         string
	cropIntent           string
}

type brollAnchor struct {
	moment      models.StoryMoment
	startMS     int
	endMS       int
	clusterID   string
	sourceTime  int64
	concepts    map[string]struct{}
	transcripts []brollTranscriptAnchor
}

type brollTranscriptAnchor struct {
	startMS  int
	endMS    int
	concepts map[string]struct{}
}

type brollScoredCandidate struct {
	candidate     brollCandidate
	score         float64
	semanticScore float64
	anchorType    string
	anchorStartMS int
	anchorEndMS   int
}

func planBRoll(request Request, moments []models.StoryMoment, captions []models.StoryCaption, candidates []candidate, totalDurationMS int) []models.StoryBRoll {
	preference := normalizedBRollPreference(request.BRollPreference)
	if preference == "none" {
		return nil
	}
	if totalDurationMS <= 0 || len(moments) == 0 || len(candidates) < 2 {
		return nil
	}
	brollCandidates := buildBRollCandidates(candidates)
	if len(brollCandidates) == 0 {
		return nil
	}
	anchors := buildBRollAnchors(moments, captions, candidates)
	requestConcepts := semanticTermsFromText(request.UserInstructions)
	for index := range anchors {
		mergeTerms(anchors[index].concepts, requestConcepts)
	}
	budgetRatio := maxBRollRatio
	matchThreshold := minBRollMatchScore
	if preference == "more" {
		budgetRatio = 0.45
		matchThreshold = 0.46
	}
	budgetMS := int(math.Round(float64(totalDurationMS) * budgetRatio))
	selected := make([]models.StoryBRoll, 0, minInt(len(anchors), len(brollCandidates)))
	usedReferences := make(map[string]struct{})
	usedAssets := make(map[string]int)
	usedDurationMS := 0
	for _, anchor := range anchors {
		if budgetMS-usedDurationMS < minBRollDurationMS || anchor.moment.DurationMS <= minBRollDurationMS+minRemainingARollVisibleMS {
			continue
		}
		match, ok := bestBRollForAnchor(anchor, brollCandidates, usedReferences, usedAssets)
		if !ok || match.score < matchThreshold {
			continue
		}
		durationMS := brollDuration(match.candidate, anchor, budgetMS-usedDurationMS)
		if durationMS < minBRollDurationMS {
			continue
		}
		startMS := brollStartMS(anchor, match.anchorStartMS, durationMS)
		if startMS < anchor.startMS || startMS+durationMS > anchor.endMS {
			continue
		}
		overlay := models.StoryBRoll{
			BrollID:              brollID(request.ProjectID, match.candidate, startMS, durationMS),
			AssetID:              match.candidate.assetID,
			MediaID:              match.candidate.assetID,
			MediaType:            match.candidate.mediaType,
			VideoAnalysisVersion: match.candidate.videoAnalysisVersion,
			SourceStartMicros:    match.candidate.sourceStartMicros,
			SourceEndMicros:      match.candidate.sourceEndMicros,
			StartMS:              startMS,
			DurationMS:           durationMS,
			Anchor: &models.StoryBRollAnchor{
				Type:     match.anchorType,
				MomentID: anchor.moment.MomentID,
				StartMS:  maxInt(startMS, match.anchorStartMS),
				EndMS:    minInt(startMS+durationMS, match.anchorEndMS),
			},
			CropIntent: match.candidate.cropIntent,
			Reason:     "semantic_match",
			Confidence: roundFloat(match.semanticScore, 3),
			Score:      roundFloat(match.score, 3),
		}
		selected = append(selected, overlay)
		usedReferences[match.candidate.referenceKey] = struct{}{}
		usedAssets[match.candidate.assetID]++
		usedDurationMS += durationMS
	}
	return selected
}

func buildBRollCandidates(candidates []candidate) []brollCandidate {
	result := make([]brollCandidate, 0, len(candidates))
	for _, entry := range candidates {
		assetID := strings.TrimSpace(firstNonEmpty(entry.moment.AssetID, entry.moment.MediaID))
		if assetID == "" || entry.moment.DurationMS <= 0 {
			continue
		}
		terms := semanticTermsForMoment(entry.moment)
		mergeTerms(terms, entry.semanticTerms)
		visualQuality := clamp(entry.moment.Score)
		storyImportance := clamp(entry.score)
		supportingValue := 0.75
		speechConflict := 0.0
		cropIntent := "fit"
		if entry.isVideo {
			visualQuality = clamp(videoVisualQuality(entry.moment, entry.score))
			cropIntent = "fill"
			supportingValue = videoSupportingValue(entry.moment)
			if entry.moment.StartMicros < 0 || entry.moment.EndMicros <= entry.moment.StartMicros {
				continue
			}
			speechConflict = videoSpeechConflict(entry)
		}
		if visualQuality < 0.40 || supportingValue <= 0 || speechConflict >= 0.75 {
			continue
		}
		baseScore := clamp(0.45*supportingValue + 0.35*visualQuality + 0.20*storyImportance - 0.35*speechConflict)
		if baseScore < 0.35 {
			continue
		}
		result = append(result, brollCandidate{
			assetID: assetID, mediaType: entry.moment.MediaType, videoAnalysisVersion: entry.moment.VideoAnalysisVersion,
			sourceStartMicros: entry.moment.StartMicros, sourceEndMicros: entry.moment.EndMicros,
			clusterID: entry.clusterID, sourceTime: entry.sourceTime, semanticTerms: terms,
			visualQuality: visualQuality, storyImportance: storyImportance, supportingValue: supportingValue,
			aestheticScore: visualQuality, speechConflict: speechConflict, baseScore: baseScore,
			referenceKey: brollReferenceKey(entry.moment), cropIntent: cropIntent,
		})
	}
	return result
}

func buildBRollAnchors(moments []models.StoryMoment, captions []models.StoryCaption, candidates []candidate) []brollAnchor {
	candidateByKey := make(map[string]candidate, len(candidates))
	for _, entry := range candidates {
		candidateByKey[momentKey(entry.moment)] = entry
	}
	captionsByMoment := make(map[string][]models.StoryCaption, len(captions))
	for _, caption := range captions {
		captionsByMoment[caption.MomentID] = append(captionsByMoment[caption.MomentID], caption)
	}
	anchors := make([]brollAnchor, 0, len(moments))
	timelineStartMS := 0
	for _, moment := range moments {
		entry := candidateByKey[momentKey(moment)]
		anchorTerms := semanticTermsForMoment(moment)
		mergeTerms(anchorTerms, entry.semanticTerms)
		anchor := brollAnchor{
			moment: moment, startMS: timelineStartMS, endMS: timelineStartMS + moment.DurationMS,
			clusterID: entry.clusterID, sourceTime: entry.sourceTime, concepts: anchorTerms,
		}
		for _, caption := range captionsByMoment[moment.MomentID] {
			if caption.EndMS <= caption.StartMS {
				continue
			}
			anchor.transcripts = append(anchor.transcripts, brollTranscriptAnchor{startMS: caption.StartMS, endMS: caption.EndMS, concepts: semanticTermsFromText(caption.Text)})
			mergeTerms(anchor.concepts, semanticTermsFromText(caption.Text))
		}
		anchors = append(anchors, anchor)
		timelineStartMS += moment.DurationMS
	}
	return anchors
}

func bestBRollForAnchor(anchor brollAnchor, candidates []brollCandidate, usedReferences map[string]struct{}, usedAssets map[string]int) (brollScoredCandidate, bool) {
	var best brollScoredCandidate
	found := false
	for _, candidate := range candidates {
		if _, used := usedReferences[candidate.referenceKey]; used {
			continue
		}
		if candidateConflictsWithAnchor(candidate, anchor.moment) {
			continue
		}
		match := scoreBRollCandidate(anchor, candidate, usedAssets[candidate.assetID])
		if !found || match.score > best.score || (match.score == best.score && candidate.sourceTime < best.candidate.sourceTime) {
			best = match
			found = true
		}
	}
	return best, found
}

func scoreBRollCandidate(anchor brollAnchor, candidate brollCandidate, usedAssetCount int) brollScoredCandidate {
	semanticScore := semanticMatch(anchor.concepts, candidate.semanticTerms)
	anchorType := "moment"
	anchorStartMS := anchor.startMS
	anchorEndMS := anchor.endMS
	for _, transcript := range anchor.transcripts {
		if transcriptScore := semanticMatch(transcript.concepts, candidate.semanticTerms); transcriptScore > 0 && (transcriptScore >= semanticScore || anchorType != "transcript") {
			semanticScore = transcriptScore
			anchorType = "transcript"
			anchorStartMS = transcript.startMS
			anchorEndMS = transcript.endMS
		}
	}
	temporalScore := temporalRelevance(anchor, candidate)
	diversityScore := 1.0
	if usedAssetCount > 0 {
		diversityScore = 0.35 / float64(usedAssetCount+1)
	}
	score := 0.35*semanticScore + 0.20*candidate.storyImportance + 0.15*candidate.visualQuality + 0.10*temporalScore + 0.10*diversityScore + 0.10*candidate.aestheticScore
	score += 0.10 * candidate.supportingValue
	score -= 0.20 * candidate.speechConflict
	return brollScoredCandidate{candidate: candidate, score: clamp(score), semanticScore: semanticScore, anchorType: anchorType, anchorStartMS: anchorStartMS, anchorEndMS: anchorEndMS}
}

func brollDuration(candidate brollCandidate, anchor brollAnchor, budgetMS int) int {
	durationMS := photoBRollDurationMS
	if candidate.mediaType == models.MediaTypeVideo {
		sourceDurationMS := int((candidate.sourceEndMicros - candidate.sourceStartMicros) / 1000)
		durationMS = minInt(videoBRollDurationMS, sourceDurationMS)
	}
	durationMS = minInt(durationMS, budgetMS)
	durationMS = minInt(durationMS, anchor.moment.DurationMS-minRemainingARollVisibleMS)
	if durationMS < 0 {
		return 0
	}
	return durationMS
}

func brollStartMS(anchor brollAnchor, desiredStartMS, durationMS int) int {
	minimum := anchor.startMS + 250
	maximum := anchor.endMS - durationMS - 250
	if maximum < minimum {
		minimum = anchor.startMS
		maximum = anchor.endMS - durationMS
	}
	if desiredStartMS <= 0 || desiredStartMS < anchor.startMS || desiredStartMS >= anchor.endMS {
		desiredStartMS = anchor.startMS + maxInt(0, (anchor.moment.DurationMS-durationMS)/2)
	}
	return clampInt(desiredStartMS, minimum, maximum)
}

func candidateConflictsWithAnchor(candidate brollCandidate, anchor models.StoryMoment) bool {
	anchorAsset := strings.TrimSpace(firstNonEmpty(anchor.AssetID, anchor.MediaID))
	if candidate.assetID != anchorAsset {
		return false
	}
	if candidate.mediaType != models.MediaTypeVideo || anchor.MediaType != models.MediaTypeVideo {
		return true
	}
	return rangesOverlap(candidate.sourceStartMicros, candidate.sourceEndMicros, anchor.StartMicros, anchor.EndMicros)
}

func rangesOverlap(leftStart, leftEnd, rightStart, rightEnd int64) bool {
	return leftStart < rightEnd && rightStart < leftEnd
}

func temporalRelevance(anchor brollAnchor, candidate brollCandidate) float64 {
	if anchor.clusterID != "" && anchor.clusterID == candidate.clusterID {
		return 1
	}
	if anchor.sourceTime > 0 && candidate.sourceTime > 0 {
		delta := math.Abs(float64(anchor.sourceTime - candidate.sourceTime))
		dayMicros := float64(24 * 60 * 60 * 1_000_000)
		return clamp(1 - delta/dayMicros)
	}
	return 0.55
}

func videoSupportingValue(moment models.StoryMoment) float64 {
	usage := strings.ToLower(strings.TrimSpace(moment.Reason + " " + moment.Narrative + " " + moment.Caption))
	if strings.Contains(usage, "context") || strings.Contains(usage, "scenery") || strings.Contains(usage, "detail") {
		return 0.95
	}
	if strings.Contains(usage, "support") || strings.Contains(usage, "establish") {
		return 0.9
	}
	return 0.6
}

func videoVisualQuality(moment models.StoryMoment, fallback float64) float64 {
	if moment.Score > 0 {
		return clamp(moment.Score)
	}
	return clamp(fallback)
}

func videoSpeechConflict(entry candidate) float64 {
	text := strings.ToLower(strings.Join([]string{entry.moment.Caption, entry.moment.Narrative, entry.moment.Reason}, " "))
	conflict := 0.0
	if strings.Contains(text, "talk") || strings.Contains(text, "speech") || strings.Contains(text, "voice") || strings.Contains(text, "audio") {
		conflict += 0.35
	}
	if entry.moment.Score >= 0.88 {
		conflict += 0.10
	}
	return clamp(conflict)
}

func semanticTermsForMoment(moment models.StoryMoment) map[string]struct{} {
	terms := semanticTermsFromText(strings.Join([]string{moment.Caption, moment.Narrative, moment.Reason}, " "))
	return terms
}

func photoSemanticTerms(photo models.PhotoAnalysis) map[string]struct{} {
	terms := semanticTermsFromText(strings.Join([]string{photo.Caption, photo.Description, photo.Location}, " "))
	mergeTerms(terms, semanticTermsFromLabels(photo.Labels))
	mergeTerms(terms, semanticTermsFromLabels(photo.People))
	return terms
}

func videoSegmentSemanticTerms(segment models.VideoSegment) map[string]struct{} {
	terms := semanticTermsFromText(strings.Join([]string{segment.Description, strings.Join(segment.Reasons, " ")}, " "))
	for _, labels := range [][]string{segment.People, segment.Activities, segment.Interactions, segment.Expressions, segment.Objects, segment.Food, segment.Locations, segment.Scenery, segment.Events, segment.InterestingActions, segment.Speech, segment.MeaningfulAudio, segment.Labels} {
		mergeTerms(terms, semanticTermsFromLabels(labels))
	}
	return terms
}

func semanticTermsFromLabels(labels []string) map[string]struct{} {
	return semanticTermsFromText(strings.Join(labels, " "))
}

func semanticTermsFromText(value string) map[string]struct{} {
	terms := make(map[string]struct{})
	for _, token := range tokenizeSemanticText(value) {
		for _, normalized := range semanticAliases(token) {
			terms[normalized] = struct{}{}
		}
	}
	return terms
}

func tokenizeSemanticText(value string) []string {
	fields := strings.FieldsFunc(strings.ToLower(value), func(char rune) bool {
		return !(unicode.IsLetter(char) || unicode.IsDigit(char))
	})
	result := make([]string, 0, len(fields))
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if len(field) < 3 || semanticStopWords[field] {
			continue
		}
		result = append(result, field)
	}
	return result
}

func semanticAliases(token string) []string {
	switch token {
	case "beach", "ocean", "sea", "water", "waves", "coast", "shore", "sand", "lake", "lakeside", "river", "pond":
		return []string{token, "beach", "water", "outdoor", "scenery", "lake"}
	case "dinner", "restaurant", "cafe", "food", "meal", "table":
		return []string{token, "food", "dinner", "restaurant"}
	case "sunset", "sunrise", "golden", "evening":
		return []string{token, "sky", "scenery", "closing", "warm"}
	case "people", "person", "family", "friends", "friend", "adult", "child", "children", "kids", "kid":
		return []string{token, "people", "family", "portrait"}
	case "laugh", "laughing", "smile", "smiling", "reaction", "reactions", "emotion", "emotional", "meaningful":
		return []string{token, "people", "emotion", "warm", "memory"}
	case "walk", "walking", "stroll":
		return []string{token, "walking", "movement"}
	case "city", "street", "building", "urban":
		return []string{token, "city", "street"}
	default:
		return []string{token}
	}
}

var semanticStopWords = map[string]bool{
	"the": true, "and": true, "for": true, "with": true, "from": true, "this": true, "that": true,
	"were": true, "was": true, "are": true, "our": true, "your": true, "their": true, "into": true,
	"moment": true, "memory": true, "selected": true, "available": true, "video": true, "photo": true,
}

func mergeTerms(destination, source map[string]struct{}) {
	for term := range source {
		destination[term] = struct{}{}
	}
}

func semanticMatch(left, right map[string]struct{}) float64 {
	if len(left) == 0 || len(right) == 0 {
		return 0
	}
	matches := 0
	for term := range left {
		if _, ok := right[term]; ok {
			matches++
		}
	}
	denominator := math.Sqrt(float64(len(left) * len(right)))
	if denominator <= 0 {
		return 0
	}
	return clamp(float64(matches) / denominator)
}

func brollReferenceKey(moment models.StoryMoment) string {
	assetID := strings.TrimSpace(firstNonEmpty(moment.AssetID, moment.MediaID))
	if moment.MediaType == models.MediaTypeVideo {
		return fmt.Sprintf("video:%s:%s:%d:%d", assetID, moment.VideoAnalysisVersion, moment.StartMicros, moment.EndMicros)
	}
	return "photo:" + assetID
}

func brollID(projectID string, candidate brollCandidate, startMS, durationMS int) string {
	hash := sha256.New()
	for _, value := range []string{projectID, candidate.referenceKey, fmt.Sprintf("%d:%d", startMS, durationMS)} {
		_, _ = hash.Write([]byte(value))
	}
	return "broll_" + hex.EncodeToString(hash.Sum(nil))[:20]
}

func derivedStoryPlannerBRollID(projectID string, overlay models.StoryBRoll, index int) string {
	mediaID := strings.TrimSpace(firstNonEmpty(overlay.MediaID, overlay.AssetID))
	anchorMomentID := ""
	anchorType := ""
	if overlay.Anchor != nil {
		anchorMomentID = strings.TrimSpace(overlay.Anchor.MomentID)
		anchorType = strings.TrimSpace(overlay.Anchor.Type)
	}
	hash := sha256.New()
	for _, value := range []string{
		strings.TrimSpace(projectID),
		mediaID,
		string(overlay.MediaType),
		strings.TrimSpace(overlay.VideoAnalysisVersion),
		fmt.Sprintf("%d:%d:%d:%d", overlay.SourceStartMicros, overlay.SourceEndMicros, overlay.StartMS, overlay.DurationMS),
		anchorMomentID,
		anchorType,
		fmt.Sprintf("%d", index),
	} {
		_, _ = hash.Write([]byte(value))
		_, _ = hash.Write([]byte{0})
	}
	return "broll_" + hex.EncodeToString(hash.Sum(nil))[:20]
}

func roundFloat(value float64, places int) float64 {
	multiplier := math.Pow10(places)
	return math.Round(value*multiplier) / multiplier
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}

func sortedBRollScoresForLog(overlays []models.StoryBRoll) string {
	if len(overlays) == 0 {
		return ""
	}
	parts := make([]string, 0, len(overlays))
	for _, overlay := range overlays {
		parts = append(parts, fmt.Sprintf("%s:%.3f", overlay.BrollID, overlay.Score))
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}
