package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/framemio/backend/internal/models"
)

func TestHeuristicStoryRevisionShortensStory(t *testing.T) {
	t.Parallel()
	input := revisionFixtureInput("Make this story shorter.")
	result, err := (HeuristicStoryRevisionPlanner{Now: func() time.Time { return time.Date(2026, time.September, 5, 8, 0, 0, 0, time.UTC) }}).Revise(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if result.RevisedStoryPlan.Version != 2 || result.RevisedStoryPlan.TotalDurationMS >= input.CurrentStoryPlan.TotalDurationMS {
		t.Fatalf("story was not shortened: %+v", result.RevisedStoryPlan)
	}
	if len(result.RevisedStoryPlan.Moments) > len(input.CurrentStoryPlan.Moments) {
		t.Fatalf("shortening added moments: %+v", result.RevisedStoryPlan.Moments)
	}
}

func TestHeuristicStoryRevisionLengthensMoments(t *testing.T) {
	t.Parallel()
	input := revisionFixtureInput("Make each moment longer.")
	result, err := (HeuristicStoryRevisionPlanner{}).Revise(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.RevisedStoryPlan.Moments) != len(input.CurrentStoryPlan.Moments) {
		t.Fatalf("lengthening should preserve moment selection: %+v", result.RevisedStoryPlan.Moments)
	}
	if result.RevisedStoryPlan.TotalDurationMS <= input.CurrentStoryPlan.TotalDurationMS {
		t.Fatalf("story was not lengthened: before=%d after=%d", input.CurrentStoryPlan.TotalDurationMS, result.RevisedStoryPlan.TotalDurationMS)
	}
	if result.RevisedStoryPlan.TotalDurationMS > input.Constraints.MaxDurationMS {
		t.Fatalf("lengthened story exceeded max duration: %+v", result.RevisedStoryPlan)
	}
	for index, moment := range result.RevisedStoryPlan.Moments {
		if moment.DurationMS <= input.CurrentStoryPlan.Moments[index].DurationMS {
			t.Fatalf("moment %d was not lengthened: before=%d after=%d", index, input.CurrentStoryPlan.Moments[index].DurationMS, moment.DurationMS)
		}
	}
}

func TestHeuristicStoryRevisionRemovesDinner(t *testing.T) {
	t.Parallel()
	result, err := (HeuristicStoryRevisionPlanner{}).Revise(context.Background(), revisionFixtureInput("Remove the dinner photos."))
	if err != nil {
		t.Fatal(err)
	}
	for _, moment := range result.RevisedStoryPlan.Moments {
		if moment.MediaID == "media-dinner" {
			t.Fatalf("dinner media was preserved: %+v", result.RevisedStoryPlan.Moments)
		}
	}
	if !revisionContainsMedia(result.RevisedStoryPlan, "media-beach") || !revisionContainsMedia(result.RevisedStoryPlan, "media-sunset") {
		t.Fatalf("unrelated media was not preserved: %+v", result.RevisedStoryPlan.Moments)
	}
}

func TestHeuristicStoryRevisionFocusesBeachWithoutInventedMedia(t *testing.T) {
	t.Parallel()
	input := revisionFixtureInput("Focus on the beach.")
	result, err := (HeuristicStoryRevisionPlanner{}).Revise(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if result.RevisedStoryPlan.Moments[0].MediaID != "media-beach" {
		t.Fatalf("beach was not prioritized: %+v", result.RevisedStoryPlan.Moments)
	}
	allowed := stringSet(input.Constraints.AllowedMediaIDs)
	for _, moment := range result.RevisedStoryPlan.Moments {
		if !allowed[moment.MediaID] {
			t.Fatalf("invented media %q in plan %+v", moment.MediaID, result.RevisedStoryPlan)
		}
	}
}

func TestHeuristicStoryRevisionUpdatesTypographyOnly(t *testing.T) {
	t.Parallel()
	input := revisionFixtureInput("Make the text more cinematic.")
	result, err := (HeuristicStoryRevisionPlanner{}).Revise(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.RevisedStoryPlan.Moments) != len(input.CurrentStoryPlan.Moments) {
		t.Fatalf("typography-only change altered media selection: %+v", result.RevisedStoryPlan.Moments)
	}
	if result.RevisedStoryPlan.Moments[0].Typography == nil || result.RevisedStoryPlan.Moments[0].Typography.Style != models.TypographyStyleCinematic {
		t.Fatalf("typography not updated: %+v", result.RevisedStoryPlan.Moments[0].Typography)
	}
}

func TestHeuristicStoryRevisionAppliesSubtitleColor(t *testing.T) {
	t.Parallel()
	input := revisionFixtureInput("Make the subtitle in red color.")
	result, err := (HeuristicStoryRevisionPlanner{}).Revise(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.RevisedStoryPlan.Moments) != len(input.CurrentStoryPlan.Moments) {
		t.Fatalf("subtitle color change altered media selection: %+v", result.RevisedStoryPlan.Moments)
	}
	if result.RevisedStoryPlan.CaptionStyle == nil || result.RevisedStoryPlan.CaptionStyle.TextColor != "#FF3B30" {
		t.Fatalf("expected red subtitle text color, got %+v", result.RevisedStoryPlan.CaptionStyle)
	}
}

func TestHeuristicStoryRevisionAddsBeachBRollFromExistingMedia(t *testing.T) {
	t.Parallel()
	input := revisionFixtureInput("Add more beach B-roll.")
	input.CurrentStoryPlan.Moments = input.CurrentStoryPlan.Moments[1:]
	input.MediaContext.SelectedMediaIDs = []string{"media-dinner", "media-sunset"}
	result, err := (HeuristicStoryRevisionPlanner{}).Revise(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.RevisedStoryPlan.BRoll) == 0 || result.RevisedStoryPlan.BRoll[0].MediaID != "media-beach" {
		t.Fatalf("expected existing beach media as B-roll: %+v", result.RevisedStoryPlan.BRoll)
	}
}

func TestOpenAIStoryRevisionNormalizesProviderOutput(t *testing.T) {
	t.Parallel()
	input := revisionFixtureInput("Make each moment longer.")
	input.CurrentStoryPlan.Moments = []models.StoryMoment{
		{MomentID: "video", AssetID: "media-video", MediaID: "media-video", MediaType: models.MediaTypeVideo, VideoAnalysisVersion: "va-1", StartMicros: 0, EndMicros: 5_000_000, Narrative: "Beach video", DurationMS: 5000, CropIntent: "fill", Transition: "fade_in", Reason: "base"},
	}
	input.CurrentStoryPlan.TotalDurationMS = 5000
	input.MediaContext.ProjectMediaIDs = []string{"media-video"}
	input.MediaContext.SelectedMediaIDs = []string{"media-video"}
	input.MediaContext.PhotoAnalyses = nil
	input.MediaContext.VideoAnalyses = []models.VideoAnalysis{{VideoID: "media-video", AnalysisVersion: "va-1", DurationMicros: 12_000_000}}
	input.Constraints.AllowedMediaIDs = []string{"media-video"}
	output := StoryRevisionResult{
		RevisionSummary: "Lengthened video moment.",
		RevisedStoryPlan: models.StoryPlan{
			SchemaVersion:       models.StoryPlanSchemaVersionV1,
			ID:                  "plan-ai-revision",
			ProjectID:           input.Story.ID,
			Version:             input.Story.NextVersion,
			RequestedDurationMS: 12000,
			StoryType:           models.StoryTypeTrip,
			VisualStyle:         models.VisualStyleNatural,
			Moments: []models.StoryMoment{{
				MomentID:             "video",
				AssetID:              "media-video",
				MediaID:              "media-video",
				MediaType:            models.MediaTypeVideo,
				VideoAnalysisVersion: "va-1",
				StartMicros:          0,
				EndMicros:            5_000_000,
				Narrative:            "Beach video",
				DurationMS:           9000,
				CropIntent:           "fill",
				Transition:           "fade_in",
				Reason:               strings.Repeat("long reason ", 30),
			}},
			Output: models.StoryOutput{AspectRatio: "9:16", MaxDurationS: 30, Preset: "premium-memory"},
		},
	}
	content, err := json.Marshal(output)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		return typographyHTTPResponse(http.StatusOK, chatTypographyResponse(string(content))), nil
	})}
	planner := OpenAICompatibleStoryRevisionPlanner{BaseURL: "https://revision.test", APIKey: "test-key", ModelName: "revision-model", WireAPI: "chat_completions", Client: client}
	result, err := planner.Revise(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if result.RevisedStoryPlan.ID == input.CurrentStoryPlan.ID || result.RevisedStoryPlan.ID == output.RevisedStoryPlan.ID {
		t.Fatalf("expected backend-owned revision plan ID, got %q", result.RevisedStoryPlan.ID)
	}
	moment := result.RevisedStoryPlan.Moments[0]
	if moment.DurationMS != 5000 {
		t.Fatalf("expected duration to match source range, got %d", moment.DurationMS)
	}
	if len(moment.Reason) > 240 {
		t.Fatalf("expected reason to be truncated, got length %d", len(moment.Reason))
	}
	if result.RevisedStoryPlan.TotalDurationMS != 5000 {
		t.Fatalf("expected total duration to be recalculated, got %d", result.RevisedStoryPlan.TotalDurationMS)
	}
}

func TestOpenAIStoryRevisionAcceptsStringChanges(t *testing.T) {
	t.Parallel()
	input := revisionFixtureInput("Make each moment longer and make subtitle red.")
	revisedPlan := input.CurrentStoryPlan
	revisedPlan.ProjectID = input.Story.ID
	revisedPlan.Version = input.Story.NextVersion
	output := map[string]any{
		"revision_summary": "Lengthened moments and changed subtitles to red.",
		"changes": []string{
			"lengthened each moment",
			"kept existing media selection",
		},
		"revised_story_plan": revisedPlan,
	}
	content, err := json.Marshal(output)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return typographyHTTPResponse(http.StatusOK, chatTypographyResponse(string(content))), nil
	})}
	planner := OpenAICompatibleStoryRevisionPlanner{BaseURL: "https://revision.test", APIKey: "test-key", ModelName: "revision-model", WireAPI: "chat_completions", Client: client}
	result, err := planner.Revise(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Changes) < 2 || result.Changes[0].Type != "note" || result.Changes[0].Reason != "lengthened each moment" {
		t.Fatalf("expected string changes to decode as note changes, got %+v", result.Changes)
	}
	if result.RevisedStoryPlan.ProjectID != input.Story.ID || result.RevisedStoryPlan.Version != input.Story.NextVersion {
		t.Fatalf("expected normalized revised plan identity, got project=%q version=%d", result.RevisedStoryPlan.ProjectID, result.RevisedStoryPlan.Version)
	}
}

func TestOpenAIStoryRevisionPreservesSelectedAddedMedia(t *testing.T) {
	t.Parallel()
	input := revisionFixtureInput("Add this video and make each moment longer.")
	input.MediaContext.ProjectMediaIDs = append(input.MediaContext.ProjectMediaIDs, "media-added-video")
	input.MediaContext.SelectedMediaIDs = append(input.MediaContext.SelectedMediaIDs, "media-added-video")
	input.MediaContext.PreferredMediaOrder = []string{"media-beach", "media-dinner", "media-sunset", "media-added-video"}
	input.MediaContext.VideoAnalyses = []models.VideoAnalysis{{VideoID: "media-added-video", AnalysisVersion: "va-added", DurationMicros: 8_000_000}}
	input.Constraints.AllowedMediaIDs = append(input.Constraints.AllowedMediaIDs, "media-added-video")
	revisedPlan := input.CurrentStoryPlan
	revisedPlan.ProjectID = input.Story.ID
	revisedPlan.Version = input.Story.NextVersion
	output := StoryRevisionResult{
		RevisionSummary:  "Lengthened existing moments.",
		Changes:          []StoryRevisionChange{{Type: "duration_change", Target: "story", Reason: "lengthened existing moments"}},
		RevisedStoryPlan: revisedPlan,
	}
	content, err := json.Marshal(output)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return typographyHTTPResponse(http.StatusOK, chatTypographyResponse(string(content))), nil
	})}
	planner := OpenAICompatibleStoryRevisionPlanner{BaseURL: "https://revision.test", APIKey: "test-key", ModelName: "revision-model", WireAPI: "chat_completions", Client: client}
	result, err := planner.Revise(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if !revisionContainsMedia(result.RevisedStoryPlan, "media-added-video") {
		t.Fatalf("expected selected added video to be forced into moments, got %+v", result.RevisedStoryPlan.Moments)
	}
	last := result.RevisedStoryPlan.Moments[len(result.RevisedStoryPlan.Moments)-1]
	if last.MediaID != "media-added-video" || last.MediaType != models.MediaTypeVideo || last.VideoAnalysisVersion != "va-added" {
		t.Fatalf("expected added video as ordered final moment, got %+v", last)
	}
}

func revisionFixtureInput(instruction string) StoryRevisionInput {
	base := models.StoryPlan{
		SchemaVersion:       models.StoryPlanSchemaVersionV1,
		ID:                  "plan-base",
		ProjectID:           "project-revision",
		Version:             1,
		RequestedDurationMS: 12000,
		StoryType:           models.StoryTypeTrip,
		VisualStyle:         models.VisualStyleNatural,
		TitleText:           "Weekend by the Sea",
		EndingText:          "The moments that stay with us.",
		Moments: []models.StoryMoment{
			{MomentID: "beach", AssetID: "media-beach", MediaID: "media-beach", MediaType: models.MediaTypePhoto, Narrative: "Beach walk", DurationMS: 4000, CropIntent: "fit", Transition: "fade_in", Score: 0.9, Typography: &models.TextOverlay{Enabled: true, Type: models.TextOverlayMomentTitle, Title: "Beach", Style: models.TypographyStyleMinimal, Placement: models.TextPlacementBottomLeft, StartMS: 200, DurationMS: 1600, MaxWidth: 0.8, Animation: models.TextAnimationFadeUp, Source: models.TextSourceAI}},
			{MomentID: "dinner", AssetID: "media-dinner", MediaID: "media-dinner", MediaType: models.MediaTypePhoto, Narrative: "Dinner together", DurationMS: 4000, CropIntent: "fit", Transition: "crossfade", Score: 0.4},
			{MomentID: "sunset", AssetID: "media-sunset", MediaID: "media-sunset", MediaType: models.MediaTypePhoto, Narrative: "Sunset", DurationMS: 4000, CropIntent: "fit", Transition: "crossfade", Score: 0.8},
		},
		Output:          models.StoryOutput{AspectRatio: "9:16", MaxDurationS: 30, Preset: "premium-memory"},
		TotalDurationMS: 12000,
	}
	return StoryRevisionInput{
		Story:            StoryRevisionStoryContext{ID: "project-revision", BaseVersion: 1, NextVersion: 2, Title: "Weekend by the Sea", DurationMS: 12000},
		CurrentStoryPlan: base,
		MediaContext: StoryRevisionMediaContext{
			ProjectMediaIDs:  []string{"media-beach", "media-dinner", "media-sunset"},
			SelectedMediaIDs: []string{"media-beach", "media-dinner", "media-sunset"},
			PhotoAnalyses: []models.PhotoAnalysis{
				{PhotoID: "media-beach", Labels: []string{"beach", "ocean", "sand"}, StoryRelevance: 0.95},
				{PhotoID: "media-dinner", Labels: []string{"dinner", "restaurant", "food"}, StoryRelevance: 0.5},
				{PhotoID: "media-sunset", Labels: []string{"sunset", "sky"}, StoryRelevance: 0.9},
			},
		},
		UserInstruction: instruction,
		Constraints:     StoryRevisionConstraints{MaxDurationMS: 30000, MinDurationMS: 1000, AllowedMediaIDs: []string{"media-beach", "media-dinner", "media-sunset"}},
		PromptVersion:   StoryRevisionPromptVersion,
	}
}

func revisionContainsMedia(plan models.StoryPlan, mediaID string) bool {
	for _, moment := range plan.Moments {
		if moment.MediaID == mediaID {
			return true
		}
	}
	return false
}
