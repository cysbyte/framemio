package orchestration

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"os"
	"strconv"
	"time"

	"github.com/framemio/backend/internal/models"
	"github.com/framemio/backend/internal/queue"
	"github.com/framemio/backend/internal/store"
)

const PipelineVersion = "async-pipeline-v1"

const (
	defaultMaxAttempts = 3
	defaultBaseBackoff = 2 * time.Second
	defaultMaxBackoff  = 5 * time.Minute
)

type Coordinator struct {
	Store       store.Store
	Publisher   queue.Publisher
	Logger      *slog.Logger
	Now         func() time.Time
	MaxAttempts int
	BaseBackoff time.Duration
	MaxBackoff  time.Duration
}

func New(dependencies Coordinator) *Coordinator {
	if dependencies.Logger == nil {
		dependencies.Logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))
	}
	if dependencies.Now == nil {
		dependencies.Now = time.Now
	}
	if dependencies.MaxAttempts <= 0 {
		dependencies.MaxAttempts = defaultMaxAttempts
	}
	if dependencies.BaseBackoff <= 0 {
		dependencies.BaseBackoff = defaultBaseBackoff
	}
	if dependencies.MaxBackoff <= 0 {
		dependencies.MaxBackoff = defaultMaxBackoff
	}
	return &dependencies
}

func (c *Coordinator) StartAnalysis(ctx context.Context, userID, projectID, idempotencyKey string) ([]models.ProcessingJob, error) {
	project, err := c.Store.GetProject(ctx, userID, projectID)
	if err != nil {
		return nil, err
	}
	jobs := make([]models.ProcessingJob, 0, len(project.MediaIDs))
	markAnalyzing := false
	for _, mediaID := range project.MediaIDs {
		job, err := c.createStageJob(ctx, userID, projectID, mediaID, models.JobTypeMediaPreprocess, map[string]string{"pipeline_version": PipelineVersion}, stableStageKey(projectID, mediaID, models.JobTypeMediaPreprocess), true)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
		if !models.IsTerminalJobStatus(job.Status) {
			markAnalyzing = true
		}
	}
	if markAnalyzing {
		if err := c.Store.SetProjectStatus(ctx, userID, projectID, models.ProjectStatusAnalyzing); err != nil {
			return nil, err
		}
	}
	return jobs, nil
}

func (c *Coordinator) RecordValidationAndQueueRender(ctx context.Context, userID, projectID, idempotencyKey string, storyPlanVersion int, outputPreset string) (models.ProcessingJob, error) {
	configuration := map[string]string{"pipeline_version": PipelineVersion, "story_plan_version": strconv.Itoa(storyPlanVersion), "output_preset": outputPreset}
	validationJob, err := c.createStageJob(ctx, userID, projectID, "", models.JobTypeStoryValidate, configuration, idempotencyKey+":"+string(models.JobTypeStoryValidate), false)
	if err != nil {
		return models.ProcessingJob{}, err
	}
	if !models.IsTerminalJobStatus(validationJob.Status) {
		if _, err := c.Store.UpdateJob(ctx, validationJob.ID, models.JobStatusCompleted, 1, "", "", "", ""); err != nil && !errors.Is(err, store.ErrConflict) {
			return models.ProcessingJob{}, err
		}
	}
	return c.createStageJob(ctx, userID, projectID, "", models.JobTypeVideoRender, configuration, idempotencyKey+":"+string(models.JobTypeVideoRender), true)
}

func (c *Coordinator) UpdateJobStatus(ctx context.Context, jobID string, status models.JobStatus, progress float64, errorCode, errorMessage, artifactS3Key, artifactType string) (models.ProcessingJob, error) {
	job, err := c.Store.GetJobByID(ctx, jobID)
	if err != nil {
		return models.ProcessingJob{}, err
	}
	if status == models.JobStatusFailed && shouldRetry(job) {
		nextRunAt := c.Now().UTC().Add(c.backoff(job.Attempt))
		retried, err := c.Store.RetryJob(ctx, jobID, nextRunAt, errorCode, errorMessage)
		if err != nil {
			return models.ProcessingJob{}, err
		}
		if err := c.publishJob(ctx, retried); err != nil {
			return models.ProcessingJob{}, err
		}
		c.Logger.Warn("job attempt failed; scheduled retry", slog.String("job_id", retried.ID), slog.String("job_type", string(retried.Type)), slog.Int("attempt", retried.Attempt), slog.Time("next_run_at", nextRunAt))
		return retried, nil
	}
	updated, err := c.Store.UpdateJob(ctx, jobID, status, progress, errorCode, errorMessage, artifactS3Key, artifactType)
	if err != nil {
		return models.ProcessingJob{}, err
	}
	if err := c.AdvanceAfterJob(ctx, updated); err != nil {
		return models.ProcessingJob{}, err
	}
	return updated, nil
}

func (c *Coordinator) AdvanceAfterJob(ctx context.Context, job models.ProcessingJob) error {
	if job.Status == models.JobStatusFailed || job.Status == models.JobStatusCancelled {
		return c.Store.SetProjectStatus(ctx, job.UserID, job.ProjectID, models.ProjectStatusFailed)
	}
	if job.Status != models.JobStatusCompleted {
		return nil
	}
	switch job.Type {
	case models.JobTypeMediaPreprocess:
		media, err := c.Store.GetMedia(ctx, job.UserID, job.MediaID)
		if err != nil {
			return err
		}
		if media.Type == models.MediaTypeVideo {
			_, err = c.createStageJob(ctx, job.UserID, job.ProjectID, job.MediaID, models.JobTypeTranscribe, inheritConfiguration(job.Configuration), stableStageKey(job.ProjectID, job.MediaID, models.JobTypeTranscribe), true)
			return err
		}
		return c.enqueueStoryPlanIfReady(ctx, job.UserID, job.ProjectID)
	case models.JobTypeTranscribe:
		_, err := c.createStageJob(ctx, job.UserID, job.ProjectID, job.MediaID, models.JobTypeVideoUnderstand, inheritConfiguration(job.Configuration), stableStageKey(job.ProjectID, job.MediaID, models.JobTypeVideoUnderstand), true)
		return err
	case models.JobTypeVideoUnderstand:
		return c.enqueueStoryPlanIfReady(ctx, job.UserID, job.ProjectID)
	case models.JobTypeStoryPlan:
		return c.Store.SetProjectStatus(ctx, job.UserID, job.ProjectID, models.ProjectStatusStoryReady)
	case models.JobTypeStoryValidate:
		version, _ := strconv.Atoi(job.Configuration["story_plan_version"])
		_, err := c.createStageJob(ctx, job.UserID, job.ProjectID, "", models.JobTypeVideoRender, inheritConfiguration(job.Configuration), stableRenderKey(job.ProjectID, version, job.Configuration["output_preset"]), true)
		return err
	case models.JobTypeVideoRender:
		return c.Store.SetProjectStatus(ctx, job.UserID, job.ProjectID, models.ProjectStatusReady)
	default:
		return nil
	}
}

func (c *Coordinator) enqueueStoryPlanIfReady(ctx context.Context, userID, projectID string) error {
	project, err := c.Store.GetProject(ctx, userID, projectID)
	if err != nil {
		return err
	}
	jobs, err := c.Store.ListProjectJobs(ctx, userID, projectID)
	if err != nil {
		return err
	}
	if hasCompletedStage(jobs, "", models.JobTypeStoryPlan) || hasActiveStage(jobs, "", models.JobTypeStoryPlan) {
		return nil
	}
	for _, mediaID := range project.MediaIDs {
		media, err := c.Store.GetMedia(ctx, userID, mediaID)
		if err != nil {
			return err
		}
		requiredStage := models.JobTypeMediaPreprocess
		if media.Type == models.MediaTypeVideo {
			requiredStage = models.JobTypeVideoUnderstand
		}
		if !hasCompletedStage(jobs, mediaID, requiredStage) {
			return nil
		}
	}
	_, err = c.createStageJob(ctx, userID, projectID, "", models.JobTypeStoryPlan, map[string]string{"pipeline_version": PipelineVersion}, stableStageKey(projectID, "", models.JobTypeStoryPlan), true)
	return err
}

func (c *Coordinator) createStageJob(ctx context.Context, userID, projectID, mediaID string, jobType models.JobType, configuration map[string]string, idempotencyKey string, publish bool) (models.ProcessingJob, error) {
	now := c.Now().UTC()
	config := inheritConfiguration(configuration)
	config["job_type"] = string(jobType)
	config["pipeline_version"] = valueOrDefault(config["pipeline_version"], PipelineVersion)
	job := models.ProcessingJob{ID: models.NewID("job"), UserID: userID, ProjectID: projectID, MediaID: mediaID, Type: jobType, Status: models.JobStatusQueued, Progress: 0, MaxAttempts: c.MaxAttempts, Configuration: config, CreatedAt: now, UpdatedAt: now}
	stored, existing, err := c.Store.CreateJob(ctx, job, idempotencyKey)
	if err != nil {
		return models.ProcessingJob{}, err
	}
	if publish && !existing && !models.IsTerminalJobStatus(stored.Status) {
		if err := c.publishJob(ctx, stored); err != nil {
			return models.ProcessingJob{}, err
		}
	}
	return stored, nil
}

func (c *Coordinator) publishJob(ctx context.Context, job models.ProcessingJob) error {
	if c.Publisher == nil {
		return fmt.Errorf("job publisher is not configured")
	}
	return c.Publisher.Publish(ctx, queue.JobMessage{JobID: job.ID, ProjectID: job.ProjectID, MediaID: job.MediaID, JobType: job.Type, Configuration: smallConfiguration(job.Configuration)})
}

func shouldRetry(job models.ProcessingJob) bool {
	maxAttempts := job.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = defaultMaxAttempts
	}
	return job.Attempt < maxAttempts
}

func (c *Coordinator) backoff(attempt int) time.Duration {
	if attempt <= 0 {
		attempt = 1
	}
	multiplier := math.Pow(2, float64(attempt-1))
	delay := time.Duration(float64(c.BaseBackoff) * multiplier)
	if delay > c.MaxBackoff {
		return c.MaxBackoff
	}
	return delay
}

func hasCompletedStage(jobs []models.ProcessingJob, mediaID string, jobType models.JobType) bool {
	for _, job := range jobs {
		if job.MediaID == mediaID && job.Type == jobType && job.Status == models.JobStatusCompleted {
			return true
		}
	}
	return false
}

func hasActiveStage(jobs []models.ProcessingJob, mediaID string, jobType models.JobType) bool {
	for _, job := range jobs {
		if job.MediaID == mediaID && job.Type == jobType && !models.IsTerminalJobStatus(job.Status) {
			return true
		}
	}
	return false
}

func inheritConfiguration(configuration map[string]string) map[string]string {
	copy := make(map[string]string, len(configuration)+1)
	for key, value := range configuration {
		copy[key] = value
	}
	return copy
}

func smallConfiguration(configuration map[string]string) map[string]string {
	copy := make(map[string]string, len(configuration))
	for key, value := range configuration {
		if len(key) <= 100 && len(value) <= 500 {
			copy[key] = value
		}
	}
	return copy
}

func stableStageKey(projectID, mediaID string, jobType models.JobType) string {
	return projectID + ":" + mediaID + ":" + string(jobType) + ":" + PipelineVersion
}

func stableRenderKey(projectID string, storyPlanVersion int, outputPreset string) string {
	return projectID + ":" + strconv.Itoa(storyPlanVersion) + ":" + outputPreset + ":" + string(models.JobTypeVideoRender) + ":" + PipelineVersion
}

func valueOrDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
