package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/framemio/backend/internal/api"
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
	publisher, err := queue.NewSQSPublisher(ctx, configuration.SQSQueueURL)
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

	storyPlanValidator := storyvalidation.New(database, objects, storyvalidation.DefaultConfig())
	coordinator := orchestration.New(orchestration.Coordinator{Store: database, Publisher: publisher, Activator: activator, Validator: storyPlanValidator, Logger: logger})
	if configuration.WorkerRunOnce {
		if err := runWorkerOnce(ctx, database, coordinator, configuration, logger); err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			logger.Error("backend worker run-once failed", slog.Any("error", err))
			os.Exit(1)
		}
		return
	}

	application := api.NewServer(api.Server{
		Store:              database,
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
	Port                    string
	DatabaseURL             string
	S3Bucket                string
	MediaDownloadBaseURL    string
	SQSQueueURL             string
	AuthHMACSecret          string
	InternalAPIToken        string
	PartSizeBytes           int64
	MultipartThresholdBytes int64
	DeadLetterQueueURL      string
	WorkerRunOnce           bool
	WorkerJobID             string
	WorkerWaitTimeout       time.Duration
}

func loadConfiguration() (configuration, error) {
	result := configuration{
		Port:                    environment("PORT", "8080"),
		DatabaseURL:             os.Getenv("DATABASE_URL"),
		S3Bucket:                os.Getenv("S3_MEDIA_BUCKET"),
		MediaDownloadBaseURL:    environment("MEDIA_DOWNLOAD_BASE_URL", os.Getenv("MEDIA_CDN_BASE_URL")),
		SQSQueueURL:             os.Getenv("SQS_PROCESSING_QUEUE_URL"),
		AuthHMACSecret:          os.Getenv("AUTH_HMAC_SECRET"),
		InternalAPIToken:        os.Getenv("INTERNAL_API_TOKEN"),
		PartSizeBytes:           environmentInt64("UPLOAD_PART_SIZE_BYTES", 8*1024*1024),
		MultipartThresholdBytes: environmentInt64("UPLOAD_MULTIPART_THRESHOLD_BYTES", 16*1024*1024),
		DeadLetterQueueURL:      os.Getenv("SQS_DEAD_LETTER_QUEUE_URL"),
		WorkerRunOnce:           environmentBool("WORKER_RUN_ONCE", false),
		WorkerJobID:             os.Getenv("JOB_ID"),
		WorkerWaitTimeout:       environmentDuration("WORKER_RUN_ONCE_WAIT_TIMEOUT", 2*time.Minute),
	}
	if result.DatabaseURL == "" || result.S3Bucket == "" || result.SQSQueueURL == "" {
		return configuration{}, errors.New("DATABASE_URL, S3_MEDIA_BUCKET, and SQS_PROCESSING_QUEUE_URL are required")
	}
	if len(result.AuthHMACSecret) < 32 {
		return configuration{}, errors.New("AUTH_HMAC_SECRET must contain at least 32 characters")
	}
	if len(result.InternalAPIToken) < 32 {
		return configuration{}, errors.New("INTERNAL_API_TOKEN must contain at least 32 characters")
	}
	return result, nil
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
	worker := orchestration.Worker{
		Store:             database,
		Consumer:          consumer,
		Coordinator:       coordinator,
		Handlers:          map[models.JobType]orchestration.JobHandler{models.JobTypeStoryPlan: orchestration.StoryPlanHandler{Store: database, Logger: logger}},
		Logger:            logger,
		TargetJobID:       configuration.WorkerJobID,
		VisibilityTimeout: 2 * time.Minute,
		WaitTime:          20 * time.Second,
	}
	logger.Info("backend worker run-once started", slog.String("job_id", configuration.WorkerJobID))
	return worker.RunOnce(workerContext)
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
