package main

import (
	"strings"
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

func TestConfiguredStoryPlannerUsesGeminiWithGPTAndLocalFallbacks(t *testing.T) {
	t.Parallel()
	planner, err := configuredStoryPlanner(configuration{
		TypographyEnabled:                 true,
		TypographyProvider:                "local",
		StoryPlannerProvider:              "gemini",
		StoryPlannerGeminiBaseURL:         "https://gemini.test",
		StoryPlannerGeminiAPIKey:          "gemini-key",
		StoryPlannerGeminiModel:           "gemini-story",
		StoryPlannerGeminiTimeout:         30 * time.Second,
		StoryPlannerGeminiFallbackTimeout: 15 * time.Second,
		StoryPlannerBaseURL:               "https://planner.test/v1",
		StoryPlannerAPIKey:                "gpt-key",
		StoryPlannerModel:                 "gpt-story",
		StoryPlannerWireAPI:               "chat_completions",
		StoryPlannerTimeout:               25 * time.Second,
		StoryPlannerFallbackTimeout:       12 * time.Second,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	outer, ok := planner.(ai.FallbackStoryPlanner)
	if !ok {
		t.Fatalf("expected Gemini fallback wrapper, got %T", planner)
	}
	gemini, ok := outer.Primary.(ai.GeminiStoryPlanner)
	if !ok {
		t.Fatalf("expected Gemini primary, got %T", outer.Primary)
	}
	inner, ok := outer.Fallback.(ai.FallbackStoryPlanner)
	if !ok {
		t.Fatalf("expected nested GPT fallback, got %T", outer.Fallback)
	}
	gpt, ok := inner.Primary.(ai.OpenAICompatibleStoryPlanner)
	if !ok {
		t.Fatalf("expected GPT fallback primary, got %T", inner.Primary)
	}
	if _, ok := inner.Fallback.(*ai.Planner); !ok {
		t.Fatalf("expected local planner as final fallback, got %T", inner.Fallback)
	}
	if gemini.ModelName != "gemini-story" || gemini.BaseURL != "https://gemini.test" ||
		gemini.Client.Timeout != 30*time.Second || outer.PrimaryTimeout != 15*time.Second {
		t.Fatalf("unexpected Gemini configuration: %+v", gemini)
	}
	if gpt.ModelName != "gpt-story" || gpt.BaseURL != "https://planner.test/v1" ||
		gpt.Client.Timeout != 25*time.Second || inner.PrimaryTimeout != 12*time.Second {
		t.Fatalf("unexpected GPT fallback configuration: %+v", gpt)
	}
}

func TestConfiguredStoryPlannerRequiresGeminiAndGPTFallbackConfig(t *testing.T) {
	t.Parallel()
	base := configuration{
		TypographyEnabled:                 true,
		TypographyProvider:                "local",
		StoryPlannerProvider:              "gemini",
		StoryPlannerGeminiModel:           "gemini-story",
		StoryPlannerGeminiTimeout:         30 * time.Second,
		StoryPlannerGeminiFallbackTimeout: 15 * time.Second,
		StoryPlannerModel:                 "gpt-story",
		StoryPlannerTimeout:               25 * time.Second,
		StoryPlannerFallbackTimeout:       12 * time.Second,
	}
	if _, err := configuredStoryPlanner(base, nil); err == nil {
		t.Fatal("expected missing Gemini API key to fail")
	}
	base.StoryPlannerGeminiAPIKey = "gemini-key"
	if _, err := configuredStoryPlanner(base, nil); err == nil {
		t.Fatal("expected missing GPT API key to fail")
	}
}

func TestConfiguredStoryPlannerRequiresFallbackTimeoutBelowHTTPTimeout(t *testing.T) {
	t.Parallel()
	_, err := configuredStoryPlanner(configuration{
		TypographyEnabled:           true,
		TypographyProvider:          "local",
		StoryPlannerProvider:        "openai-compatible",
		StoryPlannerAPIKey:          "test-key",
		StoryPlannerModel:           "gpt-test",
		StoryPlannerTimeout:         80 * time.Second,
		StoryPlannerFallbackTimeout: 80 * time.Second,
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "must be less than") {
		t.Fatalf("expected fallback timeout ordering error, got %v", err)
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
	if config.StoryPlannerBaseURL != "https://openai-compatible.test" || config.StoryPlannerAPIKey != "fallback-key" || config.StoryPlannerWireAPI != "responses" || config.StoryPlannerTimeout != 90*time.Second || config.StoryPlannerFallbackTimeout != 80*time.Second {
		t.Fatalf("unexpected story planner config: %+v", config)
	}
}

func TestLoadConfigurationUsesSpecificSQSQueues(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("S3_MEDIA_BUCKET", "bucket")
	t.Setenv("SQS_PROCESSING_QUEUE_URL", "processing-queue")
	t.Setenv("SQS_STORY_QUEUE_URL", "story-queue")
	t.Setenv("SQS_VIDEO_QUEUE_URL", "video-queue")
	t.Setenv("SQS_RENDER_QUEUE_URL", "render-queue")
	t.Setenv("AUTH_HMAC_SECRET", "12345678901234567890123456789012")
	t.Setenv("INTERNAL_API_TOKEN", "12345678901234567890123456789012")

	config, err := loadConfiguration()
	if err != nil {
		t.Fatal(err)
	}
	if config.SQSQueueURL != "story-queue" {
		t.Fatalf("story worker queue = %q, want story-queue", config.SQSQueueURL)
	}
	if config.SQSQueueURLs.Story != "story-queue" || config.SQSQueueURLs.Video != "video-queue" || config.SQSQueueURLs.Render != "render-queue" || config.SQSQueueURLs.Processing != "processing-queue" {
		t.Fatalf("specific SQS queues were not loaded: %+v", config.SQSQueueURLs)
	}
}

func TestLoadConfigurationLoadsVideoProviderSettings(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("S3_MEDIA_BUCKET", "bucket")
	t.Setenv("SQS_PROCESSING_QUEUE_URL", "queue")
	t.Setenv("AUTH_HMAC_SECRET", "12345678901234567890123456789012")
	t.Setenv("INTERNAL_API_TOKEN", "12345678901234567890123456789012")
	t.Setenv("STORY_PLANNER_BASE_URL", "https://planner.example/v1")
	t.Setenv("STORY_PLANNER_API_KEY", "planner-key")

	config, err := loadConfiguration()
	if err != nil {
		t.Fatal(err)
	}
	if config.VideoProviderBaseURL != "https://planner.example/v1" ||
		config.VideoProviderAPIKey != "planner-key" ||
		config.VideoProviderModel != "model-imagine" {
		t.Fatalf("unexpected video provider config: %+v", config)
	}
}

func TestLoadConfigurationAllowsProcessingQueueFallback(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("S3_MEDIA_BUCKET", "bucket")
	t.Setenv("SQS_PROCESSING_QUEUE_URL", "processing-queue")
	t.Setenv("AUTH_HMAC_SECRET", "12345678901234567890123456789012")
	t.Setenv("INTERNAL_API_TOKEN", "12345678901234567890123456789012")

	config, err := loadConfiguration()
	if err != nil {
		t.Fatal(err)
	}
	if config.SQSQueueURL != "processing-queue" || config.SQSQueueURLs.Processing != "processing-queue" {
		t.Fatalf("processing fallback was not loaded: %+v", config)
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

func TestLoadConfigurationLoadsGeminiStoryPlannerSettings(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("S3_MEDIA_BUCKET", "bucket")
	t.Setenv("SQS_PROCESSING_QUEUE_URL", "queue")
	t.Setenv("AUTH_HMAC_SECRET", "12345678901234567890123456789012")
	t.Setenv("INTERNAL_API_TOKEN", "12345678901234567890123456789012")
	t.Setenv("STORY_PLANNER_PROVIDER", "gemini-agentic")
	t.Setenv("STORY_PLANNER_GEMINI_API_KEY", "gemini-key")
	t.Setenv("STORY_PLANNER_GEMINI_MODEL", "gemini-story")
	t.Setenv("STORY_PLANNER_GEMINI_BASE_URL", "https://gemini.test")
	t.Setenv("STORY_PLANNER_API_KEY", "gpt-key")
	t.Setenv("STORY_PLANNER_MODEL", "gpt-story")

	config, err := loadConfiguration()
	if err != nil {
		t.Fatal(err)
	}
	if config.StoryPlannerProvider != "gemini-agentic" ||
		config.StoryPlannerGeminiAPIKey != "gemini-key" ||
		config.StoryPlannerGeminiModel != "gemini-story" ||
		config.StoryPlannerGeminiBaseURL != "https://gemini.test" {
		t.Fatalf("unexpected Gemini story planner config: %+v", config)
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
