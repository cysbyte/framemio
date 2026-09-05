package ai

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/framemio/backend/internal/models"
)

func TestOpenAICompatibleStoryPlannerResponsesReturnsStoryPlan(t *testing.T) {
	t.Parallel()
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/v1/responses" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("unexpected authorization header %q", got)
		}
		var request struct {
			Model string `json:"model"`
			Input []struct {
				Role    string `json:"role"`
				Content []struct {
					Text string `json:"text"`
				} `json:"content"`
			} `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request.Model != "gpt-story" || len(request.Input) != 2 || !strings.Contains(request.Input[1].Content[0].Text, "generate_story_plan") {
			t.Fatalf("unexpected planner request: %+v", request)
		}
		if strings.Contains(request.Input[1].Content[0].Text, "s3_key") || strings.Contains(request.Input[1].Content[0].Text, "private/frame.jpg") {
			t.Fatalf("planner request leaked internal frame storage data: %s", request.Input[1].Content[0].Text)
		}
		return typographyHTTPResponse(http.StatusOK, responsesTypographyResponse(storyPlannerProviderPlanJSON())), nil
	})}

	planner := OpenAICompatibleStoryPlanner{BaseURL: "https://planner.test", APIKey: "test-key", ProviderName: "openai-compatible", ModelName: "gpt-story", WireAPI: "responses", Client: client}
	plan, err := planner.Plan(context.Background(), storyPlannerProviderRequest())
	if err != nil {
		t.Fatal(err)
	}
	if plan.ProjectID != "project-gpt" || plan.Version != 3 || len(plan.Moments) != 1 {
		t.Fatalf("unexpected story plan: %+v", plan)
	}
	if plan.CaptionStyle == nil || plan.CaptionStyle.TextColor != "#FF3B30" {
		t.Fatalf("expected prompt color guard to apply, got %+v", plan.CaptionStyle)
	}
}

func TestOpenAICompatibleStoryPlannerNormalizesSchemaVersionAlias(t *testing.T) {
	t.Parallel()
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		planJSON := strings.Replace(storyPlannerProviderPlanJSON(), `"schema_version":"story_plan_v1"`, `"schema_version":"1.0"`, 1)
		return typographyHTTPResponse(http.StatusOK, responsesTypographyResponse(planJSON)), nil
	})}
	planner := OpenAICompatibleStoryPlanner{BaseURL: "https://planner.test", APIKey: "test-key", ProviderName: "openai-compatible", ModelName: "gpt-story", WireAPI: "responses", Client: client}
	plan, err := planner.Plan(context.Background(), storyPlannerProviderRequest())
	if err != nil {
		t.Fatal(err)
	}
	if plan.SchemaVersion != models.StoryPlanSchemaVersionV1 {
		t.Fatalf("expected canonical schema version, got %q", plan.SchemaVersion)
	}
}

func TestOpenAICompatibleStoryPlannerRejectsInvalidJSON(t *testing.T) {
	t.Parallel()
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return typographyHTTPResponse(http.StatusOK, responsesTypographyResponse("not json")), nil
	})}
	planner := OpenAICompatibleStoryPlanner{BaseURL: "https://planner.test", ModelName: "gpt-story", WireAPI: "responses", Client: client}
	if _, err := planner.Plan(context.Background(), storyPlannerProviderRequest()); err == nil {
		t.Fatal("expected invalid provider JSON to fail")
	}
}

func TestFallbackStoryPlannerUsesFallbackWhenPrimaryFails(t *testing.T) {
	t.Parallel()
	request := storyPlannerProviderRequest()
	fallbackPlan := models.StoryPlan{
		ID:                  "plan-fallback",
		ProjectID:           request.ProjectID,
		Version:             request.Version,
		RequestedDurationMS: int(request.DesiredDuration.Milliseconds()),
		StoryType:           request.StoryType,
		VisualStyle:         request.VisualStyle,
		Moments:             []models.StoryMoment{{MomentID: "moment-fallback", MediaID: "photo-gpt", AssetID: "photo-gpt", MediaType: models.MediaTypePhoto, DurationMS: 4000}},
		Output:              models.StoryOutput{AspectRatio: "9:16", MaxDurationS: 4, Preset: "premium-memory"},
		TotalDurationMS:     4000,
	}
	planner := FallbackStoryPlanner{
		Primary:  staticStoryPlanner{err: errors.New("context deadline exceeded")},
		Fallback: staticStoryPlanner{plan: fallbackPlan},
	}
	plan, err := planner.Plan(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if plan.ID != fallbackPlan.ID || plan.ProjectID != request.ProjectID || len(plan.Moments) != 1 {
		t.Fatalf("fallback plan not returned: %+v", plan)
	}
}

func TestFallbackStoryPlannerUsesFallbackWhenPrimaryReturnsInvalidPlan(t *testing.T) {
	t.Parallel()
	request := storyPlannerProviderRequest()
	invalidPlan := models.StoryPlan{
		SchemaVersion:       "story_plan_v2",
		ID:                  "plan-invalid",
		ProjectID:           request.ProjectID,
		Version:             request.Version,
		RequestedDurationMS: int(request.DesiredDuration.Milliseconds()),
		StoryType:           request.StoryType,
		VisualStyle:         request.VisualStyle,
		TitleText:           "Invalid",
		EndingText:          "Invalid",
		Moments:             []models.StoryMoment{{MomentID: "moment-invalid", MediaID: "photo-gpt", AssetID: "photo-gpt", MediaType: models.MediaTypePhoto, DurationMS: 4000, CropIntent: "fit", Transition: "cut"}},
		Output:              models.StoryOutput{AspectRatio: "9:16", MaxDurationS: 4, Preset: "premium-memory"},
		TotalDurationMS:     4000,
	}
	fallbackPlan := invalidPlan
	fallbackPlan.SchemaVersion = models.StoryPlanSchemaVersionV1
	fallbackPlan.ID = "plan-fallback"
	planner := FallbackStoryPlanner{
		Primary:  staticStoryPlanner{plan: invalidPlan},
		Fallback: staticStoryPlanner{plan: fallbackPlan},
	}
	plan, err := planner.Plan(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if plan.ID != fallbackPlan.ID || plan.SchemaVersion != models.StoryPlanSchemaVersionV1 {
		t.Fatalf("fallback plan not returned after invalid primary output: %+v", plan)
	}
}

func storyPlannerProviderRequest() Request {
	return Request{
		ProjectID:        "project-gpt",
		Version:          3,
		DesiredDuration:  4 * time.Second,
		StoryType:        models.StoryTypeTrip,
		VisualStyle:      models.VisualStyleCinematic,
		UserInstructions: "Make subtitles red.",
		SelectedMediaIDs: []string{"photo-gpt"},
		PhotoAnalyses: []models.PhotoAnalysis{{
			ID: "analysis-photo-gpt", PhotoID: "photo-gpt", ProjectID: "project-gpt", AnalysisVersion: "photo-v1",
			Caption: "Beach sunset", VisualQuality: 0.9, MemoryValue: 0.8, EmotionalValue: 0.8, StoryRelevance: 0.9, Selected: true,
		}},
		VideoAnalyses: []models.VideoAnalysis{{
			ID: "analysis-video-gpt", VideoID: "video-gpt", ProjectID: "project-gpt", AnalysisVersion: "video-v1", DurationMicros: 5_000_000,
			Scenes: []models.VideoScene{{SceneID: "scene-1", StartMicros: 0, EndMicros: 5_000_000, RepresentativeFrames: []models.Frame{{FrameID: "frame-1", TimestampMicros: 1_000_000, S3Key: "private/frame.jpg"}}}},
		}},
	}
}

func storyPlannerProviderPlanJSON() string {
	return `{"schema_version":"story_plan_v1","id":"plan-gpt","project_id":"project-gpt","version":3,"requested_duration_ms":4000,"story_type":"trip","visual_style":"cinematic","title_text":"Beach Story","ending_text":"Until next time.","user_instructions":"Make subtitles red.","moments":[{"moment_id":"moment-1","asset_id":"photo-gpt","media_id":"photo-gpt","media_type":"photo","narrative":"A beach sunset opens the story.","caption":"Beach sunset","duration_ms":4000,"crop_intent":"fit","transition":"fade_in","reason":"Strong opening image","score":0.9}],"captions":[{"moment_id":"moment-1","text":"Beach sunset","start_ms":0,"end_ms":4000}],"caption_style":{"preset":"cinematic_subtitle","font_family":"inter-bold","font_size":48,"text_color":"#FFFFFF","stroke_color":"#000000","stroke_width":4,"background_enabled":true,"background_color":"#000000","background_opacity":0.32,"position":"bottom","bottom_offset":150,"max_lines":2,"max_characters_per_line":32},"audio":{"source":"original","original_audio":true,"original_volume":0.4},"output":{"aspect_ratio":"9:16","max_duration_seconds":4,"preset":"premium-memory"},"total_duration_ms":4000}`
}

type staticStoryPlanner struct {
	plan models.StoryPlan
	err  error
}

func (p staticStoryPlanner) Plan(context.Context, Request) (models.StoryPlan, error) {
	if p.err != nil {
		return models.StoryPlan{}, p.err
	}
	return p.plan, nil
}

func (p staticStoryPlanner) Name() string { return "static" }
