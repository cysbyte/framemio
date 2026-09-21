package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/framemio/backend/internal/models"
)

func TestGeminiStoryPlannerUsesInteractionsAPIAndStreamsOutput(t *testing.T) {
	t.Parallel()
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path != "/v1beta/interactions" {
			t.Fatalf("unexpected path %s", request.URL.Path)
		}
		if got := request.Header.Get("x-goog-api-key"); got != "gemini-key" {
			t.Fatalf("unexpected Gemini API key %q", got)
		}
		var payload struct {
			Model  string `json:"model"`
			Stream bool   `json:"stream"`
			Input  []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"input"`
		}
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload.Model != "gemini-story" || !payload.Stream || len(payload.Input) != 2 ||
			payload.Input[0].Type != "text" || payload.Input[0].Text == "" ||
			payload.Input[1].Type != "text" || !strings.Contains(payload.Input[1].Text, "generate_story_plan") {
			t.Fatalf("unexpected Gemini planner request: %+v", payload)
		}
		body := `data: {"delta":"` + strings.ReplaceAll(storyPlannerProviderPlanJSON(), `"`, `\"`) + `"}` + "\n\n"
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       ioNopCloser(body),
		}, nil
	})}

	planner := GeminiStoryPlanner{
		BaseURL:   "https://generativelanguage.googleapis.com",
		APIKey:    "gemini-key",
		ModelName: "gemini-story",
		Client:    client,
	}
	plan, err := planner.Plan(context.Background(), storyPlannerProviderRequest())
	if err != nil {
		t.Fatal(err)
	}
	if plan.ProjectID != "project-gpt" || plan.SchemaVersion != models.StoryPlanSchemaVersionV1 {
		t.Fatalf("unexpected Gemini story plan: %+v", plan)
	}
}

func TestExtractGeminiInteractionTextSupportsOutputTextAndNestedOutput(t *testing.T) {
	t.Parallel()
	if got, err := extractGeminiInteractionText([]byte(`{"output_text":"{\"ok\":true}"}`)); err != nil || got != `{"ok":true}` {
		t.Fatalf("output_text extraction = %q, %v", got, err)
	}
	if got, err := extractGeminiInteractionText([]byte(`{"output":[{"content":[{"text":"{\"ok\":true}"}]}]}`)); err != nil || got != `{"ok":true}` {
		t.Fatalf("nested output extraction = %q, %v", got, err)
	}
}

func TestGeminiStoryPlannerRejectsInvalidJSONForFallback(t *testing.T) {
	t.Parallel()
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       ioNopCloser(`{"output_text":"not json"}`),
		}, nil
	})}
	planner := GeminiStoryPlanner{
		BaseURL:   "https://planner.test",
		APIKey:    "gemini-key",
		ModelName: "gemini-story",
		Client:    client,
	}
	if _, err := planner.Plan(context.Background(), storyPlannerProviderRequest()); err == nil {
		t.Fatal("expected invalid Gemini JSON to fail so the fallback planner can run")
	}
}

type stringReadCloser struct {
	*strings.Reader
}

func (stringReadCloser) Close() error { return nil }

func ioNopCloser(value string) stringReadCloser {
	return stringReadCloser{Reader: strings.NewReader(value)}
}
