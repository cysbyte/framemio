package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/framemio/backend/internal/ai"
	"github.com/framemio/backend/internal/api"
	"github.com/framemio/backend/internal/credits"
	"github.com/framemio/backend/internal/models"
	"github.com/framemio/backend/internal/orchestration"
	"github.com/framemio/backend/internal/queue"
	"github.com/framemio/backend/internal/storage"
	"github.com/framemio/backend/internal/store"
	"github.com/framemio/backend/internal/storyvalidation"
	"github.com/framemio/backend/internal/workeractivation"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	configuration, err := loadConfiguration()
	if err != nil {
		logger.Error("invalid configuration", slog.Any("error", err))
		os.Exit(1)
	}

	database, err := store.NewPostgresStore(ctx, configuration.DatabaseURL)
	if err != nil {
		logger.Error("connect database", slog.Any("error", err))
		os.Exit(1)
	}
	defer database.Close()
	if err := database.Migrate(ctx); err != nil {
		logger.Error("migrate database", slog.Any("error", err))
		os.Exit(1)
	}

	objects, err := storage.NewS3ObjectStorage(ctx, configuration.S3Bucket, configuration.PartSizeBytes, configuration.MultipartThresholdBytes, 2*time.Hour)
	if err != nil {
		logger.Error("configure object storage", slog.Any("error", err))
		os.Exit(1)
	}
	if err := objects.SetDownloadBaseURL(configuration.MediaDownloadBaseURL); err != nil {
		logger.Error("configure media download base url", slog.Any("error", err))
		os.Exit(1)
	}
	publisher, err := queue.NewRoutingSQSPublisher(ctx, configuration.SQSQueueURLs, logger)
	if err != nil {
		logger.Error("configure queue", slog.Any("error", err))
		os.Exit(1)
	}
	ecsActivator, activationEnabled, err := workeractivation.NewECSRunTaskActivatorFromEnv(ctx, logger)
	if err != nil {
		logger.Error("configure worker activation", slog.Any("error", err))
		os.Exit(1)
	}
	var activator workeractivation.Activator
	if activationEnabled {
		activator = ecsActivator
		logger.Info("ECS worker RunTask activation enabled")
	}

	creditService := credits.New(database, credits.LoadPolicyFromEnv(), time.Now)
	storyPlanValidator := storyvalidation.New(database, objects, storyvalidation.DefaultConfig())
	coordinator := orchestration.New(orchestration.Coordinator{Store: database, Publisher: publisher, Activator: activator, Validator: storyPlanValidator, Logger: logger, Credits: creditService})
	if configuration.WorkerRunOnce {
		if err := runWorkerOnce(ctx, database, coordinator, configuration, logger); err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			logger.Error("backend worker run-once failed", slog.Any("error", err))
			os.Exit(1)
		}
		return
	}

	application := api.NewServer(api.Server{
		Store:              database,
		Credits:            creditService,
		ObjectStorage:      objects,
		Publisher:          publisher,
		Activator:          activator,
		Orchestrator:       coordinator,
		StoryPlanValidator: storyPlanValidator,
		Authenticator:      api.HMACTokenAuthenticator{Secret: []byte(configuration.AuthHMACSecret)},
		InternalToken:      configuration.InternalAPIToken,
		Logger:             logger,
	})
	httpServer := &http.Server{
		Addr:              ":" + configuration.Port,
		Handler:           application.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    32 << 10,
	}

	go func() {
		logger.Info("media api listening", slog.String("address", httpServer.Addr))
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("http server failed", slog.Any("error", err))
			stop()
		}
	}()

	<-ctx.Done()
	shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownContext); err != nil {
		logger.Error("http shutdown failed", slog.Any("error", err))
	}
}

type configuration struct {
	Port                         string
	DatabaseURL                  string
	S3Bucket                     string
	MediaDownloadBaseURL         string
	SQSQueueURL                  string
	SQSQueueURLs                 queue.QueueURLs
	AuthHMACSecret               string
	InternalAPIToken             string
	PartSizeBytes                int64
	MultipartThresholdBytes      int64
	DeadLetterQueueURL           string
	TypographyEnabled            bool
	TypographyProvider           string
	TypographyBaseURL            string
	TypographyAPIKey             string
	TypographyModel              string
	TypographyWireAPI            string
	TypographyMaxTokens          int
	TypographyTimeout            time.Duration
	StoryPlannerProvider         string
	StoryPlannerBaseURL          string
	StoryPlannerAPIKey           string
	StoryPlannerModel            string
	StoryPlannerWireAPI          string
	StoryPlannerMaxTokens        int
	StoryPlannerTimeout          time.Duration
	StoryPlannerFallbackTimeout  time.Duration
	StoryRevisionProvider        string
	StoryRevisionBaseURL         string
	StoryRevisionAPIKey          string
	StoryRevisionModel           string
	StoryRevisionWireAPI         string
	StoryRevisionMaxTokens       int
	StoryRevisionTimeout         time.Duration
	StoryRevisionFallbackTimeout time.Duration
	WorkerRunOnce                bool
	WorkerJobID                  string
	WorkerWaitTimeout            time.Duration
	WorkerVisibilityTimeout      time.Duration
}

func loadConfiguration() (configuration, error) {
	result := configuration{
		Port:                 environment("PORT", "8080"),
		DatabaseURL:          os.Getenv("DATABASE_URL"),
		S3Bucket:             os.Getenv("S3_MEDIA_BUCKET"),
		MediaDownloadBaseURL: environment("MEDIA_DOWNLOAD_BASE_URL", os.Getenv("MEDIA_CDN_BASE_URL")),
		SQSQueueURL:          environment("SQS_STORY_QUEUE_URL", os.Getenv("SQS_PROCESSING_QUEUE_URL")),
		SQSQueueURLs: queue.QueueURLs{
			Processing: os.Getenv("SQS_PROCESSING_QUEUE_URL"),
			Story:      os.Getenv("SQS_STORY_QUEUE_URL"),
			Video:      os.Getenv("SQS_VIDEO_QUEUE_URL"),
			Render:     os.Getenv("SQS_RENDER_QUEUE_URL"),
		},
		AuthHMACSecret:               os.Getenv("AUTH_HMAC_SECRET"),
		InternalAPIToken:             os.Getenv("INTERNAL_API_TOKEN"),
		PartSizeBytes:                environmentInt64("UPLOAD_PART_SIZE_BYTES", 8*1024*1024),
		MultipartThresholdBytes:      environmentInt64("UPLOAD_MULTIPART_THRESHOLD_BYTES", 16*1024*1024),
		DeadLetterQueueURL:           os.Getenv("SQS_DEAD_LETTER_QUEUE_URL"),
		TypographyEnabled:            environmentBool("TYPOGRAPHY_ENABLED", true),
		TypographyProvider:           environment("TYPOGRAPHY_PROVIDER", "local"),
		TypographyBaseURL:            environment("TYPOGRAPHY_BASE_URL", os.Getenv("OPENAI_BASE_URL")),
		TypographyAPIKey:             environment("TYPOGRAPHY_API_KEY", os.Getenv("OPENAI_API_KEY")),
		TypographyModel:              os.Getenv("TYPOGRAPHY_MODEL"),
		TypographyWireAPI:            environment("TYPOGRAPHY_WIRE_API", "responses"),
		TypographyMaxTokens:          environmentInt("TYPOGRAPHY_MAX_OUTPUT_TOKENS", 1400),
		TypographyTimeout:            environmentDuration("TYPOGRAPHY_TIMEOUT", 12*time.Second),
		StoryPlannerProvider:         storyPlannerProviderFromEnv(),
		StoryPlannerBaseURL:          environment("STORY_PLANNER_BASE_URL", os.Getenv("OPENAI_BASE_URL")),
		StoryPlannerAPIKey:           environment("STORY_PLANNER_API_KEY", os.Getenv("OPENAI_API_KEY")),
		StoryPlannerModel:            os.Getenv("STORY_PLANNER_MODEL"),
		StoryPlannerWireAPI:          environment("STORY_PLANNER_WIRE_API", "responses"),
		StoryPlannerMaxTokens:        environmentInt("STORY_PLANNER_MAX_OUTPUT_TOKENS", 6000),
		StoryPlannerTimeout:          environmentDuration("STORY_PLANNER_TIMEOUT", 90*time.Second),
		StoryPlannerFallbackTimeout:  environmentDuration("STORY_PLANNER_FALLBACK_TIMEOUT", 10*time.Second),
		StoryRevisionProvider:        environment("STORY_REVISION_PROVIDER", "local"),
		StoryRevisionBaseURL:         environment("STORY_REVISION_BASE_URL", os.Getenv("OPENAI_BASE_URL")),
		StoryRevisionAPIKey:          environment("STORY_REVISION_API_KEY", os.Getenv("OPENAI_API_KEY")),
		StoryRevisionModel:           os.Getenv("STORY_REVISION_MODEL"),
		StoryRevisionWireAPI:         environment("STORY_REVISION_WIRE_API", "responses"),
		StoryRevisionMaxTokens:       environmentInt("STORY_REVISION_MAX_OUTPUT_TOKENS", 6000),
		StoryRevisionTimeout:         environmentDuration("STORY_REVISION_TIMEOUT", 90*time.Second),
		StoryRevisionFallbackTimeout: environmentDuration("STORY_REVISION_FALLBACK_TIMEOUT", 45*time.Second),
		WorkerRunOnce:                environmentBool("WORKER_RUN_ONCE", false),
		WorkerJobID:                  os.Getenv("JOB_ID"),
		WorkerWaitTimeout:            environmentDuration("WORKER_RUN_ONCE_WAIT_TIMEOUT", 6*time.Minute),
		WorkerVisibilityTimeout:      environmentDuration("SQS_VISIBILITY_TIMEOUT", 5*time.Minute),
	}
	if result.DatabaseURL == "" || result.S3Bucket == "" {
		return configuration{}, errors.New("DATABASE_URL and S3_MEDIA_BUCKET are required")
	}
	if missing := queue.MissingRouteKinds(result.SQSQueueURLs); len(missing) > 0 {
		return configuration{}, errors.New("SQS_STORY_QUEUE_URL, SQS_VIDEO_QUEUE_URL, SQS_RENDER_QUEUE_URL, or SQS_PROCESSING_QUEUE_URL fallback are required")
	}
	if len(result.AuthHMACSecret) < 32 {
		return configuration{}, errors.New("AUTH_HMAC_SECRET must contain at least 32 characters")
	}
	if len(result.InternalAPIToken) < 32 {
		return configuration{}, errors.New("INTERNAL_API_TOKEN must contain at least 32 characters")
	}
	if isExternalStoryPlannerProvider(result.StoryPlannerProvider) {
		if strings.TrimSpace(result.StoryPlannerAPIKey) == "" {
			return configuration{}, errors.New("STORY_PLANNER_API_KEY or OPENAI_API_KEY is required when STORY_PLANNER_PROVIDER uses an external AI provider")
		}
		if strings.TrimSpace(result.StoryPlannerModel) == "" {
			return configuration{}, errors.New("STORY_PLANNER_MODEL is required when STORY_PLANNER_PROVIDER uses an external AI provider")
		}
	}
	return result, nil
}

func storyPlannerProviderFromEnv() string {
	provider := environment("STORY_PLANNER_PROVIDER", "local")
	if strings.TrimSpace(os.Getenv("STORY_PLANNER_PROVIDER")) != "" {
		return provider
	}
	if environmentBool("ENABLE_GPT_STORY_PLANNER", false) {
		return "openai-compatible"
	}
	return provider
}

func isExternalStoryPlannerProvider(provider string) bool {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "openai", "openai-compatible", "ai-gateway", "ai_gateway":
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

func environmentInt64(name string, fallback int64) int64 {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func runWorkerOnce(ctx context.Context, database store.Store, coordinator *orchestration.Coordinator, configuration configuration, logger *slog.Logger) error {
	consumer, err := queue.NewSQSConsumer(ctx, configuration.SQSQueueURL, configuration.DeadLetterQueueURL)
	if err != nil {
		return err
	}
	workerContext := ctx
	if configuration.WorkerJobID != "" {
		var cancel context.CancelFunc
		workerContext, cancel = context.WithTimeout(ctx, configuration.WorkerWaitTimeout)
		defer cancel()
	}
	storyPlanner, err := configuredStoryPlanner(configuration, logger)
	if err != nil {
		return err
	}
	revisionPlanner, err := configuredStoryRevisionPlanner(configuration, logger)
	if err != nil {
		return err
	}
	worker := orchestration.Worker{
		Store:       database,
		Consumer:    consumer,
		Coordinator: coordinator,
		Handlers: map[models.JobType]orchestration.JobHandler{
			models.JobTypeStoryPlan: orchestration.StoryPlanHandler{Store: database, Planner: storyPlanner, RevisionPlanner: revisionPlanner, Validator: coordinator.Validator, Logger: logger},
		},
		Logger:            logger,
		TargetJobID:       configuration.WorkerJobID,
		VisibilityTimeout: configuration.WorkerVisibilityTimeout,
		WaitTime:          20 * time.Second,
	}
	logger.Info("backend worker run-once started", slog.String("job_id", configuration.WorkerJobID))
	return worker.RunOnce(workerContext)
}

func configuredStoryPlanner(configuration configuration, logger *slog.Logger) (ai.StoryPlanner, error) {
	typographyPlanner, err := configuredTypographyPlanner(configuration)
	if err != nil {
		return nil, err
	}
	switch strings.ToLower(strings.TrimSpace(configuration.StoryPlannerProvider)) {
	case "", "local", "default", "rules", "rule", "heuristic":
		return &ai.Planner{TypographyPlanner: typographyPlanner, Logger: logger}, nil
	case "openai", "openai-compatible", "ai-gateway", "ai_gateway":
		if strings.TrimSpace(configuration.StoryPlannerAPIKey) == "" {
			return nil, errors.New("STORY_PLANNER_API_KEY or OPENAI_API_KEY is required when STORY_PLANNER_PROVIDER uses an external AI provider")
		}
		if strings.TrimSpace(configuration.StoryPlannerModel) == "" {
			return nil, errors.New("STORY_PLANNER_MODEL is required when STORY_PLANNER_PROVIDER uses an external AI provider")
		}
		primary := ai.OpenAICompatibleStoryPlanner{
			BaseURL:         configuration.StoryPlannerBaseURL,
			APIKey:          configuration.StoryPlannerAPIKey,
			ProviderName:    configuration.StoryPlannerProvider,
			ModelName:       configuration.StoryPlannerModel,
			WireAPI:         configuration.StoryPlannerWireAPI,
			MaxOutputTokens: configuration.StoryPlannerMaxTokens,
			Client:          &http.Client{Timeout: configuration.StoryPlannerTimeout},
		}
		return ai.FallbackStoryPlanner{
			Primary:        primary,
			Fallback:       &ai.Planner{TypographyPlanner: typographyPlanner, Logger: logger},
			Logger:         logger,
			PrimaryTimeout: configuration.StoryPlannerFallbackTimeout,
		}, nil
	default:
		return nil, fmt.Errorf("unsupported STORY_PLANNER_PROVIDER %q", configuration.StoryPlannerProvider)
	}
}

func configuredStoryRevisionPlanner(configuration configuration, logger *slog.Logger) (ai.StoryRevisionPlanner, error) {
	switch strings.ToLower(strings.TrimSpace(configuration.StoryRevisionProvider)) {
	case "", "local", "default", "rules", "rule", "heuristic":
		return ai.HeuristicStoryRevisionPlanner{Logger: logger}, nil
	case "openai", "openai-compatible", "ai-gateway", "ai_gateway":
		if strings.TrimSpace(configuration.StoryRevisionAPIKey) == "" {
			return nil, errors.New("STORY_REVISION_API_KEY or OPENAI_API_KEY is required when STORY_REVISION_PROVIDER uses an external AI provider")
		}
		if strings.TrimSpace(configuration.StoryRevisionModel) == "" {
			return nil, errors.New("STORY_REVISION_MODEL is required when STORY_REVISION_PROVIDER uses an external AI provider")
		}
		primary := ai.OpenAICompatibleStoryRevisionPlanner{BaseURL: configuration.StoryRevisionBaseURL, APIKey: configuration.StoryRevisionAPIKey, ModelName: configuration.StoryRevisionModel, ProviderName: configuration.StoryRevisionProvider, WireAPI: configuration.StoryRevisionWireAPI, MaxOutputTokens: configuration.StoryRevisionMaxTokens, Client: &http.Client{Timeout: configuration.StoryRevisionTimeout}}
		return ai.FallbackStoryRevisionPlanner{Primary: primary, Fallback: ai.HeuristicStoryRevisionPlanner{Logger: logger}, Logger: logger, PrimaryTimeout: configuration.StoryRevisionFallbackTimeout}, nil
	case "disabled", "off", "none":
		return nil, nil
	default:
		return nil, fmt.Errorf("unsupported STORY_REVISION_PROVIDER %q", configuration.StoryRevisionProvider)
	}
}

func configuredTypographyPlanner(configuration configuration) (ai.TypographyPlanner, error) {
	if !configuration.TypographyEnabled {
		return ai.DisabledTypographyPlanner{}, nil
	}
	switch strings.ToLower(strings.TrimSpace(configuration.TypographyProvider)) {
	case "", "local", "default", "rules", "rule":
		return ai.DefaultTypographyPlanner{}, nil
	case "disabled", "off", "none":
		return ai.DisabledTypographyPlanner{}, nil
	case "openai", "openai-compatible", "ai-gateway", "ai_gateway":
		if strings.TrimSpace(configuration.TypographyAPIKey) == "" {
			return nil, errors.New("TYPOGRAPHY_API_KEY or OPENAI_API_KEY is required when TYPOGRAPHY_PROVIDER uses an external AI provider")
		}
		if strings.TrimSpace(configuration.TypographyModel) == "" {
			return nil, errors.New("TYPOGRAPHY_MODEL is required when TYPOGRAPHY_PROVIDER uses an external AI provider")
		}
		return ai.OpenAICompatibleTypographyPlanner{
			BaseURL:         configuration.TypographyBaseURL,
			APIKey:          configuration.TypographyAPIKey,
			ProviderName:    configuration.TypographyProvider,
			ModelName:       configuration.TypographyModel,
			WireAPI:         configuration.TypographyWireAPI,
			MaxOutputTokens: configuration.TypographyMaxTokens,
			Client:          &http.Client{Timeout: configuration.TypographyTimeout},
		}, nil
	default:
		return nil, fmt.Errorf("unsupported TYPOGRAPHY_PROVIDER %q", configuration.TypographyProvider)
	}
}

func environmentInt(name string, fallback int) int {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func environmentBool(name string, fallback bool) bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(name)))
	switch value {
	case "1", "true", "yes", "enabled":
		return true
	case "0", "false", "no", "disabled":
		return false
	default:
		return fallback
	}
}

func environmentDuration(name string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}
