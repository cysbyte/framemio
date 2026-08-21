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
