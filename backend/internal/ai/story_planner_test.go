package ai

import (
	"context"
	"encoding/json"
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
	if plan.TitleText == "" || plan.EndingText == "" || plan.MusicMood == "" || plan.TotalDurationMS <= 0 {
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
	if !strings.Contains(strings.ToLower(plan.TitleText), "trip") || !strings.Contains(strings.ToLower(plan.MusicMood), "uplifting") {
		t.Fatalf("unexpected metadata: title=%q mood=%q", plan.TitleText, plan.MusicMood)
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
		TitleText: "Your day in moments", EndingText: "A story made from the moments that mattered most.", MusicMood: "calm and reflective",
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
