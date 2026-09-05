package main

import (
	"testing"
	"time"

	"github.com/framemio/backend/internal/ai"
)

func TestConfiguredStoryPlannerUsesLocalByDefault(t *testing.T) {
	t.Parallel()
	planner, err := configuredStoryPlanner(configuration{TypographyEnabled: true, TypographyProvider: "local"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := planner.(*ai.Planner); !ok {
		t.Fatalf("expected local story planner, got %T", planner)
	}
}

func TestConfiguredStoryPlannerRequiresExternalProviderConfig(t *testing.T) {
	t.Parallel()
	if _, err := configuredStoryPlanner(configuration{TypographyEnabled: true, TypographyProvider: "local", StoryPlannerProvider: "openai", StoryPlannerModel: "gpt-test"}, nil); err == nil {
		t.Fatal("expected missing API key to fail")
	}
	if _, err := configuredStoryPlanner(configuration{TypographyEnabled: true, TypographyProvider: "local", StoryPlannerProvider: "openai", StoryPlannerAPIKey: "test-key"}, nil); err == nil {
		t.Fatal("expected missing model to fail")
	}
}

func TestConfiguredStoryPlannerUsesOpenAICompatibleProvider(t *testing.T) {
	t.Parallel()
	planner, err := configuredStoryPlanner(configuration{
		TypographyEnabled:           true,
		TypographyProvider:          "local",
		StoryPlannerProvider:        "openai-compatible",
		StoryPlannerBaseURL:         "https://planner.test/v1",
		StoryPlannerAPIKey:          "test-key",
		StoryPlannerModel:           "gpt-test",
		StoryPlannerWireAPI:         "responses",
		StoryPlannerMaxTokens:       7000,
		StoryPlannerTimeout:         25 * time.Second,
		StoryPlannerFallbackTimeout: 12 * time.Second,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	fallback, ok := planner.(ai.FallbackStoryPlanner)
	if !ok {
		t.Fatalf("expected external story planner with local fallback, got %T", planner)
	}
	external, ok := fallback.Primary.(ai.OpenAICompatibleStoryPlanner)
	if !ok {
		t.Fatalf("expected external primary story planner, got %T", fallback.Primary)
	}
	if _, ok := fallback.Fallback.(*ai.Planner); !ok {
		t.Fatalf("expected local fallback story planner, got %T", fallback.Fallback)
	}
	if external.BaseURL != "https://planner.test/v1" || external.ModelName != "gpt-test" || external.WireAPI != "responses" || external.MaxOutputTokens != 7000 || external.Client.Timeout != 25*time.Second || fallback.PrimaryTimeout != 12*time.Second {
		t.Fatalf("external story planner not configured: %+v", external)
	}
}

func TestLoadConfigurationStoryPlannerFallsBackToOpenAIEnv(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("S3_MEDIA_BUCKET", "bucket")
	t.Setenv("SQS_PROCESSING_QUEUE_URL", "queue")
	t.Setenv("AUTH_HMAC_SECRET", "12345678901234567890123456789012")
	t.Setenv("INTERNAL_API_TOKEN", "12345678901234567890123456789012")
	t.Setenv("STORY_PLANNER_PROVIDER", "openai-compatible")
	t.Setenv("OPENAI_BASE_URL", "https://openai-compatible.test")
	t.Setenv("OPENAI_API_KEY", "fallback-key")
	t.Setenv("STORY_PLANNER_MODEL", "gpt-5.6-luna")

	config, err := loadConfiguration()
	if err != nil {
		t.Fatal(err)
	}
	if config.StoryPlannerBaseURL != "https://openai-compatible.test" || config.StoryPlannerAPIKey != "fallback-key" || config.StoryPlannerWireAPI != "responses" || config.StoryPlannerTimeout != 90*time.Second || config.StoryPlannerFallbackTimeout != 30*time.Second {
		t.Fatalf("unexpected story planner config: %+v", config)
	}
}

func TestLoadConfigurationRejectsIncompleteExternalStoryPlanner(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("S3_MEDIA_BUCKET", "bucket")
	t.Setenv("SQS_PROCESSING_QUEUE_URL", "queue")
	t.Setenv("AUTH_HMAC_SECRET", "12345678901234567890123456789012")
	t.Setenv("INTERNAL_API_TOKEN", "12345678901234567890123456789012")
	t.Setenv("STORY_PLANNER_PROVIDER", "openai-compatible")
	t.Setenv("STORY_PLANNER_MODEL", "gpt-5.6-luna")

	if _, err := loadConfiguration(); err == nil {
		t.Fatal("expected missing story planner API key to fail startup config")
	}

	t.Setenv("STORY_PLANNER_API_KEY", "planner-key")
	t.Setenv("STORY_PLANNER_MODEL", "")
	if _, err := loadConfiguration(); err == nil {
		t.Fatal("expected missing story planner model to fail startup config")
	}
}

func TestStoryPlannerProviderEnvAlias(t *testing.T) {
	t.Setenv("STORY_PLANNER_PROVIDER", "")
	t.Setenv("ENABLE_GPT_STORY_PLANNER", "true")
	if got := storyPlannerProviderFromEnv(); got != "openai-compatible" {
		t.Fatalf("provider alias = %q, want openai-compatible", got)
	}
}

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
