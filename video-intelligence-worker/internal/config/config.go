package config

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	DatabaseURL                       string
	S3Bucket                          string
	QueueURL                          string
	AWSRegion                         string
	FFmpegPath                        string
	FFprobePath                       string
	WorkRoot                          string
	AnalysisVersion                   string
	UnderstandingURL                  string
	UnderstandingAPIKey               string
	UnderstandingProvider             string
	UnderstandingModel                string
	UnderstandingVersion              string
	UnderstandingRetries              int
	UnderstandingTimeout              time.Duration
	UnderstandingDirect               bool
	UnderstandingUpstreamBaseURL      string
	UnderstandingWireAPI              string
	UnderstandingImageDetail          string
	UnderstandingMaxOutputTokens      int
	TranscriptionURL                  string
	TranscriptionAPIKey               string
	TranscriptionProvider             string
	TranscriptionModel                string
	TranscriptionVersion              string
	TranscriptionDirect               bool
	TranscriptionUpstreamBaseURL      string
	EnableTranscription               bool
	TranscriptionRetries              int
	TranscriptionTimeout              time.Duration
	TranscriptionRawArtifactThreshold int
	SceneThreshold                    float64
	MinSceneDuration                  time.Duration
	MaxSegmentDuration                time.Duration
	LongVideoThreshold                time.Duration
	MaxFramesShort                    int
	MaxFramesLong                     int
	MaxSceneBoundaries                int
	DuplicateHashDistance             int
	FrameWidth                        int
	StageRetries                      int
	MaxJobAttempts                    int
	VisibilityTimeout                 time.Duration
	RunOnce                           bool
	JobID                             string
	RunOnceWaitTimeout                time.Duration
	PresignedEvidenceExpiry           time.Duration
	BackendURL                        string
	InternalToken                     string
}

func Load() (Config, error) {
	workRoot := environment("VIDEO_WORK_ROOT", filepath.Join(os.TempDir(), "framemio-video-intelligence"))
	configuration := Config{
		DatabaseURL:                       os.Getenv("DATABASE_URL"),
		S3Bucket:                          os.Getenv("S3_MEDIA_BUCKET"),
		QueueURL:                          os.Getenv("SQS_VIDEO_ANALYSIS_QUEUE_URL"),
		AWSRegion:                         os.Getenv("AWS_REGION"),
		FFmpegPath:                        environment("FFMPEG_PATH", "ffmpeg"),
		FFprobePath:                       environment("FFPROBE_PATH", "ffprobe"),
		WorkRoot:                          workRoot,
		AnalysisVersion:                   environment("VIDEO_ANALYSIS_VERSION", "video-v2"),
		UnderstandingURL:                  environmentFirst([]string{"VIDEO_UNDERSTANDING_API_URL", "MULTIMODAL_API_URL"}, ""),
		UnderstandingAPIKey:               environmentFirst([]string{"VIDEO_UNDERSTANDING_API_KEY", "MULTIMODAL_API_KEY", "AI_GATEWAY_API_KEY", "OPENAI_API_KEY"}, ""),
		UnderstandingProvider:             environmentFirst([]string{"VIDEO_UNDERSTANDING_PROVIDER", "MULTIMODAL_PROVIDER"}, "external"),
		UnderstandingModel:                environmentFirst([]string{"VIDEO_UNDERSTANDING_MODEL", "MULTIMODAL_MODEL", "OPENAI_VISION_MODEL"}, "configured-model"),
		UnderstandingVersion:              environment("VIDEO_UNDERSTANDING_VERSION", "video-understanding-v1"),
		UnderstandingRetries:              environmentInt("VIDEO_UNDERSTANDING_RETRIES", 3),
		UnderstandingTimeout:              environmentDurationFirst([]string{"VIDEO_UNDERSTANDING_PROVIDER_TIMEOUT", "PROVIDER_TIMEOUT", "OPENAI_REQUEST_TIMEOUT_MS"}, 2*time.Minute),
		UnderstandingDirect:               environmentBool("VIDEO_UNDERSTANDING_DIRECT", false),
		UnderstandingUpstreamBaseURL:      environmentFirst([]string{"VIDEO_UNDERSTANDING_UPSTREAM_BASE_URL", "AI_GATEWAY_BASE_URL", "OPENAI_BASE_URL", "GATEWAY_UPSTREAM_BASE_URL"}, ""),
		UnderstandingWireAPI:              environmentFirst([]string{"VIDEO_UNDERSTANDING_WIRE_API", "AI_GATEWAY_WIRE_API", "GATEWAY_UPSTREAM_WIRE_API"}, "chat_completions"),
		UnderstandingImageDetail:          environment("VIDEO_UNDERSTANDING_IMAGE_DETAIL", "low"),
		UnderstandingMaxOutputTokens:      environmentInt("VIDEO_UNDERSTANDING_MAX_OUTPUT_TOKENS", 5000),
		TranscriptionURL:                  environmentFirst([]string{"TRANSCRIPTION_API_URL", "SPEECH_API_URL"}, ""),
		TranscriptionAPIKey:               environmentFirst([]string{"TRANSCRIPTION_API_KEY", "DEEPGRAM_API_KEY", "SPEECH_API_KEY", "AI_GATEWAY_API_KEY", "OPENAI_API_KEY", "VIDEO_UNDERSTANDING_API_KEY"}, ""),
		TranscriptionProvider:             environmentFirst([]string{"TRANSCRIPTION_PROVIDER", "SPEECH_PROVIDER"}, "external"),
		TranscriptionModel:                environmentFirst([]string{"TRANSCRIPTION_MODEL", "SPEECH_MODEL", "OPENAI_TRANSCRIPTION_MODEL"}, "configured-model"),
		TranscriptionVersion:              environment("TRANSCRIPTION_VERSION", "transcription-v1"),
		TranscriptionDirect:               environmentBool("TRANSCRIPTION_DIRECT", false),
		TranscriptionUpstreamBaseURL:      environmentFirst([]string{"TRANSCRIPTION_UPSTREAM_BASE_URL", "AI_SPEECH_BASE_URL", "OPENAI_SPEECH_BASE_URL", "AI_GATEWAY_BASE_URL", "OPENAI_BASE_URL", "VIDEO_UNDERSTANDING_UPSTREAM_BASE_URL"}, ""),
		EnableTranscription:               environmentBoolFirst([]string{"ENABLE_TRANSCRIPTION", "ENABLE_SPEECH_TO_TEXT"}, true),
		TranscriptionRetries:              environmentInt("TRANSCRIPTION_RETRIES", 3),
		TranscriptionTimeout:              environmentDuration("TRANSCRIPTION_PROVIDER_TIMEOUT", 90*time.Second),
		TranscriptionRawArtifactThreshold: environmentInt("TRANSCRIPTION_RAW_ARTIFACT_THRESHOLD_BYTES", 256<<10),
		SceneThreshold:                    environmentFloat("SCENE_CHANGE_THRESHOLD", 0.32),
		MinSceneDuration:                  environmentDuration("MIN_SCENE_DURATION", 750*time.Millisecond),
		MaxSegmentDuration:                environmentDuration("MAX_SEGMENT_DURATION", 4*time.Second),
		LongVideoThreshold:                environmentDuration("LONG_VIDEO_THRESHOLD", 5*time.Minute),
		MaxFramesShort:                    environmentInt("MAX_FRAMES_SHORT", 24),
		MaxFramesLong:                     environmentInt("MAX_FRAMES_LONG", 48),
		MaxSceneBoundaries:                environmentInt("MAX_SCENE_BOUNDARIES", 240),
		DuplicateHashDistance:             environmentInt("DUPLICATE_HASH_DISTANCE", 6),
		FrameWidth:                        environmentInt("ANALYSIS_FRAME_WIDTH", 1280),
		StageRetries:                      environmentInt("STAGE_RETRIES", 3),
		MaxJobAttempts:                    environmentInt("MAX_JOB_ATTEMPTS", 5),
		VisibilityTimeout:                 environmentDuration("SQS_VISIBILITY_TIMEOUT", 2*time.Minute),
		RunOnce:                           environmentBool("WORKER_RUN_ONCE", false),
		JobID:                             os.Getenv("JOB_ID"),
		RunOnceWaitTimeout:                environmentDuration("WORKER_RUN_ONCE_WAIT_TIMEOUT", 2*time.Minute),
		PresignedEvidenceExpiry:           environmentDuration("EVIDENCE_URL_EXPIRY", 30*time.Minute),
		BackendURL:                        environmentFirst([]string{"BACKEND_INTERNAL_API_URL", "BACKEND_URL", "API_BASE_URL"}, ""),
		InternalToken:                     os.Getenv("INTERNAL_API_TOKEN"),
	}
	if configuration.DatabaseURL == "" || configuration.S3Bucket == "" || configuration.QueueURL == "" {
		return Config{}, errors.New("DATABASE_URL, S3_MEDIA_BUCKET, and SQS_VIDEO_ANALYSIS_QUEUE_URL are required")
	}
	if !configuration.UsesLocalUnderstanding() && !configuration.UsesDirectUnderstanding() && configuration.UnderstandingURL == "" {
		return Config{}, errors.New("VIDEO_UNDERSTANDING_API_URL is required")
	}
	if configuration.UsesDirectUnderstanding() && configuration.UnderstandingUpstreamBaseURL == "" {
		return Config{}, errors.New("VIDEO_UNDERSTANDING_UPSTREAM_BASE_URL is required when direct video understanding is enabled")
	}
	if configuration.UsesDirectUnderstanding() && configuration.UnderstandingAPIKey == "" {
		return Config{}, errors.New("VIDEO_UNDERSTANDING_API_KEY is required when direct video understanding is enabled")
	}
	if configuration.UsesDeepgramTranscription() {
		if configuration.TranscriptionModel == "configured-model" {
			configuration.TranscriptionModel = "nova-3"
		}
		if deepgramAPIKey := os.Getenv("DEEPGRAM_API_KEY"); deepgramAPIKey != "" {
			configuration.TranscriptionAPIKey = deepgramAPIKey
		}
		if deepgramBaseURL := os.Getenv("DEEPGRAM_BASE_URL"); deepgramBaseURL != "" {
			configuration.TranscriptionUpstreamBaseURL = deepgramBaseURL
		} else if !strings.Contains(configuration.TranscriptionUpstreamBaseURL, "deepgram") {
			configuration.TranscriptionUpstreamBaseURL = ""
		}
	}
	if configuration.EnableTranscription && !configuration.UsesDirectTranscription() && !configuration.UsesDeepgramTranscription() && configuration.TranscriptionURL == "" {
		return Config{}, errors.New("TRANSCRIPTION_API_URL is required when transcription is enabled")
	}
	if configuration.EnableTranscription && configuration.UsesDirectTranscription() && configuration.TranscriptionUpstreamBaseURL == "" {
		return Config{}, errors.New("TRANSCRIPTION_UPSTREAM_BASE_URL is required when direct transcription is enabled")
	}
	if configuration.EnableTranscription && (configuration.UsesDirectTranscription() || configuration.UsesDeepgramTranscription()) && configuration.TranscriptionAPIKey == "" {
		return Config{}, errors.New("TRANSCRIPTION_API_KEY or DEEPGRAM_API_KEY is required when transcription provider requires backend credentials")
	}
	if configuration.SceneThreshold <= 0 || configuration.SceneThreshold >= 1 {
		return Config{}, errors.New("SCENE_CHANGE_THRESHOLD must be between zero and one")
	}
	if configuration.MaxFramesShort <= 0 || configuration.MaxFramesLong < configuration.MaxFramesShort || configuration.FrameWidth <= 0 {
		return Config{}, errors.New("frame limits and ANALYSIS_FRAME_WIDTH are invalid")
	}
	return configuration, nil
}

func (c Config) UsesLocalUnderstanding() bool {
	switch strings.ToLower(strings.TrimSpace(c.UnderstandingProvider)) {
	case "local", "heuristic":
		return true
	default:
		return false
	}
}

func (c Config) UsesDirectUnderstanding() bool {
	if c.UnderstandingDirect {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(c.UnderstandingProvider)) {
	case "openai-direct", "gpt-direct":
		return true
	default:
		return false
	}
}

func (c Config) UsesDirectTranscription() bool {
	if c.UsesDeepgramTranscription() {
		return false
	}
	if c.TranscriptionDirect {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(c.TranscriptionProvider)) {
	case "openai-direct", "gpt-direct":
		return true
	default:
		return false
	}
}

func (c Config) UsesDeepgramTranscription() bool {
	switch strings.ToLower(strings.TrimSpace(c.TranscriptionProvider)) {
	case "deepgram", "deepgram-direct":
		return true
	default:
		return false
	}
}

func environment(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func environmentFirst(names []string, fallback string) string {
	for _, name := range names {
		if value := os.Getenv(name); value != "" {
			return value
		}
	}
	return fallback
}

func environmentInt(name string, fallback int) int {
	value, err := strconv.Atoi(os.Getenv(name))
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func environmentFloat(name string, fallback float64) float64 {
	value, err := strconv.ParseFloat(os.Getenv(name), 64)
	if err != nil {
		return fallback
	}
	return value
}

func environmentBool(name string, fallback bool) bool {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func environmentBoolFirst(names []string, fallback bool) bool {
	for _, name := range names {
		if os.Getenv(name) != "" {
			return environmentBool(name, fallback)
		}
	}
	return fallback
}

func environmentDuration(name string, fallback time.Duration) time.Duration {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func environmentDurationFirst(names []string, fallback time.Duration) time.Duration {
	for _, name := range names {
		if os.Getenv(name) != "" {
			return environmentDuration(name, fallback)
		}
	}
	return fallback
}
