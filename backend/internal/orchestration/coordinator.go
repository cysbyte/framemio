package orchestration

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/framemio/backend/internal/models"
	"github.com/framemio/backend/internal/queue"
	"github.com/framemio/backend/internal/store"
	"github.com/framemio/backend/internal/storyvalidation"
	"github.com/framemio/backend/internal/workeractivation"
)

const PipelineVersion = "async-pipeline-v1"

const (
	defaultMaxAttempts  = 3
	defaultBaseBackoff  = 2 * time.Second
	defaultMaxBackoff   = 5 * time.Minute
	defaultOutputPreset = "social"
)

type Coordinator struct {
	Store       store.Store
	Publisher   queue.Publisher
	Activator   workeractivation.Activator
	Validator   storyvalidation.Service
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

func (c *Coordinator) StartIOSAnalysis(ctx context.Context, userID, projectID, idempotencyKey string) ([]models.ProcessingJob, error) {
	project, err := c.Store.GetProject(ctx, userID, projectID)
	if err != nil {
		return nil, err
	}
	jobs := make([]models.ProcessingJob, 0, len(project.MediaIDs))
	markAnalyzing := false
	for _, mediaID := range project.MediaIDs {
		media, err := c.Store.GetMedia(ctx, userID, mediaID)
		if err != nil {
			return nil, err
		}
		job, err := c.createStageJob(ctx, userID, projectID, mediaID, models.JobTypeMediaPreprocess, map[string]string{"pipeline_version": PipelineVersion}, stableStageKey(projectID, mediaID, models.JobTypeMediaPreprocess), media.Type == models.MediaTypeVideo)
		if err != nil {
			return nil, err
		}
		if media.Type != models.MediaTypeVideo && !models.IsTerminalJobStatus(job.Status) {
			job, err = c.UpdateJobStatus(ctx, job.ID, models.JobStatusCompleted, 1, "", "", "", "")
			if err != nil {
				return nil, err
			}
		} else if job.Status == models.JobStatusCompleted {
			if err := c.AdvanceAfterJob(ctx, job); err != nil {
				return nil, err
			}
		}
		jobs = append(jobs, job)
		if !models.IsTerminalJobStatus(job.Status) {
			markAnalyzing = true
		}
	}
	if !markAnalyzing {
		projectJobs, err := c.Store.ListProjectJobs(ctx, userID, projectID)
		if err != nil {
			return nil, err
		}
		for _, job := range projectJobs {
			if !models.IsTerminalJobStatus(job.Status) {
				markAnalyzing = true
				break
			}
		}
	}
	if markAnalyzing {
		if err := c.Store.SetProjectStatus(ctx, userID, projectID, models.ProjectStatusAnalyzing); err != nil {
			return nil, err
		}
	}
	return jobs, nil
}

// RecordValidationAndQueueRender records the deterministic validation stage and
// queues the render.
//
// A render is identified by (project, story plan version, output preset) rather
// than by the caller's idempotency key, because that triple already determines
// the output S3 object. Keying on it means the automatic post-story-plan advance
// and an explicit /render request for the same plan converge on one job instead
// of rendering the same film twice.
func (c *Coordinator) RecordValidationAndQueueRender(ctx context.Context, userID, projectID, idempotencyKey string, storyPlanVersion int, outputPreset string) (models.ProcessingJob, error) {
	preset := valueOrDefault(strings.TrimSpace(outputPreset), defaultOutputPreset)
	configuration := map[string]string{"pipeline_version": PipelineVersion, "story_plan_version": strconv.Itoa(storyPlanVersion), "output_preset": preset, "output_s3_key": renderOutputKey(userID, projectID, storyPlanVersion, preset)}
	validationJob, err := c.createStageJob(ctx, userID, projectID, "", models.JobTypeStoryValidate, configuration, stableValidateKey(projectID, storyPlanVersion, preset), false)
	if err != nil {
		return models.ProcessingJob{}, err
	}
	if !models.IsTerminalJobStatus(validationJob.Status) {
		if _, err := c.Store.UpdateJob(ctx, validationJob.ID, models.JobStatusCompleted, 1, "", "", "", ""); err != nil && !errors.Is(err, store.ErrConflict) {
			return models.ProcessingJob{}, err
		}
	}
	renderKey := stableRenderKey(projectID, storyPlanVersion, preset)
	renderJob, err := c.createStageJob(ctx, userID, projectID, "", models.JobTypeVideoRender, configuration, renderKey, true)
	if err != nil {
		return models.ProcessingJob{}, err
	}
	if strings.TrimSpace(idempotencyKey) == "" || !models.IsTerminalJobStatus(renderJob.Status) {
		return renderJob, nil
	}
	if activeJob, ok, err := c.activeRenderJob(ctx, userID, projectID, storyPlanVersion, preset); err != nil || ok {
		return activeJob, err
	}
	return c.createStageJob(ctx, userID, projectID, "", models.JobTypeVideoRender, configuration, renderKey+":retry:"+strings.TrimSpace(idempotencyKey), true)
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
		if err := c.Store.SetProjectStatus(ctx, job.UserID, job.ProjectID, models.ProjectStatusStoryReady); err != nil {
			return err
		}
		return c.AdvanceStoryPlanToRender(ctx, job.UserID, job.ProjectID)
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

// AdvanceStoryPlanToRender carries a completed StoryPlan through deterministic
// validation and into the render stage. The end-to-end pipeline owns this
// transition: without it a project settles on ProjectStatusStoryReady and never
// produces an MP4, which iOS renders forever as "Finding meaningful moments".
func (c *Coordinator) AdvanceStoryPlanToRender(ctx context.Context, userID, projectID string) error {
	plan, err := c.Store.GetStoryPlan(ctx, userID, projectID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			c.Logger.Warn("story plan is not stored; cannot queue render", slog.String("project_id", projectID))
			return nil
		}
		return err
	}
	preset := valueOrDefault(strings.TrimSpace(plan.Output.Preset), defaultOutputPreset)
	// Section 17: validation is deterministic and invalid plans never reach rendering.
	if c.Validator != nil {
		if validationErr := c.Validator.Validate(ctx, userID, plan); validationErr != nil {
			c.Logger.Error("story plan failed validation; render not queued", slog.String("project_id", projectID), slog.Int("story_plan_version", plan.Version), slog.Any("error", validationErr))
			return c.Store.SetProjectStatus(ctx, userID, projectID, models.ProjectStatusFailed)
		}
	}
	renderJob, err := c.RecordValidationAndQueueRender(ctx, userID, projectID, "", plan.Version, preset)
	if err != nil {
		return err
	}
	c.Logger.Info("queued render for completed story plan", slog.String("project_id", projectID), slog.String("job_id", renderJob.ID), slog.Int("story_plan_version", plan.Version), slog.String("output_preset", preset), slog.String("status", string(renderJob.Status)))
	if models.IsTerminalJobStatus(renderJob.Status) {
		return nil
	}
	return c.Store.SetProjectStatus(ctx, userID, projectID, models.ProjectStatusRendering)
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

func (c *Coordinator) activeRenderJob(ctx context.Context, userID, projectID string, storyPlanVersion int, outputPreset string) (models.ProcessingJob, bool, error) {
	jobs, err := c.Store.ListProjectJobs(ctx, userID, projectID)
	if err != nil {
		return models.ProcessingJob{}, false, err
	}
	version := strconv.Itoa(storyPlanVersion)
	for _, job := range jobs {
		if job.Type != models.JobTypeVideoRender || models.IsTerminalJobStatus(job.Status) {
			continue
		}
		if job.Configuration["story_plan_version"] == version && valueOrDefault(strings.TrimSpace(job.Configuration["output_preset"]), defaultOutputPreset) == outputPreset {
			return job, true, nil
		}
	}
	return models.ProcessingJob{}, false, nil
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
	c.Logger.Info("stage job stored", slog.String("job_id", stored.ID), slog.String("job_type", string(stored.Type)), slog.String("project_id", stored.ProjectID), slog.String("media_id", stored.MediaID), slog.String("status", string(stored.Status)), slog.Bool("existing", existing), slog.Bool("publish", publish))
	if publish && !existing && !models.IsTerminalJobStatus(stored.Status) {
		if err := c.publishJob(ctx, stored); err != nil {
			c.Logger.Error("publish stage job failed", slog.String("job_id", stored.ID), slog.String("job_type", string(stored.Type)), slog.Any("error", err))
			return models.ProcessingJob{}, err
		}
	}
	return stored, nil
}

func (c *Coordinator) publishJob(ctx context.Context, job models.ProcessingJob) error {
	if c.Publisher == nil {
		return fmt.Errorf("job publisher is not configured")
	}
	message := queue.JobMessage{JobID: job.ID, ProjectID: job.ProjectID, MediaID: job.MediaID, JobType: job.Type, Configuration: smallConfiguration(job.Configuration)}
	c.Logger.Info("publishing job message", slog.String("job_id", job.ID), slog.String("job_type", string(job.Type)), slog.String("project_id", job.ProjectID), slog.String("media_id", job.MediaID))
	if err := c.Publisher.Publish(ctx, message); err != nil {
		return err
	}
	c.Logger.Info("published job message", slog.String("job_id", job.ID), slog.String("job_type", string(job.Type)))
	if c.Activator != nil {
		if err := c.Activator.Activate(ctx, message); err != nil {
			c.Logger.Warn("worker activation failed", slog.String("job_id", job.ID), slog.String("job_type", string(job.Type)), slog.Any("error", err))
		}
	}
	return nil
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

func stableValidateKey(projectID string, storyPlanVersion int, outputPreset string) string {
	return projectID + ":" + strconv.Itoa(storyPlanVersion) + ":" + outputPreset + ":" + string(models.JobTypeStoryValidate) + ":" + PipelineVersion
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

func renderOutputKey(userID, projectID string, storyPlanVersion int, outputPreset string) string {
	preset := strings.TrimSpace(outputPreset)
	if preset == "" {
		preset = "social"
	}
	return "users/" + userID + "/projects/" + projectID + "/renders/story-plan-v" + strconv.Itoa(storyPlanVersion) + "-" + preset + ".mp4"
}
