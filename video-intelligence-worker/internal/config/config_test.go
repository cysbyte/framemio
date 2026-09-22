package config

import (
	"testing"
	"time"
)

func TestLoadAllowsDirectUnderstandingWithoutAdapterURL(t *testing.T) {
	setRequiredEnvironment(t)
	t.Setenv("VIDEO_UNDERSTANDING_DIRECT", "true")
	t.Setenv("VIDEO_UNDERSTANDING_PROVIDER", "openai")
	t.Setenv("VIDEO_UNDERSTANDING_API_URL", "")
	t.Setenv("VIDEO_UNDERSTANDING_UPSTREAM_BASE_URL", "https://api.cheaperinference.com/v1")
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
	if configuration.UnderstandingUpstreamBaseURL != "https://api.cheaperinference.com/v1" || configuration.UnderstandingMaxOutputTokens != 6000 {
		t.Fatalf("direct understanding settings were not loaded: %+v", configuration)
	}
}

func TestLoadUsesOpenAICompatibleGatewayForGeminiModel(t *testing.T) {
	setRequiredEnvironment(t)
	t.Setenv("VIDEO_UNDERSTANDING_PROVIDER", "openai-direct")
	t.Setenv("VIDEO_UNDERSTANDING_MODEL", "gemini-3.7-flash")
	t.Setenv("VIDEO_UNDERSTANDING_DIRECT", "true")
	t.Setenv("VIDEO_UNDERSTANDING_API_URL", "")
	t.Setenv("VIDEO_UNDERSTANDING_UPSTREAM_BASE_URL", "https://api.cheaperinference.com/v1")
	t.Setenv("VIDEO_UNDERSTANDING_API_KEY", "gateway-secret")
	t.Setenv("VIDEO_UNDERSTANDING_WIRE_API", "chat_completions")

	configuration, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !configuration.UsesDirectUnderstanding() || configuration.UsesGeminiUnderstanding() {
		t.Fatalf("unexpected understanding mode: %+v", configuration)
	}
	if configuration.UnderstandingModel != "gemini-3.7-flash" ||
		configuration.UnderstandingUpstreamBaseURL != "https://api.cheaperinference.com/v1" ||
		configuration.UnderstandingWireAPI != "chat_completions" {
		t.Fatalf("unexpected gateway settings: %+v", configuration)
	}
}

func TestLoadAllowsGeminiAgenticUnderstandingWithoutAdapterURL(t *testing.T) {
	setRequiredEnvironment(t)
	t.Setenv("VIDEO_UNDERSTANDING_PROVIDER", "gemini-agentic")
	t.Setenv("VIDEO_UNDERSTANDING_API_URL", "")
	t.Setenv("VIDEO_UNDERSTANDING_GEMINI_API_KEY", "gemini-secret")
	t.Setenv("VIDEO_UNDERSTANDING_MODEL", "")

	configuration, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !configuration.UsesGeminiUnderstanding() || configuration.UsesDirectUnderstanding() {
		t.Fatalf("unexpected Gemini mode: %+v", configuration)
	}
	if configuration.UnderstandingModel != "gemini-3.7-flash" || configuration.UnderstandingTimeout != 15*time.Minute || configuration.UnderstandingRetries != 1 {
		t.Fatalf("unexpected Gemini defaults: %+v", configuration)
	}
}

func TestLoadRequiresGeminiAPIKey(t *testing.T) {
	setRequiredEnvironment(t)
	t.Setenv("VIDEO_UNDERSTANDING_PROVIDER", "gemini")
	t.Setenv("VIDEO_UNDERSTANDING_API_URL", "")
	t.Setenv("VIDEO_UNDERSTANDING_GEMINI_API_KEY", "")
	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("GOOGLE_API_KEY", "")
	t.Setenv("VIDEO_UNDERSTANDING_API_KEY", "")

	if _, err := Load(); err == nil {
		t.Fatal("expected missing Gemini API key error")
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
	t.Setenv("VIDEO_UNDERSTANDING_UPSTREAM_BASE_URL", "https://api.cheaperinference.com/v1")

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
	t.Setenv("TRANSCRIPTION_UPSTREAM_BASE_URL", "https://api.cheaperinference.com/v1")

	configuration, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !configuration.UsesDirectTranscription() || configuration.TranscriptionUpstreamBaseURL != "https://api.cheaperinference.com/v1" {
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
	t.Setenv("TRANSCRIPTION_UPSTREAM_BASE_URL", "https://api.cheaperinference.com/v1")

	if _, err := Load(); err == nil {
		t.Fatal("expected missing direct transcription API key error")
	}
}

func TestLoadAllowsDeepgramTranscriptionWithoutAdapterURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://framemio_app@example.test/postgres")
	t.Setenv("S3_MEDIA_BUCKET", "framemio-media")
	t.Setenv("SQS_VIDEO_QUEUE_URL", "https://sqs.example.test/video")
	t.Setenv("VIDEO_UNDERSTANDING_PROVIDER", "local")
	t.Setenv("ENABLE_TRANSCRIPTION", "true")
	t.Setenv("TRANSCRIPTION_PROVIDER", "deepgram")
	t.Setenv("TRANSCRIPTION_API_URL", "")
	t.Setenv("TRANSCRIPTION_API_KEY", "")
	t.Setenv("DEEPGRAM_API_KEY", "deepgram-secret")
	t.Setenv("TRANSCRIPTION_MODEL", "")

	configuration, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !configuration.UsesDeepgramTranscription() || configuration.UsesDirectTranscription() {
		t.Fatalf("unexpected transcription mode: %+v", configuration)
	}
	if configuration.TranscriptionAPIKey != "deepgram-secret" || configuration.TranscriptionModel != "nova-3" || configuration.TranscriptionUpstreamBaseURL != "" {
		t.Fatalf("deepgram settings were not loaded: %+v", configuration)
	}
}

func TestLoadUsesOptimizedEvidenceDefaults(t *testing.T) {
	setRequiredEnvironment(t)
	t.Setenv("VIDEO_UNDERSTANDING_PROVIDER", "local")

	configuration, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if configuration.MaxSegmentDuration != 5_000_000_000 ||
		configuration.MaxFramesShort != 12 ||
		configuration.MaxFramesLong != 24 ||
		configuration.MaxCandidateFrames != 450 ||
		configuration.MaxModelFramesShort != 120 ||
		configuration.MaxModelFramesMedium != 120 ||
		configuration.MaxModelFramesLong != 150 ||
		configuration.MaxModelFramesVeryLong != 180 {
		t.Fatalf("evidence defaults were not loaded: %+v", configuration)
	}
}

func TestLoadPrefersVideoQueueURL(t *testing.T) {
	setRequiredEnvironment(t)
	t.Setenv("SQS_VIDEO_QUEUE_URL", "https://sqs.example.test/video")
	t.Setenv("SQS_VIDEO_ANALYSIS_QUEUE_URL", "https://sqs.example.test/video-analysis")
	t.Setenv("SQS_PROCESSING_QUEUE_URL", "https://sqs.example.test/processing")
	t.Setenv("VIDEO_UNDERSTANDING_PROVIDER", "local")

	configuration, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if configuration.QueueURL != "https://sqs.example.test/video" {
		t.Fatalf("QueueURL = %q, want video queue", configuration.QueueURL)
	}
}

func TestLoadFallsBackToVideoAnalysisQueueURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://framemio_app@example.test/postgres")
	t.Setenv("S3_MEDIA_BUCKET", "framemio-media")
	t.Setenv("SQS_VIDEO_ANALYSIS_QUEUE_URL", "https://sqs.example.test/video-analysis")
	t.Setenv("SQS_PROCESSING_QUEUE_URL", "https://sqs.example.test/processing")
	t.Setenv("VIDEO_UNDERSTANDING_PROVIDER", "local")
	t.Setenv("ENABLE_TRANSCRIPTION", "false")

	configuration, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if configuration.QueueURL != "https://sqs.example.test/video-analysis" {
		t.Fatalf("QueueURL = %q, want video-analysis fallback", configuration.QueueURL)
	}
}

func TestLoadFallsBackToProcessingQueueURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://framemio_app@example.test/postgres")
	t.Setenv("S3_MEDIA_BUCKET", "framemio-media")
	t.Setenv("SQS_PROCESSING_QUEUE_URL", "https://sqs.example.test/processing")
	t.Setenv("VIDEO_UNDERSTANDING_PROVIDER", "local")
	t.Setenv("ENABLE_TRANSCRIPTION", "false")

	configuration, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if configuration.QueueURL != "https://sqs.example.test/processing" {
		t.Fatalf("QueueURL = %q, want processing fallback", configuration.QueueURL)
	}
}

func TestLoadPrefersDeepgramSpecificKeyAndBaseURL(t *testing.T) {
	setRequiredEnvironment(t)
	t.Setenv("VIDEO_UNDERSTANDING_PROVIDER", "local")
	t.Setenv("ENABLE_TRANSCRIPTION", "true")
	t.Setenv("TRANSCRIPTION_PROVIDER", "deepgram")
	t.Setenv("TRANSCRIPTION_DIRECT", "true")
	t.Setenv("TRANSCRIPTION_API_URL", "")
	t.Setenv("TRANSCRIPTION_API_KEY", "old-openai-key")
	t.Setenv("DEEPGRAM_API_KEY", "deepgram-secret")
	t.Setenv("TRANSCRIPTION_UPSTREAM_BASE_URL", "https://api.cheaperinference.com/v1")
	t.Setenv("DEEPGRAM_BASE_URL", "https://api.deepgram.com")

	configuration, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if configuration.UsesDirectTranscription() || !configuration.UsesDeepgramTranscription() {
		t.Fatalf("unexpected transcription mode: %+v", configuration)
	}
	if configuration.TranscriptionAPIKey != "deepgram-secret" || configuration.TranscriptionUpstreamBaseURL != "https://api.deepgram.com" {
		t.Fatalf("deepgram-specific env did not take precedence: %+v", configuration)
	}
}

func TestLoadRequiresDeepgramAPIKey(t *testing.T) {
	setRequiredEnvironment(t)
	t.Setenv("VIDEO_UNDERSTANDING_PROVIDER", "local")
	t.Setenv("ENABLE_TRANSCRIPTION", "true")
	t.Setenv("TRANSCRIPTION_PROVIDER", "deepgram")
	t.Setenv("TRANSCRIPTION_API_URL", "")
	t.Setenv("TRANSCRIPTION_API_KEY", "")
	t.Setenv("DEEPGRAM_API_KEY", "")
	t.Setenv("SPEECH_API_KEY", "")
	t.Setenv("AI_GATEWAY_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("VIDEO_UNDERSTANDING_API_KEY", "")

	if _, err := Load(); err == nil {
		t.Fatal("expected missing deepgram API key error")
	}
}

func setRequiredEnvironment(t *testing.T) {
	t.Helper()
	t.Setenv("DATABASE_URL", "postgres://framemio_app@example.test/postgres")
	t.Setenv("S3_MEDIA_BUCKET", "framemio-media")
	t.Setenv("SQS_VIDEO_QUEUE_URL", "https://sqs.example.test/video")
	t.Setenv("TRANSCRIPTION_API_URL", "http://localhost:9001")
	t.Setenv("ENABLE_TRANSCRIPTION", "false")
	t.Setenv("MULTIMODAL_API_URL", "")
	t.Setenv("AI_GATEWAY_BASE_URL", "")
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("GATEWAY_UPSTREAM_BASE_URL", "")
}
