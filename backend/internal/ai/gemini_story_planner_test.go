package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/framemio/backend/internal/models"
)

func TestGeminiStoryPlannerUsesOpenAICompatibleGateway(t *testing.T) {
	t.Parallel()
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path != "/v1/chat/completions" {
			t.Fatalf("unexpected path %s", request.URL.Path)
		}
		if got := request.Header.Get("Authorization"); got != "Bearer gemini-key" {
			t.Fatalf("unexpected gateway authorization %q", got)
		}
		var payload struct {
			Model    string `json:"model"`
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload.Model != "gemini-story" || len(payload.Messages) != 2 ||
			payload.Messages[0].Role != "system" || payload.Messages[0].Content == "" ||
			payload.Messages[1].Role != "user" || !strings.Contains(payload.Messages[1].Content, "generate_story_plan") {
			t.Fatalf("unexpected Gemini planner request: %+v", payload)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       ioNopCloser(string(chatTypographyResponse(storyPlannerProviderPlanJSON()))),
		}, nil
	})}

	planner := GeminiStoryPlanner{
		BaseURL:   "https://api2.65535.space",
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

func TestGeminiStoryPlannerRejectsInvalidJSONForFallback(t *testing.T) {
	t.Parallel()
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       ioNopCloser(string(chatTypographyResponse("not json"))),
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
