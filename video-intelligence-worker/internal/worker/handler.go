package worker

import (
	"context"
	"log/slog"

	"github.com/framemio/video-intelligence-worker/internal/domain"
	"github.com/framemio/video-intelligence-worker/internal/pipeline"
)

type Handler struct {
	Processor *pipeline.Processor
	Logger    *slog.Logger
}

func (h Handler) Process(ctx context.Context, message domain.JobMessage) error {
	h.logger().Info("video analysis started",
		slog.String("job_id", message.JobID), slog.String("project_id", message.ProjectID),
		slog.String("media_id", message.MediaID), slog.String("job_type", message.JobType),
	)
	result, err := h.Processor.Process(ctx, message)
	if err != nil {
		h.logger().Error("video analysis failed",
			slog.String("job_id", message.JobID), slog.String("project_id", message.ProjectID),
			slog.String("media_id", message.MediaID), slog.String("job_type", message.JobType), slog.Any("error", err),
		)
		return err
	}
	h.logger().Info("video analysis completed",
		slog.String("job_id", message.JobID), slog.String("media_id", message.MediaID),
		slog.String("analysis_id", result.Analysis.ID), slog.Bool("reused", result.Reused),
	)
	return nil
}

func (h Handler) MarkFailed(ctx context.Context, message domain.JobMessage, processingError error) error {
	h.logger().Error("mark video analysis terminal failure",
		slog.String("job_id", message.JobID), slog.String("project_id", message.ProjectID),
		slog.String("media_id", message.MediaID), slog.String("job_type", message.JobType), slog.Any("error", processingError),
	)
	return h.Processor.MarkFailed(ctx, message, processingError)
}

func (h Handler) logger() *slog.Logger {
	if h.Logger != nil {
		return h.Logger
	}
	return slog.Default()
}
