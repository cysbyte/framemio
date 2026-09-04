package main

import (
	"testing"
	"time"

	"github.com/framemio/backend/internal/ai"
)

func TestConfiguredTypographyPlannerUsesLocalByDefault(t *testing.T) {
	t.Parallel()
	planner, err := configuredTypographyPlanner(configuration{TypographyEnabled: true, TypographyProvider: "local"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := planner.(ai.DefaultTypographyPlanner); !ok {
		t.Fatalf("expected local typography planner, got %T", planner)
	}
}

func TestConfiguredTypographyPlannerCanBeDisabled(t *testing.T) {
	t.Parallel()
	planner, err := configuredTypographyPlanner(configuration{TypographyEnabled: false, TypographyProvider: "openai"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := planner.(ai.DisabledTypographyPlanner); !ok {
		t.Fatalf("expected disabled typography planner, got %T", planner)
	}
}

func TestConfiguredTypographyPlannerRequiresExternalProviderConfig(t *testing.T) {
	t.Parallel()
	if _, err := configuredTypographyPlanner(configuration{TypographyEnabled: true, TypographyProvider: "openai", TypographyModel: "gpt-test"}); err == nil {
		t.Fatal("expected missing API key to fail")
	}
	if _, err := configuredTypographyPlanner(configuration{TypographyEnabled: true, TypographyProvider: "openai", TypographyAPIKey: "test-key"}); err == nil {
		t.Fatal("expected missing model to fail")
	}
}

func TestConfiguredTypographyPlannerUsesOpenAICompatibleProvider(t *testing.T) {
	t.Parallel()
	planner, err := configuredTypographyPlanner(configuration{
		TypographyEnabled:   true,
		TypographyProvider:  "openai-compatible",
		TypographyBaseURL:   "https://typography.test/v1",
		TypographyAPIKey:    "test-key",
		TypographyModel:     "gpt-test",
		TypographyWireAPI:   "responses",
		TypographyMaxTokens: 900,
		TypographyTimeout:   5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	external, ok := planner.(ai.OpenAICompatibleTypographyPlanner)
	if !ok {
		t.Fatalf("expected external typography planner, got %T", planner)
	}
	if external.ModelName != "gpt-test" || external.WireAPI != "responses" || external.MaxOutputTokens != 900 || external.Client.Timeout != 5*time.Second {
		t.Fatalf("external typography planner not configured: %+v", external)
	}
}
