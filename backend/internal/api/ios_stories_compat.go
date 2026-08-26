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

	"github.com/framemio/backend/internal/models"
	"github.com/framemio/backend/internal/store"
)

const iosMaxStoryUploadBytes int64 = 1_000_000_000
const iosStoryVideoURLExpiry = 2 * time.Hour

type iosStory struct {
	ID               string           `json:"id"`
	Title            string           `json:"title"`
	Context          string           `json:"context"`
	CreatedAt        string           `json:"createdAt"`
	Duration         float64          `json:"duration"`
	AssetIDs         []string         `json:"assetIDs"`
	Status           string           `json:"status"`
	VideoURL         *string          `json:"videoURL,omitempty"`
	Description      string           `json:"description,omitempty"`
	Moments          []iosStoryMoment `json:"moments,omitempty"`
	AvailableMoments []iosStoryMoment `json:"availableMoments,omitempty"`
}

type iosStoryMoment struct {
	ID           string  `json:"id"`
	MomentID     string  `json:"momentId,omitempty"`
	MediaID      string  `json:"mediaId"`
	Type         string  `json:"type,omitempty"`
	ThumbnailURL *string `json:"thumbnailUrl,omitempty"`
	Title        string  `json:"title,omitempty"`
	Description  string  `json:"description,omitempty"`
	Selected     bool    `json:"selected"`
}

type iosCreateStoryRequest struct {
	Context  string             `json:"context"`
	Assets   []iosStoryAsset    `json:"assets"`
	Analyses []iosStoryAnalysis `json:"analyses"`
}

type iosStoryEditRequest struct {
	RemoveMomentIDs []string                `json:"removeMomentIds,omitempty"`
	RemoveMediaIDs  []string                `json:"removeMediaIds,omitempty"`
	AddMediaIDs     []string                `json:"addMediaIds,omitempty"`
	Replace         []iosStoryMomentReplace `json:"replace,omitempty"`
	MusicMood       string                  `json:"musicMood,omitempty"`
	Style           string                  `json:"style,omitempty"`
	TargetDuration  float64                 `json:"targetDuration,omitempty"`
	Instruction     string                  `json:"instruction,omitempty"`
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
	if !s.allowExpensiveOperation(response, user.ID, "edit_ios_story") {
		return
	}
	config, err := s.iosStoryEditConfiguration(request, user.ID, project, input)
	if err != nil {
		if errors.Is(err, store.ErrConflict) || errors.Is(err, store.ErrNotFound) {
			s.writeError(response, http.StatusBadRequest, "invalid_story_edit", "edit references media or moments that do not belong to this story")
			return
		}
		s.internalError(response, err, "prepare iOS story edit")
		return
	}
	if s.Orchestrator == nil {
		s.internalError(response, errors.New("job orchestrator not configured"), "job orchestrator not configured")
		return
	}
	job, err := s.Orchestrator.RequestStoryPlanRevision(request.Context(), user.ID, project.ID, request.Header.Get("Idempotency-Key"), config)
	if err != nil {
		s.mapStoreError(response, err)
		return
	}
	s.Logger.Info("queued iOS story revision", "user_id", user.ID, "project_id", project.ID, "job_id", job.ID)
	updated, err := s.Store.GetProject(request.Context(), user.ID, project.ID)
	if err != nil {
		s.mapStoreError(response, err)
		return
	}
	s.writeJSON(response, http.StatusAccepted, s.makeIOSStory(request, user.ID, updated))
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
	if len(selected) == 0 {
		return nil, store.ErrConflict
	}

	version := latest.Version + 1
	if version <= 1 {
		version = 1
	}
	config := map[string]string{
		"story_plan_version": strconv.Itoa(version),
		"selected_media_ids": strings.Join(sortedKeys(selected), ","),
	}
	if len(excludedMedia) > 0 {
		config["excluded_media_ids"] = strings.Join(sortedKeys(excludedMedia), ",")
	}
	if len(excludedMoments) > 0 {
		config["excluded_moment_ids"] = strings.Join(sortedKeys(excludedMoments), ",")
	}
	if input.TargetDuration > 0 {
		if input.TargetDuration < 5 || input.TargetDuration > 60 {
			return nil, store.ErrConflict
		}
		config["target_duration_seconds"] = strconv.Itoa(int(input.TargetDuration + 0.5))
	}
	if style := normalizeIOSStoryEditStyle(input.Style); style != "" {
		config["visual_style"] = style
	}
	if mood := normalizeIOSMusicMood(input.MusicMood); mood != "" {
		config["music_mood"] = mood
	}
	if instruction := strings.TrimSpace(sanitizeIOSStoryText(input.Instruction)); instruction != "" {
		config["user_instructions"] = truncateIOSConfigValue(instruction, 480)
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

func normalizeIOSMusicMood(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "warm", "cinematic", "playful", "calm", "energetic", "natural":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return ""
	}
}

func truncateIOSConfigValue(value string, maxLength int) string {
	if len(value) <= maxLength {
		return value
	}
	return strings.TrimSpace(value[:maxLength])
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
	if !s.allowExpensiveOperation(response, user.ID, "create_ios_story") {
		return
	}
	s.Logger.Info("creating iOS story", "user_id", user.ID, "context", input.Context, "asset_count", len(input.Assets), "analysis_count", len(input.Analyses), "idempotency_key_present", strings.TrimSpace(request.Header.Get("Idempotency-Key")) != "")
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
	mediaIDs := make([]string, 0, len(resolved))
	assetIDsByMediaID := make(map[string]string, len(resolved))
	for _, item := range resolved {
		mediaIDs = append(mediaIDs, item.media.ID)
		assetIDsByMediaID[item.media.ID] = item.asset.ID
	}
	now := s.Now().UTC()
	project := models.StoryProject{
		ID:        models.NewID("project"),
		UserID:    user.ID,
		Title:     iosStoryTitle(input.Context, resolved),
		Context:   input.Context,
		MediaIDs:  mediaIDs,
		Status:    models.ProjectStatusDraft,
		CreatedAt: now,
		UpdatedAt: now,
	}
	idempotencyKey := strings.TrimSpace(request.Header.Get("Idempotency-Key"))
	if idempotencyKey == "" {
		idempotencyKey = stableIOSStoryKey(user.ID, input.Context, mediaIDs)
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
	jobs, err := s.Orchestrator.StartIOSAnalysis(request.Context(), user.ID, created.ID, idempotencyKey+":analysis")
	if err != nil {
		s.Logger.Error("start iOS story analysis failed", "user_id", user.ID, "project_id", created.ID, "error", err)
		s.mapStoreError(response, err)
		return
	}
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
	story := iosStory{
		ID:        project.ID,
		Title:     project.Title,
		Context:   project.Context,
		CreatedAt: iosTimeString(project.CreatedAt),
		AssetIDs:  append([]string(nil), project.MediaIDs...),
		Status:    iosStoryStatus(project.Status),
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
	if plan, err := s.Store.GetStoryPlan(request.Context(), userID, project.ID); err == nil {
		if strings.TrimSpace(plan.TitleText) != "" {
			story.Title = plan.TitleText
		}
		if plan.TotalDurationMS > 0 {
			story.Duration = float64(plan.TotalDurationMS) / 1000
		}
		story.Description = iosStoryDescription(plan)
		story.Moments = s.iosStoryMoments(request, plan, mediaByID)
		if len(story.Moments) > 0 {
			story.AssetIDs = storyAssetIDsFromMoments(story.Moments)
		}
	}
	story.AvailableMoments = s.iosAvailableMoments(request, project, mediaByID, story.Moments)
	story.Title = iosDisplayStoryTitle(story.Title, story.Moments, project.Context)
	if story.Duration <= 0 {
		story.Duration = float64(max(8, len(project.MediaIDs)*4))
	}
	story.VideoURL = s.iosStoryVideoURL(request, userID, project)
	return story
}

func iosStoryDescription(plan models.StoryPlan) string {
	parts := make([]string, 0, 3)
	if text := iosHumanMomentText(plan.UserInstructions); text != "" {
		parts = append(parts, text)
	}
	for _, moment := range plan.Moments {
		text := firstHumanIOSText(moment.Narrative, moment.Caption, moment.Reason)
		if text != "" {
			parts = append(parts, text)
		}
		if len(parts) >= 3 {
			break
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return truncateIOSConfigValue(strings.Join(parts, ". "), 220)
}

func (s *Server) iosStoryMoments(request *http.Request, plan models.StoryPlan, mediaByID map[string]models.MediaAsset) []iosStoryMoment {
	moments := make([]iosStoryMoment, 0, len(plan.Moments))
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
			thumbnailURL = s.iosStoryThumbnailURL(request, media)
		}
		moments = append(moments, iosStoryMoment{
			ID:           momentID,
			MomentID:     momentID,
			MediaID:      mediaID,
			Type:         mediaType,
			ThumbnailURL: thumbnailURL,
			Title:        iosMomentTitle(moment, mediaType, index),
			Description:  firstHumanIOSText(moment.Narrative, moment.Caption, moment.Reason),
			Selected:     true,
		})
	}
	return moments
}

func (s *Server) iosStoryThumbnailURL(request *http.Request, media models.MediaAsset) *string {
	if s.ObjectStorage == nil || media.S3Key == "" {
		return nil
	}
	if media.Type != models.MediaTypePhoto && media.Type != models.MediaTypeLivePhoto {
		return nil
	}
	url, err := s.ObjectStorage.PresignedDownloadURL(request.Context(), media.S3Key, iosStoryVideoURLExpiry)
	if err != nil {
		s.Logger.Warn("presign iOS story thumbnail url failed", "media_id", media.ID, "s3_key", media.S3Key, "error", err)
		return nil
	}
	return &url
}

func firstHumanIOSText(values ...string) string {
	for _, value := range values {
		if text := iosHumanMomentText(value); text != "" {
			return text
		}
	}
	return ""
}

func (s *Server) iosAvailableMoments(request *http.Request, project models.StoryProject, mediaByID map[string]models.MediaAsset, selected []iosStoryMoment) []iosStoryMoment {
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
			ID:           mediaID,
			MediaID:      mediaID,
			Type:         string(media.Type),
			ThumbnailURL: s.iosStoryThumbnailURL(request, media),
			Title:        fallbackIOSMomentTitle(string(media.Type), index),
			Selected:     isSelected,
		})
	}
	return available
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
	for _, value := range []string{moment.Caption, moment.Narrative, moment.Reason} {
		if title := iosHumanMomentTitle(value); title != "" {
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
	if trimmed == "" || isGenericIOSMomentText(trimmed) {
		return ""
	}
	trimmed = strings.Split(trimmed, " · cluster:")[0]
	trimmed = strings.Trim(trimmed, " .")
	if trimmed == "" || isGenericIOSMomentText(trimmed) {
		return ""
	}
	return trimmed
}

func isGenericIOSMomentText(value string) bool {
	normalized := strings.ToLower(strings.TrimSpace(value))
	normalized = strings.TrimSuffix(normalized, ".")
	if strings.HasPrefix(normalized, "cluster:") {
		return true
	}
	if strings.Contains(normalized, "external ai understanding is disabled") ||
		strings.Contains(normalized, "selected from available frame evidence") ||
		strings.Contains(normalized, "video moment selected from visual evidence") {
		return true
	}
	switch normalized {
	case "special moments", "special moments story", "your moments", "your day in moments", "a memory story",
		"opening moment", "the next memory", "a favorite part",
		"photo moment", "video moment", "live photo moment", "a meaningful photo moment",
		"first uploaded image", "updated image", "uploaded image", "image", "photo", "video",
		"strong opening video moment", "strong middle video moment", "strong ending video moment":
		return true
	default:
		return strings.HasPrefix(normalized, "photo moment ") || strings.HasPrefix(normalized, "video moment ") || strings.HasPrefix(normalized, "moment ")
	}
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
		return truncateIOSConfigValue(trimmed, 42)
	}
	if title := iosStoryTitleFromMoments(moments); title != "" {
		return title
	}
	if trimmed := strings.TrimSpace(context); trimmed != "" && !isGenericIOSMomentText(trimmed) {
		return truncateIOSConfigValue(trimmed, 42)
	}
	return "Your Story"
}

func iosStoryTitleFromMoments(moments []iosStoryMoment) string {
	for _, moment := range moments {
		if title := iosHumanMomentTitle(moment.Description); title != "" {
			return truncateIOSConfigValue(title, 42)
		}
	}
	for _, moment := range moments {
		if title := iosHumanMomentTitle(moment.Title); title != "" {
			return truncateIOSConfigValue(title, 42)
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
		s.Logger.Warn("ready iOS story has no completed render artifact", "user_id", userID, "project_id", project.ID, "job_count", len(jobs))
		return nil
	}
	url, err := s.ObjectStorage.PresignedDownloadURL(request.Context(), artifactKey, iosStoryVideoURLExpiry)
	if err != nil {
		s.Logger.Warn("presign iOS story video url failed", "user_id", userID, "project_id", project.ID, "artifact_s3_key", artifactKey, "error", err)
		return nil
	}
	return &url
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
	if len(assets) == 1 {
		return fmt.Sprintf("%s story", context)
	}
	return fmt.Sprintf("%s story", context)
}

func stableIOSStoryKey(userID, context string, mediaIDs []string) string {
	copyIDs := append([]string(nil), mediaIDs...)
	sort.Strings(copyIDs)
	hash := sha256.Sum256([]byte(userID + "\x00" + context + "\x00" + strings.Join(copyIDs, "\x00")))
	return "ios-story:" + hex.EncodeToString(hash[:])
}
