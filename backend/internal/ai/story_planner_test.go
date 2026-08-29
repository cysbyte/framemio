package ai

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/framemio/backend/internal/models"
)

func TestPlannerBuildsChronologicalCoherentStoryPlan(t *testing.T) {
	t.Parallel()
	planner := &Planner{Now: func() time.Time { return time.Date(2026, time.August, 21, 12, 0, 0, 0, time.UTC) }}
	plan, err := planner.Plan(context.Background(), storyPlannerRequest())
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateStoryPlan(plan); err != nil {
		t.Fatal(err)
	}
	if plan.TitleText == "" || plan.EndingText == "" || plan.TotalDurationMS <= 0 {
		t.Fatalf("plan is missing story metadata: %+v", plan)
	}
	if len(plan.Moments) < 3 {
		t.Fatalf("expected multiple moments, got %d", len(plan.Moments))
	}
	for index := 1; index < len(plan.Moments); index++ {
		if plan.Moments[index-1].MediaType == models.MediaTypeVideo && plan.Moments[index-1].VideoAnalysisVersion == "" {
			t.Fatal("video moment is missing analysis version")
		}
		if plan.Moments[index-1].MediaType == models.MediaTypeVideo && (plan.Moments[index-1].StartMicros < 0 || plan.Moments[index-1].EndMicros <= plan.Moments[index-1].StartMicros) {
			t.Fatal("video moment has invalid timestamps")
		}
		if plan.Moments[index-1].MomentID == plan.Moments[index].MomentID {
			t.Fatal("duplicate moment IDs were generated")
		}
		if plan.Moments[index-1].MediaType == models.MediaTypePhoto && plan.Moments[index-1].Caption == "" {
			t.Fatal("photo moment should preserve useful captioning")
		}
	}
	if !strings.Contains(strings.ToLower(plan.TitleText), "trip") {
		t.Fatalf("unexpected title metadata: title=%q", plan.TitleText)
	}
	if len(plan.Captions) == 0 {
		t.Fatal("expected captions for useful moments")
	}
	encoded, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	var decoded models.StoryPlan
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.ID != plan.ID || decoded.TitleText != plan.TitleText {
		t.Fatal("story plan is not stable JSON")
	}
}

func TestPlannerKeepsPhotoWhenSelectedVideoSegmentsOutscorePhotos(t *testing.T) {
	t.Parallel()
	planner := &Planner{Now: func() time.Time { return time.Date(2026, time.August, 25, 12, 0, 0, 0, time.UTC) }}
	request := Request{
		ProjectID:        "project-1",
		DesiredDuration:  12 * time.Second,
		StoryType:        models.StoryTypeCustom,
		VisualStyle:      models.VisualStyleCinematic,
		SelectedMediaIDs: []string{"video-1", "photo-1", "photo-2"},
		PhotoAnalyses: []models.PhotoAnalysis{
			{ID: "photo-analysis-1", PhotoID: "photo-1", ProjectID: "project-1", AnalysisVersion: "photo-v1", TimestampMicros: 1_000_000, Caption: "First uploaded image", MemoryValue: 0.45, EmotionalValue: 0.45, VisualQuality: 0.5, StoryRelevance: 0.45, Selected: true},
			{ID: "photo-analysis-2", PhotoID: "photo-2", ProjectID: "project-1", AnalysisVersion: "photo-v1", TimestampMicros: 6_000_000, Caption: "Updated image", MemoryValue: 0.5, EmotionalValue: 0.5, VisualQuality: 0.5, StoryRelevance: 0.5, Selected: true},
		},
		VideoAnalyses: []models.VideoAnalysis{{
			ID:              "video-analysis-1",
			VideoID:         "video-1",
			ProjectID:       "project-1",
			AnalysisVersion: "video-analysis-v1",
			DurationMicros:  15_000_000,
			QualitySignals:  models.VideoQualitySignals{OverallScore: 0.95},
			Segments: []models.VideoSegment{
				{SegmentID: "segment-1", SceneID: "scene-1", StartMicros: 0, EndMicros: 5_000_000, Description: "Strong opening video moment", Importance: 0.95, VisualQuality: 0.95, MemoryValue: 0.95, EmotionalValue: 0.95, StoryRelevance: 0.95, Recommended: true},
				{SegmentID: "segment-2", SceneID: "scene-2", StartMicros: 5_000_000, EndMicros: 10_000_000, Description: "Strong middle video moment", Importance: 0.94, VisualQuality: 0.94, MemoryValue: 0.94, EmotionalValue: 0.94, StoryRelevance: 0.94, Recommended: true},
				{SegmentID: "segment-3", SceneID: "scene-3", StartMicros: 10_000_000, EndMicros: 15_000_000, Description: "Strong ending video moment", Importance: 0.93, VisualQuality: 0.93, MemoryValue: 0.93, EmotionalValue: 0.93, StoryRelevance: 0.93, Recommended: true},
			},
		}},
	}

	plan, err := planner.Plan(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateStoryPlan(plan); err != nil {
		t.Fatal(err)
	}
	photoCount := 0
	videoCount := 0
	for _, moment := range plan.Moments {
		if moment.MediaType == models.MediaTypeVideo {
			videoCount++
		}
		if moment.MediaType == models.MediaTypePhoto {
			photoCount++
		}
	}
	if videoCount == 0 || photoCount == 0 {
		t.Fatalf("expected mixed photo/video story moments, got %+v", plan.Moments)
	}
}

func TestPlannerUsesAnalysisForStoryAndMomentNames(t *testing.T) {
	t.Parallel()
	planner := &Planner{Now: func() time.Time { return time.Date(2026, time.August, 25, 12, 0, 0, 0, time.UTC) }}
	plan, err := planner.Plan(context.Background(), Request{
		ProjectID:        "project-names",
		DesiredDuration:  12 * time.Second,
		StoryType:        models.StoryTypeCustom,
		VisualStyle:      models.VisualStyleCinematic,
		SelectedMediaIDs: []string{"photo-1", "video-1"},
		PhotoAnalyses: []models.PhotoAnalysis{{
			ID: "photo-analysis-1", PhotoID: "photo-1", ProjectID: "project-names", AnalysisVersion: "photo-v1",
			TimestampMicros: 1_000_000, Caption: "First uploaded image", Description: "Family breakfast by the window",
			MemoryValue: 0.9, EmotionalValue: 0.8, VisualQuality: 0.7, StoryRelevance: 0.9, Selected: true,
		}},
		VideoAnalyses: []models.VideoAnalysis{{
			ID: "video-analysis-1", VideoID: "video-1", ProjectID: "project-names", AnalysisVersion: "video-v1", DurationMicros: 8_000_000,
			QualitySignals: models.VideoQualitySignals{OverallScore: 0.8},
			Segments: []models.VideoSegment{{
				SegmentID: "segment-1", SceneID: "scene-1", StartMicros: 0, EndMicros: 4_000_000, Description: "Strong opening video moment",
				Reasons: []string{"Everyone laughing near the lake"}, Importance: 0.9, MemoryValue: 0.9, EmotionalValue: 0.9, StoryRelevance: 0.9, VisualQuality: 0.7, Recommended: true,
			}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(plan.TitleText), "special moments") || strings.Contains(strings.ToLower(plan.TitleText), "your moments") {
		t.Fatalf("story title should be analysis-derived, got %q", plan.TitleText)
	}
	if !strings.Contains(strings.ToLower(plan.TitleText), "family breakfast") {
		t.Fatalf("story title should use meaningful analysis text, got %q", plan.TitleText)
	}
	for _, moment := range plan.Moments {
		combined := strings.ToLower(moment.Caption + " " + moment.Narrative + " " + moment.Reason)
		if strings.Contains(combined, "first uploaded image") || strings.Contains(combined, "strong opening video moment") || strings.Contains(combined, "meaningful photo moment") {
			t.Fatalf("generic moment name leaked into story plan: %+v", moment)
		}
	}
}

func TestPlannerTruncatesLongVideoReasonsForValidation(t *testing.T) {
	t.Parallel()
	longReason := strings.Repeat("This detailed model rationale should not prevent rendering. 夕焼けの会話を含む。", 8)
	plan, err := (&Planner{Now: func() time.Time { return time.Date(2026, time.August, 27, 12, 0, 0, 0, time.UTC) }}).Plan(context.Background(), Request{
		ProjectID:        "project-long-reason",
		DesiredDuration:  8 * time.Second,
		StoryType:        models.StoryTypeCustom,
		VisualStyle:      models.VisualStyleCinematic,
		SelectedMediaIDs: []string{"video-long-reason"},
		VideoAnalyses: []models.VideoAnalysis{{
			ID: "video-analysis-long-reason", VideoID: "video-long-reason", ProjectID: "project-long-reason", AnalysisVersion: "video-v1", DurationMicros: 8_000_000,
			QualitySignals: models.VideoQualitySignals{OverallScore: 0.8},
			Segments: []models.VideoSegment{{
				SegmentID: "segment-long-reason", SceneID: "scene-long-reason", StartMicros: 0, EndMicros: 4_000_000, Description: "Family walking by the lake",
				Importance: 0.92, VisualQuality: 0.8, MemoryValue: 0.94, EmotionalValue: 0.93, StoryRelevance: 0.94, Recommended: true, RecommendedUsage: "hero",
				Reasons: []string{longReason},
			}},
		}},
		MediaClusters: []models.MediaCluster{{ID: "cluster-with-long-suffix", ProjectID: "project-long-reason", MediaIDs: []string{"video-long-reason"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateStoryPlan(plan); err != nil {
		t.Fatal(err)
	}
	if len(plan.Moments) == 0 || len(plan.Moments[0].Reason) > 240 {
		t.Fatalf("expected reason to be capped at 240 bytes, got %+v", plan.Moments)
	}
}

func TestPlannerSuppressesTechnicalGroundingText(t *testing.T) {
	t.Parallel()
	plan, err := (&Planner{Now: func() time.Time { return time.Date(2026, time.August, 29, 12, 0, 0, 0, time.UTC) }}).Plan(context.Background(), Request{
		ProjectID:        "project-grounding-metadata",
		DesiredDuration:  8 * time.Second,
		StoryType:        models.StoryTypeCustom,
		VisualStyle:      models.VisualStyleCinematic,
		SelectedMediaIDs: []string{"video-grounding-metadata"},
		VideoAnalyses: []models.VideoAnalysis{{
			ID: "video-analysis-grounding-metadata", VideoID: "video-grounding-metadata", ProjectID: "project-grounding-metadata", AnalysisVersion: "video-v1", DurationMicros: 8_000_000,
			QualitySignals: models.VideoQualitySignals{OverallScore: 0.8},
			Segments: []models.VideoSegment{{
				SegmentID: "segment-grounding-metadata", SceneID: "scene-grounding-metadata", StartMicros: 0, EndMicros: 4_000_000,
				Description: "A warm family picnic by the lake",
				Reasons: []string{
					"Strong memory moment: transcript_segment_indexes 0 and 1 reference the strawberry spill and how funny it was.",
					"Family laughing about the strawberry spill by the lake",
				},
				Importance: 0.92, VisualQuality: 0.8, MemoryValue: 0.94, EmotionalValue: 0.93, StoryRelevance: 0.94, Recommended: true, RecommendedUsage: "hero",
			}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Moments) == 0 {
		t.Fatal("expected a planned moment")
	}
	combined := strings.ToLower(plan.Moments[0].Caption + " " + plan.Moments[0].Narrative + " " + plan.Moments[0].Reason)
	if strings.Contains(combined, "transcript_segment_indexes") {
		t.Fatalf("technical grounding metadata leaked into story plan: %+v", plan.Moments[0])
	}
}

func TestPlannerUsesTranscriptTextForVideoCaptions(t *testing.T) {
	t.Parallel()
	plan, err := (&Planner{Now: func() time.Time { return time.Date(2026, time.August, 25, 12, 0, 0, 0, time.UTC) }}).Plan(context.Background(), Request{
		ProjectID:        "project-transcript",
		DesiredDuration:  8 * time.Second,
		StoryType:        models.StoryTypeCustom,
		VisualStyle:      models.VisualStyleCinematic,
		SelectedMediaIDs: []string{"video-talk"},
		VideoAnalyses: []models.VideoAnalysis{{
			ID: "video-analysis-talk", VideoID: "video-talk", ProjectID: "project-transcript", AnalysisVersion: "video-v1", DurationMicros: 8_000_000,
			Transcript: models.VideoTranscript{HasSpeech: true, Language: "en", Segments: []models.VideoTranscriptSegment{
				{StartMicros: 1_000_000, EndMicros: 2_200_000, Text: "Look at the ducks by the lake", Confidence: 0.96},
				{StartMicros: 2_200_000, EndMicros: 3_600_000, Text: "This is my favorite part", Confidence: 0.94},
			}},
			QualitySignals: models.VideoQualitySignals{OverallScore: 0.8},
			Segments: []models.VideoSegment{{
				SegmentID: "segment-talk", SceneID: "scene-talk", StartMicros: 1_000_000, EndMicros: 4_500_000, Description: "Family laughing near the lake",
				Importance: 0.92, VisualQuality: 0.8, MemoryValue: 0.94, EmotionalValue: 0.93, StoryRelevance: 0.94, Recommended: true, RecommendedUsage: "hero",
			}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Captions) == 0 {
		t.Fatalf("expected transcript caption, got %+v", plan)
	}
	if len(plan.Captions) != 2 {
		t.Fatalf("expected one timed caption per speech segment, got %+v", plan.Captions)
	}
	firstCaptionText := strings.ToLower(plan.Captions[0].Text)
	secondCaptionText := strings.ToLower(plan.Captions[1].Text)
	if !strings.Contains(firstCaptionText, "look at the ducks") || !strings.Contains(secondCaptionText, "favorite part") {
		t.Fatalf("expected speech transcript captions, got %+v", plan.Captions)
	}
	if plan.Captions[0].StartMS != 0 || plan.Captions[0].EndMS != 1200 || plan.Captions[1].StartMS != 1200 || plan.Captions[1].EndMS != 2600 {
		t.Fatalf("expected transcript captions to follow voice timing, got %+v", plan.Captions)
	}
	if len(plan.Moments) != 1 {
		t.Fatalf("expected one selected moment, got %+v", plan.Moments)
	}
	if strings.Contains(strings.ToLower(plan.Moments[0].Caption), "ducks") || strings.Contains(strings.ToLower(plan.Moments[0].Narrative), "favorite part") {
		t.Fatalf("transcript leaked into story moment prose: %+v", plan.Moments[0])
	}
	if plan.Moments[0].Caption != "Family laughing near the lake" || plan.Moments[0].Narrative != "Family laughing near the lake" {
		t.Fatalf("expected visual story prose to remain separate from subtitles, got %+v", plan.Moments[0])
	}
}

func TestPlannerSelectsCaptionStyleFromStoryContext(t *testing.T) {
	t.Parallel()
	planner := &Planner{Now: func() time.Time { return time.Date(2026, time.August, 25, 12, 0, 0, 0, time.UTC) }}

	minimalPlan, err := planner.Plan(context.Background(), Request{
		ProjectID:       "project-minimal-caption-style",
		DesiredDuration: 5 * time.Second,
		StoryType:       models.StoryTypeCustom,
		VisualStyle:     models.VisualStyleMinimal,
		PhotoAnalyses: []models.PhotoAnalysis{{
			ID: "photo-analysis-1", PhotoID: "photo-1", ProjectID: "project-minimal-caption-style", AnalysisVersion: "photo-v1",
			Caption: "Quiet lake", MemoryValue: 0.9, EmotionalValue: 0.8, VisualQuality: 0.8, StoryRelevance: 0.9, Selected: true,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if minimalPlan.CaptionStyle == nil || minimalPlan.CaptionStyle.Preset != "clean_minimal" || minimalPlan.CaptionStyle.BackgroundEnabled {
		t.Fatalf("expected clean minimal caption style, got %+v", minimalPlan.CaptionStyle)
	}

	playfulPlan, err := planner.Plan(context.Background(), Request{
		ProjectID:       "project-playful-caption-style",
		DesiredDuration: 5 * time.Second,
		StoryType:       models.StoryTypeBirthday,
		VisualStyle:     models.VisualStyleNatural,
		PhotoAnalyses: []models.PhotoAnalysis{{
			ID: "photo-analysis-2", PhotoID: "photo-2", ProjectID: "project-playful-caption-style", AnalysisVersion: "photo-v1",
			Caption: "Cake candles", MemoryValue: 0.9, EmotionalValue: 0.9, VisualQuality: 0.8, StoryRelevance: 0.9, Selected: true,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if playfulPlan.CaptionStyle == nil || playfulPlan.CaptionStyle.Preset != "playful_pop" || playfulPlan.CaptionStyle.FontSize <= minimalPlan.CaptionStyle.FontSize {
		t.Fatalf("expected playful caption style, got %+v", playfulPlan.CaptionStyle)
	}
}

func TestPlannerOrdersPhotosAndVideosByAbsoluteCaptureTime(t *testing.T) {
	t.Parallel()
	photoTime := time.Date(2026, time.August, 25, 10, 0, 0, 0, time.UTC)
	videoTime := time.Date(2026, time.August, 25, 10, 1, 0, 0, time.UTC)
	planner := &Planner{Now: func() time.Time { return time.Date(2026, time.August, 25, 12, 0, 0, 0, time.UTC) }}
	plan, err := planner.Plan(context.Background(), Request{
		ProjectID:        "project-absolute-chronology",
		DesiredDuration:  9 * time.Second,
		StoryType:        models.StoryTypeCustom,
		VisualStyle:      models.VisualStyleCinematic,
		SelectedMediaIDs: []string{"photo-before", "video-after"},
		PhotoAnalyses: []models.PhotoAnalysis{{
			ID: "photo-analysis-before", PhotoID: "photo-before", ProjectID: "project-absolute-chronology", AnalysisVersion: "photo-v1", TakenAt: &photoTime,
			Caption: "The photo came first", MemoryValue: 0.9, EmotionalValue: 0.8, VisualQuality: 0.8, StoryRelevance: 0.9, Selected: true,
		}},
		VideoAnalyses: []models.VideoAnalysis{{
			ID: "video-analysis-after", VideoID: "video-after", ProjectID: "project-absolute-chronology", AnalysisVersion: "video-v1", DurationMicros: 8_000_000,
			QualitySignals: models.VideoQualitySignals{OverallScore: 0.9},
			Segments: []models.VideoSegment{{
				SegmentID: "segment-after", SceneID: "scene-after", StartMicros: 1_000_000, EndMicros: 5_000_000, Description: "The video came later",
				Importance: 0.9, VisualQuality: 0.8, MemoryValue: 0.9, EmotionalValue: 0.8, StoryRelevance: 0.9, Recommended: true,
			}},
		}},
		MediaClusters: []models.MediaCluster{
			{ID: "cluster-photo", ProjectID: "project-absolute-chronology", MediaIDs: []string{"photo-before"}, StartAt: &photoTime},
			{ID: "cluster-video", ProjectID: "project-absolute-chronology", MediaIDs: []string{"video-after"}, StartAt: &videoTime},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Moments) < 2 {
		t.Fatalf("expected photo and video moments, got %+v", plan.Moments)
	}
	if plan.Moments[0].AssetID != "photo-before" || plan.Moments[1].AssetID != "video-after" {
		t.Fatalf("expected absolute chronology photo then video, got %+v", plan.Moments)
	}
}

func TestPlannerUsesFadeBetweenDifferentMedia(t *testing.T) {
	t.Parallel()
	planner := &Planner{Now: func() time.Time { return time.Date(2026, time.August, 25, 12, 0, 0, 0, time.UTC) }}
	plan, err := planner.Plan(context.Background(), Request{
		ProjectID:       "project-fade-transition",
		DesiredDuration: 8 * time.Second,
		StoryType:       models.StoryTypeCustom,
		VisualStyle:     models.VisualStyleCinematic,
		PhotoAnalyses: []models.PhotoAnalysis{
			{ID: "photo-analysis-1", PhotoID: "photo-1", ProjectID: "project-fade-transition", AnalysisVersion: "photo-v1", TimestampMicros: 1_000_000, Caption: "Opening by the lake", MemoryValue: 0.9, EmotionalValue: 0.8, VisualQuality: 0.8, StoryRelevance: 0.9, Selected: true},
			{ID: "photo-analysis-2", PhotoID: "photo-2", ProjectID: "project-fade-transition", AnalysisVersion: "photo-v1", TimestampMicros: 2_000_000, Caption: "Friends laughing", MemoryValue: 0.8, EmotionalValue: 0.8, VisualQuality: 0.8, StoryRelevance: 0.8, Selected: true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Moments) < 2 {
		t.Fatalf("expected multiple moments, got %+v", plan.Moments)
	}
	if plan.Moments[1].Transition != "fade" {
		t.Fatalf("expected fade between different media, got %+v", plan.Moments)
	}
}

func TestPlannerHonorsSubtitleOptions(t *testing.T) {
	t.Parallel()
	disabled := false
	planner := &Planner{Now: func() time.Time { return time.Date(2026, time.August, 25, 12, 0, 0, 0, time.UTC) }}
	request := Request{
		ProjectID:          "project-subtitle-options",
		DesiredDuration:    8 * time.Second,
		StoryType:          models.StoryTypeCustom,
		VisualStyle:        models.VisualStyleCinematic,
		SelectedMediaIDs:   []string{"video-talk"},
		SubtitlesEnabled:   &disabled,
		CaptionStylePreset: "playful_pop",
		VideoAnalyses: []models.VideoAnalysis{{
			ID: "video-analysis-talk", VideoID: "video-talk", ProjectID: "project-subtitle-options", AnalysisVersion: "video-v1", DurationMicros: 8_000_000,
			Transcript: models.VideoTranscript{HasSpeech: true, Language: "en", Segments: []models.VideoTranscriptSegment{
				{StartMicros: 1_000_000, EndMicros: 2_200_000, Text: "Look at the lake", Confidence: 0.96},
			}},
			QualitySignals: models.VideoQualitySignals{OverallScore: 0.8},
			Segments: []models.VideoSegment{{
				SegmentID: "segment-talk", SceneID: "scene-talk", StartMicros: 1_000_000, EndMicros: 4_500_000, Description: "Family near the lake",
				Importance: 0.92, VisualQuality: 0.8, MemoryValue: 0.94, EmotionalValue: 0.93, StoryRelevance: 0.94, Recommended: true, RecommendedUsage: "hero",
			}},
		}},
	}

	plan, err := planner.Plan(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Captions) != 0 {
		t.Fatalf("expected subtitles disabled, got %+v", plan.Captions)
	}
	if plan.CaptionStyle == nil || plan.CaptionStyle.Preset != "playful_pop" {
		t.Fatalf("expected selected subtitle style to be preserved, got %+v", plan.CaptionStyle)
	}
}

func TestPlannerSelectsMeaningfulVideoRanges(t *testing.T) {
	t.Parallel()
	planner := &Planner{Now: func() time.Time { return time.Date(2026, time.August, 25, 12, 0, 0, 0, time.UTC) }}
	plan, err := planner.Plan(context.Background(), Request{
		ProjectID:        "project-1",
		DesiredDuration:  12 * time.Second,
		StoryType:        models.StoryTypeCustom,
		VisualStyle:      models.VisualStyleCinematic,
		SelectedMediaIDs: []string{"video-1"},
		VideoAnalyses: []models.VideoAnalysis{{
			ID: "analysis-1", VideoID: "video-1", ProjectID: "project-1", AnalysisVersion: "video-analysis-v1", DurationMicros: 18_000_000,
			QualitySignals: models.VideoQualitySignals{OverallScore: 0.8},
			Segments: []models.VideoSegment{
				{SegmentID: "segment-1", SceneID: "scene-1", StartMicros: 0, EndMicros: 4_000_000, Description: "Walking toward lake", Importance: 0.2, VisualQuality: 0.7, MemoryValue: 0.2, EmotionalValue: 0.2, StoryRelevance: 0.2, RecommendedUsage: "exclude"},
				{SegmentID: "segment-2", SceneID: "scene-2", StartMicros: 4_000_000, EndMicros: 8_000_000, Description: "Family playing near lake", Importance: 0.95, VisualQuality: 0.8, MemoryValue: 0.95, EmotionalValue: 0.92, StoryRelevance: 0.95, Recommended: true, RecommendedUsage: "hero"},
				{SegmentID: "segment-3", SceneID: "scene-3", StartMicros: 8_000_000, EndMicros: 12_000_000, Description: "Static footage", Importance: 0.1, VisualQuality: 0.5, MemoryValue: 0.1, EmotionalValue: 0.1, StoryRelevance: 0.1, RecommendedUsage: "exclude"},
				{SegmentID: "segment-4", SceneID: "scene-4", StartMicros: 12_000_000, EndMicros: 16_000_000, Description: "Family laughing", Importance: 0.94, VisualQuality: 0.78, MemoryValue: 0.94, EmotionalValue: 0.96, StoryRelevance: 0.93, Recommended: true, RecommendedUsage: "supporting"},
				{SegmentID: "segment-5", SceneID: "scene-5", StartMicros: 16_000_000, EndMicros: 18_000_000, Description: "Camera pointed at ground", Importance: 0.05, VisualQuality: 0.2, MemoryValue: 0.05, EmotionalValue: 0.05, StoryRelevance: 0.05, RecommendedUsage: "exclude"},
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Moments) != 2 {
		t.Fatalf("expected two meaningful video ranges, got %+v", plan.Moments)
	}
	wantStarts := map[int64]struct{}{4_000_000: {}, 12_000_000: {}}
	for _, moment := range plan.Moments {
		if _, ok := wantStarts[moment.StartMicros]; !ok || moment.EndMicros-moment.StartMicros > maxVideoMomentDuration.Microseconds() {
			t.Fatalf("selected wrong video range: %+v", plan.Moments)
		}
	}
}

func TestPlannerBuildsPhotoOnlyStory(t *testing.T) {
	t.Parallel()
	request := storyPlannerRequest()
	request.VideoAnalyses = nil
	request.SelectedMediaIDs = []string{"photo-1", "photo-2", "photo-3"}
	plan, err := (&Planner{}).Plan(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Moments) == 0 {
		t.Fatal("expected photo-only moments")
	}
	for _, moment := range plan.Moments {
		if moment.MediaType != models.MediaTypePhoto {
			t.Fatalf("photo-only story included non-photo moment: %+v", plan.Moments)
		}
	}
}

func TestPlannerUsesPreferredMediaOrderForEditStory(t *testing.T) {
	t.Parallel()
	request := storyPlannerRequest()
	request.VideoAnalyses = nil
	request.SelectedMediaIDs = []string{"photo-1", "photo-2", "photo-3"}
	request.PreferredMediaOrder = []string{"photo-3", "photo-1", "photo-2"}
	request.DesiredDuration = 15 * time.Second

	plan, err := (&Planner{}).Plan(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Moments) < 3 {
		t.Fatalf("expected all selected photos to be represented, got %+v", plan.Moments)
	}
	got := []string{plan.Moments[0].MediaID, plan.Moments[1].MediaID, plan.Moments[2].MediaID}
	want := []string{"photo-3", "photo-1", "photo-2"}
	if !slices.Equal(got, want) {
		t.Fatalf("preferred order not applied: got %+v want %+v moments=%+v", got, want, plan.Moments)
	}
}

func TestPlannerBuildsVideoOnlyStoryAcrossVideos(t *testing.T) {
	t.Parallel()
	request := Request{ProjectID: "project-1", DesiredDuration: 12 * time.Second, StoryType: models.StoryTypeCustom, VisualStyle: models.VisualStyleCinematic, SelectedMediaIDs: []string{"video-1", "video-2", "video-3"}}
	for index, mediaID := range request.SelectedMediaIDs {
		start := int64(index) * 5_000_000
		request.VideoAnalyses = append(request.VideoAnalyses, models.VideoAnalysis{
			ID: "analysis-" + mediaID, VideoID: mediaID, ProjectID: "project-1", AnalysisVersion: "video-analysis-v1", DurationMicros: 20_000_000,
			QualitySignals: models.VideoQualitySignals{OverallScore: 0.8},
			Segments:       []models.VideoSegment{{SegmentID: "segment-1", SceneID: "scene-1", StartMicros: start, EndMicros: start + 4_000_000, Description: "Meaningful clip", Importance: 0.85, VisualQuality: 0.8, MemoryValue: 0.86, EmotionalValue: 0.84, StoryRelevance: 0.87, Recommended: true, RecommendedUsage: "hero"}},
		})
	}
	plan, err := (&Planner{}).Plan(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	seen := make(map[string]struct{})
	for _, moment := range plan.Moments {
		seen[moment.AssetID] = struct{}{}
	}
	if len(seen) != 3 {
		t.Fatalf("expected each selected video to contribute, got %+v", plan.Moments)
	}
}

func TestPlannerExcludesRemovedMomentsDuringRevision(t *testing.T) {
	t.Parallel()
	request := storyPlannerRequest()
	request.Version = 8
	request.ExcludedMomentIDs = []string{"video-1:video-analysis-v1:0:4000000"}

	plan, err := (&Planner{}).Plan(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	for _, moment := range plan.Moments {
		if moment.MomentID == "video-1:video-analysis-v1:0:4000000" {
			t.Fatalf("excluded moment reached story plan: %+v", plan.Moments)
		}
	}
}

func TestValidateStoryPlanRejectsBrokenReferences(t *testing.T) {
	t.Parallel()
	plan := models.StoryPlan{
		ID: "plan_1", ProjectID: "project-1", Version: 1, StoryType: models.StoryTypeToday, VisualStyle: models.VisualStyleCinematic,
		TitleText: "Your day in moments", EndingText: "A story made from the moments that mattered most.",
		Moments: []models.StoryMoment{{
			MomentID: "moment-1", AssetID: "media-1", MediaType: models.MediaTypeVideo, VideoAnalysisVersion: "video-analysis-v1",
			StartMicros: 0, EndMicros: 4_000_000, DurationMS: 4000, CropIntent: "fill", Transition: "cut",
		}},
		Captions:  []models.StoryCaption{{MomentID: "unknown", Text: "oops"}},
		Output:    models.StoryOutput{AspectRatio: "9:16", MaxDurationS: 12, Preset: "premium-memory"},
		CreatedAt: time.Date(2026, time.August, 21, 12, 0, 0, 0, time.UTC),
	}
	if err := ValidateStoryPlan(plan); err == nil {
		t.Fatal("expected broken caption reference to fail validation")
	}
}

func storyPlannerRequest() Request {
	selected := []string{"video-1", "photo-2"}
	return Request{
		ProjectID:        "project-1",
		Version:          7,
		DesiredDuration:  14 * time.Second,
		StoryType:        models.StoryTypeTrip,
		VisualStyle:      models.VisualStyleCinematic,
		UserInstructions: "keep it calm and emotional",
		SelectedMediaIDs: selected,
		Locations:        []string{"Paris"},
		Timestamps:       []time.Time{time.Date(2026, time.August, 21, 9, 0, 0, 0, time.UTC)},
		PhotoAnalyses: []models.PhotoAnalysis{
			{ID: "photo-analysis-1", PhotoID: "photo-1", ProjectID: "project-1", AnalysisVersion: "photo-v1", TimestampMicros: 1_000_000, Location: "Paris", Caption: "A quiet street", Description: "A quiet street with warm light", MemoryValue: 0.72, EmotionalValue: 0.55, VisualQuality: 0.85, StoryRelevance: 0.68, ClusterID: "cluster-a", CreatedAt: time.Date(2026, time.August, 21, 9, 0, 0, 0, time.UTC)},
			{ID: "photo-analysis-2", PhotoID: "photo-2", ProjectID: "project-1", AnalysisVersion: "photo-v1", TimestampMicros: 1_500_000, Location: "Paris", Caption: "A smiling moment", Description: "A smiling moment with friends", MemoryValue: 0.92, EmotionalValue: 0.88, VisualQuality: 0.7, StoryRelevance: 0.9, ClusterID: "cluster-a", Selected: true, CreatedAt: time.Date(2026, time.August, 21, 9, 5, 0, 0, time.UTC)},
			{ID: "photo-analysis-3", PhotoID: "photo-3", ProjectID: "project-1", AnalysisVersion: "photo-v1", TimestampMicros: 6_000_000, Location: "Paris", Caption: "Dinner", Description: "Dinner at a tiny cafe", MemoryValue: 0.8, EmotionalValue: 0.75, VisualQuality: 0.75, StoryRelevance: 0.82, ClusterID: "cluster-b", CreatedAt: time.Date(2026, time.August, 21, 9, 10, 0, 0, time.UTC)},
		},
		VideoAnalyses: []models.VideoAnalysis{
			{ID: "video-analysis-1", VideoID: "video-1", ProjectID: "project-1", AnalysisVersion: "video-analysis-v1", DurationMicros: 9_000_000, QualitySignals: models.VideoQualitySignals{OverallScore: 0.74},
				Segments: []models.VideoSegment{
					{SegmentID: "segment-1", SceneID: "scene-1", StartMicros: 0, EndMicros: 4_000_000, Description: "Friends laughing over coffee", Importance: 0.95, VisualQuality: 0.45, MemoryValue: 0.94, EmotionalValue: 0.96, StoryRelevance: 0.93, Recommended: true, Reasons: []string{"warm laughter"}},
					{SegmentID: "segment-2", SceneID: "scene-2", StartMicros: 4_000_000, EndMicros: 9_000_000, Description: "A wide scenic view", Importance: 0.3, VisualQuality: 0.92, MemoryValue: 0.2, EmotionalValue: 0.18, StoryRelevance: 0.22, Recommended: false, Reasons: []string{"context only"}},
				},
			},
		},
		MediaClusters: []models.MediaCluster{{ID: "cluster-a", ProjectID: "project-1", Label: "friends", MediaIDs: []string{"photo-1", "photo-2"}, Location: "Paris"}},
	}
}
