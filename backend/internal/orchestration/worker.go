package orchestration

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/framemio/backend/internal/models"
	"github.com/framemio/backend/internal/queue"
	"github.com/framemio/backend/internal/store"
)

type JobHandler interface {
	HandleJob(ctx context.Context, job models.ProcessingJob) error
}

type Worker struct {
	Store             store.Store
	Consumer          queue.Consumer
	Coordinator       *Coordinator
	Handlers          map[models.JobType]JobHandler
	Logger            *slog.Logger
	MaxReceiveCount   int
	VisibilityTimeout time.Duration
	WaitTime          time.Duration
	Now               func() time.Time
	TargetJobID       string
}

func (w *Worker) RunOnce(ctx context.Context) error {
	if w.Consumer == nil || w.Store == nil || w.Coordinator == nil {
		return errors.New("worker dependencies are not configured")
	}
	maxReceiveCount := w.MaxReceiveCount
	if maxReceiveCount <= 0 {
		maxReceiveCount = 5
	}
	visibilityTimeout := w.VisibilityTimeout
	if visibilityTimeout <= 0 {
		visibilityTimeout = 2 * time.Minute
	}
	logger(w).Info("orchestration worker run-once polling started", slog.String("target_job_id", w.TargetJobID), slog.Duration("wait_time", w.WaitTime), slog.Duration("visibility_timeout", visibilityTimeout))
	for {
		receivedMessages, err := w.Consumer.Receive(ctx, 10, w.WaitTime, visibilityTimeout)
		if err != nil {
			return err
		}
		if len(receivedMessages) > 0 {
			logger(w).Info("orchestration worker run-once received messages", slog.String("target_job_id", w.TargetJobID), slog.Int("count", len(receivedMessages)))
		}
		for _, received := range receivedMessages {
			logger(w).Info("orchestration worker run-once inspecting message", slog.String("target_job_id", w.TargetJobID), slog.String("job_id", received.Message.JobID), slog.String("job_type", string(received.Message.JobType)), slog.String("project_id", received.Message.ProjectID), slog.String("media_id", received.Message.MediaID), slog.Int("receive_count", received.ApproximateReceiveCount), slog.Bool("is_target", w.TargetJobID != "" && received.Message.JobID == w.TargetJobID))
			if w.TargetJobID != "" && received.Message.JobID != w.TargetJobID && w.Handlers[received.Message.JobType] == nil {
				logger(w).Info("orchestration worker run-once deferring unsupported message", slog.String("target_job_id", w.TargetJobID), slog.String("job_id", received.Message.JobID), slog.String("job_type", string(received.Message.JobType)))
				if err := w.Consumer.ExtendVisibility(ctx, received.ReceiptHandle, 2*time.Second); err != nil {
					return err
				}
				if err := sleep(ctx, 1500*time.Millisecond); err != nil {
					return err
				}
				continue
			}
			if received.ApproximateReceiveCount > maxReceiveCount {
				job, err := w.Store.GetJobByID(ctx, received.Message.JobID)
				if errors.Is(err, store.ErrNotFound) {
					logger(w).Warn("orchestration worker deleting stale queue message", slog.String("target_job_id", w.TargetJobID), slog.String("job_id", received.Message.JobID), slog.String("job_type", string(received.Message.JobType)), slog.String("project_id", received.Message.ProjectID), slog.String("media_id", received.Message.MediaID), slog.Int("receive_count", received.ApproximateReceiveCount))
					if err := w.Consumer.Delete(ctx, received.ReceiptHandle); err != nil {
						return err
					}
					if w.TargetJobID != "" && received.Message.JobID == w.TargetJobID {
						return store.ErrNotFound
					}
					continue
				}
				if err != nil {
					logger(w).Error("orchestration worker failed to load received job", slog.String("target_job_id", w.TargetJobID), slog.String("job_id", received.Message.JobID), slog.String("job_type", string(received.Message.JobType)), slog.String("project_id", received.Message.ProjectID), slog.String("media_id", received.Message.MediaID), slog.Any("error", err))
					return err
				}
				if w.TargetJobID == "" || shouldDeadLetterForReceiveLimit(job) {
					if _, err := w.Store.UpdateJob(ctx, received.Message.JobID, models.JobStatusFailed, 0, "max_receive_count_exceeded", "job exceeded queue receive limit", "", ""); err != nil {
						return err
					}
					if err := w.deadLetterOrDelete(ctx, received, "max_receive_count_exceeded"); err != nil {
						return err
					}
					if w.TargetJobID != "" && received.Message.JobID != w.TargetJobID {
						logger(w).Warn("orchestration worker removed poison non-target message while waiting for target", slog.String("target_job_id", w.TargetJobID), slog.String("job_id", received.Message.JobID), slog.String("job_type", string(received.Message.JobType)), slog.String("project_id", received.Message.ProjectID), slog.Int("receive_count", received.ApproximateReceiveCount))
						continue
					}
					return nil
				}
			}
			if w.TargetJobID != "" && received.Message.JobID != w.TargetJobID {
				logger(w).Info("orchestration worker run-once handling available message", slog.String("target_job_id", w.TargetJobID), slog.String("job_id", received.Message.JobID), slog.String("job_type", string(received.Message.JobType)))
			}
			handled, err := w.handleReceived(ctx, received, visibilityTimeout)
			if err != nil {
				return err
			}
			if w.TargetJobID != "" && received.Message.JobID == w.TargetJobID && handled {
				logger(w).Info("orchestration worker run-once target handled", slog.String("target_job_id", w.TargetJobID), slog.String("job_id", received.Message.JobID), slog.String("job_type", string(received.Message.JobType)))
				return nil
			}
			if w.TargetJobID != "" {
				logger(w).Info("orchestration worker run-once continuing after available message", slog.String("target_job_id", w.TargetJobID), slog.String("handled_job_id", received.Message.JobID), slog.String("handled_job_type", string(received.Message.JobType)))
			}
		}
		if w.TargetJobID == "" {
			return nil
		}
		if err := ctx.Err(); err != nil {
			logger(w).Info("orchestration worker run-once stopped before target", slog.String("target_job_id", w.TargetJobID), slog.Any("error", err))
			return err
		}
	}
}

func (w *Worker) handleReceived(ctx context.Context, received queue.ReceivedJobMessage, visibilityTimeout time.Duration) (bool, error) {
	logger(w).Info("orchestration worker loading received job", slog.String("job_id", received.Message.JobID), slog.String("job_type", string(received.Message.JobType)), slog.String("project_id", received.Message.ProjectID), slog.String("media_id", received.Message.MediaID), slog.Int("receive_count", received.ApproximateReceiveCount))
	job, err := w.Store.GetJobByID(ctx, received.Message.JobID)
	if errors.Is(err, store.ErrNotFound) {
		logger(w).Warn("orchestration worker deleting stale queue message", slog.String("job_id", received.Message.JobID), slog.String("job_type", string(received.Message.JobType)), slog.String("project_id", received.Message.ProjectID), slog.String("media_id", received.Message.MediaID), slog.Int("receive_count", received.ApproximateReceiveCount))
		if err := w.Consumer.Delete(ctx, received.ReceiptHandle); err != nil {
			return false, err
		}
		return false, nil
	}
	if err != nil {
		logger(w).Error("orchestration worker failed to load received job", slog.String("job_id", received.Message.JobID), slog.String("job_type", string(received.Message.JobType)), slog.String("project_id", received.Message.ProjectID), slog.String("media_id", received.Message.MediaID), slog.Any("error", err))
		return false, err
	}
	logger(w).Info("orchestration worker handling job", slog.String("job_id", job.ID), slog.String("job_type", string(job.Type)), slog.String("project_id", job.ProjectID), slog.String("media_id", job.MediaID), slog.String("status", string(job.Status)), slog.Int("attempt", job.Attempt), slog.Int("receive_count", received.ApproximateReceiveCount))
	if job.Status == models.JobStatusCompleted || job.Status == models.JobStatusCancelled {
		logger(w).Info("orchestration worker deleting terminal message", slog.String("job_id", job.ID), slog.String("job_type", string(job.Type)), slog.String("status", string(job.Status)))
		return true, w.Consumer.Delete(ctx, received.ReceiptHandle)
	}
	if job.NextRunAt != nil && now(w).Before(*job.NextRunAt) {
		logger(w).Info("orchestration worker deferring job until next run", slog.String("job_id", job.ID), slog.String("job_type", string(job.Type)), slog.Time("next_run_at", *job.NextRunAt))
		return true, w.Consumer.ExtendVisibility(ctx, received.ReceiptHandle, time.Until(*job.NextRunAt))
	}
	handler := w.Handlers[job.Type]
	if handler == nil {
		logger(w).Warn("orchestration worker handler not configured", slog.String("job_id", job.ID), slog.String("job_type", string(job.Type)))
		_, err := w.Coordinator.UpdateJobStatus(ctx, job.ID, models.JobStatusFailed, job.Progress, "handler_not_configured", "no worker handler is configured for this job type", "", "")
		return true, err
	}
	if _, err := w.Coordinator.UpdateJobStatus(ctx, job.ID, models.JobStatusRunning, job.Progress, "", "", "", ""); err != nil {
		return true, err
	}
	if err := w.Consumer.ExtendVisibility(ctx, received.ReceiptHandle, visibilityTimeout); err != nil {
		return true, err
	}
	if err := handler.HandleJob(ctx, job); err != nil {
		logger(w).Error("orchestration worker handler failed", slog.String("job_id", job.ID), slog.String("job_type", string(job.Type)), slog.String("project_id", job.ProjectID), slog.Any("error", err))
		_, updateErr := w.Coordinator.UpdateJobStatus(ctx, job.ID, models.JobStatusFailed, job.Progress, "handler_failed", err.Error(), "", "")
		if updateErr != nil {
			return true, updateErr
		}
		return true, nil
	}
	if _, err := w.Coordinator.UpdateJobStatus(ctx, job.ID, models.JobStatusCompleted, 1, "", "", job.ArtifactS3Key, job.ArtifactType); err != nil {
		return true, err
	}
	logger(w).Info("orchestration worker completed job", slog.String("job_id", job.ID), slog.String("job_type", string(job.Type)), slog.String("project_id", job.ProjectID), slog.String("media_id", job.MediaID))
	if err := w.Consumer.Delete(ctx, received.ReceiptHandle); err != nil {
		return true, err
	}
	return true, nil
}

func (w *Worker) deadLetterOrDelete(ctx context.Context, received queue.ReceivedJobMessage, reason string) error {
	if err := w.Consumer.MoveToDeadLetter(ctx, received, reason); err == nil {
		return nil
	} else {
		logger(w).Warn("orchestration worker dead-letter failed; deleting poison queue message", slog.String("job_id", received.Message.JobID), slog.String("job_type", string(received.Message.JobType)), slog.String("project_id", received.Message.ProjectID), slog.String("reason", reason), slog.Any("error", err))
	}
	return w.Consumer.Delete(ctx, received.ReceiptHandle)
}

func sleep(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func logger(w *Worker) *slog.Logger {
	if w.Logger != nil {
		return w.Logger
	}
	return slog.Default()
}

func shouldDeadLetterForReceiveLimit(job models.ProcessingJob) bool {
	return job.Attempt > 0 || job.Status == models.JobStatusRunning || job.Status == models.JobStatusFailed
}

func now(w *Worker) time.Time {
	if w.Now != nil {
		return w.Now().UTC()
	}
	return time.Now().UTC()
}
