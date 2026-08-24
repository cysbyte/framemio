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

func TestStartIOSAnalysisAdvancesExistingCompletedVideoJob(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	memoryStore, publisher, coordinator, project, video := setupProject(t, models.MediaTypeVideo)
	now := time.Date(2026, time.August, 21, 11, 0, 0, 0, time.UTC)
	completedPreprocess := models.ProcessingJob{
		ID:             "job-existing-video-preprocess",
		UserID:         project.UserID,
		ProjectID:      project.ID,
		MediaID:        video.ID,
		Type:           models.JobTypeMediaPreprocess,
		Status:         models.JobStatusCompleted,
		Progress:       1,
		MaxAttempts:    2,
		Configuration:  map[string]string{"pipeline_version": PipelineVersion, "job_type": string(models.JobTypeMediaPreprocess)},
		CreatedAt:      now,
		UpdatedAt:      now,
		CompletedAt:    &now,
		IdempotencyKey: stableStageKey(project.ID, video.ID, models.JobTypeMediaPreprocess),
	}
	if _, _, err := memoryStore.CreateJob(ctx, completedPreprocess, stableStageKey(project.ID, video.ID, models.JobTypeMediaPreprocess)); err != nil {
		t.Fatal(err)
	}

	jobs, err := coordinator.StartIOSAnalysis(ctx, project.UserID, project.ID, "ios-analysis-key")
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 || jobs[0].ID != completedPreprocess.ID || jobs[0].Status != models.JobStatusCompleted {
		t.Fatalf("expected existing completed preprocess job, got %+v", jobs)
	}
	transcribe := latestMessageOfType(t, publisher, models.JobTypeTranscribe)
	if transcribe.MediaID != video.ID || transcribe.ProjectID != project.ID {
		t.Fatalf("existing completed video preprocess did not advance to transcribe: %+v", transcribe)
	}
	projectAfter, err := memoryStore.GetProject(ctx, project.UserID, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if projectAfter.Status != models.ProjectStatusAnalyzing {
		t.Fatalf("project status = %s, want analyzing", projectAfter.Status)
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

// A completed story plan must carry itself into rendering. Parking on
// story_ready waits for a render request that the iOS flow never sends, which
// the app shows as an endless "Finding meaningful moments".
func TestCompletedStoryPlanAdvancesItselfIntoRendering(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	memoryStore, publisher, coordinator, project, _ := setupProject(t, models.MediaTypePhoto)
	storyPlan := completeThroughStoryPlan(t, memoryStore, publisher, coordinator, project, "advance-key")

	rendering, err := memoryStore.GetProject(ctx, project.UserID, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rendering.Status != models.ProjectStatusRendering {
		t.Fatalf("project status = %s, want rendering", rendering.Status)
	}
	render := latestMessageOfType(t, publisher, models.JobTypeVideoRender)
	if render.ProjectID != project.ID {
		t.Fatalf("render message is not project scoped: %+v", render)
	}
	jobs, err := memoryStore.ListProjectJobs(ctx, project.UserID, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if countType(jobs, models.JobTypeStoryValidate) != 1 || countType(jobs, models.JobTypeVideoRender) != 1 {
		t.Fatalf("want exactly one validate and one render job: %+v", jobs)
	}

	// Replaying the same story-plan completion must converge on the one render.
	if err := coordinator.AdvanceStoryPlanToRender(ctx, project.UserID, project.ID); err != nil {
		t.Fatal(err)
	}
	replayed, err := memoryStore.ListProjectJobs(ctx, project.UserID, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if countType(replayed, models.JobTypeVideoRender) != 1 || countType(replayed, models.JobTypeStoryValidate) != 1 {
		t.Fatalf("advancing twice duplicated jobs: %+v", replayed)
	}
	if storyPlan.Version != 1 {
		t.Fatalf("story plan version = %d, want 1", storyPlan.Version)
	}
}

// Section 17: validation is deterministic and an invalid plan never reaches the
// render stage.
func TestInvalidStoryPlanFailsProjectInsteadOfRendering(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	memoryStore, publisher, coordinator, project, _ := setupProject(t, models.MediaTypePhoto)
	coordinator.Validator = rejectingValidator{}
	completeThroughStoryPlan(t, memoryStore, publisher, coordinator, project, "invalid-key")

	failed, err := memoryStore.GetProject(ctx, project.UserID, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if failed.Status != models.ProjectStatusFailed {
		t.Fatalf("project status = %s, want failed", failed.Status)
	}
	jobs, err := memoryStore.ListProjectJobs(ctx, project.UserID, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if countType(jobs, models.JobTypeVideoRender) != 0 {
		t.Fatalf("invalid plan reached the render stage: %+v", jobs)
	}
}

// completeThroughStoryPlan drives the analysis chain to a completed STORY_PLAN
// job with a stored plan, which is the state the auto-advance reacts to.
func completeThroughStoryPlan(t *testing.T, memoryStore *store.MemoryStore, publisher *queue.MemoryPublisher, coordinator *Coordinator, project models.StoryProject, idempotencyKey string) models.StoryPlan {
	t.Helper()
	ctx := context.Background()
	jobs, err := coordinator.StartAnalysis(ctx, project.UserID, project.ID, idempotencyKey)
	if err != nil {
		t.Fatal(err)
	}
	for _, stage := range jobs {
		if _, err := coordinator.UpdateJobStatus(ctx, stage.ID, models.JobStatusCompleted, 1, "", "", "", ""); err != nil {
			t.Fatal(err)
		}
	}
	planMessage := latestMessageOfType(t, publisher, models.JobTypeStoryPlan)
	plan := models.StoryPlan{
		ID:        "story-plan-1",
		ProjectID: project.ID,
		Version:   1,
		Moments:   []models.StoryMoment{{MediaID: "media-1", DurationMS: 2000}},
		Output:    models.StoryOutput{AspectRatio: "9:16", MaxDurationS: 30, Preset: "premium-memory"},
	}
	if err := memoryStore.SaveStoryPlan(ctx, project.UserID, plan); err != nil {
		t.Fatal(err)
	}
	if _, err := coordinator.UpdateJobStatus(ctx, planMessage.JobID, models.JobStatusCompleted, 1, "", "", "plans/story-plan-1.json", "application/json"); err != nil {
		t.Fatal(err)
	}
	return plan
}

type rejectingValidator struct{}

func (rejectingValidator) Validate(context.Context, string, models.StoryPlan) error {
	return errors.New("moment references missing media")
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
