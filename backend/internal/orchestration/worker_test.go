package orchestration

import (
	"context"
	"testing"
	"time"

	"github.com/framemio/backend/internal/models"
	"github.com/framemio/backend/internal/queue"
)

func TestWorkerRunOnceDefersNonTargetUnsupportedMessage(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	memoryStore, _, coordinator, project, _ := setupProject(t, models.MediaTypePhoto)
	otherJob, _, err := memoryStore.CreateJob(ctx, models.ProcessingJob{ID: "job-other", UserID: project.UserID, ProjectID: project.ID, Type: models.JobTypeMediaPreprocess, Status: models.JobStatusQueued, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}, "other-job")
	if err != nil {
		t.Fatal(err)
	}
	targetJob, _, err := memoryStore.CreateJob(ctx, models.ProcessingJob{ID: "job-target", UserID: project.UserID, ProjectID: project.ID, Type: models.JobTypeStoryPlan, Status: models.JobStatusQueued, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}, "target-job")
	if err != nil {
		t.Fatal(err)
	}
	consumer := &memoryConsumer{received: []queue.ReceivedJobMessage{
		{Message: queue.JobMessage{JobID: otherJob.ID, ProjectID: project.ID, JobType: otherJob.Type}, ReceiptHandle: "receipt-other"},
		{Message: queue.JobMessage{JobID: targetJob.ID, ProjectID: project.ID, JobType: targetJob.Type}, ReceiptHandle: "receipt-target"},
	}}
	worker := Worker{
		Store:       memoryStore,
		Consumer:    consumer,
		Coordinator: coordinator,
		Handlers:    map[models.JobType]JobHandler{models.JobTypeStoryPlan: noopHandler{}},
		TargetJobID: targetJob.ID,
	}
	if err := worker.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if len(consumer.deleted) != 1 || consumer.deleted[0] != "receipt-target" {
		t.Fatalf("target message was not deleted after processing: %+v", consumer.deleted)
	}
	if len(consumer.extended) == 0 || consumer.extended[0] != 2*time.Second {
		t.Fatalf("non-target message was not deferred briefly: %+v", consumer.extended)
	}
	otherAfter, err := memoryStore.GetJob(ctx, project.UserID, otherJob.ID)
	if err != nil {
		t.Fatal(err)
	}
	if otherAfter.Status != models.JobStatusQueued {
		t.Fatalf("non-target job should not be processed: %+v", otherAfter)
	}
	targetAfter, err := memoryStore.GetJob(ctx, project.UserID, targetJob.ID)
	if err != nil {
		t.Fatal(err)
	}
	if targetAfter.Status != models.JobStatusCompleted {
		t.Fatalf("target job should complete: %+v", targetAfter)
	}
}

func TestWorkerRunOnceProcessesAvailableSameHandlerMessage(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	memoryStore, _, coordinator, project, _ := setupProject(t, models.MediaTypePhoto)
	otherJob, _, err := memoryStore.CreateJob(ctx, models.ProcessingJob{ID: "job-other-story", UserID: project.UserID, ProjectID: project.ID, Type: models.JobTypeStoryPlan, Status: models.JobStatusQueued, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}, "other-story-job")
	if err != nil {
		t.Fatal(err)
	}
	targetJob, _, err := memoryStore.CreateJob(ctx, models.ProcessingJob{ID: "job-target-story", UserID: project.UserID, ProjectID: project.ID, Type: models.JobTypeStoryPlan, Status: models.JobStatusQueued, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}, "target-story-job")
	if err != nil {
		t.Fatal(err)
	}
	consumer := &memoryConsumer{received: []queue.ReceivedJobMessage{
		{Message: queue.JobMessage{JobID: otherJob.ID, ProjectID: project.ID, JobType: otherJob.Type}, ReceiptHandle: "receipt-other-story"},
		{Message: queue.JobMessage{JobID: targetJob.ID, ProjectID: project.ID, JobType: targetJob.Type}, ReceiptHandle: "receipt-target-story"},
	}}
	worker := Worker{
		Store:       memoryStore,
		Consumer:    consumer,
		Coordinator: coordinator,
		Handlers:    map[models.JobType]JobHandler{models.JobTypeStoryPlan: noopHandler{}},
		TargetJobID: targetJob.ID,
	}
	if err := worker.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	for _, timeout := range consumer.extended {
		if timeout == 0 {
			t.Fatalf("same-handler job should not be released immediately: %+v", consumer.extended)
		}
	}
	if len(consumer.deleted) != 2 || consumer.deleted[0] != "receipt-other-story" || consumer.deleted[1] != "receipt-target-story" {
		t.Fatalf("available same-handler messages were not deleted after processing: %+v", consumer.deleted)
	}
	otherAfter, err := memoryStore.GetJob(ctx, project.UserID, otherJob.ID)
	if err != nil {
		t.Fatal(err)
	}
	if otherAfter.Status != models.JobStatusCompleted {
		t.Fatalf("available same-handler job should complete: %+v", otherAfter)
	}
	targetAfter, err := memoryStore.GetJob(ctx, project.UserID, targetJob.ID)
	if err != nil {
		t.Fatal(err)
	}
	if targetAfter.Status != models.JobStatusCompleted {
		t.Fatalf("target job should complete after the available job: %+v", targetAfter)
	}
}

func TestWorkerRunOnceDeletesStaleSameHandlerMessageAndContinuesToTarget(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	memoryStore, _, coordinator, project, _ := setupProject(t, models.MediaTypePhoto)
	targetJob, _, err := memoryStore.CreateJob(ctx, models.ProcessingJob{ID: "job-target-after-stale", UserID: project.UserID, ProjectID: project.ID, Type: models.JobTypeStoryPlan, Status: models.JobStatusQueued, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}, "target-after-stale-job")
	if err != nil {
		t.Fatal(err)
	}
	consumer := &memoryConsumer{received: []queue.ReceivedJobMessage{
		{Message: queue.JobMessage{JobID: "job-stale-story", ProjectID: project.ID, JobType: models.JobTypeStoryPlan}, ReceiptHandle: "receipt-stale-story"},
		{Message: queue.JobMessage{JobID: targetJob.ID, ProjectID: project.ID, JobType: targetJob.Type}, ReceiptHandle: "receipt-target-story"},
	}}
	worker := Worker{
		Store:       memoryStore,
		Consumer:    consumer,
		Coordinator: coordinator,
		Handlers:    map[models.JobType]JobHandler{models.JobTypeStoryPlan: noopHandler{}},
		TargetJobID: targetJob.ID,
	}
	if err := worker.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if len(consumer.deleted) != 2 || consumer.deleted[0] != "receipt-stale-story" || consumer.deleted[1] != "receipt-target-story" {
		t.Fatalf("stale and target messages were not deleted in order: %+v", consumer.deleted)
	}
	targetAfter, err := memoryStore.GetJob(ctx, project.UserID, targetJob.ID)
	if err != nil {
		t.Fatal(err)
	}
	if targetAfter.Status != models.JobStatusCompleted {
		t.Fatalf("target job should complete after stale message: %+v", targetAfter)
	}
}

func TestWorkerRunOnceProcessesQueuedTargetWithInflatedReceiveCount(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	memoryStore, _, coordinator, project, _ := setupProject(t, models.MediaTypePhoto)
	targetJob, _, err := memoryStore.CreateJob(ctx, models.ProcessingJob{ID: "job-target-inflated", UserID: project.UserID, ProjectID: project.ID, Type: models.JobTypeStoryPlan, Status: models.JobStatusQueued, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}, "target-job-inflated")
	if err != nil {
		t.Fatal(err)
	}
	consumer := &memoryConsumer{received: []queue.ReceivedJobMessage{
		{Message: queue.JobMessage{JobID: targetJob.ID, ProjectID: project.ID, JobType: targetJob.Type}, ReceiptHandle: "receipt-target", ApproximateReceiveCount: 10},
	}}
	worker := Worker{
		Store:           memoryStore,
		Consumer:        consumer,
		Coordinator:     coordinator,
		Handlers:        map[models.JobType]JobHandler{models.JobTypeStoryPlan: noopHandler{}},
		TargetJobID:     targetJob.ID,
		MaxReceiveCount: 5,
	}
	if err := worker.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if len(consumer.deadLetters) != 0 {
		t.Fatalf("queued target should not be dead-lettered for pre-handler receives: %+v", consumer.deadLetters)
	}
	if len(consumer.deleted) != 1 || consumer.deleted[0] != "receipt-target" {
		t.Fatalf("target message was not deleted after processing: %+v", consumer.deleted)
	}
	targetAfter, err := memoryStore.GetJob(ctx, project.UserID, targetJob.ID)
	if err != nil {
		t.Fatal(err)
	}
	if targetAfter.Status != models.JobStatusCompleted {
		t.Fatalf("target job should complete: %+v", targetAfter)
	}
}

func TestWorkerRunOnceRemovesPoisonNonTargetAndContinuesToTarget(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	memoryStore, _, coordinator, project, _ := setupProject(t, models.MediaTypePhoto)
	poisonJob, _, err := memoryStore.CreateJob(ctx, models.ProcessingJob{ID: "job-poison-story", UserID: project.UserID, ProjectID: project.ID, Type: models.JobTypeStoryPlan, Status: models.JobStatusFailed, Attempt: 1, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}, "poison-story-job")
	if err != nil {
		t.Fatal(err)
	}
	targetJob, _, err := memoryStore.CreateJob(ctx, models.ProcessingJob{ID: "job-target-after-poison", UserID: project.UserID, ProjectID: project.ID, Type: models.JobTypeStoryPlan, Status: models.JobStatusQueued, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}, "target-after-poison-job")
	if err != nil {
		t.Fatal(err)
	}
	consumer := &memoryConsumer{received: []queue.ReceivedJobMessage{
		{Message: queue.JobMessage{JobID: poisonJob.ID, ProjectID: project.ID, JobType: poisonJob.Type}, ReceiptHandle: "receipt-poison-story", ApproximateReceiveCount: 301},
		{Message: queue.JobMessage{JobID: targetJob.ID, ProjectID: project.ID, JobType: targetJob.Type}, ReceiptHandle: "receipt-target-story"},
	}}
	worker := Worker{
		Store:           memoryStore,
		Consumer:        consumer,
		Coordinator:     coordinator,
		Handlers:        map[models.JobType]JobHandler{models.JobTypeStoryPlan: noopHandler{}},
		TargetJobID:     targetJob.ID,
		MaxReceiveCount: 5,
	}
	if err := worker.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if len(consumer.deadLetters) != 1 || consumer.deadLetters[0].Message.JobID != poisonJob.ID {
		t.Fatalf("poison non-target was not dead-lettered: %+v", consumer.deadLetters)
	}
	if len(consumer.deleted) != 2 || consumer.deleted[0] != "receipt-poison-story" || consumer.deleted[1] != "receipt-target-story" {
		t.Fatalf("poison and target messages were not deleted in order: %+v", consumer.deleted)
	}
	targetAfter, err := memoryStore.GetJob(ctx, project.UserID, targetJob.ID)
	if err != nil {
		t.Fatal(err)
	}
	if targetAfter.Status != models.JobStatusCompleted {
		t.Fatalf("target job should complete after poison non-target: %+v", targetAfter)
	}
}

func TestWorkerRunOnceDeletesCurrentReceiptAfterSchedulingRetry(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	memoryStore, publisher, coordinator, project, _ := setupProject(t, models.MediaTypePhoto)
	targetJob, _, err := memoryStore.CreateJob(ctx, models.ProcessingJob{ID: "job-retry-target", UserID: project.UserID, ProjectID: project.ID, Type: models.JobTypeStoryPlan, Status: models.JobStatusQueued, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(), MaxAttempts: 2}, "retry-target-job")
	if err != nil {
		t.Fatal(err)
	}
	consumer := &memoryConsumer{received: []queue.ReceivedJobMessage{
		{Message: queue.JobMessage{JobID: targetJob.ID, ProjectID: project.ID, JobType: targetJob.Type}, ReceiptHandle: "receipt-target-retry", ApproximateReceiveCount: 1},
	}}
	worker := Worker{
		Store:           memoryStore,
		Consumer:        consumer,
		Coordinator:     coordinator,
		Handlers:        map[models.JobType]JobHandler{models.JobTypeStoryPlan: failingHandler{}},
		TargetJobID:     targetJob.ID,
		MaxReceiveCount: 5,
	}
	if err := worker.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if len(consumer.deleted) != 1 || consumer.deleted[0] != "receipt-target-retry" {
		t.Fatalf("failed attempt receipt should be deleted after retry is queued: %+v", consumer.deleted)
	}
	if len(publisher.Messages()) != 1 || publisher.Messages()[0].JobID != targetJob.ID {
		t.Fatalf("retry message was not published: %+v", publisher.Messages())
	}
	retried, err := memoryStore.GetJob(ctx, project.UserID, targetJob.ID)
	if err != nil {
		t.Fatal(err)
	}
	if retried.Status != models.JobStatusQueued || retried.Attempt != 1 || retried.NextRunAt == nil {
		t.Fatalf("job should be queued for retry: %+v", retried)
	}
}

func TestWorkerRunOnceDeletesMessageWhenJobRemovedDuringHandler(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	memoryStore, _, coordinator, project, _ := setupProject(t, models.MediaTypePhoto)
	targetJob, _, err := memoryStore.CreateJob(ctx, models.ProcessingJob{ID: "job-deleted-during-handler", UserID: project.UserID, ProjectID: project.ID, Type: models.JobTypeStoryPlan, Status: models.JobStatusQueued, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}, "deleted-during-handler")
	if err != nil {
		t.Fatal(err)
	}
	consumer := &memoryConsumer{received: []queue.ReceivedJobMessage{
		{Message: queue.JobMessage{JobID: targetJob.ID, ProjectID: project.ID, JobType: targetJob.Type}, ReceiptHandle: "receipt-deleted-during-handler", ApproximateReceiveCount: 1},
	}}
	worker := Worker{
		Store:       memoryStore,
		Consumer:    consumer,
		Coordinator: coordinator,
		Handlers: map[models.JobType]JobHandler{models.JobTypeStoryPlan: deletingHandler{
			store:     memoryStore,
			userID:    project.UserID,
			projectID: project.ID,
		}},
		TargetJobID: targetJob.ID,
	}
	if err := worker.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if len(consumer.deleted) != 1 || consumer.deleted[0] != "receipt-deleted-during-handler" {
		t.Fatalf("stale in-flight message should be deleted after handler returns: %+v", consumer.deleted)
	}
	if _, err := memoryStore.GetJobByID(ctx, targetJob.ID); err == nil {
		t.Fatalf("job should have been deleted by handler")
	}
}

func TestWorkerRunOnceProcessesRunningTargetWithInflatedReceiveCount(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	memoryStore, _, coordinator, project, _ := setupProject(t, models.MediaTypePhoto)
	runningJob, _, err := memoryStore.CreateJob(ctx, models.ProcessingJob{ID: "job-running-target-inflated", UserID: project.UserID, ProjectID: project.ID, Type: models.JobTypeStoryPlan, Status: models.JobStatusRunning, Attempt: 1, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(), MaxAttempts: 2}, "running-target-inflated")
	if err != nil {
		t.Fatal(err)
	}
	consumer := &memoryConsumer{received: []queue.ReceivedJobMessage{
		{Message: queue.JobMessage{JobID: runningJob.ID, ProjectID: project.ID, JobType: runningJob.Type}, ReceiptHandle: "receipt-running-target", ApproximateReceiveCount: 25},
	}}
	worker := Worker{
		Store:           memoryStore,
		Consumer:        consumer,
		Coordinator:     coordinator,
		Handlers:        map[models.JobType]JobHandler{models.JobTypeStoryPlan: noopHandler{}},
		TargetJobID:     runningJob.ID,
		MaxReceiveCount: 5,
	}
	if err := worker.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if len(consumer.deadLetters) != 0 {
		t.Fatalf("target should not be dead-lettered for inflated receives: %+v", consumer.deadLetters)
	}
	updated, err := memoryStore.GetJob(ctx, project.UserID, runningJob.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != models.JobStatusCompleted {
		t.Fatalf("running target should complete despite inflated receives: %+v", updated)
	}
}

func TestWorkerRunOnceFailsRunningOverReceivedJobAndMarksProjectFailed(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	memoryStore, _, coordinator, project, _ := setupProject(t, models.MediaTypePhoto)
	if err := memoryStore.SetProjectStatus(ctx, project.UserID, project.ID, models.ProjectStatusAnalyzing); err != nil {
		t.Fatal(err)
	}
	runningJob, _, err := memoryStore.CreateJob(ctx, models.ProcessingJob{ID: "job-running-poison", UserID: project.UserID, ProjectID: project.ID, Type: models.JobTypeStoryPlan, Status: models.JobStatusRunning, Attempt: 1, CreatedAt: time.Now().UTC().Add(-10 * time.Minute), UpdatedAt: time.Now().UTC().Add(-10 * time.Minute), MaxAttempts: 2}, "running-poison-job")
	if err != nil {
		t.Fatal(err)
	}
	consumer := &memoryConsumer{received: []queue.ReceivedJobMessage{
		{Message: queue.JobMessage{JobID: runningJob.ID, ProjectID: project.ID, JobType: runningJob.Type}, ReceiptHandle: "receipt-running-poison", ApproximateReceiveCount: 10},
	}}
	worker := Worker{Store: memoryStore, Consumer: consumer, Coordinator: coordinator, Handlers: map[models.JobType]JobHandler{models.JobTypeStoryPlan: noopHandler{}}, MaxReceiveCount: 5}
	if err := worker.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if len(consumer.deadLetters) != 1 || consumer.deadLetters[0].Message.JobID != runningJob.ID {
		t.Fatalf("running poison job should be dead-lettered: %+v", consumer.deadLetters)
	}
	failed, err := memoryStore.GetJob(ctx, project.UserID, runningJob.ID)
	if err != nil {
		t.Fatal(err)
	}
	if failed.Status != models.JobStatusFailed || failed.ErrorCode != "max_receive_count_exceeded" {
		t.Fatalf("running poison job should be failed: %+v", failed)
	}
	projectAfter, err := memoryStore.GetProject(ctx, project.UserID, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if projectAfter.Status != models.ProjectStatusFailed {
		t.Fatalf("project should be marked failed after terminal job failure: %+v", projectAfter)
	}
}

type noopHandler struct{}

func (noopHandler) HandleJob(context.Context, models.ProcessingJob) error { return nil }

type deletingHandler struct {
	store     interface{ DeleteProject(context.Context, string, string) error }
	userID    string
	projectID string
}

func (h deletingHandler) HandleJob(ctx context.Context, _ models.ProcessingJob) error {
	return h.store.DeleteProject(ctx, h.userID, h.projectID)
}
