package api

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/framemio/backend/internal/models"
	"github.com/framemio/backend/internal/store"
)

type iosStory struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Context   string    `json:"context"`
	CreatedAt time.Time `json:"createdAt"`
	Duration  float64   `json:"duration"`
	AssetIDs  []string  `json:"assetIDs"`
	Status    string    `json:"status"`
	VideoURL  *string   `json:"videoURL,omitempty"`
}

type iosCreateStoryRequest struct {
	Context  string             `json:"context"`
	Assets   []iosStoryAsset    `json:"assets"`
	Analyses []iosStoryAnalysis `json:"analyses"`
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
	s.writeJSON(response, http.StatusOK, s.makeIOSStory(request, user.ID, project))
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
	input.Context = strings.TrimSpace(input.Context)
	if input.Context == "" || len(input.Assets) == 0 {
		s.writeError(response, http.StatusBadRequest, "invalid_story", "context and at least one asset are required")
		return
	}
	resolved, err := s.resolveIOSStoryAssets(request, user.ID, input.Assets)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) || errors.Is(err, store.ErrConflict) {
			s.writeError(response, http.StatusConflict, "media_not_ready", "selected media must be uploaded before creating a story")
			return
		}
		s.internalError(response, err, "resolve iOS story media")
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
	created, _, err := s.Store.CreateProject(request.Context(), project, idempotencyKey)
	if err != nil {
		s.mapStoreError(response, err)
		return
	}
	if err := s.saveIOSAnalyses(request, user.ID, created.ID, mediaIDs, assetIDsByMediaID, input.Analyses); err != nil {
		s.mapStoreError(response, err)
		return
	}
	if s.Orchestrator == nil {
		s.internalError(response, errors.New("job orchestrator not configured"), "job orchestrator not configured")
		return
	}
	_, err = s.Orchestrator.StartIOSAnalysis(request.Context(), user.ID, created.ID, idempotencyKey+":analysis")
	if err != nil {
		s.mapStoreError(response, err)
		return
	}
	updated, err := s.Store.GetProject(request.Context(), user.ID, created.ID)
	if err != nil {
		s.mapStoreError(response, err)
		return
	}
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
			return nil, err
		}
		if media.Status != models.MediaStatusReady {
			return nil, store.ErrConflict
		}
		resolved = append(resolved, resolvedIOSStoryAsset{asset: asset, media: media})
	}
	if len(resolved) == 0 {
		return nil, store.ErrConflict
	}
	return resolved, nil
}

func (s *Server) mediaForIOSAsset(request *http.Request, userID, assetID string) (models.MediaAsset, error) {
	if media, err := s.Store.GetMedia(request.Context(), userID, assetID); err == nil {
		return media, nil
	}
	for _, key := range []string{"media-upload-original-" + assetID, "media-upload-optimized-" + assetID} {
		session, err := s.Store.GetUploadSessionByIdempotency(request.Context(), userID, key)
		if err != nil {
			continue
		}
		return s.Store.GetMedia(request.Context(), userID, session.MediaID)
	}
	return models.MediaAsset{}, store.ErrNotFound
}

func (s *Server) saveIOSAnalyses(request *http.Request, userID, projectID string, mediaIDs []string, assetIDsByMediaID map[string]string, analyses []iosStoryAnalysis) error {
	analysisByAssetID := make(map[string]iosStoryAnalysis, len(analyses))
	for _, analysis := range analyses {
		analysis.AssetID = strings.TrimSpace(analysis.AssetID)
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
				"asset_id": assetID,
			},
			CreatedAt: now,
			UpdatedAt: now,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) makeIOSStory(request *http.Request, userID string, project models.StoryProject) iosStory {
	story := iosStory{
		ID:        project.ID,
		Title:     project.Title,
		Context:   project.Context,
		CreatedAt: project.CreatedAt,
		AssetIDs:  append([]string(nil), project.MediaIDs...),
		Status:    iosStoryStatus(project.Status),
	}
	for _, mediaID := range project.MediaIDs {
		media, err := s.Store.GetMedia(request.Context(), userID, mediaID)
		if err != nil {
			continue
		}
		if media.DurationSeconds != nil {
			story.Duration += *media.DurationSeconds
		}
	}
	if story.Duration <= 0 {
		story.Duration = float64(max(8, len(project.MediaIDs)*4))
	}
	return story
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
