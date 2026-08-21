package orchestration

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/framemio/backend/internal/models"
	"github.com/framemio/backend/internal/queue"
	"github.com/framemio/backend/internal/store"
)

func TestCoordinatorChainsReusableVideoStages(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	memoryStore, publisher, coordinator, project, video := setupProject(t, models.MediaTypeVideo)

	jobs, err := coordinator.StartAnalysis(ctx, project.UserID, project.ID, "analysis-key")
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 || jobs[0].Type != models.JobTypeMediaPreprocess {
		t.Fatalf("unexpected first jobs: %+v", jobs)
	}
	if messages := publisher.Messages(); len(messages) != 1 || messages[0].JobType != models.JobTypeMediaPreprocess {
		t.Fatalf("unexpected initial messages: %+v", messages)
	}
	preprocess, err := coordinator.UpdateJobStatus(ctx, jobs[0].ID, models.JobStatusRunning, 0.2, "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if preprocess.Attempt != 1 || preprocess.StartedAt == nil {
		t.Fatalf("running job did not record attempt/start: %+v", preprocess)
	}
	if _, err := coordinator.UpdateJobStatus(ctx, jobs[0].ID, models.JobStatusCompleted, 1, "", "", "", ""); err != nil {
		t.Fatal(err)
	}
	transcribe := latestMessageOfType(t, publisher, models.JobTypeTranscribe)
	if transcribe.MediaID != video.ID {
		t.Fatalf("transcribe did not preserve media id: %+v", transcribe)
	}
	if _, err := coordinator.UpdateJobStatus(ctx, transcribe.JobID, models.JobStatusCompleted, 1, "", "", "transcripts/video.json", "application/json"); err != nil {
		t.Fatal(err)
	}
	understand := latestMessageOfType(t, publisher, models.JobTypeVideoUnderstand)
	if understand.MediaID != video.ID {
		t.Fatalf("understand did not preserve media id: %+v", understand)
	}
	if _, err := coordinator.UpdateJobStatus(ctx, understand.JobID, models.JobStatusCompleted, 1, "", "", "analysis/video.json", "application/json"); err != nil {
		t.Fatal(err)
	}
	storyPlan := latestMessageOfType(t, publisher, models.JobTypeStoryPlan)
	if storyPlan.MediaID != "" {
		t.Fatalf("story plan must be project scoped: %+v", storyPlan)
	}

	jobsAfter, err := memoryStore.ListProjectJobs(ctx, project.UserID, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if countType(jobsAfter, models.JobTypeTranscribe) != 1 || countType(jobsAfter, models.JobTypeVideoUnderstand) != 1 || countType(jobsAfter, models.JobTypeStoryPlan) != 1 {
		t.Fatalf("pipeline created duplicate or missing stages: %+v", jobsAfter)
	}

	if _, err := coordinator.UpdateJobStatus(ctx, understand.JobID, models.JobStatusCompleted, 1, "", "", "analysis/video.json", "application/json"); err != nil {
		t.Fatal(err)
	}
	jobsAfterReplay, err := memoryStore.ListProjectJobs(ctx, project.UserID, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if countType(jobsAfterReplay, models.JobTypeStoryPlan) != 1 {
		t.Fatalf("completed stage replay created duplicate story plan: %+v", jobsAfterReplay)
	}
}

func TestCoordinatorRetriesWithBackoffBeforeFailingTerminally(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, publisher, coordinator, project, _ := setupProject(t, models.MediaTypePhoto)
	coordinator.BaseBackoff = time.Second
	coordinator.MaxAttempts = 2
	jobs, err := coordinator.StartAnalysis(ctx, project.UserID, project.ID, "retry-key")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := coordinator.UpdateJobStatus(ctx, jobs[0].ID, models.JobStatusRunning, 0.1, "", "", "", ""); err != nil {
		t.Fatal(err)
	}
	retried, err := coordinator.UpdateJobStatus(ctx, jobs[0].ID, models.JobStatusFailed, 0.1, "temporary", "try again", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if retried.Status != models.JobStatusQueued || retried.NextRunAt == nil || retried.Attempt != 1 {
		t.Fatalf("job was not scheduled for retry: %+v", retried)
	}
	if countMessageType(publisher.Messages(), models.JobTypeMediaPreprocess) != 1 {
		t.Fatalf("retry should not duplicate memory publisher messages: %+v", publisher.Messages())
	}
	if _, err := coordinator.UpdateJobStatus(ctx, jobs[0].ID, models.JobStatusRunning, 0.2, "", "", "", ""); err != nil {
		t.Fatal(err)
	}
	failed, err := coordinator.UpdateJobStatus(ctx, jobs[0].ID, models.JobStatusFailed, 0.2, "permanent", "stop", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if failed.Status != models.JobStatusFailed || failed.CompletedAt == nil {
		t.Fatalf("job did not fail terminally after max attempts: %+v", failed)
	}
	projectAfter, err := coordinator.Store.GetProject(ctx, project.UserID, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if projectAfter.Status != models.ProjectStatusFailed {
		t.Fatalf("project was not marked failed: %+v", projectAfter)
	}
}

func TestWorkerUsesVisibilityAndDeadLetterQueue(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	memoryStore, publisher, coordinator, project, _ := setupProject(t, models.MediaTypePhoto)
	jobs, err := coordinator.StartAnalysis(ctx, project.UserID, project.ID, "worker-key")
	if err != nil {
		t.Fatal(err)
	}
	consumer := &memoryConsumer{received: []queue.ReceivedJobMessage{{Message: publisher.Messages()[0], ReceiptHandle: "receipt-1", ApproximateReceiveCount: 6}}}
	worker := Worker{Store: memoryStore, Consumer: consumer, Coordinator: coordinator, MaxReceiveCount: 5}
	if err := worker.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if len(consumer.deadLetters) != 1 || len(consumer.deleted) != 1 {
		t.Fatalf("message was not dead-lettered and deleted: %+v", consumer)
	}
	job, err := memoryStore.GetJob(ctx, project.UserID, jobs[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != models.JobStatusFailed {
		t.Fatalf("job was not terminally failed for DLQ: %+v", job)
	}
}

func setupProject(t *testing.T, mediaType models.MediaType) (*store.MemoryStore, *queue.MemoryPublisher, *Coordinator, models.StoryProject, models.MediaAsset) {
	t.Helper()
	ctx := context.Background()
	memoryStore := store.NewMemoryStore()
	publisher := queue.NewMemoryPublisher()
	now := time.Date(2026, time.August, 21, 10, 0, 0, 0, time.UTC)
	media := models.MediaAsset{ID: "media-1", UserID: "user-1", Type: mediaType, Status: models.MediaStatusReady, FileName: "IMG_0001", ContentType: "image/jpeg", FileSizeBytes: 100, S3Key: "source/media-1", UploadID: "upload-1", CreatedAt: now, UpdatedAt: now}
	if mediaType == models.MediaTypeVideo {
		media.ContentType = "video/quicktime"
		duration := 12.3
		media.DurationSeconds = &duration
	}
	session := models.UploadSession{ID: media.UploadID, UserID: media.UserID, MediaID: media.ID, S3Key: media.S3Key, Strategy: models.UploadStrategySingle, CreatedAt: now}
	if err := memoryStore.CreateMediaUpload(ctx, media, session); err != nil {
		t.Fatal(err)
	}
	project := models.StoryProject{ID: "project-1", UserID: media.UserID, Title: "Weekend", MediaIDs: []string{media.ID}, Status: models.ProjectStatusDraft, CreatedAt: now, UpdatedAt: now}
	created, _, err := memoryStore.CreateProject(ctx, project, "project-key")
	if err != nil {
		t.Fatal(err)
	}
	coordinator := New(Coordinator{Store: memoryStore, Publisher: publisher, Now: func() time.Time { return now }, MaxAttempts: 2, BaseBackoff: time.Second})
	return memoryStore, publisher, coordinator, created, media
}

func latestMessageOfType(t *testing.T, publisher *queue.MemoryPublisher, jobType models.JobType) queue.JobMessage {
	t.Helper()
	messages := publisher.Messages()
	for index := len(messages) - 1; index >= 0; index-- {
		if messages[index].JobType == jobType {
			return messages[index]
		}
	}
	t.Fatalf("missing message type %s in %+v", jobType, messages)
	return queue.JobMessage{}
}

func countType(jobs []models.ProcessingJob, jobType models.JobType) int {
	count := 0
	for _, job := range jobs {
		if job.Type == jobType {
			count++
		}
	}
	return count
}

func countMessageType(messages []queue.JobMessage, jobType models.JobType) int {
	count := 0
	for _, message := range messages {
		if message.JobType == jobType {
			count++
		}
	}
	return count
}

type memoryConsumer struct {
	received    []queue.ReceivedJobMessage
	deleted     []string
	extended    []time.Duration
	deadLetters []queue.ReceivedJobMessage
}

func (c *memoryConsumer) Receive(context.Context, int32, time.Duration, time.Duration) ([]queue.ReceivedJobMessage, error) {
	return c.received, nil
}

func (c *memoryConsumer) Delete(_ context.Context, receiptHandle string) error {
	c.deleted = append(c.deleted, receiptHandle)
	return nil
}

func (c *memoryConsumer) ExtendVisibility(_ context.Context, _ string, timeout time.Duration) error {
	c.extended = append(c.extended, timeout)
	return nil
}

func (c *memoryConsumer) MoveToDeadLetter(_ context.Context, received queue.ReceivedJobMessage, _ string) error {
	c.deadLetters = append(c.deadLetters, received)
	return c.Delete(context.Background(), received.ReceiptHandle)
}

type failingHandler struct{}

func (failingHandler) HandleJob(context.Context, models.ProcessingJob) error {
	return errors.New("failed")
}
