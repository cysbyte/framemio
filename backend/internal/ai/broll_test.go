package ai

import (
	"context"
	"testing"
	"time"

	"github.com/framemio/backend/internal/models"
)

func TestBRollSemanticMatchingChoosesBeachPhoto(t *testing.T) {
	t.Parallel()
	anchor := brollAnchor{startMS: 0, endMS: 4000, concepts: semanticTermsFromText("beautiful beach")}
	beach := brollCandidate{assetID: "beach.jpg", mediaType: models.MediaTypePhoto, semanticTerms: semanticTermsFromText("beach ocean scenery"), visualQuality: 0.9, storyImportance: 0.8, supportingValue: 0.8, aestheticScore: 0.9, referenceKey: "photo:beach"}
	restaurant := brollCandidate{assetID: "restaurant.jpg", mediaType: models.MediaTypePhoto, semanticTerms: semanticTermsFromText("restaurant dinner food"), visualQuality: 0.9, storyImportance: 0.8, supportingValue: 0.8, aestheticScore: 0.9, referenceKey: "photo:restaurant"}
	city := brollCandidate{assetID: "city.jpg", mediaType: models.MediaTypePhoto, semanticTerms: semanticTermsFromText("city street"), visualQuality: 0.9, storyImportance: 0.8, supportingValue: 0.8, aestheticScore: 0.9, referenceKey: "photo:city"}

	best, ok := bestBRollForAnchor(anchor, []brollCandidate{restaurant, city, beach}, map[string]struct{}{}, map[string]int{})
	if !ok || best.candidate.assetID != beach.assetID {
		t.Fatalf("expected beach candidate to win, got %+v", best)
	}
}

func TestBRollCandidateScoringPenalizesQualityDuplicatesTimingAndSpeech(t *testing.T) {
	t.Parallel()
	anchor := brollAnchor{startMS: 0, endMS: 4000, clusterID: "same-event", sourceTime: 10_000_000, concepts: semanticTermsFromText("beach water")}
	strong := brollCandidate{assetID: "photo-1", mediaType: models.MediaTypePhoto, clusterID: "same-event", sourceTime: 10_500_000, semanticTerms: semanticTermsFromText("beach ocean water"), visualQuality: 0.95, storyImportance: 0.8, supportingValue: 0.85, aestheticScore: 0.95, referenceKey: "photo:1"}
	weak := strong
	weak.assetID = "photo-2"
	weak.referenceKey = "photo:2"
	weak.semanticTerms = semanticTermsFromText("restaurant dinner")
	lowQuality := strong
	lowQuality.assetID = "photo-3"
	lowQuality.referenceKey = "photo:3"
	lowQuality.visualQuality = 0.42
	lowQuality.aestheticScore = 0.42
	far := strong
	far.assetID = "photo-4"
	far.referenceKey = "photo:4"
	far.clusterID = "other-event"
	far.sourceTime = 5 * 24 * 60 * 60 * 1_000_000
	speech := strong
	speech.assetID = "video-1"
	speech.mediaType = models.MediaTypeVideo
	speech.referenceKey = "video:1"
	speech.speechConflict = 0.6

	strongScore := scoreBRollCandidate(anchor, strong, 0).score
	if strongScore <= scoreBRollCandidate(anchor, weak, 0).score {
		t.Fatal("semantic match should outscore weak match")
	}
	if strongScore <= scoreBRollCandidate(anchor, lowQuality, 0).score {
		t.Fatal("high quality candidate should outscore low quality candidate")
	}
	if strongScore <= scoreBRollCandidate(anchor, far, 0).score {
		t.Fatal("same-event candidate should outscore unrelated timing")
	}
	if strongScore <= scoreBRollCandidate(anchor, speech, 0).score {
		t.Fatal("speech conflict should reduce b-roll score")
	}
	if scoreBRollCandidate(anchor, strong, 1).score >= strongScore {
		t.Fatal("duplicate asset usage should be penalized")
	}
}

func TestPlannerInsertsTranscriptAwareBRollAndPreservesARollAudio(t *testing.T) {
	t.Parallel()
	planner := &Planner{Now: func() time.Time { return time.Date(2026, time.August, 21, 12, 0, 0, 0, time.UTC) }}
	request := Request{
		ProjectID:        "project-broll",
		Version:          1,
		DesiredDuration:  9 * time.Second,
		StoryType:        models.StoryTypeTrip,
		VisualStyle:      models.VisualStyleCinematic,
		SelectedMediaIDs: []string{"video-1", "photo-beach", "photo-dinner"},
		PhotoAnalyses: []models.PhotoAnalysis{
			{PhotoID: "photo-beach", ProjectID: "project-broll", AnalysisVersion: "photo-v1", TimestampMicros: 2_000_000, Caption: "Beach photo", Labels: []string{"beach", "ocean"}, MemoryValue: 0.8, EmotionalValue: 0.75, VisualQuality: 0.92, StoryRelevance: 0.85, Selected: true},
			{PhotoID: "photo-dinner", ProjectID: "project-broll", AnalysisVersion: "photo-v1", TimestampMicros: 8_000_000, Caption: "Dinner table", Labels: []string{"dinner", "food"}, MemoryValue: 0.8, EmotionalValue: 0.75, VisualQuality: 0.9, StoryRelevance: 0.82, Selected: true},
		},
		VideoAnalyses: []models.VideoAnalysis{{
			VideoID: "video-1", ProjectID: "project-broll", AnalysisVersion: "video-v1", DurationMicros: 9_000_000, QualitySignals: models.VideoQualitySignals{OverallScore: 0.82},
			Transcript: models.VideoTranscript{HasSpeech: true, Segments: []models.VideoTranscriptSegment{{StartMicros: 0, EndMicros: 3_000_000, Text: "We arrived at the beautiful beach", Confidence: 0.95}, {StartMicros: 6_000_000, EndMicros: 9_000_000, Text: "Then we went for dinner", Confidence: 0.95}}},
			Segments: []models.VideoSegment{
				{SegmentID: "talking", SceneID: "scene-1", StartMicros: 0, EndMicros: 4_000_000, Description: "Talking to camera at the beach", Importance: 0.95, VisualQuality: 0.72, MemoryValue: 0.93, EmotionalValue: 0.86, StoryRelevance: 0.94, Recommended: true, RecommendedUsage: "hero", Reasons: []string{"primary speech about beach"}, Speech: []string{"speech"}},
				{SegmentID: "scenery", SceneID: "scene-2", StartMicros: 4_000_000, EndMicros: 6_500_000, Description: "Beach ocean scenery", Importance: 0.5, VisualQuality: 0.95, MemoryValue: 0.55, EmotionalValue: 0.52, StoryRelevance: 0.72, Recommended: true, RecommendedUsage: "context", Reasons: []string{"context scenery"}, Scenery: []string{"beach", "ocean"}},
			},
		}},
	}

	plan, err := planner.Plan(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.BRoll) == 0 {
		t.Fatalf("expected b-roll in plan: %+v", plan)
	}
	foundTranscript := false
	foundVideoSegment := false
	for _, overlay := range plan.BRoll {
		if overlay.Anchor != nil && overlay.Anchor.Type == "transcript" {
			foundTranscript = true
		}
		if overlay.MediaType == models.MediaTypeVideo && overlay.AssetID == "video-1" && overlay.SourceStartMicros == 4_000_000 && overlay.SourceEndMicros == 6_500_000 {
			foundVideoSegment = true
		}
	}
	if !foundTranscript {
		t.Fatal("expected transcript-aware b-roll anchor")
	}
	if !foundVideoSegment {
		t.Fatal("expected analyzed video segment to be eligible as b-roll")
	}
	if plan.Audio != nil && plan.Audio.OriginalAudio != nil && !*plan.Audio.OriginalAudio {
		t.Fatal("b-roll planning must not disable original A-roll audio")
	}
}

func TestPlannerSkipsBRollWhenNoSuitableCandidateExists(t *testing.T) {
	t.Parallel()
	planner := &Planner{Now: func() time.Time { return time.Date(2026, time.August, 21, 12, 0, 0, 0, time.UTC) }}
	request := Request{ProjectID: "project-none", Version: 1, DesiredDuration: 6 * time.Second, StoryType: models.StoryTypeCustom, VisualStyle: models.VisualStyleCinematic, SelectedMediaIDs: []string{"photo-1"}, PhotoAnalyses: []models.PhotoAnalysis{{PhotoID: "photo-1", ProjectID: "project-none", Caption: "Only memory", VisualQuality: 0.9, MemoryValue: 0.8, EmotionalValue: 0.8, StoryRelevance: 0.8, Selected: true}}}
	plan, err := planner.Plan(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.BRoll) != 0 {
		t.Fatalf("expected no b-roll fallback, got %+v", plan.BRoll)
	}
}

func TestAddMomentMediaCanBecomeBRollOnReplan(t *testing.T) {
	t.Parallel()
	planner := &Planner{Now: func() time.Time { return time.Date(2026, time.August, 21, 12, 0, 0, 0, time.UTC) }}
	request := storyPlannerRequest()
	request.ProjectID = "project-add-moment"
	request.Version = 2
	request.DesiredDuration = 10 * time.Second
	request.UserInstructions = "remember the beach walk"
	request.SelectedMediaIDs = append(request.SelectedMediaIDs, "photo-new-beach")
	request.PhotoAnalyses = append(request.PhotoAnalyses, models.PhotoAnalysis{PhotoID: "photo-new-beach", ProjectID: request.ProjectID, AnalysisVersion: "photo-v1", TimestampMicros: 2_000_000, Caption: "New beach detail", Labels: []string{"beach", "water"}, MemoryValue: 0.7, EmotionalValue: 0.7, VisualQuality: 0.95, StoryRelevance: 0.9, Selected: true})
	for index := range request.VideoAnalyses {
		request.VideoAnalyses[index].ProjectID = request.ProjectID
	}
	for index := range request.PhotoAnalyses {
		if request.PhotoAnalyses[index].ProjectID != request.ProjectID {
			request.PhotoAnalyses[index].ProjectID = request.ProjectID
		}
	}

	plan, err := planner.Plan(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	for _, overlay := range plan.BRoll {
		if overlay.AssetID == "photo-new-beach" {
			return
		}
	}
	t.Fatalf("expected newly added media to be eligible for b-roll, got %+v", plan.BRoll)
}
