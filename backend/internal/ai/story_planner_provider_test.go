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

func TestOpenAICompatibleStoryPlannerNormalizesTotalDurationMismatch(t *testing.T) {
	t.Parallel()
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		planJSON := strings.Replace(storyPlannerProviderPlanJSON(), `"total_duration_ms":4000`, `"total_duration_ms":5000`, 1)
		return typographyHTTPResponse(http.StatusOK, responsesTypographyResponse(planJSON)), nil
	})}
	planner := OpenAICompatibleStoryPlanner{BaseURL: "https://planner.test", APIKey: "test-key", ProviderName: "openai-compatible", ModelName: "gpt-story", WireAPI: "responses", Client: client}
	plan, err := planner.Plan(context.Background(), storyPlannerProviderRequest())
	if err != nil {
		t.Fatal(err)
	}
	if plan.TotalDurationMS != 4000 {
		t.Fatalf("expected total duration repaired to moment sum, got %d", plan.TotalDurationMS)
	}
	if err := ValidateStoryPlan(plan); err != nil {
		t.Fatalf("expected repaired plan to pass lightweight validation: %v", err)
	}
}

func TestOpenAICompatibleStoryPlannerSanitizesUnsupportedTransitions(t *testing.T) {
	t.Parallel()
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		planJSON := `{"schema_version":"story_plan_v1","id":"plan-gpt","project_id":"project-gpt","version":3,"requested_duration_ms":4000,"story_type":"trip","visual_style":"cinematic","title_text":"Video Story","ending_text":"Until next time.","moments":[{"moment_id":"moment-photo","asset_id":"photo-gpt","media_id":"photo-gpt","media_type":"photo","narrative":"A photo moment","duration_ms":2000,"crop_intent":"fit","transition":"iris wipe"},{"moment_id":"moment-video","asset_id":"video-gpt","media_id":"video-gpt","media_type":"video","narrative":"A video moment","duration_ms":2000,"crop_intent":"fill","transition":"push-left"}],"output":{"aspect_ratio":"9:16","max_duration_seconds":4,"preset":"premium-memory"},"total_duration_ms":4000}`
		return typographyHTTPResponse(http.StatusOK, responsesTypographyResponse(planJSON)), nil
	})}
	planner := OpenAICompatibleStoryPlanner{BaseURL: "https://planner.test", APIKey: "test-key", ProviderName: "openai-compatible", ModelName: "gpt-story", WireAPI: "responses", Client: client}
	plan, err := planner.Plan(context.Background(), storyPlannerProviderRequest())
	if err != nil {
		t.Fatal(err)
	}
	if got := plan.Moments[0].Transition; got != "fade_in" {
		t.Fatalf("expected unsupported first transition to fall back to fade_in, got %q", got)
	}
	if got := plan.Moments[1].Transition; got != "cut" {
		t.Fatalf("expected unsupported later transition to fall back to cut, got %q", got)
	}
	if err := ValidateStoryPlan(plan); err != nil {
		t.Fatalf("expected sanitized plan to pass lightweight validation: %v", err)
	}
}

func TestOpenAICompatibleStoryPlannerNormalizesVideoMomentReferences(t *testing.T) {
	t.Parallel()
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		planJSON := `{"schema_version":"story_plan_v1","id":"plan-gpt","project_id":"project-gpt","version":3,"requested_duration_ms":4000,"story_type":"trip","visual_style":"cinematic","title_text":"Video Story","ending_text":"Until next time.","moments":[{"moment_id":"moment-video","asset_id":"video-gpt","media_id":"video-gpt","media_type":"video","video_analysis_version":"invented-analysis","narrative":"A video moment","duration_ms":4000,"crop_intent":"fill","transition":"fade_in"}],"broll":[{"broll_id":"broll-video","asset_id":"video-gpt","media_id":"video-gpt","media_type":"video","video_analysis_version":"invented-analysis","source_start_micros":0,"source_end_micros":4000000,"start_ms":0,"duration_ms":500,"crop_intent":"fill","confidence":0.8,"score":0.8}],"output":{"aspect_ratio":"9:16","max_duration_seconds":4,"preset":"premium-memory"},"total_duration_ms":4000}`
		return typographyHTTPResponse(http.StatusOK, responsesTypographyResponse(planJSON)), nil
	})}
	request := storyPlannerProviderRequest()
	request.VideoAnalyses[0].Segments = []models.VideoSegment{{
		SegmentID:      "segment-1",
		SceneID:        "scene-1",
		StartMicros:    500_000,
		EndMicros:      4_500_000,
		Importance:     0.9,
		MemoryValue:    0.9,
		EmotionalValue: 0.8,
		VisualQuality:  0.9,
		StoryRelevance: 0.95,
		Recommended:    true,
	}}
	planner := OpenAICompatibleStoryPlanner{BaseURL: "https://planner.test", APIKey: "test-key", ProviderName: "openai-compatible", ModelName: "gpt-story", WireAPI: "responses", Client: client}
	plan, err := planner.Plan(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	moment := plan.Moments[0]
	if moment.VideoAnalysisVersion != "video-v1" || moment.StartMicros != 500_000 || moment.EndMicros != 4_500_000 || moment.DurationMS != 4000 {
		t.Fatalf("expected video references normalized from analysis, got %+v", moment)
	}
	if len(plan.BRoll) != 1 || plan.BRoll[0].VideoAnalysisVersion != "video-v1" {
		t.Fatalf("expected b-roll analysis version normalized from analysis, got %+v", plan.BRoll)
	}
	if err := ValidateStoryPlan(plan); err != nil {
		t.Fatalf("expected normalized plan to pass lightweight validation: %v", err)
	}
}

func TestOpenAICompatibleStoryPlannerSynthesizesCaptionsWhenExternalPlanOmitsThem(t *testing.T) {
	t.Parallel()
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		planJSON := `{"schema_version":"story_plan_v1","id":"plan-gpt","project_id":"project-gpt","version":3,"requested_duration_ms":4000,"story_type":"trip","visual_style":"cinematic","title_text":"Video Story","ending_text":"Until next time.","moments":[{"moment_id":"moment-video","asset_id":"video-gpt","media_id":"video-gpt","media_type":"video","narrative":"A video moment","caption":"External planner put speech here.","start_micros":0,"end_micros":4000000,"duration_ms":4000,"crop_intent":"fill","transition":"fade_in"}],"output":{"aspect_ratio":"9:16","max_duration_seconds":4,"preset":"premium-memory"},"total_duration_ms":4000}`
		return typographyHTTPResponse(http.StatusOK, responsesTypographyResponse(planJSON)), nil
	})}
	request := storyPlannerProviderRequest()
	request.VideoAnalyses[0].Transcript = models.VideoTranscript{
		HasSpeech: true,
		Segments: []models.VideoTranscriptSegment{
			{StartMicros: 0, EndMicros: 1_200_000, Text: "Look at the ducks by the water."},
			{StartMicros: 1_200_000, EndMicros: 2_600_000, Text: "This is the best little picnic."},
		},
	}
	planner := OpenAICompatibleStoryPlanner{BaseURL: "https://planner.test", APIKey: "test-key", ModelName: "gpt-story", WireAPI: "responses", Client: client}
	plan, err := planner.Plan(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Captions) != 2 {
		t.Fatalf("expected transcript captions to be synthesized, got %+v", plan.Captions)
	}
	if plan.Captions[0].Text != "Look at the ducks by the water" || plan.Captions[0].StartMS != 0 || plan.Captions[0].EndMS != 1200 {
		t.Fatalf("unexpected first synthesized caption: %+v", plan.Captions[0])
	}
	if plan.Captions[1].Text != "This is the best little picnic" || plan.Captions[1].StartMS != 1200 || plan.Captions[1].EndMS != 2600 {
		t.Fatalf("unexpected second synthesized caption: %+v", plan.Captions[1])
	}
	if err := ValidateStoryPlan(plan); err != nil {
		t.Fatalf("expected synthesized captions to pass lightweight validation: %v", err)
	}
}

func TestOpenAICompatibleStoryPlannerCoercesStringOriginalAudio(t *testing.T) {
	t.Parallel()
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		planJSON := strings.Replace(storyPlannerProviderPlanJSON(), `"original_audio":true`, `"original_audio":"true"`, 1)
		return typographyHTTPResponse(http.StatusOK, responsesTypographyResponse(planJSON)), nil
	})}
	planner := OpenAICompatibleStoryPlanner{BaseURL: "https://planner.test", APIKey: "test-key", ProviderName: "openai-compatible", ModelName: "gpt-story", WireAPI: "responses", Client: client}
	plan, err := planner.Plan(context.Background(), storyPlannerProviderRequest())
	if err != nil {
		t.Fatal(err)
	}
	if plan.Audio == nil || plan.Audio.OriginalAudio == nil || !*plan.Audio.OriginalAudio {
		t.Fatalf("expected string original_audio to be coerced to true, got %+v", plan.Audio)
	}
	if err := ValidateStoryPlan(plan); err != nil {
		t.Fatalf("expected coerced audio plan to pass lightweight validation: %v", err)
	}
}

func TestOpenAICompatibleStoryPlannerDeduplicatesRepeatedMomentReferences(t *testing.T) {
	t.Parallel()
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		planJSON := `{"schema_version":"story_plan_v1","id":"plan-gpt","project_id":"project-gpt","version":3,"requested_duration_ms":8000,"story_type":"trip","visual_style":"cinematic","title_text":"Beach Story","ending_text":"Until next time.","moments":[{"moment_id":"moment-video-1","asset_id":"video-gpt","media_id":"video-gpt","media_type":"video","duration_ms":4000,"crop_intent":"fill","transition":"fade_in"},{"moment_id":"moment-video-2","asset_id":"video-gpt","media_id":"video-gpt","media_type":"video","duration_ms":4000,"crop_intent":"fill","transition":"cut"}],"output":{"aspect_ratio":"9:16","max_duration_seconds":8,"preset":"premium-memory"},"total_duration_ms":8000}`
		return typographyHTTPResponse(http.StatusOK, responsesTypographyResponse(planJSON)), nil
	})}
	planner := OpenAICompatibleStoryPlanner{BaseURL: "https://planner.test", APIKey: "test-key", ProviderName: "openai-compatible", ModelName: "gpt-story", WireAPI: "responses", Client: client}
	plan, err := planner.Plan(context.Background(), storyPlannerProviderRequest())
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Moments) != 1 {
		t.Fatalf("expected repeated moment reference to be removed, got %d moments", len(plan.Moments))
	}
	if plan.TotalDurationMS != 4000 {
		t.Fatalf("expected duration to be recomputed after deduplication, got %d", plan.TotalDurationMS)
	}
	if err := ValidateStoryPlan(plan); err != nil {
		t.Fatalf("expected deduplicated plan to pass lightweight validation: %v", err)
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

func TestOpenAICompatibleStoryPlannerLetsModelChooseDurationUpToSixtySeconds(t *testing.T) {
	t.Parallel()
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		var request struct {
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
		prompt := request.Input[1].Content[0].Text
		if !strings.Contains(prompt, `"desiredDurationMs":4000`) || !strings.Contains(prompt, `"maxDurationMs":60000`) || !strings.Contains(prompt, `"targetDurationFixed":false`) {
			t.Fatalf("expected suggested 4s with flexible 60s max, got %s", prompt)
		}
		planJSON := strings.Replace(storyPlannerProviderPlanJSON(), `"requested_duration_ms":4000`, `"requested_duration_ms":12000`, 1)
		planJSON = strings.Replace(planJSON, `"duration_ms":4000`, `"duration_ms":12000`, 1)
		planJSON = strings.Replace(planJSON, `"end_ms":4000`, `"end_ms":12000`, 1)
		planJSON = strings.Replace(planJSON, `"max_duration_seconds":4`, `"max_duration_seconds":12`, 1)
		planJSON = strings.Replace(planJSON, `"total_duration_ms":4000`, `"total_duration_ms":12000`, 1)
		return typographyHTTPResponse(http.StatusOK, responsesTypographyResponse(planJSON)), nil
	})}
	planner := OpenAICompatibleStoryPlanner{BaseURL: "https://planner.test", APIKey: "test-key", ModelName: "gpt-story", WireAPI: "responses", Client: client}
	plan, err := planner.Plan(context.Background(), storyPlannerProviderRequest())
	if err != nil {
		t.Fatal(err)
	}
	if plan.TotalDurationMS != 12000 || plan.RequestedDurationMS != 12000 || plan.Output.MaxDurationS != 12 {
		t.Fatalf("expected model-selected 12s duration to be preserved, got %+v", plan)
	}
}

func TestOpenAICompatibleStoryPlannerRejectsFlexibleDurationAboveSixtySeconds(t *testing.T) {
	t.Parallel()
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		planJSON := strings.Replace(storyPlannerProviderPlanJSON(), `"requested_duration_ms":4000`, `"requested_duration_ms":61000`, 1)
		planJSON = strings.Replace(planJSON, `"duration_ms":4000`, `"duration_ms":61000`, 1)
		planJSON = strings.Replace(planJSON, `"end_ms":4000`, `"end_ms":61000`, 1)
		planJSON = strings.Replace(planJSON, `"max_duration_seconds":4`, `"max_duration_seconds":61`, 1)
		planJSON = strings.Replace(planJSON, `"total_duration_ms":4000`, `"total_duration_ms":61000`, 1)
		return typographyHTTPResponse(http.StatusOK, responsesTypographyResponse(planJSON)), nil
	})}
	planner := OpenAICompatibleStoryPlanner{BaseURL: "https://planner.test", APIKey: "test-key", ModelName: "gpt-story", WireAPI: "responses", Client: client}
	if _, err := planner.Plan(context.Background(), storyPlannerProviderRequest()); err == nil {
		t.Fatal("expected flexible story plan above 60s to fail")
	}
}

func TestOpenAICompatibleStoryPlannerKeepsExplicitTargetDurationFixed(t *testing.T) {
	t.Parallel()
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		var request struct {
			Input []struct {
				Content []struct {
					Text string `json:"text"`
				} `json:"content"`
			} `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		prompt := request.Input[1].Content[0].Text
		if !strings.Contains(prompt, `"maxDurationMs":4000`) || !strings.Contains(prompt, `"targetDurationFixed":true`) {
			t.Fatalf("expected fixed 4s max, got %s", prompt)
		}
		return typographyHTTPResponse(http.StatusOK, responsesTypographyResponse(storyPlannerProviderPlanJSON())), nil
	})}
	request := storyPlannerProviderRequest()
	request.TargetDurationFixed = true
	planner := OpenAICompatibleStoryPlanner{BaseURL: "https://planner.test", APIKey: "test-key", ModelName: "gpt-story", WireAPI: "responses", Client: client}
	if _, err := planner.Plan(context.Background(), request); err != nil {
		t.Fatal(err)
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

func TestFallbackStoryPlannerKeepsPrimaryWhenCaptionsAreRepairable(t *testing.T) {
	t.Parallel()
	request := storyPlannerProviderRequest()
	primaryPlan := models.StoryPlan{
		SchemaVersion:       models.StoryPlanSchemaVersionV1,
		ID:                  "plan-primary-repaired",
		ProjectID:           request.ProjectID,
		Version:             request.Version,
		RequestedDurationMS: int(request.DesiredDuration.Milliseconds()),
		StoryType:           request.StoryType,
		VisualStyle:         request.VisualStyle,
		TitleText:           "Repairable",
		EndingText:          "Done",
		Moments: []models.StoryMoment{{
			MediaID:    "photo-gpt",
			AssetID:    "photo-gpt",
			MediaType:  models.MediaTypePhoto,
			Narrative:  "A beach sunset opens the story.",
			Caption:    "Beach sunset",
			DurationMS: 4000,
			CropIntent: "fit",
			Transition: "fade_in",
		}},
		Captions:        []models.StoryCaption{{Text: "Caption without a moment reference", StartMS: 0, EndMS: 4000}},
		Output:          models.StoryOutput{AspectRatio: "9:16", MaxDurationS: 4, Preset: "premium-memory"},
		TotalDurationMS: 4000,
	}
	fallbackPlan := primaryPlan
	fallbackPlan.ID = "plan-fallback"
	fallbackPlan.Captions = nil
	planner := FallbackStoryPlanner{
		Primary:  staticStoryPlanner{plan: primaryPlan},
		Fallback: staticStoryPlanner{plan: fallbackPlan},
	}
	plan, err := planner.Plan(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if plan.ID != primaryPlan.ID {
		t.Fatalf("expected repairable primary plan to be kept, got %+v", plan)
	}
	if len(plan.Captions) != 1 || strings.TrimSpace(plan.Captions[0].MomentID) == "" || plan.Captions[0].Text != "Beach sunset" {
		t.Fatalf("expected malformed captions to be replaced from moment caption, got %+v", plan.Captions)
	}
	if plan.Moments[0].MomentID != plan.Captions[0].MomentID {
		t.Fatalf("expected generated moment id to be used by captions, moment=%q caption=%q", plan.Moments[0].MomentID, plan.Captions[0].MomentID)
	}
	if err := ValidateStoryPlan(plan); err != nil {
		t.Fatalf("expected repaired primary plan to pass lightweight validation: %v", err)
	}
}

func TestFallbackStoryPlannerUsesFallbackWhenExternalPlannerReferencesUnknownMedia(t *testing.T) {
	t.Parallel()
	request := storyPlannerProviderRequest()
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		planJSON := `{"schema_version":"story_plan_v1","id":"plan-invented","project_id":"project-gpt","version":3,"requested_duration_ms":4000,"story_type":"trip","visual_style":"cinematic","title_text":"Invented","ending_text":"Done","moments":[{"moment_id":"moment-invented","asset_id":"invented-media","media_id":"invented-media","media_type":"photo","duration_ms":4000,"crop_intent":"fit","transition":"fade_in"}],"output":{"aspect_ratio":"9:16","max_duration_seconds":4,"preset":"premium-memory"},"total_duration_ms":4000}`
		return typographyHTTPResponse(http.StatusOK, responsesTypographyResponse(planJSON)), nil
	})}
	fallbackPlan := models.StoryPlan{
		SchemaVersion:       models.StoryPlanSchemaVersionV1,
		ID:                  "plan-fallback-known-media",
		ProjectID:           request.ProjectID,
		Version:             request.Version,
		RequestedDurationMS: int(request.DesiredDuration.Milliseconds()),
		StoryType:           request.StoryType,
		VisualStyle:         request.VisualStyle,
		TitleText:           "Fallback",
		EndingText:          "Done",
		Moments:             []models.StoryMoment{{MomentID: "moment-fallback", MediaID: "photo-gpt", AssetID: "photo-gpt", MediaType: models.MediaTypePhoto, DurationMS: 4000, CropIntent: "fit", Transition: "fade_in"}},
		Output:              models.StoryOutput{AspectRatio: "9:16", MaxDurationS: 4, Preset: "premium-memory"},
		TotalDurationMS:     4000,
	}
	planner := FallbackStoryPlanner{
		Primary:  OpenAICompatibleStoryPlanner{BaseURL: "https://planner.test", ModelName: "gpt-story", WireAPI: "responses", Client: client},
		Fallback: staticStoryPlanner{plan: fallbackPlan},
	}
	plan, err := planner.Plan(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if plan.ID != fallbackPlan.ID {
		t.Fatalf("fallback plan not returned for invented external media: %+v", plan)
	}
}

func TestFallbackStoryPlannerUsesFallbackAfterPrimaryTimeout(t *testing.T) {
	t.Parallel()
	request := storyPlannerProviderRequest()
	fallbackPlan := models.StoryPlan{
		SchemaVersion:       models.StoryPlanSchemaVersionV1,
		ID:                  "plan-fallback-timeout",
		ProjectID:           request.ProjectID,
		Version:             request.Version,
		RequestedDurationMS: int(request.DesiredDuration.Milliseconds()),
		StoryType:           request.StoryType,
		VisualStyle:         request.VisualStyle,
		TitleText:           "Fallback",
		EndingText:          "Done",
		Moments:             []models.StoryMoment{{MomentID: "moment-fallback", MediaID: "photo-gpt", AssetID: "photo-gpt", MediaType: models.MediaTypePhoto, DurationMS: 4000, CropIntent: "fit", Transition: "cut"}},
		Output:              models.StoryOutput{AspectRatio: "9:16", MaxDurationS: 4, Preset: "premium-memory"},
		TotalDurationMS:     4000,
	}
	planner := FallbackStoryPlanner{
		Primary:        blockingStoryPlanner{},
		Fallback:       staticStoryPlanner{plan: fallbackPlan},
		PrimaryTimeout: 10 * time.Millisecond,
	}
	started := time.Now()
	plan, err := planner.Plan(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("fallback took too long: %s", elapsed)
	}
	if plan.ID != fallbackPlan.ID {
		t.Fatalf("fallback plan not returned after primary timeout: %+v", plan)
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

type blockingStoryPlanner struct{}

func (blockingStoryPlanner) Plan(ctx context.Context, _ Request) (models.StoryPlan, error) {
	<-ctx.Done()
	return models.StoryPlan{}, ctx.Err()
}

func (blockingStoryPlanner) Name() string { return "blocking" }
