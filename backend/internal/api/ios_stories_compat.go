package api

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"

	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/framemio/backend/internal/credits"
	"github.com/framemio/backend/internal/models"
	"github.com/framemio/backend/internal/orchestration"
	"github.com/framemio/backend/internal/queue"
	"github.com/framemio/backend/internal/store"
	"github.com/framemio/backend/internal/templates"
)

const iosMaxStoryUploadBytes int64 = 1_000_000_000
const iosStoryTitleMaxLength = 30
const iosStoryVideoURLExpiry = 2 * time.Hour
const iosStoryDescriptionMaxLength = 240

type iosStory struct {
	ID               string             `json:"id"`
	Type             models.ProjectType `json:"type,omitempty"`
	Title            string             `json:"title"`
	Context          string             `json:"context"`
	CreatedAt        string             `json:"createdAt"`
	Duration         float64            `json:"duration"`
	AssetIDs         []string           `json:"assetIDs"`
	Status           string             `json:"status"`
	VideoURL         *string            `json:"videoURL,omitempty"`
	Description      string             `json:"description,omitempty"`
	ProgressTitle    string             `json:"progressTitle,omitempty"`
	ProgressDetail   string             `json:"progressDetail,omitempty"`
	ProgressValue    *float64           `json:"progressValue,omitempty"`
	StoryPlanVersion int                `json:"storyPlanVersion,omitempty"`
	Moments          []iosStoryMoment   `json:"moments,omitempty"`
	AvailableMoments []iosStoryMoment   `json:"availableMoments,omitempty"`
}

type iosStoryMoment struct {
	ID            string     `json:"id"`
	MomentID      string     `json:"momentId,omitempty"`
	MediaID       string     `json:"mediaId"`
	SourceAssetID string     `json:"sourceAssetId,omitempty"`
	Type          string     `json:"type,omitempty"`
	ThumbnailURL  *string    `json:"thumbnailUrl,omitempty"`
	Title         string     `json:"title,omitempty"`
	Description   string     `json:"description,omitempty"`
	CreationDate  *time.Time `json:"creationDate,omitempty"`
	Selected      bool       `json:"selected"`
}

type iosCreateStoryRequest struct {
	Context          string                            `json:"context"`
	Assets           []iosStoryAsset                   `json:"assets"`
	Analyses         []iosStoryAnalysis                `json:"analyses"`
	Style            string                            `json:"style,omitempty"`
	SubtitlesEnabled *bool                             `json:"subtitlesEnabled,omitempty"`
	SubtitleStyle    string                            `json:"subtitleStyle,omitempty"`
	SceneDetailStyle string                            `json:"sceneDetailStyle,omitempty"`
	DurationMode     models.DurationMode               `json:"duration_mode,omitempty"`
	TargetDuration   float64                           `json:"targetDuration,omitempty"`
	RemoveFiller     bool                              `json:"remove_filler_and_pauses,omitempty"`
	Instruction      string                            `json:"instruction,omitempty"`
	TemplateID       string                            `json:"template_id,omitempty"`
	TemplateVersion  int                               `json:"template_version,omitempty"`
	TemplateConfig   models.VideoTemplateConfiguration `json:"template_configuration,omitempty"`
}

type iosStoryEditRequest struct {
	RemoveMomentIDs  []string                `json:"removeMomentIds,omitempty"`
	RemoveMediaIDs   []string                `json:"removeMediaIds,omitempty"`
	AddMediaIDs      []string                `json:"addMediaIds,omitempty"`
	OrderedMediaIDs  []string                `json:"orderedMediaIds,omitempty"`
	Replace          []iosStoryMomentReplace `json:"replace,omitempty"`
	Analyses         []iosStoryAnalysis      `json:"analyses,omitempty"`
	Style            string                  `json:"style,omitempty"`
	SubtitlesEnabled *bool                   `json:"subtitlesEnabled,omitempty"`
	SubtitleStyle    string                  `json:"subtitleStyle,omitempty"`
	SceneDetailStyle string                  `json:"sceneDetailStyle,omitempty"`
	DurationMode     models.DurationMode     `json:"duration_mode,omitempty"`
	TargetDuration   float64                 `json:"targetDuration,omitempty"`
	RemoveFiller     bool                    `json:"remove_filler_and_pauses,omitempty"`
	Instruction      string                  `json:"instruction,omitempty"`
	BaseVersion      int                     `json:"baseVersion,omitempty"`
}

type iosStoryMomentReplace struct {
	FromMomentID string `json:"fromMomentId,omitempty"`
	FromMediaID  string `json:"fromMediaId,omitempty"`
	ToMediaID    string `json:"toMediaId"`
}

type iosStoryAsset struct {
	ID              string     `json:"id"`
	LocalIdentifier string     `json:"localIdentifier,omitempty"`
	Kind            string     `json:"kind"`
	CreationDate    *time.Time `json:"creationDate,omitempty"`
	Duration        float64    `json:"duration"`
	PixelWidth      int        `json:"pixelWidth"`
	PixelHeight     int        `json:"pixelHeight"`
}

type iosStoryAnalysis struct {
	AssetID     string   `json:"assetID"`
	Labels      []string `json:"labels,omitempty"`
	PeopleCount int      `json:"peopleCount"`
	Confidence  float64  `json:"confidence,omitempty"`
}

func (s *Server) listIOSStories(response http.ResponseWriter, request *http.Request) {
	user, ok := s.authenticate(response, request)
	if !ok {
		return
	}
	projects, err := s.Store.ListProjects(request.Context(), user.ID)
	if err != nil {
		s.internalError(response, err, "list iOS stories")
		return
	}
	stories := make([]iosStory, 0, len(projects))
	for _, project := range projects {
		project = s.reconcileIOSStoryProgress(request, user.ID, project)
		stories = append(stories, s.makeIOSStory(request, user.ID, project))
	}
	s.writeJSON(response, http.StatusOK, stories)
}

func (s *Server) getIOSStory(response http.ResponseWriter, request *http.Request, storyID string) {
	user, ok := s.authenticate(response, request)
	if !ok {
		return
	}
	project, err := s.Store.GetProject(request.Context(), user.ID, storyID)
	if err != nil {
		s.mapStoreError(response, err)
		return
	}
	project = s.reconcileIOSStoryProgress(request, user.ID, project)
	s.writeJSON(response, http.StatusOK, s.makeIOSStory(request, user.ID, project))
}

func (s *Server) deleteIOSStory(response http.ResponseWriter, request *http.Request, storyID string) {
	user, ok := s.authenticate(response, request)
	if !ok {
		return
	}
	storyID = strings.TrimSpace(storyID)
	if storyID == "" {
		s.writeError(response, http.StatusBadRequest, "invalid_story", "story id is required")
		return
	}
	jobs, err := s.Store.ListProjectJobs(request.Context(), user.ID, storyID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		s.mapStoreError(response, err)
		return
	}
	jobIDs := make([]string, 0, len(jobs))
	activeJobCount := 0
	for _, job := range jobs {
		jobIDs = append(jobIDs, job.ID)
		if models.IsTerminalJobStatus(job.Status) {
			continue
		}
		activeJobCount++
		if _, err := s.Store.UpdateJob(request.Context(), job.ID, models.JobStatusCancelled, job.Progress, "story_deleted", "story was deleted by user", job.ArtifactS3Key, job.ArtifactType); err != nil && !errors.Is(err, store.ErrConflict) && !errors.Is(err, store.ErrNotFound) {
			s.Logger.Warn("cancel story job before delete failed", "user_id", user.ID, "project_id", storyID, "job_id", job.ID, "job_type", string(job.Type), "error", err)
		}
	}
	if cleaner, ok := s.Publisher.(queue.ProjectMessageCleaner); ok && len(jobIDs) > 0 {
		deleted, err := cleaner.DeleteProjectMessages(request.Context(), storyID, jobIDs, 100)
		if err != nil {
			s.Logger.Warn("delete story queue messages failed", "user_id", user.ID, "project_id", storyID, "active_job_count", activeJobCount, "job_count", len(jobIDs), "error", err)
		} else {
			s.Logger.Info("deleted story queue messages", "user_id", user.ID, "project_id", storyID, "active_job_count", activeJobCount, "job_count", len(jobIDs), "deleted_message_count", deleted)
		}
	}
	if err := s.Store.DeleteProject(request.Context(), user.ID, storyID); err != nil {
		s.mapStoreError(response, err)
		return
	}
	s.writeJSON(response, http.StatusOK, map[string]bool{"deleted": true})
}

func (s *Server) createIOSStoryVersion(response http.ResponseWriter, request *http.Request, storyID string) {
	user, ok := s.authenticate(response, request)
	if !ok {
		return
	}
	storyID = strings.TrimSpace(storyID)
	if storyID == "" {
		s.writeError(response, http.StatusBadRequest, "invalid_story", "story id is required")
		return
	}
	var input iosStoryEditRequest
	if !s.decodeJSON(response, request, &input) {
		return
	}
	project, err := s.Store.GetProject(request.Context(), user.ID, storyID)
	if err != nil {
		s.mapStoreError(response, err)
		return
	}
	if strings.TrimSpace(input.Instruction) != "" && input.BaseVersion <= 0 {
		s.writeError(response, http.StatusBadRequest, "base_version_required", "baseVersion is required for AI story revisions")
		return
	}
	if existing, replayed := s.replayedIOSStoryRevision(request, user.ID, project.ID); replayed {
		s.Logger.Info("story_revision_requested", "user_id", user.ID, "project_id", project.ID, "job_id", existing.ID, "status", string(existing.Status), "replayed", true)
		s.writeJSON(response, http.StatusAccepted, s.makeIOSStory(request, user.ID, project))
		return
	}
	if !s.verifyIOSStoryBaseVersion(response, request, user.ID, project.ID, input.BaseVersion) {
		return
	}
	newMediaIDs := s.newIOSStoryEditMediaIDs(project, input)
	s.Logger.Info("trace iOS story edit media request",
		"user_id", user.ID,
		"project_id", project.ID,
		"project_media_ids", joinStringsOrUnset(project.MediaIDs),
		"requested_add_media_ids", joinStringsOrUnset(cleanStringList(input.AddMediaIDs)),
		"new_media_ids", joinStringsOrUnset(newMediaIDs),
		"requested_add_sources", s.iosStoryMediaSourcePairs(request, user.ID, cleanStringList(input.AddMediaIDs)),
		"project_sources", s.iosStoryMediaSourcePairs(request, user.ID, project.MediaIDs),
	)
	if len(newMediaIDs) > 0 {
		project, err = s.Store.AddProjectMedia(request.Context(), user.ID, project.ID, newMediaIDs)
		if err != nil {
			s.Logger.Warn("attach iOS story edit media failed", "user_id", user.ID, "project_id", project.ID, "new_media_count", len(newMediaIDs), "error", err)
			if errors.Is(err, store.ErrConflict) || errors.Is(err, store.ErrNotFound) || errors.Is(err, store.ErrInvalidOwner) {
				s.writeError(response, http.StatusBadRequest, "invalid_story_edit", "edit references media that is not ready or does not belong to this story")
				return
			}
			s.internalError(response, err, "attach iOS story edit media")
			return
		}
	}
	if err := s.saveIOSStoryEditAnalyses(request, user.ID, project.ID, input); err != nil {
		s.Logger.Error("save iOS story edit analyses failed", "user_id", user.ID, "project_id", project.ID, "analysis_count", len(input.Analyses), "error", err)
		s.mapStoreError(response, err)
		return
	}
	s.Logger.Info("received iOS story revision request",
		"user_id", user.ID,
		"project_id", project.ID,
		"project_status", string(project.Status),
		"remove_moment_count", len(input.RemoveMomentIDs),
		"remove_media_count", len(input.RemoveMediaIDs),
		"add_media_count", len(input.AddMediaIDs),
		"ordered_media_count", len(input.OrderedMediaIDs),
		"new_project_media_count", len(newMediaIDs),
		"replacement_count", len(input.Replace),
		"analysis_count", len(input.Analyses),
		"has_visual_style", strings.TrimSpace(input.Style) != "",
		"subtitles_enabled", boolPointerLogValue(input.SubtitlesEnabled),
		"subtitle_style", normalizeIOSSubtitleStyle(input.SubtitleStyle),
		"scene_detail_style", normalizeIOSSceneDetailStyle(input.SceneDetailStyle),
		"has_target_duration", input.TargetDuration > 0,
		"has_instruction", strings.TrimSpace(input.Instruction) != "",
		"base_version", input.BaseVersion,
	)
	if !s.allowExpensiveOperation(response, user.ID, "edit_ios_story") {
		return
	}
	config, err := s.iosStoryEditConfiguration(request, user.ID, project, input)
	if err != nil {
		if errors.Is(err, store.ErrConflict) || errors.Is(err, store.ErrNotFound) {
			s.Logger.Warn("rejected iOS story revision request",
				"user_id", user.ID,
				"project_id", project.ID,
				"reason", "invalid_media_or_moment_reference",
				"error", err,
			)
			s.writeError(response, http.StatusBadRequest, "invalid_story_edit", "edit references media or moments that do not belong to this story")
			return
		}
		s.internalError(response, err, "prepare iOS story edit")
		return
	}
	creditActions := []models.CreditAction{models.CreditActionRegenerateStory}
	statsByAction := map[models.CreditAction]credits.StoryMediaStats{}
	stats := credits.StoryMediaStats{}
	if len(newMediaIDs) > 0 {
		// no-op placeholder to keep the existing attachment flow below
	}
	requestedAddMediaIDs := cleanStringList(input.AddMediaIDs)
	if len(requestedAddMediaIDs) > 0 {
		creditActions = append(creditActions, models.CreditActionAddMoment)
		stats, err = s.addedIOSStoryCreditStats(request, user.ID, requestedAddMediaIDs)
		if err != nil {
			s.mapStoreError(response, err)
			return
		}
		statsByAction[models.CreditActionAddMoment] = stats
	}
	if _, ok := statsByAction[models.CreditActionRegenerateStory]; !ok {
		storyStats, err := s.currentIOSStoryCreditStats(request, user.ID, project.ID)
		if err != nil {
			s.mapStoreError(response, err)
			return
		}
		statsByAction[models.CreditActionRegenerateStory] = storyStats
	}
	s.Logger.Info("prepared iOS story edit credit scope",
		"user_id", user.ID,
		"project_id", project.ID,
		"selected_action", joinCreditActionsForProject(creditActions),
		"story_plan_version", config["story_plan_version"],
		"requested_add_media_ids", joinStringsOrUnset(cleanStringList(input.AddMediaIDs)),
		"requested_replace_to_media_ids", joinIOSStoryReplacementTargets(input.Replace),
		"project_media_ids", joinStringsOrUnset(project.MediaIDs),
		"new_media_ids", joinStringsOrUnset(newMediaIDs),
		"remove_moment_ids", joinStringsOrUnset(cleanStringList(input.RemoveMomentIDs)),
		"remove_media_ids", joinStringsOrUnset(cleanStringList(input.RemoveMediaIDs)),
		"ordered_media_ids", joinStringsOrUnset(cleanStringList(input.OrderedMediaIDs)),
		"instruction_present", strings.TrimSpace(input.Instruction) != "",
		"credit_price", creditPricingForActions(s.Credits.Policy, statsByAction, creditActions),
		"add_media_count", stats.AddedMediaCount,
		"add_photo_count", stats.AddedPhotoCount,
		"add_video_seconds", stats.AddedVideoSeconds,
		"story_media_count", stats.StoryMediaCount,
		"story_video_seconds", stats.StoryVideoSeconds,
	)
	reservations := make([]models.CreditReservation, 0, len(creditActions))
	if len(creditActions) > 0 {
		version := strings.TrimSpace(config["story_plan_version"])
		if version == "" {
			s.writeError(response, http.StatusBadRequest, "invalid_story_edit", "story version is required")
			return
		}
		referenceID := project.ID + ":v" + version
		for _, action := range creditActions {
			actionStats := statsByAction[action]
			s.Logger.Info("reserving iOS story edit credits",
				"user_id", user.ID,
				"project_id", project.ID,
				"reference_id", referenceID,
				"action", string(action),
				"credit_price", creditPricingForAction(s.Credits.Policy, action, actionStats),
				"add_media_count", actionStats.AddedMediaCount,
				"story_media_count", actionStats.StoryMediaCount,
				"story_video_seconds", actionStats.StoryVideoSeconds,
			)
			reservation, _ := s.reserveIOSStoryCredits(response, request, user.ID, action, referenceID, actionStats, map[string]any{"project_id": project.ID, "version": version})
			if reservation.ID == "" {
				s.Logger.Warn("reserve iOS story edit credits stopped",
					"user_id", user.ID,
					"project_id", project.ID,
					"reference_id", referenceID,
					"action", string(action),
				)
				return
			}
			s.Logger.Info("reserved iOS story edit credits",
				"user_id", user.ID,
				"project_id", project.ID,
				"reference_id", referenceID,
				"action", string(action),
				"reservation_id", reservation.ID,
				"reserved_amount", reservation.Amount,
				"allocation_count", len(reservation.Allocations),
			)
			reservations = append(reservations, reservation)
		}
		defer func() {
			for _, reservation := range reservations {
				if reservation.ID == "" {
					continue
				}
				if _, err := s.Credits.Release(request.Context(), user.ID, reservation.ID); err != nil && !errors.Is(err, store.ErrConflict) && !errors.Is(err, store.ErrNotFound) {
					s.Logger.Warn("release iOS story edit reservation failed", "user_id", user.ID, "project_id", project.ID, "reservation_id", reservation.ID, "action", string(reservation.Action), "error", err)
				}
			}
		}()
	}
	if s.Orchestrator == nil {
		s.internalError(response, errors.New("job orchestrator not configured"), "job orchestrator not configured")
		return
	}
	job, err := s.Orchestrator.RequestStoryPlanRevision(request.Context(), user.ID, project.ID, request.Header.Get("Idempotency-Key"), config)
	if err != nil {
		s.Logger.Error("queue iOS story revision failed", "user_id", user.ID, "project_id", project.ID, "story_plan_version", config["story_plan_version"], "error", err)
		s.mapStoreError(response, err)
		return
	}
	s.Logger.Info("queued iOS story revision request details",
		"user_id", user.ID,
		"project_id", project.ID,
		"job_id", job.ID,
		"story_plan_version", job.Configuration["story_plan_version"],
		"selected_media_ids", job.Configuration["selected_media_ids"],
		"preferred_media_order", job.Configuration["preferred_media_order"],
		"excluded_media_ids", job.Configuration["excluded_media_ids"],
		"excluded_moment_ids", job.Configuration["excluded_moment_ids"],
		"edit_intent", job.Configuration["edit_intent"],
	)
	if len(creditActions) > 0 {
		creditOperationKey := joinCreditActionsForProject(creditActions)
		if err := s.Store.SetProjectCreditOperationKey(request.Context(), user.ID, project.ID, creditOperationKey); err != nil {
			s.internalError(response, err, "store iOS story credit operation")
			return
		}
		s.Logger.Info("stored iOS story edit credit operation key",
			"user_id", user.ID,
			"project_id", project.ID,
			"story_plan_version", job.Configuration["story_plan_version"],
			"credit_operation_key", creditOperationKey,
			"reservation_count", len(reservations),
		)
		reservations = nil
	}
	s.Logger.Info("queued iOS story revision", "user_id", user.ID, "project_id", project.ID, "job_id", job.ID, "job_status", string(job.Status), "story_plan_version", job.Configuration["story_plan_version"], "subtitles_enabled", valueOrUnset(job.Configuration["subtitles_enabled"]), "caption_style_preset", valueOrUnset(job.Configuration["caption_style_preset"]), "broll_preference", valueOrUnset(job.Configuration["broll_preference"]))
	updated, err := s.Store.GetProject(request.Context(), user.ID, project.ID)
	if err != nil {
		s.mapStoreError(response, err)
		return
	}
	s.writeJSON(response, http.StatusAccepted, s.makeIOSStory(request, user.ID, updated))
}

func (s *Server) regenerateIOSStory(response http.ResponseWriter, request *http.Request, storyID string) {
	user, ok := s.authenticate(response, request)
	if !ok {
		return
	}
	storyID = strings.TrimSpace(storyID)
	if storyID == "" {
		s.writeError(response, http.StatusBadRequest, "invalid_story", "story id is required")
		return
	}
	project, err := s.Store.GetProject(request.Context(), user.ID, storyID)
	if err != nil {
		s.mapStoreError(response, err)
		return
	}
	if !s.allowExpensiveOperation(response, user.ID, "regenerate_ios_story") {
		return
	}
	config, err := s.iosStoryEditConfiguration(request, user.ID, project, iosStoryEditRequest{})
	if err != nil {
		if errors.Is(err, store.ErrConflict) || errors.Is(err, store.ErrNotFound) {
			s.writeError(response, http.StatusBadRequest, "invalid_story_edit", "story is not ready to regenerate")
			return
		}
		s.internalError(response, err, "prepare iOS story regenerate")
		return
	}
	stats, err := s.currentIOSStoryCreditStats(request, user.ID, project.ID)
	if err != nil {
		s.mapStoreError(response, err)
		return
	}
	s.Logger.Info("prepared iOS story regenerate credit scope",
		"user_id", user.ID,
		"project_id", project.ID,
		"story_plan_version", config["story_plan_version"],
		"project_media_ids", joinStringsOrUnset(project.MediaIDs),
		"selected_media_ids", config["selected_media_ids"],
		"preferred_media_order", config["preferred_media_order"],
		"excluded_media_ids", config["excluded_media_ids"],
		"excluded_moment_ids", config["excluded_moment_ids"],
		"credit_price", creditPricingForAction(s.Credits.Policy, models.CreditActionRegenerateStory, stats),
		"story_media_count", stats.StoryMediaCount,
		"story_video_seconds", stats.StoryVideoSeconds,
	)
	version := strings.TrimSpace(config["story_plan_version"])
	if version == "" {
		s.writeError(response, http.StatusBadRequest, "invalid_story_edit", "story version is required")
		return
	}
	referenceID := project.ID + ":v" + version
	reservation, ok := s.reserveIOSStoryCredits(response, request, user.ID, models.CreditActionRegenerateStory, referenceID, stats, map[string]any{"project_id": project.ID, "version": version})
	if !ok {
		return
	}
	defer func() {
		if reservation.ID == "" {
			return
		}
		if _, err := s.Credits.Release(request.Context(), user.ID, reservation.ID); err != nil && !errors.Is(err, store.ErrConflict) && !errors.Is(err, store.ErrNotFound) {
			s.Logger.Warn("release iOS story regenerate reservation failed", "user_id", user.ID, "project_id", project.ID, "reservation_id", reservation.ID, "error", err)
		}
	}()
	config["edit_intent"] = "regenerate_story"
	if s.Orchestrator == nil {
		s.internalError(response, errors.New("job orchestrator not configured"), "job orchestrator not configured")
		return
	}
	job, err := s.Orchestrator.RequestStoryPlanRevision(request.Context(), user.ID, project.ID, request.Header.Get("Idempotency-Key"), config)
	if err != nil {
		s.Logger.Error("queue iOS story regeneration failed", "user_id", user.ID, "project_id", project.ID, "story_plan_version", config["story_plan_version"], "error", err)
		s.mapStoreError(response, err)
		return
	}
	if err := s.Store.SetProjectCreditOperationKey(request.Context(), user.ID, project.ID, string(models.CreditActionRegenerateStory)); err != nil {
		s.internalError(response, err, "store iOS story credit operation")
		return
	}
	reservation.ID = ""
	s.Logger.Info("queued iOS story regeneration", "user_id", user.ID, "project_id", project.ID, "job_id", job.ID, "job_status", string(job.Status), "story_plan_version", job.Configuration["story_plan_version"])
	updated, err := s.Store.GetProject(request.Context(), user.ID, project.ID)
	if err != nil {
		s.mapStoreError(response, err)
		return
	}
	s.writeJSON(response, http.StatusAccepted, s.makeIOSStory(request, user.ID, updated))
}

func (s *Server) replayedIOSStoryRevision(request *http.Request, userID, projectID string) (models.ProcessingJob, bool) {
	key := strings.TrimSpace(request.Header.Get("Idempotency-Key"))
	if key == "" {
		return models.ProcessingJob{}, false
	}
	job, err := s.Store.GetJobByIdempotency(request.Context(), userID, projectID+":story_revision:"+key)
	if err != nil {
		return models.ProcessingJob{}, false
	}
	if job.ProjectID != projectID || job.Type != models.JobTypeStoryPlan || strings.TrimSpace(job.Configuration["edit_intent"]) == "" {
		return models.ProcessingJob{}, false
	}
	return job, true
}

func (s *Server) verifyIOSStoryBaseVersion(response http.ResponseWriter, request *http.Request, userID, projectID string, baseVersion int) bool {
	if baseVersion <= 0 {
		return true
	}
	latest, err := s.Store.GetStoryPlan(request.Context(), userID, projectID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			s.writeError(response, http.StatusConflict, "story_not_ready", "story plan is not ready for editing")
			return false
		}
		s.mapStoreError(response, err)
		return false
	}
	if latest.Version != baseVersion {
		s.writeError(response, http.StatusConflict, "stale_story_plan_version", fmt.Sprintf("story plan version %d is no longer current", baseVersion))
		return false
	}
	return true
}

func (s *Server) reserveIOSStoryCredits(response http.ResponseWriter, request *http.Request, userID string, action models.CreditAction, referenceID string, stats credits.StoryMediaStats, metadata map[string]any) (models.CreditReservation, bool) {
	if s.Credits == nil {
		s.internalError(response, errors.New("credit service not configured"), "credit service not configured")
		return models.CreditReservation{}, false
	}
	quote, err := s.Credits.Quote(request.Context(), userID, action, stats)
	if err != nil {
		s.internalError(response, err, "quote credit action")
		return models.CreditReservation{}, false
	}
	s.Logger.Info("quoted iOS story credits",
		"user_id", userID,
		"action", string(action),
		"reference_id", referenceID,
		"required_credits", quote.Credits,
		"available_credits", quote.Balance,
		"balance_after", quote.BalanceAfter,
		"eligible", quote.Eligible,
		"reason", valueOrUnset(quote.Reason),
	)
	if !quote.Eligible {
		s.Logger.Warn("rejected iOS story credit reservation as ineligible", "user_id", userID, "action", string(action), "reference_id", referenceID, "reason", quote.Reason)
		s.writeJSON(response, http.StatusBadRequest, map[string]any{"error": "story_too_large", "reason": quote.Reason})
		return models.CreditReservation{}, false
	}
	if quote.Balance < quote.Credits {
		s.Logger.Warn("rejected iOS story credit reservation for insufficient credits", "user_id", userID, "action", string(action), "reference_id", referenceID, "required_credits", quote.Credits, "available_credits", quote.Balance)
		s.writeJSON(response, http.StatusPaymentRequired, map[string]any{"error": "insufficient_credits", "required": quote.Credits, "available": quote.Balance})
		return models.CreditReservation{}, false
	}
	reservation, _, err := s.Credits.Reserve(request.Context(), userID, action, "project", referenceID, stats, metadata)
	if err != nil {
		if errors.Is(err, store.ErrInsufficientCredits) {
			s.Logger.Warn("iOS story credit reservation raced insufficient credits", "user_id", userID, "action", string(action), "reference_id", referenceID, "required_credits", quote.Credits, "available_credits", quote.Balance)
			s.writeJSON(response, http.StatusPaymentRequired, map[string]any{"error": "insufficient_credits", "required": quote.Credits, "available": quote.Balance})
			return models.CreditReservation{}, false
		}
		s.internalError(response, err, "reserve credit action")
		return models.CreditReservation{}, false
	}
	s.Logger.Info("created iOS story credit reservation", "user_id", userID, "action", string(action), "reference_id", referenceID, "reservation_id", reservation.ID, "reserved_amount", reservation.Amount, "allocation_count", len(reservation.Allocations))
	return reservation, true
}

func (s *Server) currentIOSStoryCreditStats(request *http.Request, userID, projectID string) (credits.StoryMediaStats, error) {
	stats := credits.StoryMediaStats{}
	plan, err := s.Store.GetStoryPlan(request.Context(), userID, projectID)
	if err == nil {
		stats.StoryMediaCount = len(plan.Moments)
		stats.StoryVideoSeconds = max(0, int((time.Duration(plan.TotalDurationMS)*time.Millisecond)/time.Second))
		return stats, nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return stats, err
	}
	project, err := s.Store.GetProject(request.Context(), userID, projectID)
	if err != nil {
		return stats, err
	}
	stats.StoryMediaCount = len(project.MediaIDs)
	for _, mediaID := range project.MediaIDs {
		media, err := s.Store.GetMedia(request.Context(), userID, mediaID)
		if err != nil || media.DurationSeconds == nil {
			continue
		}
		stats.StoryVideoSeconds += int(*media.DurationSeconds + 0.5)
	}
	return stats, nil
}

func (s *Server) addedIOSStoryCreditStats(request *http.Request, userID string, mediaIDs []string) (credits.StoryMediaStats, error) {
	stats := credits.StoryMediaStats{AddedMediaCount: len(mediaIDs)}
	for _, mediaID := range mediaIDs {
		media, err := s.Store.GetMedia(request.Context(), userID, mediaID)
		if err != nil {
			return stats, err
		}
		switch media.Type {
		case models.MediaTypePhoto, models.MediaTypeLivePhoto:
			stats.AddedPhotoCount++
		case models.MediaTypeVideo:
			if media.DurationSeconds != nil {
				stats.AddedVideoSeconds += int(*media.DurationSeconds + 0.5)
			}
		}
	}
	return stats, nil
}

func (s *Server) reconcileIOSStoryProgress(request *http.Request, userID string, project models.StoryProject) models.StoryProject {
	// story_ready is included because a StoryPlan that completed before the
	// pipeline queued its render leaves the project parked there forever, which
	// iOS reports as an endless "Finding meaningful moments".
	if s.Orchestrator == nil || (project.Status != models.ProjectStatusAnalyzing && project.Status != models.ProjectStatusStoryReady) {
		return project
	}
	jobs, err := s.Store.ListProjectJobs(request.Context(), userID, project.ID)
	if err != nil {
		s.Logger.Warn("reconcile iOS story jobs failed", "user_id", userID, "project_id", project.ID, "error", err)
		return project
	}
	for _, job := range jobs {
		if !models.IsTerminalJobStatus(job.Status) {
			return project
		}
	}
	for _, job := range jobs {
		if job.Status != models.JobStatusFailed && job.Status != models.JobStatusCancelled {
			continue
		}
		if err := s.Orchestrator.AdvanceAfterJob(request.Context(), job); err != nil {
			s.Logger.Warn("reconcile iOS story terminal failure failed", "user_id", userID, "project_id", project.ID, "job_id", job.ID, "job_type", string(job.Type), "status", string(job.Status), "error", err)
			return project
		}
		updated, err := s.Store.GetProject(request.Context(), userID, project.ID)
		if err != nil {
			s.Logger.Warn("reload reconciled failed iOS story failed", "user_id", userID, "project_id", project.ID, "error", err)
			return project
		}
		s.Logger.Info("reconciled failed iOS story", "user_id", userID, "project_id", project.ID, "status", string(updated.Status), "job_id", job.ID, "job_type", string(job.Type))
		return updated
	}
	advanced := false
	for _, job := range jobs {
		if job.Status != models.JobStatusCompleted {
			continue
		}
		if err := s.Orchestrator.AdvanceAfterJob(request.Context(), job); err != nil {
			s.Logger.Warn("reconcile iOS story advancement failed", "user_id", userID, "project_id", project.ID, "job_id", job.ID, "job_type", string(job.Type), "error", err)
			return project
		}
		advanced = true
	}
	if !advanced {
		return project
	}
	updated, err := s.Store.GetProject(request.Context(), userID, project.ID)
	if err != nil {
		s.Logger.Warn("reload reconciled iOS story failed", "user_id", userID, "project_id", project.ID, "error", err)
		return project
	}
	s.Logger.Info("reconciled stalled iOS story", "user_id", userID, "project_id", project.ID, "status", string(updated.Status))
	return updated
}

func (s *Server) newIOSStoryEditMediaIDs(project models.StoryProject, input iosStoryEditRequest) []string {
	projectMedia := make(map[string]struct{}, len(project.MediaIDs))
	for _, mediaID := range project.MediaIDs {
		projectMedia[mediaID] = struct{}{}
	}
	candidates := cleanStringList(input.AddMediaIDs)
	for _, replacement := range input.Replace {
		if toMediaID := strings.TrimSpace(replacement.ToMediaID); toMediaID != "" {
			candidates = append(candidates, toMediaID)
		}
	}
	seen := make(map[string]struct{}, len(candidates))
	newMediaIDs := make([]string, 0, len(candidates))
	for _, mediaID := range candidates {
		mediaID = strings.TrimSpace(mediaID)
		if mediaID == "" {
			continue
		}
		if _, exists := projectMedia[mediaID]; exists {
			continue
		}
		if _, duplicate := seen[mediaID]; duplicate {
			continue
		}
		seen[mediaID] = struct{}{}
		newMediaIDs = append(newMediaIDs, mediaID)
	}
	return newMediaIDs
}

func (s *Server) iosStoryMediaSourcePairs(request *http.Request, userID string, mediaIDs []string) string {
	if len(mediaIDs) == 0 {
		return "unset"
	}
	pairs := make([]string, 0, len(mediaIDs))
	for _, mediaID := range mediaIDs {
		mediaID = strings.TrimSpace(mediaID)
		if mediaID == "" {
			continue
		}
		media, err := s.Store.GetMedia(request.Context(), userID, mediaID)
		if err != nil {
			pairs = append(pairs, mediaID+":lookup_error")
			continue
		}
		sourceAssetID := strings.TrimSpace(media.SourceAssetID)
		if sourceAssetID == "" {
			sourceAssetID = "unset"
		}
		pairs = append(pairs, mediaID+":"+sourceAssetID)
	}
	if len(pairs) == 0 {
		return "unset"
	}
	sort.Strings(pairs)
	return strings.Join(pairs, ",")
}

func (s *Server) iosStorySelectedMediaHasDuplicateSource(request *http.Request, userID string, selected map[string]struct{}) (bool, string) {
	seen := make(map[string]string, len(selected))
	for mediaID := range selected {
		media, err := s.Store.GetMedia(request.Context(), userID, mediaID)
		if err != nil {
			continue
		}
		sourceAssetID := strings.TrimSpace(media.SourceAssetID)
		if sourceAssetID == "" {
			continue
		}
		if existingMediaID, ok := seen[sourceAssetID]; ok && existingMediaID != mediaID {
			return true, sourceAssetID
		}
		seen[sourceAssetID] = mediaID
	}
	return false, ""
}

func (s *Server) saveIOSStoryEditAnalyses(request *http.Request, userID, projectID string, input iosStoryEditRequest) error {
	if len(input.Analyses) == 0 {
		return nil
	}
	referenced := make(map[string]struct{})
	for _, mediaID := range cleanStringList(input.AddMediaIDs) {
		referenced[mediaID] = struct{}{}
	}
	for _, replacement := range input.Replace {
		if toMediaID := strings.TrimSpace(replacement.ToMediaID); toMediaID != "" {
			referenced[toMediaID] = struct{}{}
		}
	}
	mediaIDs := make([]string, 0, len(referenced))
	assetIDsByMediaID := make(map[string]string, len(referenced))
	for mediaID := range referenced {
		media, err := s.Store.GetMedia(request.Context(), userID, mediaID)
		if err != nil {
			return err
		}
		if media.Type == models.MediaTypeVideo {
			continue
		}
		mediaIDs = append(mediaIDs, mediaID)
		assetIDsByMediaID[mediaID] = mediaID
	}
	if len(mediaIDs) == 0 {
		return nil
	}
	return s.saveIOSAnalyses(request, userID, projectID, mediaIDs, assetIDsByMediaID, input.Analyses)
}

func (s *Server) iosStoryEditConfiguration(request *http.Request, userID string, project models.StoryProject, input iosStoryEditRequest) (map[string]string, error) {
	projectMedia := make(map[string]struct{}, len(project.MediaIDs))
	for _, mediaID := range project.MediaIDs {
		projectMedia[mediaID] = struct{}{}
	}
	latest, err := s.Store.GetStoryPlan(request.Context(), userID, project.ID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}

	selected := make(map[string]struct{}, len(project.MediaIDs))
	if len(latest.Moments) > 0 {
		for _, moment := range latest.Moments {
			mediaID := storyMomentMediaID(moment)
			if mediaID != "" {
				selected[mediaID] = struct{}{}
			}
		}
	} else {
		for _, mediaID := range project.MediaIDs {
			selected[mediaID] = struct{}{}
		}
	}
	excludedMedia := make(map[string]struct{})
	excludedMoments := make(map[string]struct{})
	for _, mediaID := range cleanStringList(input.RemoveMediaIDs) {
		if _, ok := projectMedia[mediaID]; !ok {
			return nil, store.ErrConflict
		}
		excludedMedia[mediaID] = struct{}{}
		delete(selected, mediaID)
	}
	for _, momentID := range cleanStringList(input.RemoveMomentIDs) {
		if len(latest.Moments) > 0 && !storyPlanHasMoment(latest, momentID) {
			return nil, store.ErrConflict
		}
		excludedMoments[momentID] = struct{}{}
		if mediaID := mediaIDForStoryMoment(latest, momentID); mediaID != "" {
			delete(selected, mediaID)
		}
	}
	for _, replacement := range input.Replace {
		toMediaID := strings.TrimSpace(replacement.ToMediaID)
		if _, ok := projectMedia[toMediaID]; !ok || toMediaID == "" {
			return nil, store.ErrConflict
		}
		if fromMomentID := strings.TrimSpace(replacement.FromMomentID); fromMomentID != "" {
			if len(latest.Moments) > 0 && !storyPlanHasMoment(latest, fromMomentID) {
				return nil, store.ErrConflict
			}
			excludedMoments[fromMomentID] = struct{}{}
			if mediaID := mediaIDForStoryMoment(latest, fromMomentID); mediaID != "" {
				delete(selected, mediaID)
			}
		}
		if fromMediaID := strings.TrimSpace(replacement.FromMediaID); fromMediaID != "" {
			if _, ok := projectMedia[fromMediaID]; !ok {
				return nil, store.ErrConflict
			}
			excludedMedia[fromMediaID] = struct{}{}
			delete(selected, fromMediaID)
		}
		selected[toMediaID] = struct{}{}
	}
	for _, mediaID := range cleanStringList(input.AddMediaIDs) {
		if _, ok := projectMedia[mediaID]; !ok {
			return nil, store.ErrConflict
		}
		selected[mediaID] = struct{}{}
		delete(excludedMedia, mediaID)
	}
	s.Logger.Info("trace iOS story edit selected media sources",
		"user_id", userID,
		"project_id", project.ID,
		"selected_media_ids", joinStringsOrUnset(sortedKeys(selected)),
		"selected_sources", s.iosStoryMediaSourcePairs(request, userID, sortedKeys(selected)),
	)
	if hasDuplicate, duplicateSourceID := s.iosStorySelectedMediaHasDuplicateSource(request, userID, selected); hasDuplicate {
		s.Logger.Warn("rejected iOS story revision duplicate source asset", "user_id", userID, "project_id", project.ID, "source_asset_id", duplicateSourceID, "selected_sources", s.iosStoryMediaSourcePairs(request, userID, sortedKeys(selected)))
		return nil, store.ErrConflict
	}

	if len(selected) == 0 {
		return nil, store.ErrConflict
	}

	baseVersion := latest.Version
	if input.BaseVersion > 0 {
		baseVersion = input.BaseVersion
	}
	version := baseVersion + 1
	if version <= 1 {
		version = 1
	}
	selectedMediaIDs := sortedKeys(selected)
	preferredMediaOrder, err := preferredIOSStoryMediaOrder(input.OrderedMediaIDs, selected, projectMedia)
	if err != nil {
		return nil, err
	}
	config := map[string]string{
		"story_plan_version":      strconv.Itoa(version),
		"base_story_plan_version": strconv.Itoa(baseVersion),
		"selected_media_ids":      strings.Join(selectedMediaIDs, ","),
	}
	if len(preferredMediaOrder) > 0 {
		config["preferred_media_order"] = strings.Join(preferredMediaOrder, ",")
	}
	if len(excludedMedia) > 0 {
		config["excluded_media_ids"] = strings.Join(sortedKeys(excludedMedia), ",")
	}
	if len(excludedMoments) > 0 {
		config["excluded_moment_ids"] = strings.Join(sortedKeys(excludedMoments), ",")
	}
	durationMode := models.NormalizeDurationMode(input.DurationMode)
	if input.TargetDuration > 0 && input.DurationMode == "" {
		durationMode = models.DurationModeFixed
	}
	config["duration_mode"] = string(durationMode)
	if durationMode == models.DurationModeFixed {
		if input.TargetDuration <= 0 {
			return nil, store.ErrConflict
		}
		if input.TargetDuration < 5 || input.TargetDuration > 60 {
			return nil, store.ErrConflict
		}
		config["target_duration_seconds"] = strconv.Itoa(int(input.TargetDuration + 0.5))
	} else if durationMode == models.DurationModeFullSource {
		if input.TargetDuration > 0 {
			return nil, store.ErrConflict
		}
		config["remove_filler_and_pauses"] = strconv.FormatBool(input.RemoveFiller)
	} else if input.RemoveFiller {
		return nil, store.ErrConflict
	}
	if style := normalizeIOSStoryEditStyle(input.Style); style != "" {
		config["visual_style"] = style
	}
	if input.SubtitlesEnabled != nil {
		config["subtitles_enabled"] = strconv.FormatBool(*input.SubtitlesEnabled)
	}
	if subtitleStyle := normalizeIOSSubtitleStyle(input.SubtitleStyle); subtitleStyle != "" {
		config["caption_style_preset"] = subtitleStyle
	}
	if preference := normalizeIOSSceneDetailStyle(input.SceneDetailStyle); preference != "" {
		config["broll_preference"] = preference
	}
	if instruction := strings.TrimSpace(sanitizeIOSStoryText(input.Instruction)); instruction != "" {
		config["user_instructions"] = truncateIOSConfigValue(instruction, 480)
		config["revision_mode"] = "ai_story_plan_revision"
	}
	config["edit_intent"] = "ios_story_refinement"
	return config, nil
}

func storyPlanHasMoment(plan models.StoryPlan, momentID string) bool {
	momentID = strings.TrimSpace(momentID)
	for _, moment := range plan.Moments {
		if moment.MomentID == momentID || storyMomentMediaID(moment) == momentID {
			return true
		}
	}
	return false
}

func mediaIDForStoryMoment(plan models.StoryPlan, momentID string) string {
	momentID = strings.TrimSpace(momentID)
	for _, moment := range plan.Moments {
		if moment.MomentID == momentID || storyMomentMediaID(moment) == momentID {
			return storyMomentMediaID(moment)
		}
	}
	return ""
}

func storyMomentMediaID(moment models.StoryMoment) string {
	if strings.TrimSpace(moment.MediaID) != "" {
		return strings.TrimSpace(moment.MediaID)
	}
	return strings.TrimSpace(moment.AssetID)
}

func cleanStringList(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	cleaned := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		cleaned = append(cleaned, value)
	}
	return cleaned
}

func sortedKeys(values map[string]struct{}) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func joinStringsOrUnset(values []string) string {
	if len(values) == 0 {
		return "unset"
	}
	return strings.Join(values, ",")
}

func joinIOSStoryReplacementTargets(replacements []iosStoryMomentReplace) string {
	if len(replacements) == 0 {
		return "unset"
	}
	targets := make([]string, 0, len(replacements))
	for _, replacement := range replacements {
		if target := strings.TrimSpace(replacement.ToMediaID); target != "" {
			targets = append(targets, target)
		}
	}
	return joinStringsOrUnset(targets)
}

func creditPricingForAction(policy credits.Policy, action models.CreditAction, stats credits.StoryMediaStats) int {
	if price, ok := policy.Price(action, stats); ok {
		return price
	}
	return 0
}

func creditPricingForActions(policy credits.Policy, statsByAction map[models.CreditAction]credits.StoryMediaStats, actions []models.CreditAction) int {
	total := 0
	for _, action := range uniqueCreditActions(actions) {
		total += creditPricingForAction(policy, action, statsByAction[action])
	}
	return total
}

func joinCreditActionsForProject(actions []models.CreditAction) string {
	unique := uniqueCreditActions(actions)
	values := make([]string, 0, len(unique))
	for _, action := range unique {
		values = append(values, string(action))
	}
	return strings.Join(values, ",")
}

func uniqueCreditActions(actions []models.CreditAction) []models.CreditAction {
	seen := make(map[models.CreditAction]struct{}, len(actions))
	unique := make([]models.CreditAction, 0, len(actions))
	for _, action := range actions {
		if action == "" {
			continue
		}
		if _, exists := seen[action]; exists {
			continue
		}
		seen[action] = struct{}{}
		unique = append(unique, action)
	}
	return unique
}

func preferredIOSStoryMediaOrder(input []string, selected, projectMedia map[string]struct{}) ([]string, error) {
	ordered := cleanStringList(input)
	if len(ordered) == 0 {
		return nil, nil
	}
	seen := make(map[string]struct{}, len(selected))
	result := make([]string, 0, len(selected))
	for _, mediaID := range ordered {
		if _, ok := projectMedia[mediaID]; !ok {
			return nil, store.ErrConflict
		}
		if _, ok := selected[mediaID]; !ok {
			return nil, store.ErrConflict
		}
		seen[mediaID] = struct{}{}
		result = append(result, mediaID)
	}
	for _, mediaID := range sortedKeys(selected) {
		if _, ok := seen[mediaID]; ok {
			continue
		}
		result = append(result, mediaID)
	}
	return result, nil
}

func normalizeIOSStoryEditStyle(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "natural":
		return string(models.VisualStyleNatural)
	case "cinematic":
		return string(models.VisualStyleCinematic)
	case "warm":
		return string(models.VisualStyleWarm)
	case "playful":
		return string(models.VisualStylePlayful)
	case "calm":
		return string(models.VisualStyleCalm)
	case "minimal":
		return string(models.VisualStyleMinimal)
	case "elegant":
		return string(models.VisualStyleElegant)
	default:
		return ""
	}
}

func normalizeIOSSubtitleStyle(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "bold", "bold_social":
		return "bold_social"
	case "cinematic", "cinematic_subtitle":
		// Older iOS builds used cinematic_subtitle as the default. Keep those
		// clients on the current social caption behavior.
		return "bold_social"
	case "minimal", "clean_minimal":
		return "clean_minimal"
	case "playful", "playful_pop":
		return "playful_pop"
	default:
		return ""
	}
}

func normalizeIOSSceneDetailStyle(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "more", "more_details", "moredetails", "more-details", "extra", "rich":
		return "more"
	case "main", "main_moments", "mainmoments", "main-moments", "none", "off":
		return "none"
	default:
		return ""
	}
}

func boolPointerLogValue(value *bool) string {
	if value == nil {
		return "unset"
	}
	return strconv.FormatBool(*value)
}

func valueOrUnset(value string) string {
	if trimmed := strings.TrimSpace(value); trimmed != "" {
		return trimmed
	}
	return "unset"
}

func configListCount(value string) int {
	count := 0
	for _, part := range strings.Split(value, ",") {
		if strings.TrimSpace(part) != "" {
			count++
		}
	}
	return count
}

func truncateIOSConfigValue(value string, maxLength int) string {
	if maxLength <= 0 {
		return ""
	}
	if len(value) <= maxLength {
		return value
	}
	truncated := make([]rune, 0, maxLength)
	currentLength := 0
	for _, char := range value {
		charText := string(char)
		if currentLength+len(charText) > maxLength {
			break
		}
		truncated = append(truncated, char)
		currentLength += len(charText)
	}
	text := strings.TrimSpace(string(truncated))
	if cut := strings.LastIndexAny(text, " .,!?:;"); cut >= maxLength-24 {
		text = strings.TrimSpace(text[:cut])
	}
	return strings.TrimRight(text, ".…")
}

func (s *Server) createIOSStory(response http.ResponseWriter, request *http.Request) {
	user, ok := s.authenticate(response, request)
	if !ok {
		return
	}
	var input iosCreateStoryRequest
	if !s.decodeJSON(response, request, &input) {
		return
	}
	input.Context = strings.TrimSpace(sanitizeIOSStoryText(input.Context))
	if input.Context == "" || len(input.Assets) == 0 {
		s.writeError(response, http.StatusBadRequest, "invalid_story", "context and at least one asset are required")
		return
	}
	templateConfiguration := models.VideoProjectConfiguration{
		TemplateID:            strings.TrimSpace(input.TemplateID),
		TemplateVersion:       input.TemplateVersion,
		TemplateConfiguration: input.TemplateConfig,
	}
	normalizedTemplateConfiguration, err := templates.NormalizeProjectConfiguration(models.ProjectTypeStory, templateConfiguration)
	if err != nil {
		s.writeError(response, http.StatusBadRequest, "invalid_template_configuration", err.Error())
		return
	}
	if !s.allowExpensiveOperation(response, user.ID, "create_ios_story") {
		return
	}
	instruction := strings.TrimSpace(sanitizeIOSStoryText(input.Instruction))
	durationMode := models.NormalizeDurationMode(input.DurationMode)
	if input.TargetDuration > 0 && input.DurationMode == "" {
		durationMode = models.DurationModeFixed
	}
	s.Logger.Info("creating iOS story", "user_id", user.ID, "context", input.Context, "asset_count", len(input.Assets), "analysis_count", len(input.Analyses), "visual_style", normalizeIOSStoryEditStyle(input.Style), "subtitles_enabled", boolPointerLogValue(input.SubtitlesEnabled), "caption_style_preset", normalizeIOSSubtitleStyle(input.SubtitleStyle), "scene_detail_style", normalizeIOSSceneDetailStyle(input.SceneDetailStyle), "duration_mode", durationMode, "target_duration_seconds", input.TargetDuration, "has_target_duration", input.TargetDuration > 0, "remove_filler_and_pauses", input.RemoveFiller, "prompt_present", instruction != "", "prompt_length", len(instruction), "idempotency_key_present", strings.TrimSpace(request.Header.Get("Idempotency-Key")) != "")
	storyConfig := map[string]string{"pipeline_version": orchestration.PipelineVersion}
	storyConfig["duration_mode"] = string(durationMode)
	if durationMode == models.DurationModeFixed {
		if input.TargetDuration <= 0 {
			s.writeError(response, http.StatusBadRequest, "invalid_story_duration", "fixed duration requires target duration")
			return
		}
		if input.TargetDuration < 5 || input.TargetDuration > 60 {
			s.writeError(response, http.StatusBadRequest, "invalid_story_duration", "target duration must be between 5 and 60 seconds")
			return
		}
		storyConfig["target_duration_seconds"] = strconv.Itoa(int(input.TargetDuration + 0.5))
	} else if durationMode == models.DurationModeFullSource {
		if input.TargetDuration > 0 {
			s.writeError(response, http.StatusBadRequest, "invalid_story_duration", "full length must not include target duration")
			return
		}
		storyConfig["remove_filler_and_pauses"] = strconv.FormatBool(input.RemoveFiller)
	} else if input.RemoveFiller {
		s.writeError(response, http.StatusBadRequest, "invalid_story_duration", "remove filler words and pauses is only valid with full length")
		return
	}
	if style := normalizeIOSStoryEditStyle(input.Style); style != "" {
		storyConfig["visual_style"] = style
	}
	if input.SubtitlesEnabled != nil {
		storyConfig["subtitles_enabled"] = strconv.FormatBool(*input.SubtitlesEnabled)
	}
	if subtitleStyle := normalizeIOSSubtitleStyle(input.SubtitleStyle); subtitleStyle != "" {
		storyConfig["caption_style_preset"] = subtitleStyle
	}
	if preference := normalizeIOSSceneDetailStyle(input.SceneDetailStyle); preference != "" {
		storyConfig["broll_preference"] = preference
	}
	if instruction != "" {
		storyConfig["user_instructions"] = truncateIOSConfigValue(instruction, 480)
	}
	resolved, err := s.resolveIOSStoryAssets(request, user.ID, input.Assets)
	if err != nil {
		s.Logger.Warn("resolve iOS story media failed", "user_id", user.ID, "asset_count", len(input.Assets), "error", err)
		if errors.Is(err, store.ErrNotFound) || errors.Is(err, store.ErrConflict) {
			s.writeError(response, http.StatusConflict, "media_not_ready", "selected media must be uploaded before creating a story")
			return
		}
		s.internalError(response, err, "resolve iOS story media")
		return
	}
	totalBytes := totalIOSStoryUploadBytes(resolved)
	if totalBytes > iosMaxStoryUploadBytes {
		s.Logger.Warn("iOS story media too large", "user_id", user.ID, "asset_count", len(resolved), "total_bytes", totalBytes, "limit_bytes", iosMaxStoryUploadBytes)
		s.writeError(response, http.StatusRequestEntityTooLarge, "story_too_large", "selected media must be under 1 GB per story")
		return
	}
	idempotencyKey := strings.TrimSpace(request.Header.Get("Idempotency-Key"))
	if idempotencyKey == "" {
		idempotencyKey = stableIOSStoryKey(user.ID, input.Context, storyAssetIDsFromResolved(resolved))
	}
	projectID := stableIOSProjectID(user.ID, idempotencyKey)
	mediaIDs := make([]string, 0, len(resolved))
	assetIDsByMediaID := make(map[string]string, len(resolved))
	for _, item := range resolved {
		mediaIDs = append(mediaIDs, item.media.ID)
		assetIDsByMediaID[item.media.ID] = item.asset.ID
	}
	stats := credits.StoryMediaStats{
		PhotoCount:        countIOSStoryMedia(resolved, models.MediaTypePhoto, models.MediaTypeLivePhoto),
		VideoCount:        countIOSStoryMedia(resolved, models.MediaTypeVideo),
		TotalMediaCount:   len(resolved),
		TotalVideoSeconds: sumIOSStoryVideoSeconds(resolved),
		TotalBytes:        totalBytes,
	}
	s.Logger.Info("ios_story_duration_input_resolved",
		"user_id", user.ID,
		"project_id", projectID,
		"duration_mode", durationMode,
		"target_duration_seconds", input.TargetDuration,
		"has_target_duration", input.TargetDuration > 0,
		"total_uploaded_video_seconds", totalIOSStoryVideoDurationSeconds(resolved),
		"long_video_threshold_seconds", 120,
		"template_duration_mode", normalizedTemplateConfiguration.TemplateConfiguration.DurationMode,
		"template_duration_seconds", normalizedTemplateConfiguration.TemplateConfiguration.DurationSeconds,
		"automatic_long_video_candidate", durationMode == models.DurationModeAuto && input.TargetDuration <= 0 && totalIOSStoryVideoDurationSeconds(resolved) > 120,
	)
	reservation, ok := s.reserveIOSStoryCredits(response, request, user.ID, models.CreditActionCreateStory, projectID, stats, map[string]any{"context": input.Context, "asset_count": len(resolved)})
	if !ok {
		return
	}
	defer func() {
		if reservation.ID == "" {
			return
		}
		if _, err := s.Credits.Release(request.Context(), user.ID, reservation.ID); err != nil && !errors.Is(err, store.ErrConflict) && !errors.Is(err, store.ErrNotFound) {
			s.Logger.Warn("release create story reservation failed", "user_id", user.ID, "project_id", projectID, "reservation_id", reservation.ID, "error", err)
		}
	}()
	project := models.StoryProject{
		ID:                 projectID,
		UserID:             user.ID,
		Type:               models.ProjectTypeStory,
		Title:              iosStoryTitle(input.Context, resolved),
		Context:            input.Context,
		MediaIDs:           mediaIDs,
		Configuration:      normalizedTemplateConfiguration,
		CreditOperationKey: string(models.CreditActionCreateStory),
		Status:             models.ProjectStatusDraft,
		CreatedAt:          s.Now().UTC(),
		UpdatedAt:          s.Now().UTC(),
	}
	created, existingProject, err := s.Store.CreateProject(request.Context(), project, idempotencyKey)
	if err != nil {
		s.Logger.Error("create iOS story project failed", "user_id", user.ID, "asset_count", len(resolved), "total_bytes", totalBytes, "error", err)
		s.mapStoreError(response, err)
		return
	}
	s.Logger.Info("created iOS story project", "user_id", user.ID, "project_id", created.ID, "asset_count", len(resolved), "total_bytes", totalBytes, "existing", existingProject)
	if err := s.saveIOSAnalyses(request, user.ID, created.ID, mediaIDs, assetIDsByMediaID, input.Analyses); err != nil {
		s.Logger.Error("save iOS story analyses failed", "user_id", user.ID, "project_id", created.ID, "analysis_count", len(input.Analyses), "error", err)
		s.mapStoreError(response, err)
		return
	}
	if s.Orchestrator == nil {
		s.internalError(response, errors.New("job orchestrator not configured"), "job orchestrator not configured")
		return
	}
	jobs, err := s.Orchestrator.StartIOSAnalysis(request.Context(), user.ID, created.ID, idempotencyKey+":analysis", storyConfig)
	if err != nil {
		s.Logger.Error("start iOS story analysis failed", "user_id", user.ID, "project_id", created.ID, "error", err)
		s.mapStoreError(response, err)
		return
	}
	reservation.ID = ""
	s.Logger.Info("started iOS story analysis", "user_id", user.ID, "project_id", created.ID, "job_count", len(jobs))
	updated, err := s.Store.GetProject(request.Context(), user.ID, created.ID)
	if err != nil {
		s.mapStoreError(response, err)
		return
	}
	s.Logger.Info("iOS story create response ready", "user_id", user.ID, "project_id", updated.ID, "status", string(updated.Status))
	s.writeJSON(response, http.StatusCreated, s.makeIOSStory(request, user.ID, updated))
}

type resolvedIOSStoryAsset struct {
	asset iosStoryAsset
	media models.MediaAsset
}

func stableIOSProjectID(userID, idempotencyKey string) string {
	hash := sha256.Sum256([]byte(userID + "\x00" + idempotencyKey))
	return "project-" + hex.EncodeToString(hash[:16])
}

func storyAssetIDsFromResolved(assets []resolvedIOSStoryAsset) []string {
	ids := make([]string, 0, len(assets))
	for _, asset := range assets {
		ids = append(ids, asset.media.ID)
	}
	return ids
}

func countIOSStoryMedia(assets []resolvedIOSStoryAsset, mediaTypes ...models.MediaType) int {
	allowed := make(map[models.MediaType]struct{}, len(mediaTypes))
	for _, mediaType := range mediaTypes {
		allowed[mediaType] = struct{}{}
	}
	count := 0
	for _, asset := range assets {
		if _, ok := allowed[asset.media.Type]; ok {
			count++
		}
	}
	return count
}

func sumIOSStoryVideoSeconds(assets []resolvedIOSStoryAsset) int {
	var total int
	for _, asset := range assets {
		if asset.media.Type != models.MediaTypeVideo || asset.media.DurationSeconds == nil {
			continue
		}
		total += int(*asset.media.DurationSeconds + 0.5)
	}
	return total
}

func totalIOSStoryVideoDurationSeconds(assets []resolvedIOSStoryAsset) float64 {
	var total float64
	for _, asset := range assets {
		if asset.media.Type != models.MediaTypeVideo || asset.media.DurationSeconds == nil || *asset.media.DurationSeconds <= 0 {
			continue
		}
		total += *asset.media.DurationSeconds
	}
	return total
}

func (s *Server) resolveIOSStoryAssets(request *http.Request, userID string, assets []iosStoryAsset) ([]resolvedIOSStoryAsset, error) {
	seen := make(map[string]struct{}, len(assets))
	resolved := make([]resolvedIOSStoryAsset, 0, len(assets))
	for _, asset := range assets {
		asset.ID = strings.TrimSpace(asset.ID)
		if asset.ID == "" {
			return nil, store.ErrConflict
		}
		if _, duplicate := seen[asset.ID]; duplicate {
			continue
		}
		seen[asset.ID] = struct{}{}
		media, err := s.mediaForIOSAsset(request, userID, asset.ID)
		if err != nil {
			s.Logger.Warn("iOS story asset media lookup failed", "user_id", userID, "asset_id", asset.ID, "error", err)
			return nil, err
		}
		if media.Status != models.MediaStatusReady {
			s.Logger.Warn("iOS story asset media not ready", "user_id", userID, "asset_id", asset.ID, "media_id", media.ID, "status", string(media.Status))
			return nil, store.ErrConflict
		}
		resolved = append(resolved, resolvedIOSStoryAsset{asset: asset, media: media})
	}
	if len(resolved) == 0 {
		return nil, store.ErrConflict
	}
	return resolved, nil
}

func totalIOSStoryUploadBytes(assets []resolvedIOSStoryAsset) int64 {
	var total int64
	for _, asset := range assets {
		total += asset.media.FileSizeBytes
	}
	return total
}

func (s *Server) mediaForIOSAsset(request *http.Request, userID, assetID string) (models.MediaAsset, error) {
	if media, err := s.Store.GetMedia(request.Context(), userID, assetID); err == nil {
		return media, nil
	}
	for _, key := range []string{"media-upload-original-" + assetID, "media-upload-optimized-" + assetID} {
		if session, err := s.Store.GetUploadSessionByIdempotency(request.Context(), userID, key); err == nil {
			media, mediaErr := s.Store.GetMedia(request.Context(), userID, session.MediaID)
			if mediaErr == nil && media.Status == models.MediaStatusReady {
				return media, nil
			}
		}
		media, err := s.Store.GetReadyMediaByUploadIdempotencyPrefix(request.Context(), userID, key+"-bytes-")
		if err == nil {
			return media, nil
		}
	}
	return models.MediaAsset{}, store.ErrNotFound
}

func (s *Server) saveIOSAnalyses(request *http.Request, userID, projectID string, mediaIDs []string, assetIDsByMediaID map[string]string, analyses []iosStoryAnalysis) error {
	analysisByAssetID := make(map[string]iosStoryAnalysis, len(analyses))
	for _, analysis := range analyses {
		analysis.AssetID = strings.TrimSpace(analysis.AssetID)
		analysis.Labels = sanitizeIOSStoryLabels(analysis.Labels)
		if analysis.AssetID != "" {
			analysisByAssetID[analysis.AssetID] = analysis
		}
	}
	now := s.Now().UTC()
	for _, mediaID := range mediaIDs {
		assetID := assetIDsByMediaID[mediaID]
		analysis := analysisByAssetID[assetID]
		quality := analysis.Confidence
		if quality < 0 {
			quality = 0
		}
		if quality > 1 {
			quality = 1
		}
		if quality == 0 {
			quality = 0.5
		}
		if err := s.Store.SaveAnalysis(request.Context(), userID, models.MediaAnalysis{
			ID:           models.NewID("analysis"),
			ProjectID:    projectID,
			MediaID:      mediaID,
			Labels:       append([]string(nil), analysis.Labels...),
			PeopleCount:  max(0, analysis.PeopleCount),
			QualityScore: quality,
			Signals: map[string]interface{}{
				"source":   "ios_local_analysis",
				"asset_id": sanitizeIOSStoryText(assetID),
			},
			CreatedAt: now,
			UpdatedAt: now,
		}); err != nil {
			return err
		}
	}
	return nil
}

func sanitizeIOSStoryLabels(labels []string) []string {
	cleaned := make([]string, 0, len(labels))
	for _, label := range labels {
		label = strings.TrimSpace(sanitizeIOSStoryText(label))
		if label != "" {
			cleaned = append(cleaned, label)
		}
	}
	return cleaned
}

func sanitizeIOSStoryText(value string) string {
	return strings.ReplaceAll(value, "\x00", "")
}

func (s *Server) makeIOSStory(request *http.Request, userID string, project models.StoryProject) iosStory {
	progress := s.iosStoryProgress(request, userID, project)
	story := iosStory{
		ID:             project.ID,
		Type:           project.Type,
		Title:          project.Title,
		Context:        project.Context,
		CreatedAt:      iosTimeString(project.CreatedAt),
		AssetIDs:       append([]string(nil), project.MediaIDs...),
		Status:         iosStoryStatus(project.Status),
		ProgressTitle:  progress.Title,
		ProgressDetail: progress.Detail,
		ProgressValue:  progress.Value,
	}
	mediaByID := make(map[string]models.MediaAsset, len(project.MediaIDs))
	for _, mediaID := range project.MediaIDs {
		media, err := s.Store.GetMedia(request.Context(), userID, mediaID)
		if err != nil {
			continue
		}
		mediaByID[mediaID] = media
		if media.DurationSeconds != nil {
			story.Duration += *media.DurationSeconds
		}
	}
	videoAnalysesByMediaID := s.iosVideoAnalysesByMediaID(request, userID, project.ID)
	if plan, err := s.Store.GetStoryPlan(request.Context(), userID, project.ID); err == nil {
		story.StoryPlanVersion = plan.Version
		if strings.TrimSpace(plan.TitleText) != "" {
			story.Title = plan.TitleText
		}
		if plan.TotalDurationMS > 0 {
			story.Duration = float64(plan.TotalDurationMS) / 1000
		}
		story.Description = iosStoryDescription(plan)
		story.Moments = s.iosStoryMoments(request, plan, mediaByID, videoAnalysesByMediaID)
		if len(story.Moments) > 0 {
			story.AssetIDs = storyAssetIDsFromMoments(story.Moments)
		}
	}
	story.AvailableMoments = s.iosAvailableMoments(request, project, mediaByID, story.Moments, videoAnalysesByMediaID)
	story.Title = iosDisplayStoryTitle(story.Title, story.Moments, project.Context)
	if story.Duration <= 0 {
		story.Duration = float64(max(8, len(project.MediaIDs)*4))
	}
	story.VideoURL = s.iosStoryVideoURL(request, userID, project)
	return story
}

func iosStoryDescription(plan models.StoryPlan) string {
	parts := make([]string, 0, 3)
	captionTexts := iosCaptionTextMatchers(plan.Captions)
	for _, moment := range plan.Moments {
		text := firstHumanIOSPresentationText(captionTexts, moment.Narrative, moment.Caption, moment.Reason)
		if text != "" {
			parts = append(parts, text)
		}
		if len(parts) >= 3 {
			break
		}
	}
	return joinIOSStoryDescriptionParts(parts, iosStoryDescriptionMaxLength)
}

func joinIOSStoryDescriptionParts(parts []string, maxLength int) string {
	if maxLength <= 0 {
		return ""
	}
	joined := ""
	for _, part := range parts {
		part = strings.TrimSpace(strings.TrimRight(part, ".…"))
		if part == "" {
			continue
		}
		candidate := part
		if joined != "" {
			candidate = joined + ". " + part
		}
		if len(candidate) > maxLength {
			if joined != "" {
				return joined
			}
			return truncateIOSDescriptionText(part, maxLength)
		}
		joined = candidate
	}
	return joined
}

func truncateIOSDescriptionText(value string, maxLength int) string {
	if maxLength <= 0 {
		return ""
	}
	value = strings.TrimSpace(value)
	if len(value) <= maxLength {
		return value
	}
	suffix := "…"
	if maxLength <= len(suffix) {
		return suffix[:maxLength]
	}
	limit := maxLength - len(suffix)
	truncated := make([]rune, 0, limit)
	currentLength := 0
	for _, char := range value {
		charText := string(char)
		if currentLength+len(charText) > limit {
			break
		}
		truncated = append(truncated, char)
		currentLength += len(charText)
	}
	text := strings.TrimSpace(string(truncated))
	if cut := strings.LastIndexAny(text, " .,!?:;"); cut >= limit-32 {
		text = strings.TrimSpace(text[:cut])
	}
	return strings.TrimRight(text, " .,!?:;…") + suffix
}

func (s *Server) iosStoryMoments(request *http.Request, plan models.StoryPlan, mediaByID map[string]models.MediaAsset, videoAnalysesByMediaID map[string]models.VideoAnalysis) []iosStoryMoment {
	moments := make([]iosStoryMoment, 0, len(plan.Moments))
	captionTexts := iosCaptionTextMatchers(plan.Captions)
	for index, moment := range plan.Moments {
		mediaID := storyMomentMediaID(moment)
		if mediaID == "" {
			continue
		}
		mediaType := string(moment.MediaType)
		if mediaType == "" {
			if media, ok := mediaByID[mediaID]; ok {
				mediaType = string(media.Type)
			}
		}
		momentID := strings.TrimSpace(moment.MomentID)
		if momentID == "" {
			momentID = fmt.Sprintf("%s-%d", mediaID, index)
		}
		var thumbnailURL *string
		if media, ok := mediaByID[mediaID]; ok {
			thumbnailURL = s.iosStoryThumbnailURL(request, media, iosStoryMomentVideoThumbnailKey(moment, videoAnalysesByMediaID[mediaID]))
		}
		var creationDate *time.Time
		if media, ok := mediaByID[mediaID]; ok {
			creationDate = media.CreationDate
		}
		moments = append(moments, iosStoryMoment{
			ID:            momentID,
			MomentID:      momentID,
			MediaID:       mediaID,
			SourceAssetID: sourceAssetIDForMedia(mediaByID[mediaID]),
			Type:          mediaType,
			ThumbnailURL:  thumbnailURL,
			Title:         iosMomentTitleIgnoringSubtitles(moment, mediaType, index, captionTexts),
			Description:   firstHumanIOSPresentationText(captionTexts, moment.Narrative, moment.Caption, moment.Reason),
			CreationDate:  creationDate,
			Selected:      true,
		})
	}
	return moments
}

func (s *Server) iosVideoAnalysesByMediaID(request *http.Request, userID, projectID string) map[string]models.VideoAnalysis {
	_, videoAnalyses, err := s.Store.ListProjectAnalyses(request.Context(), userID, projectID)
	if err != nil {
		s.Logger.Warn("load iOS story video analyses for thumbnails failed", "user_id", userID, "project_id", projectID, "error", err)
		return nil
	}
	analysesByMediaID := make(map[string]models.VideoAnalysis, len(videoAnalyses))
	for _, analysis := range videoAnalyses {
		mediaID := strings.TrimSpace(analysis.VideoID)
		if mediaID != "" {
			analysesByMediaID[mediaID] = analysis
		}
	}
	return analysesByMediaID
}

func (s *Server) iosStoryThumbnailURL(request *http.Request, media models.MediaAsset, videoThumbnailKeys ...string) *string {
	if s.ObjectStorage == nil {
		return nil
	}
	thumbnailKey := ""
	switch media.Type {
	case models.MediaTypePhoto, models.MediaTypeLivePhoto:
		thumbnailKey = strings.TrimSpace(media.S3Key)
	case models.MediaTypeVideo:
		for _, key := range videoThumbnailKeys {
			if trimmed := strings.TrimSpace(key); trimmed != "" {
				thumbnailKey = trimmed
				break
			}
		}
	}
	if thumbnailKey == "" {
		return nil
	}
	url, err := s.ObjectStorage.PresignedDownloadURL(request.Context(), thumbnailKey, iosStoryVideoURLExpiry)
	if err != nil {
		s.Logger.Warn("presign iOS story thumbnail url failed", "media_id", media.ID, "s3_key", thumbnailKey, "error", err)
		return nil
	}
	return &url
}

func iosStoryMomentVideoThumbnailKey(moment models.StoryMoment, analysis models.VideoAnalysis) string {
	if strings.TrimSpace(analysis.VideoID) == "" {
		return ""
	}
	if strings.TrimSpace(moment.VideoAnalysisVersion) != "" && strings.TrimSpace(analysis.AnalysisVersion) != "" && strings.TrimSpace(moment.VideoAnalysisVersion) != strings.TrimSpace(analysis.AnalysisVersion) {
		return iosVideoAnalysisFallbackThumbnailKey(analysis)
	}
	if moment.StartMicros < moment.EndMicros {
		for _, scene := range analysis.Scenes {
			if scene.EndMicros <= moment.StartMicros || scene.StartMicros >= moment.EndMicros {
				continue
			}
			if key := iosFrameThumbnailKey(scene.RepresentativeFrames, moment.StartMicros, moment.EndMicros); key != "" {
				return key
			}
		}
	}
	return iosVideoAnalysisFallbackThumbnailKey(analysis)
}

func iosVideoAnalysisFallbackThumbnailKey(analysis models.VideoAnalysis) string {
	for _, scene := range analysis.Scenes {
		if key := iosFrameThumbnailKey(scene.RepresentativeFrames, 0, 0); key != "" {
			return key
		}
	}
	return ""
}

func iosFrameThumbnailKey(frames []models.Frame, startMicros, endMicros int64) string {
	if startMicros < endMicros {
		for _, frame := range frames {
			key := strings.TrimSpace(frame.S3Key)
			if key != "" && frame.TimestampMicros >= startMicros && frame.TimestampMicros <= endMicros {
				return key
			}
		}
	}
	for _, frame := range frames {
		if key := strings.TrimSpace(frame.S3Key); key != "" {
			return key
		}
	}
	return ""
}

func firstHumanIOSText(values ...string) string {
	return firstHumanIOSPresentationText(nil, values...)
}

func firstHumanIOSPresentationText(captionTexts []string, values ...string) string {
	for _, value := range values {
		if text := iosHumanMomentText(value); text != "" && !isIOSSubtitlePresentationText(text, captionTexts) {
			return text
		}
	}
	return ""
}

func iosCaptionTextMatchers(captions []models.StoryCaption) []string {
	texts := make([]string, 0, len(captions))
	seen := make(map[string]struct{}, len(captions))
	for _, caption := range captions {
		text := normalizeIOSSubtitleComparison(caption.Text)
		if len(text) < 12 {
			continue
		}
		if _, exists := seen[text]; exists {
			continue
		}
		seen[text] = struct{}{}
		texts = append(texts, text)
	}
	return texts
}

func isIOSSubtitlePresentationText(text string, captionTexts []string) bool {
	if len(captionTexts) == 0 {
		return false
	}
	normalized := normalizeIOSSubtitleComparison(text)
	if len(normalized) < 12 {
		return false
	}
	for _, captionText := range captionTexts {
		if normalized == captionText || strings.Contains(normalized, captionText) || strings.Contains(captionText, normalized) {
			return true
		}
	}
	return false
}

func normalizeIOSSubtitleComparison(value string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.Trim(value, " .!?\n\t\r"))), " ")
}

func (s *Server) iosAvailableMoments(request *http.Request, project models.StoryProject, mediaByID map[string]models.MediaAsset, selected []iosStoryMoment, videoAnalysesByMediaID map[string]models.VideoAnalysis) []iosStoryMoment {
	selectedMedia := make(map[string]struct{}, len(selected))
	for _, moment := range selected {
		selectedMedia[moment.MediaID] = struct{}{}
	}
	available := make([]iosStoryMoment, 0, len(project.MediaIDs))
	for index, mediaID := range project.MediaIDs {
		media, ok := mediaByID[mediaID]
		if !ok {
			continue
		}
		_, isSelected := selectedMedia[mediaID]
		available = append(available, iosStoryMoment{
			ID:            mediaID,
			MediaID:       mediaID,
			SourceAssetID: sourceAssetIDForMedia(media),
			Type:          string(media.Type),
			ThumbnailURL:  s.iosStoryThumbnailURL(request, media, iosVideoAnalysisFallbackThumbnailKey(videoAnalysesByMediaID[mediaID])),
			Title:         fallbackIOSMomentTitle(string(media.Type), index),
			CreationDate:  media.CreationDate,
			Selected:      isSelected,
		})
	}
	return available
}

func sourceAssetIDForMedia(media models.MediaAsset) string {
	return strings.TrimSpace(media.SourceAssetID)
}

func storyAssetIDsFromMoments(moments []iosStoryMoment) []string {
	assetIDs := make([]string, 0, len(moments))
	for _, moment := range moments {
		if strings.TrimSpace(moment.MediaID) != "" {
			assetIDs = append(assetIDs, moment.MediaID)
		}
	}
	return assetIDs
}

func iosMomentTitle(moment models.StoryMoment, mediaType string, index int) string {
	return iosMomentTitleIgnoringSubtitles(moment, mediaType, index, nil)
}

func iosMomentTitleIgnoringSubtitles(moment models.StoryMoment, mediaType string, index int, captionTexts []string) string {
	for _, value := range []string{moment.Caption, moment.Narrative, moment.Reason} {
		if title := iosHumanMomentTitle(value); title != "" && !isIOSSubtitlePresentationText(title, captionTexts) {
			return truncateIOSConfigValue(title, 36)
		}
	}
	return fallbackIOSMomentTitle(mediaType, index)
}

func iosHumanMomentTitle(value string) string {
	return iosHumanMomentText(value)
}

func iosHumanMomentText(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || isGenericIOSMomentText(trimmed) || isTechnicalIOSMomentText(trimmed) {
		return ""
	}
	trimmed = strings.Split(trimmed, " · cluster:")[0]
	trimmed = strings.Trim(trimmed, " .")
	if trimmed == "" || isGenericIOSMomentText(trimmed) || isTechnicalIOSMomentText(trimmed) {
		return ""
	}
	return iosPersonalMemoryText(trimmed)
}

func iosPersonalMemoryText(value string) string {
	lower := strings.ToLower(strings.TrimSpace(value))
	if lower == "" {
		return ""
	}
	if containsAnyIOSPhrase(lower, "woman", "man", "adult", "person", "people", "mother", "father", "parent") &&
		containsAnyIOSPhrase(lower, "child", "kid", "baby", "toddler", "children") {
		return iosFamilyMemorySummary(lower)
	}
	if strings.HasPrefix(lower, "a woman ") || strings.HasPrefix(lower, "the woman ") || strings.HasPrefix(lower, "a man ") || strings.HasPrefix(lower, "the man ") || strings.HasPrefix(lower, "a person ") || strings.HasPrefix(lower, "the person ") {
		return iosEverydayMemorySummary(lower)
	}
	if strings.HasPrefix(lower, "people ") || strings.HasPrefix(lower, "a group of people ") || strings.HasPrefix(lower, "three people ") || strings.HasPrefix(lower, "two people ") {
		return iosEverydayMemorySummary(lower)
	}
	return value
}

func iosFamilyMemorySummary(lower string) string {
	setting := iosMemorySetting(lower)
	action := "spending time together"
	switch {
	case strings.Contains(lower, "duck"):
		action = "watching ducks together"
	case strings.Contains(lower, "picnic"):
		action = "sharing a relaxed picnic"
	case strings.Contains(lower, "laugh") || strings.Contains(lower, "smil"):
		action = "sharing a happy moment"
	case strings.Contains(lower, "breakfast") || strings.Contains(lower, "kitchen") || strings.Contains(lower, "table"):
		action = "sharing an everyday moment"
	}
	if setting != "" {
		return "A warm family memory " + setting + ", " + action
	}
	return "A warm family memory, " + action
}

func iosEverydayMemorySummary(lower string) string {
	setting := iosMemorySetting(lower)
	action := "spending time together"
	switch {
	case strings.Contains(lower, "laugh") || strings.Contains(lower, "smil"):
		action = "sharing a happy moment"
	case strings.Contains(lower, "picnic"):
		action = "sharing a relaxed picnic"
	case strings.Contains(lower, "breakfast") || strings.Contains(lower, "kitchen") || strings.Contains(lower, "table"):
		action = "sharing an everyday moment"
	}
	if setting != "" {
		return "A personal memory " + setting + ", " + action
	}
	return "A personal memory, " + action
}

func iosMemorySetting(lower string) string {
	switch {
	case strings.Contains(lower, "lake"):
		return "by the lake"
	case strings.Contains(lower, "beach") || strings.Contains(lower, "ocean") || strings.Contains(lower, "waves"):
		return "by the water"
	case strings.Contains(lower, "park"):
		return "at the park"
	case strings.Contains(lower, "kitchen"):
		return "in the kitchen"
	case strings.Contains(lower, "table") || strings.Contains(lower, "breakfast"):
		return "around the table"
	case strings.Contains(lower, "home") || strings.Contains(lower, "living room"):
		return "at home"
	default:
		return ""
	}
}

func containsAnyIOSPhrase(value string, phrases ...string) bool {
	for _, phrase := range phrases {
		if strings.Contains(value, phrase) {
			return true
		}
	}
	return false
}

func isGenericIOSMomentText(value string) bool {
	normalized := strings.ToLower(strings.TrimSpace(value))
	normalized = strings.TrimSuffix(normalized, ".")
	if strings.HasPrefix(normalized, "cluster:") {
		return true
	}
	if strings.Contains(normalized, "external ai understanding is disabled") ||
		strings.Contains(normalized, "selected from available frame evidence") ||
		strings.Contains(normalized, "video moment selected from visual evidence") ||
		strings.Contains(normalized, "the transcript mentions") ||
		strings.Contains(normalized, "transcript mentions") {
		return true
	}
	switch normalized {
	case "selected moments", "selected moments story", "special moments", "special moments story", "your moments", "your day in moments", "a memory story",
		"opening moment", "the next memory", "a favorite part",
		"photo moment", "video moment", "live photo moment", "a meaningful photo moment",
		"first uploaded image", "updated image", "uploaded image", "image", "photo", "video",
		"strong opening video moment", "strong middle video moment", "strong ending video moment":
		return true
	default:
		return strings.HasPrefix(normalized, "photo moment ") || strings.HasPrefix(normalized, "video moment ") || strings.HasPrefix(normalized, "moment ")
	}
}

func isTechnicalIOSMomentText(value string) bool {
	normalized := strings.ToLower(strings.TrimSpace(value))
	normalized = strings.ReplaceAll(normalized, "-", "_")
	technicalTokens := []string{
		"transcript_segment_index",
		"transcript_segment_indexes",
		"transcript segment index",
		"transcript segment indexes",
		"frame_id",
		"frame_ids",
		"frame index",
		"frame indexes",
		"segment_id",
		"scene_id",
	}
	for _, token := range technicalTokens {
		if strings.Contains(normalized, token) {
			return true
		}
	}
	return false
}

func fallbackIOSMomentTitle(_ string, index int) string {
	switch index {
	case 0:
		return "Opening moment"
	case 1:
		return "The next memory"
	case 2:
		return "A favorite part"
	default:
		return fmt.Sprintf("Moment %d", index+1)
	}
}

func iosDisplayStoryTitle(title string, moments []iosStoryMoment, context string) string {
	if trimmed := strings.TrimSpace(title); trimmed != "" && !isGenericIOSMomentText(trimmed) {
		return truncateIOSStoryTitle(trimmed)
	}
	if title := iosStoryTitleFromMoments(moments); title != "" {
		return title
	}
	if trimmed := strings.TrimSpace(context); trimmed != "" && !isGenericIOSMomentText(trimmed) {
		return truncateIOSStoryTitle(trimmed)
	}
	return "My Story"
}

func truncateIOSStoryTitle(title string) string {
	trimmed := truncateIOSConfigValue(strings.TrimSpace(title), iosStoryTitleMaxLength)
	for {
		words := strings.Fields(trimmed)
		if len(words) <= 1 {
			return trimmed
		}
		lastWord := strings.ToLower(strings.Trim(words[len(words)-1], " .,!?:;…"))
		switch lastWord {
		case "a", "an", "the", "and", "or", "of", "over", "by", "to", "in", "on", "at", "for", "with":
			trimmed = strings.Join(words[:len(words)-1], " ")
		default:
			return trimmed
		}
	}
}

func iosStoryTitleFromMoments(moments []iosStoryMoment) string {
	for _, moment := range moments {
		if title := iosHumanMomentTitle(moment.Description); title != "" {
			return truncateIOSStoryTitle(title)
		}
	}
	for _, moment := range moments {
		if title := iosHumanMomentTitle(moment.Title); title != "" {
			return truncateIOSStoryTitle(title)
		}
	}
	return ""
}

// iosStoryVideoURL resolves the latest completed film artifact. Revisions can
// leave a project rendering or failed while an older completed render is still
// valid, so this intentionally checks render jobs instead of only project status.
func (s *Server) iosStoryVideoURL(request *http.Request, userID string, project models.StoryProject) *string {
	if project.Status == models.ProjectStatusDraft {
		return nil
	}
	if s.ObjectStorage == nil {
		s.Logger.Warn("cannot build iOS story video url without object storage", "user_id", userID, "project_id", project.ID)
		return nil
	}
	jobs, err := s.Store.ListProjectJobs(request.Context(), userID, project.ID)
	if err != nil {
		s.Logger.Warn("list jobs for iOS story video url failed", "user_id", userID, "project_id", project.ID, "error", err)
		return nil
	}
	// Latest completed render wins: a re-render supersedes earlier artifacts.
	var artifactKey string
	var completedAt time.Time
	for _, job := range jobs {
		if job.Type != models.JobTypeVideoRender || job.Status != models.JobStatusCompleted || job.ArtifactS3Key == "" {
			continue
		}
		stamp := job.UpdatedAt
		if job.CompletedAt != nil {
			stamp = *job.CompletedAt
		}
		if artifactKey == "" || stamp.After(completedAt) {
			artifactKey = job.ArtifactS3Key
			completedAt = stamp
		}
	}
	if artifactKey == "" {
		if project.Status == models.ProjectStatusReady {
			s.Logger.Warn("ready iOS story has no completed render artifact", "user_id", userID, "project_id", project.ID, "project_status", string(project.Status), "job_count", len(jobs))
		}
		return nil
	}
	url, err := s.ObjectStorage.PresignedDownloadURL(request.Context(), artifactKey, iosStoryVideoURLExpiry)
	if err != nil {
		s.Logger.Warn("presign iOS story video url failed", "user_id", userID, "project_id", project.ID, "artifact_s3_key", artifactKey, "error", err)
		return nil
	}
	return &url
}

type iosStoryProgress struct {
	Title  string
	Detail string
	Value  *float64
}

func (s *Server) iosStoryProgress(request *http.Request, userID string, project models.StoryProject) iosStoryProgress {
	if project.Status == models.ProjectStatusReady || project.Status == models.ProjectStatusFailed {
		return iosStoryFallbackProgress(project.Status)
	}
	jobs, err := s.Store.ListProjectJobs(request.Context(), userID, project.ID)
	if err != nil {
		s.Logger.Warn("list iOS story jobs for progress failed", "user_id", userID, "project_id", project.ID, "error", err)
		return iosStoryFallbackProgress(project.Status)
	}
	return iosStoryProgressFromJobs(project.Status, jobs)
}

func iosStoryProgressFromJobs(status models.ProjectStatus, jobs []models.ProcessingJob) iosStoryProgress {
	switch status {
	case models.ProjectStatusReady:
		return iosStoryProgress{Title: "Ready to watch.", Detail: "Your story is ready.", Value: iosProgressValue(1)}
	case models.ProjectStatusFailed:
		return iosStoryProgress{Title: "This story needs another try.", Detail: "Something interrupted the last render.", Value: iosProgressValue(0)}
	}

	if len(jobs) == 0 {
		return iosStoryFallbackProgress(status)
	}

	stages := []iosStoryProgressStage{
		{
			jobTypes:      []models.JobType{models.JobTypeMediaPreprocess},
			title:         "Preparing video clips...",
			queuedDetail:  "Your media is waiting for the next worker.",
			runningDetail: "Reading photos and videos before choosing moments.",
			valueStart:    0.25,
			valueSpan:     0.10,
		},
		{
			jobTypes:      []models.JobType{models.JobTypeTranscribe},
			title:         "Listening for voices...",
			queuedDetail:  "Audio transcription is queued for this story.",
			runningDetail: "Capturing spoken moments from video clips.",
			valueStart:    0.40,
			valueSpan:     0.10,
		},
		{
			jobTypes:      []models.JobType{models.JobTypeVideoUnderstand},
			title:         "Finding the best parts...",
			queuedDetail:  "Visual understanding is queued; longer videos can take a bit.",
			runningDetail: "Scanning scenes, motion, faces, and meaningful highlights.",
			valueStart:    0.55,
			valueSpan:     0.13,
		},
		{
			jobTypes:      []models.JobType{models.JobTypeStoryPlan},
			title:         "Writing story...",
			queuedDetail:  "The story planner is waiting for the analyzed moments.",
			runningDetail: "Choosing the rhythm, captions, and moment order.",
			valueStart:    0.72,
			valueSpan:     0.08,
		},
		{
			jobTypes:      []models.JobType{models.JobTypeStoryValidate},
			title:         "Checking the story flow...",
			queuedDetail:  "FrameMio is preparing the render plan.",
			runningDetail: "Making sure the planned story is ready to render.",
			valueStart:    0.82,
			valueSpan:     0.04,
		},
		{
			jobTypes:      []models.JobType{models.JobTypeVideoRender},
			title:         "Creating film...",
			queuedDetail:  "The render worker is queued; first renders can take a moment to start.",
			runningDetail: "Rendering the final video with timing, motion, and captions.",
			valueStart:    0.88,
			valueSpan:     0.10,
		},
	}
	for _, stage := range stages {
		if progress, hasRunning, _ := iosStoryProgressForStage(jobs, stage); hasRunning {
			return progress
		}
	}
	for _, stage := range stages {
		if progress, _, hasQueued := iosStoryProgressForStage(jobs, stage); hasQueued {
			return progress
		}
	}
	if status == models.ProjectStatusRendering || status == models.ProjectStatusStoryReady {
		return iosStoryProgress{Title: "Preparing the final render...", Detail: "Your moments are selected; the video worker is getting the film ready.", Value: iosProgressValue(0.84)}
	}
	return iosStoryFallbackProgress(status)
}

type iosStoryProgressStage struct {
	jobTypes      []models.JobType
	title         string
	queuedDetail  string
	runningDetail string
	valueStart    float64
	valueSpan     float64
}

func iosStoryProgressForStage(jobs []models.ProcessingJob, stage iosStoryProgressStage) (iosStoryProgress, bool, bool) {
	total := 0
	completed := 0
	running := 0
	queued := 0
	runningProgress := 0.0
	for _, job := range jobs {
		if !containsIOSStoryJobType(stage.jobTypes, job.Type) {
			continue
		}
		total++
		switch job.Status {
		case models.JobStatusCompleted:
			completed++
		case models.JobStatusRunning:
			running++
			runningProgress += clampIOSStoryJobProgress(job.Progress)
		case models.JobStatusQueued:
			queued++
		}
	}
	if total == 0 || completed == total {
		return iosStoryProgress{}, false, false
	}
	if running == 0 && completed == 0 && queued > 0 {
		return iosStoryProgress{Title: stage.title, Detail: stage.queuedDetail, Value: iosProgressValue(stage.valueStart)}, false, true
	}
	progressFraction := float64(completed)
	if running > 0 {
		progressFraction += runningProgress / float64(running)
	}
	progressFraction /= float64(total)
	detail := stage.runningDetail
	if total > 1 {
		detail = fmt.Sprintf("%s (%d/%d complete).", stage.runningDetail, completed, total)
	}
	return iosStoryProgress{Title: stage.title, Detail: detail, Value: iosProgressValue(stage.valueStart + stage.valueSpan*progressFraction)}, true, queued > 0
}

func containsIOSStoryJobType(jobTypes []models.JobType, jobType models.JobType) bool {
	for _, candidate := range jobTypes {
		if candidate == jobType {
			return true
		}
	}
	return false
}

func iosStoryFallbackProgress(status models.ProjectStatus) iosStoryProgress {
	switch status {
	case models.ProjectStatusDraft:
		return iosStoryProgress{Title: "Preparing memories...", Detail: "FrameMio is getting story ready.", Value: iosProgressValue(0.12)}
	case models.ProjectStatusAnalyzing, models.ProjectStatusStoryReady:
		return iosStoryProgress{Title: "Finding meaningful moments...", Detail: "This can take longer for video-heavy stories.", Value: iosProgressValue(0.58)}
	case models.ProjectStatusRendering:
		return iosStoryProgress{Title: "Creating film...", Detail: "Rendering the final video now.", Value: iosProgressValue(0.86)}
	case models.ProjectStatusReady:
		return iosStoryProgress{Title: "Ready to watch.", Detail: "Your story is ready.", Value: iosProgressValue(1)}
	case models.ProjectStatusFailed:
		return iosStoryProgress{Title: "This story needs another try.", Detail: "Something interrupted the last render.", Value: iosProgressValue(0)}
	default:
		return iosStoryProgress{Title: "Preparing memories...", Detail: "FrameMio is getting your story ready.", Value: iosProgressValue(0.08)}
	}
}

func iosProgressValue(value float64) *float64 {
	clamped := value
	if clamped < 0 {
		clamped = 0
	}
	if clamped > 1 {
		clamped = 1
	}
	return &clamped
}

func clampIOSStoryJobProgress(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}

func iosStoryStatus(status models.ProjectStatus) string {
	switch status {
	case models.ProjectStatusDraft:
		return "queued"
	case models.ProjectStatusAnalyzing, models.ProjectStatusStoryReady:
		return "analyzing"
	case models.ProjectStatusRendering:
		return "rendering"
	case models.ProjectStatusReady:
		return "ready"
	case models.ProjectStatusFailed:
		return "failed"
	default:
		return "queued"
	}
}

func iosStoryTitle(context string, assets []resolvedIOSStoryAsset) string {
	context = strings.TrimSpace(context)
	if context == "" {
		context = "Story"
	}
	return truncateIOSStoryTitle(fmt.Sprintf("%s story", context))
}

func stableIOSStoryKey(userID, context string, mediaIDs []string) string {
	copyIDs := append([]string(nil), mediaIDs...)
	sort.Strings(copyIDs)
	hash := sha256.Sum256([]byte(userID + "\x00" + context + "\x00" + strings.Join(copyIDs, "\x00")))
	return "ios-story:" + hex.EncodeToString(hash[:])
}
