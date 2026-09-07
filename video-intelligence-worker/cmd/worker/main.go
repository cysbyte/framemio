package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/framemio/video-intelligence-worker/internal/callback"
	"github.com/framemio/video-intelligence-worker/internal/config"
	"github.com/framemio/video-intelligence-worker/internal/ffmpeg"
	"github.com/framemio/video-intelligence-worker/internal/pipeline"
	"github.com/framemio/video-intelligence-worker/internal/providers"
	"github.com/framemio/video-intelligence-worker/internal/queue"
	"github.com/framemio/video-intelligence-worker/internal/storage"
	"github.com/framemio/video-intelligence-worker/internal/store"
	"github.com/framemio/video-intelligence-worker/internal/taskprotection"
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
	if err := os.MkdirAll(configuration.WorkRoot, 0o700); err != nil {
		logger.Error("create video intelligence work root", slog.String("work_root", configuration.WorkRoot), slog.Any("error", err))
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
	transcriptionProvider := transcription.TranscriptionProvider(providers.HTTPTranscriptionProvider{
		URL: configuration.TranscriptionURL, APIKey: configuration.TranscriptionAPIKey,
		ProviderName: configuration.TranscriptionProvider, ModelName: configuration.TranscriptionModel,
		Client: transcriptionClient,
	})
	if configuration.UsesDeepgramTranscription() {
		transcriptionProvider = providers.DeepgramTranscriptionProvider{
			BaseURL: configuration.TranscriptionUpstreamBaseURL, APIKey: configuration.TranscriptionAPIKey,
			ProviderName: configuration.TranscriptionProvider, ModelName: configuration.TranscriptionModel,
			Client: transcriptionClient,
		}
	} else if configuration.UsesDirectTranscription() {
		transcriptionProvider = providers.OpenAICompatibleTranscriptionProvider{
			BaseURL: configuration.TranscriptionUpstreamBaseURL, APIKey: configuration.TranscriptionAPIKey,
			ProviderName: configuration.TranscriptionProvider, ModelName: configuration.TranscriptionModel,
			Client: transcriptionClient,
		}
	}
	transcriptionService := &transcription.Service{
		Provider:   transcriptionProvider,
		Repository: repository, Artifacts: objects, RetryAttempts: configuration.TranscriptionRetries,
		ReferenceExpiry:           configuration.PresignedEvidenceExpiry,
		RawArtifactThresholdBytes: configuration.TranscriptionRawArtifactThreshold,
	}
	understandingProvider := understanding.VideoUnderstandingProvider(providers.HTTPVideoUnderstandingProvider{
		URL: configuration.UnderstandingURL, APIKey: configuration.UnderstandingAPIKey,
		ProviderName: configuration.UnderstandingProvider, ModelName: configuration.UnderstandingModel,
		Client: understandingClient,
	})
	if configuration.UsesDirectUnderstanding() {
		understandingProvider = providers.OpenAICompatibleVideoUnderstandingProvider{
			BaseURL: configuration.UnderstandingUpstreamBaseURL, APIKey: configuration.UnderstandingAPIKey,
			ProviderName: configuration.UnderstandingProvider, ModelName: configuration.UnderstandingModel,
			WireAPI: configuration.UnderstandingWireAPI, ImageDetail: configuration.UnderstandingImageDetail,
			MaxOutputTokens: configuration.UnderstandingMaxOutputTokens, Client: understandingClient,
		}
	} else if configuration.UsesLocalUnderstanding() {
		understandingProvider = providers.HeuristicVideoUnderstandingProvider{
			ProviderName: configuration.UnderstandingProvider,
			ModelName:    configuration.UnderstandingModel,
		}
	}
	understandingService := &understanding.Service{
		Provider: understandingProvider, Repository: repository, Artifacts: objects,
		RetryAttempts: configuration.UnderstandingRetries, FrameURLExpiry: configuration.PresignedEvidenceExpiry,
		Logger: logger,
	}
	var completionReporter pipeline.CompletionReporter
	if configuration.BackendURL != "" && configuration.InternalToken != "" {
		completionReporter = callback.BackendReporter{BaseURL: configuration.BackendURL, InternalToken: configuration.InternalToken, Logger: logger}
		logger.Info("backend job completion callback enabled")
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
		Repository: repository, Objects: objects, Tools: tools, Logger: logger, Completion: completionReporter,
		Transcription: transcriptionService, Understanding: understandingService,
	}
	consumer, err := queue.NewConsumer(ctx, configuration.QueueURL, configuration.AWSRegion, configuration.VisibilityTimeout, configuration.MaxJobAttempts, logger)
	if err != nil {
		logger.Error("configure video analysis queue", slog.Any("error", err))
		os.Exit(1)
	}
	consumer.SetTaskProtector(taskprotection.NewFromEnv())
	handler := worker.Handler{Processor: processor, Logger: logger}
	logger.Info("video intelligence worker started",
		slog.String("analysis_version", configuration.AnalysisVersion),
		slog.Bool("run_once", configuration.RunOnce),
		slog.Duration("sqs_visibility_timeout", configuration.VisibilityTimeout),
		slog.Duration("run_once_wait_timeout", configuration.RunOnceWaitTimeout),
		slog.Duration("video_understanding_timeout", configuration.UnderstandingTimeout),
		slog.Int("video_understanding_retries", configuration.UnderstandingRetries),
		slog.String("video_understanding_provider", configuration.UnderstandingProvider),
		slog.String("video_understanding_model", configuration.UnderstandingModel),
		slog.String("queue_name", queueName(configuration.QueueURL)),
		slog.String("target_job_id", configuration.JobID),
	)
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

func queueName(queueURL string) string {
	trimmed := strings.TrimRight(strings.TrimSpace(queueURL), "/")
	if index := strings.LastIndex(trimmed, "/"); index >= 0 {
		return trimmed[index+1:]
	}
	return trimmed
}
