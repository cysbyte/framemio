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
	for {
		receivedMessages, err := w.Consumer.Receive(ctx, 10, w.WaitTime, visibilityTimeout)
		if err != nil {
			return err
		}
		for _, received := range receivedMessages {
			if w.TargetJobID != "" && received.Message.JobID != w.TargetJobID {
				if err := w.Consumer.ExtendVisibility(ctx, received.ReceiptHandle, 0); err != nil {
					return err
				}
				continue
			}
			if received.ApproximateReceiveCount > maxReceiveCount {
				if _, err := w.Store.UpdateJob(ctx, received.Message.JobID, models.JobStatusFailed, 0, "max_receive_count_exceeded", "job exceeded queue receive limit", "", ""); err != nil {
					return err
				}
				if err := w.Consumer.MoveToDeadLetter(ctx, received, "max_receive_count_exceeded"); err != nil {
					return err
				}
				return nil
			}
			if err := w.handleReceived(ctx, received, visibilityTimeout); err != nil {
				return err
			}
			if w.TargetJobID != "" {
				return nil
			}
		}
		if w.TargetJobID == "" {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
	}
}

func (w *Worker) handleReceived(ctx context.Context, received queue.ReceivedJobMessage, visibilityTimeout time.Duration) error {
	job, err := w.Store.GetJobByID(ctx, received.Message.JobID)
	if err != nil {
		return err
	}
	if job.Status == models.JobStatusCompleted || job.Status == models.JobStatusCancelled {
		return w.Consumer.Delete(ctx, received.ReceiptHandle)
	}
	if job.NextRunAt != nil && now(w).Before(*job.NextRunAt) {
		return w.Consumer.ExtendVisibility(ctx, received.ReceiptHandle, time.Until(*job.NextRunAt))
	}
	handler := w.Handlers[job.Type]
	if handler == nil {
		_, err := w.Coordinator.UpdateJobStatus(ctx, job.ID, models.JobStatusFailed, job.Progress, "handler_not_configured", "no worker handler is configured for this job type", "", "")
		return err
	}
	if _, err := w.Coordinator.UpdateJobStatus(ctx, job.ID, models.JobStatusRunning, job.Progress, "", "", "", ""); err != nil {
		return err
	}
	if err := w.Consumer.ExtendVisibility(ctx, received.ReceiptHandle, visibilityTimeout); err != nil {
		return err
	}
	if err := handler.HandleJob(ctx, job); err != nil {
		_, updateErr := w.Coordinator.UpdateJobStatus(ctx, job.ID, models.JobStatusFailed, job.Progress, "handler_failed", err.Error(), "", "")
		if updateErr != nil {
			return updateErr
		}
		return nil
	}
	if _, err := w.Coordinator.UpdateJobStatus(ctx, job.ID, models.JobStatusCompleted, 1, "", "", job.ArtifactS3Key, job.ArtifactType); err != nil {
		return err
	}
	return w.Consumer.Delete(ctx, received.ReceiptHandle)
}

func now(w *Worker) time.Time {
	if w.Now != nil {
		return w.Now().UTC()
	}
	return time.Now().UTC()
}
