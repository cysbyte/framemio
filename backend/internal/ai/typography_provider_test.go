package ai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/framemio/backend/internal/models"
)

func TestOpenAICompatibleTypographyPlannerChatCompletions(t *testing.T) {
	t.Parallel()
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("unexpected authorization header %q", got)
		}
		var request struct {
			Model    string `json:"model"`
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request.Model != "typography-model" || len(request.Messages) != 2 || !strings.Contains(request.Messages[1].Content, "Weekend by the Sea") {
			t.Fatalf("unexpected provider request: %+v", request)
		}
		return typographyHTTPResponse(http.StatusOK, chatTypographyResponse(`{"overlays":[{"moment_id":"photo-1","enabled":true,"title":"Golden Hour","style":"cinematic","placement":"bottom_left","animation":"fade_up","source":"ai","confidence":0.91}]}`)), nil
	})}

	planner := OpenAICompatibleTypographyPlanner{BaseURL: "https://typography.test", APIKey: "test-key", ProviderName: "openai", ModelName: "typography-model", WireAPI: "chat_completions", Client: client}
	plan, err := planner.Generate(context.Background(), typographyProviderTestInput())
	if err != nil {
		t.Fatal(err)
	}
	if overlay := plan.Overlays["photo-1"]; overlay.Title != "Golden Hour" || overlay.Placement != models.TextPlacementBottomLeft || overlay.DurationMS <= 0 {
		t.Fatalf("unexpected typography overlay: %+v", overlay)
	}
}

func TestOpenAICompatibleTypographyPlannerResponses(t *testing.T) {
	t.Parallel()
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/v1/responses" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		return typographyHTTPResponse(http.StatusOK, responsesTypographyResponse("```json\n"+`{"overlays":[{"moment_id":"photo-1","enabled":true,"title":"Sea Light","style":"memory","placement":"top_right","animation":"fade","source":"ai"}]}`+"\n```")), nil
	})}

	planner := OpenAICompatibleTypographyPlanner{BaseURL: "https://typography.test/v1", ModelName: "typography-model", WireAPI: "responses", Client: client}
	plan, err := planner.Generate(context.Background(), typographyProviderTestInput())
	if err != nil {
		t.Fatal(err)
	}
	if overlay := plan.Overlays["photo-1"]; overlay.Title != "Sea Light" || overlay.Style != models.TypographyStyleMemory || overlay.Placement != models.TextPlacementTopRight {
		t.Fatalf("unexpected typography overlay: %+v", overlay)
	}
}

func TestOpenAICompatibleTypographyPlannerRejectsBadProviderOutput(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		status int
		body   string
	}{
		{name: "http error", status: http.StatusTooManyRequests, body: `rate limited`},
		{name: "missing text", status: http.StatusOK, body: `{"choices":[{"message":{"content":""}}]}`},
		{name: "invalid json", status: http.StatusOK, body: `{"choices":[{"message":{"content":"not json"}}]}`},
		{name: "unsupported enum", status: http.StatusOK, body: string(chatTypographyResponse(`{"overlays":[{"moment_id":"photo-1","enabled":true,"title":"Golden Hour","style":"neon","placement":"bottom_left","animation":"fade_up","source":"ai"}]}`))},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				return typographyHTTPResponse(tc.status, []byte(tc.body)), nil
			})}

			planner := OpenAICompatibleTypographyPlanner{BaseURL: "https://typography.test", ModelName: "typography-model", WireAPI: "chat_completions", Client: client}
			if _, err := planner.Generate(context.Background(), typographyProviderTestInput()); err == nil {
				t.Fatal("expected provider output to be rejected")
			}
		})
	}
}

func TestTypographyProviderHelpers(t *testing.T) {
	t.Parallel()
	if got := typographyEndpointFor("https://example.com/v1", "responses"); got != "https://example.com/v1/responses" {
		t.Fatalf("unexpected endpoint %q", got)
	}
	if got := typographyEndpointFor("https://example.com/v1/responses", "responses"); got != "https://example.com/v1/responses" {
		t.Fatalf("unexpected endpoint %q", got)
	}
	if got := normalizeTypographyWireAPI("chat-completions"); got != "chat_completions" {
		t.Fatalf("unexpected wire API %q", got)
	}
	if client := typographyHTTPClient(0); client.Timeout != 12*time.Second {
		t.Fatalf("expected default timeout, got %s", client.Timeout)
	}
}

func TestStoryPlannerProviderResponseMetadata(t *testing.T) {
	t.Parallel()
	chat := storyPlannerProviderResponseMetadata([]byte(`{"choices":[{"finish_reason":"length","message":{"content":"{}"}}]}`), "chat_completions")
	if chat.FinishReason != "length" {
		t.Fatalf("chat finish_reason = %q", chat.FinishReason)
	}
	responses := storyPlannerProviderResponseMetadata([]byte(`{"status":"incomplete","incomplete_details":{"reason":"max_output_tokens"},"output":[{"status":"incomplete","content":[{"text":"{}"}]}]}`), "responses")
	if responses.Status != "incomplete" || responses.IncompleteReason != "max_output_tokens" {
		t.Fatalf("responses metadata = %+v", responses)
	}
}

func typographyProviderTestInput() TypographyPlanningInput {
	return TypographyPlanningInput{
		ProjectID: "project-1", StoryPlanID: "plan-1", StoryPlanVersion: 1, TypographyVersion: TypographyPromptVersion,
		StoryTitle: "Weekend by the Sea", StoryType: models.StoryTypeTrip, VisualStyle: models.VisualStyleCinematic,
		NarrativeSummary: "Weekend by the Sea",
		Moments: []TypographyMomentContext{{
			MomentID: "photo-1", AssetID: "photo-1", MediaType: models.MediaTypePhoto,
			Narrative: "A person near the ocean at sunset", Caption: "person ocean sunset", DurationMS: 4000,
			Score: 0.91, Index: 0, Location: "Santa Cruz", Labels: []string{"person", "ocean", "sunset"},
		}},
	}
}

func chatTypographyResponse(content string) []byte {
	data, err := json.Marshal(map[string]any{
		"choices": []map[string]any{{"message": map[string]string{"content": content}}},
	})
	if err != nil {
		panic(err)
	}
	return data
}

func responsesTypographyResponse(text string) []byte {
	data, err := json.Marshal(map[string]any{
		"output": []map[string]any{{"content": []map[string]string{{"text": text}}}},
	})
	if err != nil {
		panic(err)
	}
	return data
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func typographyHTTPResponse(status int, body []byte) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(string(body))),
		Header:     make(http.Header),
	}
}
