package config

import "testing"

func TestLoadAllowsDirectUnderstandingWithoutAdapterURL(t *testing.T) {
	setRequiredEnvironment(t)
	t.Setenv("VIDEO_UNDERSTANDING_DIRECT", "true")
	t.Setenv("VIDEO_UNDERSTANDING_PROVIDER", "openai")
	t.Setenv("VIDEO_UNDERSTANDING_API_URL", "")
	t.Setenv("VIDEO_UNDERSTANDING_UPSTREAM_BASE_URL", "https://api2.65535.space")
	t.Setenv("VIDEO_UNDERSTANDING_API_KEY", "secret")
	t.Setenv("VIDEO_UNDERSTANDING_WIRE_API", "chat_completions")
	t.Setenv("VIDEO_UNDERSTANDING_IMAGE_DETAIL", "low")
	t.Setenv("VIDEO_UNDERSTANDING_MAX_OUTPUT_TOKENS", "6000")

	configuration, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !configuration.UsesDirectUnderstanding() || configuration.UsesLocalUnderstanding() {
		t.Fatalf("unexpected understanding mode: %+v", configuration)
	}
	if configuration.UnderstandingUpstreamBaseURL != "https://api2.65535.space" || configuration.UnderstandingMaxOutputTokens != 6000 {
		t.Fatalf("direct understanding settings were not loaded: %+v", configuration)
	}
}

func TestLoadRequiresDirectUnderstandingUpstreamBaseURL(t *testing.T) {
	setRequiredEnvironment(t)
	t.Setenv("VIDEO_UNDERSTANDING_DIRECT", "true")
	t.Setenv("VIDEO_UNDERSTANDING_PROVIDER", "openai")
	t.Setenv("VIDEO_UNDERSTANDING_API_URL", "")
	t.Setenv("VIDEO_UNDERSTANDING_API_KEY", "secret")
	t.Setenv("VIDEO_UNDERSTANDING_UPSTREAM_BASE_URL", "")

	if _, err := Load(); err == nil {
		t.Fatal("expected missing direct upstream base URL error")
	}
}

func TestLoadRequiresDirectUnderstandingAPIKey(t *testing.T) {
	setRequiredEnvironment(t)
	t.Setenv("VIDEO_UNDERSTANDING_DIRECT", "true")
	t.Setenv("VIDEO_UNDERSTANDING_PROVIDER", "openai")
	t.Setenv("VIDEO_UNDERSTANDING_API_URL", "")
	t.Setenv("VIDEO_UNDERSTANDING_API_KEY", "")
	t.Setenv("AI_GATEWAY_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("VIDEO_UNDERSTANDING_UPSTREAM_BASE_URL", "https://api2.65535.space")

	if _, err := Load(); err == nil {
		t.Fatal("expected missing direct API key error")
	}
}

func TestLoadAllowsDirectTranscriptionWithoutAdapterURL(t *testing.T) {
	setRequiredEnvironment(t)
	t.Setenv("VIDEO_UNDERSTANDING_PROVIDER", "local")
	t.Setenv("ENABLE_TRANSCRIPTION", "true")
	t.Setenv("TRANSCRIPTION_DIRECT", "true")
	t.Setenv("TRANSCRIPTION_PROVIDER", "openai-direct")
	t.Setenv("TRANSCRIPTION_API_URL", "")
	t.Setenv("TRANSCRIPTION_API_KEY", "speech-secret")
	t.Setenv("TRANSCRIPTION_UPSTREAM_BASE_URL", "https://api2.65535.space")

	configuration, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !configuration.UsesDirectTranscription() || configuration.TranscriptionUpstreamBaseURL != "https://api2.65535.space" {
		t.Fatalf("direct transcription settings were not loaded: %+v", configuration)
	}
}

func TestLoadRequiresDirectTranscriptionAPIKey(t *testing.T) {
	setRequiredEnvironment(t)
	t.Setenv("VIDEO_UNDERSTANDING_PROVIDER", "local")
	t.Setenv("ENABLE_TRANSCRIPTION", "true")
	t.Setenv("TRANSCRIPTION_DIRECT", "true")
	t.Setenv("TRANSCRIPTION_PROVIDER", "openai-direct")
	t.Setenv("TRANSCRIPTION_API_URL", "")
	t.Setenv("TRANSCRIPTION_API_KEY", "")
	t.Setenv("SPEECH_API_KEY", "")
	t.Setenv("AI_GATEWAY_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("VIDEO_UNDERSTANDING_API_KEY", "")
	t.Setenv("TRANSCRIPTION_UPSTREAM_BASE_URL", "https://api2.65535.space")

	if _, err := Load(); err == nil {
		t.Fatal("expected missing direct transcription API key error")
	}
}

func setRequiredEnvironment(t *testing.T) {
	t.Helper()
	t.Setenv("DATABASE_URL", "postgres://framemio_app@example.test/postgres")
	t.Setenv("S3_MEDIA_BUCKET", "framemio-media")
	t.Setenv("SQS_VIDEO_ANALYSIS_QUEUE_URL", "https://sqs.example.test/queue")
	t.Setenv("TRANSCRIPTION_API_URL", "http://localhost:9001")
	t.Setenv("ENABLE_TRANSCRIPTION", "false")
	t.Setenv("MULTIMODAL_API_URL", "")
	t.Setenv("AI_GATEWAY_BASE_URL", "")
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("GATEWAY_UPSTREAM_BASE_URL", "")
}
