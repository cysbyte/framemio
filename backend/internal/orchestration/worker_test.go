package orchestration

import (
	"context"
	"testing"
	"time"

	"github.com/framemio/backend/internal/models"
	"github.com/framemio/backend/internal/queue"
)

func TestWorkerRunOnceReleasesNonTargetMessage(t *testing.T) {
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
	if len(consumer.extended) == 0 || consumer.extended[0] != 0 {
		t.Fatalf("non-target message was not released immediately: %+v", consumer.extended)
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

type noopHandler struct{}

func (noopHandler) HandleJob(context.Context, models.ProcessingJob) error { return nil }
