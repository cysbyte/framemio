package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/framemio/video-intelligence-worker/internal/config"
	"github.com/framemio/video-intelligence-worker/internal/ffmpeg"
	"github.com/framemio/video-intelligence-worker/internal/pipeline"
	"github.com/framemio/video-intelligence-worker/internal/providers"
	"github.com/framemio/video-intelligence-worker/internal/queue"
	"github.com/framemio/video-intelligence-worker/internal/storage"
	"github.com/framemio/video-intelligence-worker/internal/store"
	"github.com/framemio/video-intelligence-worker/internal/transcription"
	"github.com/framemio/video-intelligence-worker/internal/understanding"
	"github.com/framemio/video-intelligence-worker/internal/worker"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	configuration, err := config.Load()
	if err != nil {
		logger.Error("invalid video intelligence configuration", slog.Any("error", err))
		os.Exit(1)
	}
	tools := ffmpeg.Toolchain{
		FFmpegPath: configuration.FFmpegPath, FFprobePath: configuration.FFprobePath,
		FrameWidth: configuration.FrameWidth, Runner: ffmpeg.CommandRunner{},
	}
	if err := tools.Validate(); err != nil {
		logger.Error("video tools unavailable", slog.Any("error", err))
		os.Exit(1)
	}
	repository, err := store.NewPostgres(ctx, configuration.DatabaseURL)
	if err != nil {
		logger.Error("connect video analysis database", slog.Any("error", err))
		os.Exit(1)
	}
	defer repository.Close()
	if err := repository.Migrate(ctx); err != nil {
		logger.Error("migrate video analysis records", slog.Any("error", err))
		os.Exit(1)
	}
	objects, err := storage.NewS3(ctx, configuration.S3Bucket, configuration.AWSRegion)
	if err != nil {
		logger.Error("configure video analysis S3", slog.Any("error", err))
		os.Exit(1)
	}
	understandingClient := &http.Client{Timeout: configuration.UnderstandingTimeout}
	transcriptionClient := &http.Client{Timeout: configuration.TranscriptionTimeout}
	transcriptionService := &transcription.Service{
		Provider: providers.HTTPTranscriptionProvider{
			URL: configuration.TranscriptionURL, APIKey: configuration.TranscriptionAPIKey,
			ProviderName: configuration.TranscriptionProvider, ModelName: configuration.TranscriptionModel,
			Client: transcriptionClient,
		},
		Repository: repository, Artifacts: objects, RetryAttempts: configuration.TranscriptionRetries,
		ReferenceExpiry:           configuration.PresignedEvidenceExpiry,
		RawArtifactThresholdBytes: configuration.TranscriptionRawArtifactThreshold,
	}
	understandingService := &understanding.Service{
		Provider: providers.HTTPVideoUnderstandingProvider{
			URL: configuration.UnderstandingURL, APIKey: configuration.UnderstandingAPIKey,
			ProviderName: configuration.UnderstandingProvider, ModelName: configuration.UnderstandingModel,
			Client: understandingClient,
		},
		Repository: repository, Artifacts: objects, RetryAttempts: configuration.UnderstandingRetries,
		FrameURLExpiry: configuration.PresignedEvidenceExpiry,
	}
	processor := &pipeline.Processor{
		Config: pipeline.Config{
			WorkRoot: configuration.WorkRoot, AnalysisVersion: configuration.AnalysisVersion,
			SceneThreshold: configuration.SceneThreshold, MaxSceneBoundaries: configuration.MaxSceneBoundaries,
			Planner: pipeline.PlannerConfig{
				MinSceneDuration: configuration.MinSceneDuration, MaxSegmentDuration: configuration.MaxSegmentDuration,
				LongVideoThreshold: configuration.LongVideoThreshold, MaxFramesShort: configuration.MaxFramesShort,
				MaxFramesLong: configuration.MaxFramesLong,
			},
			DuplicateHashDistance: configuration.DuplicateHashDistance, StageRetries: configuration.StageRetries,
			EnableTranscription: configuration.EnableTranscription, TranscriptionVersion: configuration.TranscriptionVersion,
			UnderstandingVersion: configuration.UnderstandingVersion, EvidenceURLExpiry: configuration.PresignedEvidenceExpiry,
		},
		Repository: repository, Objects: objects, Tools: tools, Logger: logger,
		Transcription: transcriptionService, Understanding: understandingService,
	}
	consumer, err := queue.NewConsumer(ctx, configuration.QueueURL, configuration.AWSRegion, configuration.VisibilityTimeout, configuration.MaxJobAttempts, logger)
	if err != nil {
		logger.Error("configure video analysis queue", slog.Any("error", err))
		os.Exit(1)
	}
	handler := worker.Handler{Processor: processor, Logger: logger}
	logger.Info("video intelligence worker started", slog.String("analysis_version", configuration.AnalysisVersion), slog.Bool("run_once", configuration.RunOnce))
	if configuration.RunOnce {
		if err := consumer.RunOnce(ctx, handler, configuration.JobID, configuration.RunOnceWaitTimeout); err != nil && !errors.Is(err, context.Canceled) {
			logger.Error("video intelligence worker run-once failed", slog.Any("error", err))
			os.Exit(1)
		}
	} else if err := consumer.Run(ctx, handler); err != nil && !errors.Is(err, context.Canceled) {
		logger.Error("video intelligence worker stopped", slog.Any("error", err))
		os.Exit(1)
	}
	logger.Info("video intelligence worker stopped")
}
