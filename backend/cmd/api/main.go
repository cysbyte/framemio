package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/framemio/backend/internal/api"
	"github.com/framemio/backend/internal/queue"
	"github.com/framemio/backend/internal/storage"
	"github.com/framemio/backend/internal/store"
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

	objects, err := storage.NewS3ObjectStorage(ctx, configuration.S3Bucket, configuration.PartSizeBytes, configuration.MultipartThresholdBytes, 15*time.Minute)
	if err != nil {
		logger.Error("configure object storage", slog.Any("error", err))
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

	application := api.NewServer(api.Server{
		Store:         database,
		ObjectStorage: objects,
		Publisher:     publisher,
		Activator:     activator,
		Authenticator: api.HMACTokenAuthenticator{Secret: []byte(configuration.AuthHMACSecret)},
		InternalToken: configuration.InternalAPIToken,
		Logger:        logger,
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
	SQSQueueURL             string
	AuthHMACSecret          string
	InternalAPIToken        string
	PartSizeBytes           int64
	MultipartThresholdBytes int64
}

func loadConfiguration() (configuration, error) {
	result := configuration{
		Port:                    environment("PORT", "8080"),
		DatabaseURL:             os.Getenv("DATABASE_URL"),
		S3Bucket:                os.Getenv("S3_MEDIA_BUCKET"),
		SQSQueueURL:             os.Getenv("SQS_PROCESSING_QUEUE_URL"),
		AuthHMACSecret:          os.Getenv("AUTH_HMAC_SECRET"),
		InternalAPIToken:        os.Getenv("INTERNAL_API_TOKEN"),
		PartSizeBytes:           environmentInt64("UPLOAD_PART_SIZE_BYTES", 8*1024*1024),
		MultipartThresholdBytes: environmentInt64("UPLOAD_MULTIPART_THRESHOLD_BYTES", 16*1024*1024),
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
