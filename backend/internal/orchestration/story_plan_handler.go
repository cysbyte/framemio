package orchestration

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/framemio/backend/internal/ai"
	"github.com/framemio/backend/internal/models"
	"github.com/framemio/backend/internal/store"
)

type StoryPlanHandler struct {
	Store   store.Store
	Planner *ai.Planner
	Now     func() time.Time
}

func (h StoryPlanHandler) HandleJob(ctx context.Context, job models.ProcessingJob) error {
	if job.Type != models.JobTypeStoryPlan {
		return fmt.Errorf("story plan handler received %s job", job.Type)
	}
	if h.Store == nil {
		return fmt.Errorf("story plan handler store is not configured")
	}
	project, err := h.Store.GetProject(ctx, job.UserID, job.ProjectID)
	if err != nil {
		return err
	}
	mediaByID := make(map[string]models.MediaAsset, len(project.MediaIDs))
	for _, mediaID := range project.MediaIDs {
		media, err := h.Store.GetMedia(ctx, job.UserID, mediaID)
		if err != nil {
			return err
		}
		mediaByID[mediaID] = media
	}
	mediaAnalyses, videoAnalyses, err := h.Store.ListProjectAnalyses(ctx, job.UserID, project.ID)
	if err != nil {
		return err
	}
	photoAnalyses := h.photoAnalyses(project, mediaByID, mediaAnalyses)
	planner := h.Planner
	if planner == nil {
		planner = &ai.Planner{Now: h.Now}
	}
	plan, err := planner.Plan(ctx, ai.Request{
		ProjectID:        project.ID,
		Version:          1,
		DesiredDuration:  defaultStoryDuration(project),
		StoryType:        storyTypeFromContext(project.Context),
		VisualStyle:      models.VisualStyleCinematic,
		UserInstructions: project.Context,
		SelectedMediaIDs: append([]string(nil), project.MediaIDs...),
		PhotoAnalyses:    photoAnalyses,
		VideoAnalyses:    videoAnalyses,
		MediaClusters:    defaultMediaClusters(project),
	})
	if err != nil {
		return err
	}
	return h.Store.SaveStoryPlan(ctx, job.UserID, plan)
}

func (h StoryPlanHandler) photoAnalyses(project models.StoryProject, mediaByID map[string]models.MediaAsset, mediaAnalyses []models.MediaAnalysis) []models.PhotoAnalysis {
	analysisByMedia := make(map[string]models.MediaAnalysis, len(mediaAnalyses))
	for _, analysis := range mediaAnalyses {
		analysisByMedia[analysis.MediaID] = analysis
	}
	result := make([]models.PhotoAnalysis, 0, len(project.MediaIDs))
	for _, mediaID := range project.MediaIDs {
		media := mediaByID[mediaID]
		if media.Type == models.MediaTypeVideo {
			continue
		}
		analysis, ok := analysisByMedia[mediaID]
		if !ok {
			continue
		}
		quality := analysis.QualityScore
		if quality <= 0 {
			quality = 0.5
		}
		result = append(result, models.PhotoAnalysis{
			ID:              analysis.ID,
			PhotoID:         mediaID,
			ProjectID:       project.ID,
			AnalysisVersion: "ios-local-v1",
			TakenAt:         media.CreationDate,
			Labels:          append([]string(nil), analysis.Labels...),
			MemoryValue:     quality,
			EmotionalValue:  quality,
			VisualQuality:   quality,
			StoryRelevance:  quality,
			Selected:        true,
			CreatedAt:       analysis.CreatedAt,
		})
	}
	return result
}

func defaultStoryDuration(project models.StoryProject) time.Duration {
	seconds := len(project.MediaIDs) * 4
	if seconds < 8 {
		seconds = 8
	}
	return time.Duration(seconds) * time.Second
}

func defaultMediaClusters(project models.StoryProject) []models.MediaCluster {
	if len(project.MediaIDs) == 0 {
		return nil
	}
	return []models.MediaCluster{{
		ID:             "cluster-" + project.ID,
		ProjectID:      project.ID,
		Label:          "selected moments",
		MediaIDs:       append([]string(nil), project.MediaIDs...),
		PrimaryMediaID: project.MediaIDs[0],
		CreatedAt:      project.CreatedAt,
	}}
}

func storyTypeFromContext(value string) models.StoryType {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "today":
		return models.StoryTypeToday
	case "weekend":
		return models.StoryTypeWeekend
	case "trip", "travel":
		return models.StoryTypeTrip
	case "birthday":
		return models.StoryTypeBirthday
	case "family":
		return models.StoryTypeFamily
	case "friends":
		return models.StoryTypeFriends
	case "pet", "pets":
		return models.StoryTypePet
	case "event":
		return models.StoryTypeEvent
	default:
		return models.StoryTypeCustom
	}
}
