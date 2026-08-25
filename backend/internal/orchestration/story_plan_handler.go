package orchestration

import (
	"context"
	"fmt"
	"sort"
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
		MediaClusters:    mediaClusters(project, mediaByID, photoAnalyses, videoAnalyses),
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
	if len(project.MediaIDs) >= 4 && seconds < 15 {
		seconds = 15
	}
	if seconds > 30 {
		seconds = 30
	}
	return time.Duration(seconds) * time.Second
}

type mediaClusterEvidence struct {
	mediaID   string
	order     int
	timestamp *time.Time
	location  string
	labels    map[string]struct{}
}

func mediaClusters(project models.StoryProject, mediaByID map[string]models.MediaAsset, photoAnalyses []models.PhotoAnalysis, videoAnalyses []models.VideoAnalysis) []models.MediaCluster {
	if len(project.MediaIDs) == 0 {
		return nil
	}
	evidenceByMedia := make(map[string]*mediaClusterEvidence, len(project.MediaIDs))
	for index, mediaID := range project.MediaIDs {
		evidence := &mediaClusterEvidence{mediaID: mediaID, order: index, labels: make(map[string]struct{})}
		if media, ok := mediaByID[mediaID]; ok && media.CreationDate != nil {
			takenAt := media.CreationDate.UTC()
			evidence.timestamp = &takenAt
		}
		evidenceByMedia[mediaID] = evidence
	}
	for _, photo := range photoAnalyses {
		evidence := evidenceByMedia[photo.PhotoID]
		if evidence == nil {
			continue
		}
		if photo.TimestampMicros > 0 {
			takenAt := time.UnixMicro(photo.TimestampMicros).UTC()
			evidence.timestamp = &takenAt
		} else if photo.TakenAt != nil {
			takenAt := photo.TakenAt.UTC()
			evidence.timestamp = &takenAt
		}
		evidence.location = firstNonEmpty(evidence.location, photo.Location)
		addClusterLabels(evidence.labels, photo.Labels...)
		addClusterLabels(evidence.labels, photo.People...)
	}
	for _, analysis := range videoAnalyses {
		evidence := evidenceByMedia[analysis.VideoID]
		if evidence == nil {
			continue
		}
		addClusterLabels(evidence.labels, analysis.DetectedPeople...)
		addClusterLabels(evidence.labels, analysis.DetectedActivities...)
		addClusterLabels(evidence.labels, analysis.DetectedLocations...)
		if evidence.location == "" && len(analysis.DetectedLocations) > 0 {
			evidence.location = analysis.DetectedLocations[0]
		}
	}
	evidences := make([]*mediaClusterEvidence, 0, len(evidenceByMedia))
	for _, mediaID := range project.MediaIDs {
		if evidence := evidenceByMedia[mediaID]; evidence != nil {
			evidences = append(evidences, evidence)
		}
	}
	sort.SliceStable(evidences, func(left, right int) bool {
		leftTime := evidences[left].timestamp
		rightTime := evidences[right].timestamp
		if leftTime != nil && rightTime != nil && !leftTime.Equal(*rightTime) {
			return leftTime.Before(*rightTime)
		}
		if leftTime != nil && rightTime == nil {
			return true
		}
		if leftTime == nil && rightTime != nil {
			return false
		}
		return evidences[left].order < evidences[right].order
	})
	groups := make([][]*mediaClusterEvidence, 0, len(evidences))
	for _, evidence := range evidences {
		if len(groups) == 0 || !relatedEvidence(groups[len(groups)-1], evidence) {
			groups = append(groups, []*mediaClusterEvidence{evidence})
			continue
		}
		groups[len(groups)-1] = append(groups[len(groups)-1], evidence)
	}
	clusters := make([]models.MediaCluster, 0, len(groups))
	for index, group := range groups {
		mediaIDs := make([]string, 0, len(group))
		for _, evidence := range group {
			mediaIDs = append(mediaIDs, evidence.mediaID)
		}
		clusterID := fmt.Sprintf("cluster-%s-%02d", project.ID, index+1)
		if len(groups) == 1 {
			clusterID = "cluster-" + project.ID
		}
		cluster := models.MediaCluster{
			ID:             clusterID,
			ProjectID:      project.ID,
			Label:          clusterLabel(group, len(groups)),
			MediaIDs:       mediaIDs,
			PrimaryMediaID: mediaIDs[0],
			CreatedAt:      project.CreatedAt,
		}
		if group[0].timestamp != nil {
			startAt := *group[0].timestamp
			cluster.StartAt = &startAt
		}
		if group[len(group)-1].timestamp != nil {
			endAt := *group[len(group)-1].timestamp
			cluster.EndAt = &endAt
		}
		cluster.Location = sharedLocation(group)
		clusters = append(clusters, cluster)
	}
	return clusters
}

func relatedEvidence(existing []*mediaClusterEvidence, next *mediaClusterEvidence) bool {
	if len(existing) == 0 || next == nil {
		return false
	}
	previous := existing[len(existing)-1]
	if normalizedClusterValue(previous.location) != "" && normalizedClusterValue(previous.location) == normalizedClusterValue(next.location) {
		return true
	}
	if sharedClusterLabel(previous.labels, next.labels) {
		return true
	}
	if previous.timestamp == nil || next.timestamp == nil {
		return len(previous.labels) == 0 && len(next.labels) == 0 && previous.location == "" && next.location == ""
	}
	gap := next.timestamp.Sub(*previous.timestamp)
	if gap < 0 {
		gap = -gap
	}
	if len(previous.labels) == 0 && len(next.labels) == 0 && previous.location == "" && next.location == "" {
		return gap <= 6*time.Hour
	}
	return gap <= 90*time.Minute
}

func addClusterLabels(labels map[string]struct{}, values ...string) {
	for _, value := range values {
		if normalized := normalizedClusterValue(value); normalized != "" {
			labels[normalized] = struct{}{}
		}
	}
}

func sharedClusterLabel(left, right map[string]struct{}) bool {
	for label := range left {
		if _, exists := right[label]; exists {
			return true
		}
	}
	return false
}

func normalizedClusterValue(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func clusterLabel(group []*mediaClusterEvidence, groupCount int) string {
	if groupCount > 1 {
		return "memory cluster"
	}
	return "selected moments"
}

func sharedLocation(group []*mediaClusterEvidence) string {
	if len(group) == 0 {
		return ""
	}
	location := strings.TrimSpace(group[0].location)
	if location == "" {
		return ""
	}
	for _, evidence := range group[1:] {
		if normalizedClusterValue(evidence.location) != normalizedClusterValue(location) {
			return ""
		}
	}
	return location
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
