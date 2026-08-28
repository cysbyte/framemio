package orchestration

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strconv"
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
	Logger  *slog.Logger
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
	h.logger().Info("story planner input loaded", slog.String("job_id", job.ID), slog.String("project_id", project.ID), slog.Int("project_media_count", len(project.MediaIDs)), slog.Int("photo_analysis_count", len(photoAnalyses)), slog.Int("video_analysis_count", len(videoAnalyses)), slog.Int("video_with_speech_count", videoAnalysesWithSpeech(videoAnalyses)), slog.Int("transcript_segment_count", totalTranscriptSegments(videoAnalyses)))
	planner := h.Planner
	if planner == nil {
		planner = &ai.Planner{Now: h.Now}
	}
	request := h.storyPlanRequest(project, job.Configuration, photoAnalyses, videoAnalyses, mediaClusters(project, mediaByID, photoAnalyses, videoAnalyses))
	h.logger().Info("story planner request prepared",
		slog.String("job_id", job.ID),
		slog.String("project_id", project.ID),
		slog.Int("story_plan_version", request.Version),
		slog.Int("selected_media_count", len(request.SelectedMediaIDs)),
		slog.Int("preferred_media_order_count", len(request.PreferredMediaOrder)),
		slog.Int("excluded_media_count", len(request.ExcludedMediaIDs)),
		slog.Int("excluded_moment_count", len(request.ExcludedMomentIDs)),
		slog.String("visual_style", string(request.VisualStyle)),
		slog.String("subtitles_enabled", boolPointerLogValue(request.SubtitlesEnabled)),
		slog.String("caption_style_preset", valueOrUnset(request.CaptionStylePreset)),
	)
	plan, err := planner.Plan(ctx, request)
	if err != nil {
		h.logger().Error("story planner failed", slog.String("job_id", job.ID), slog.String("project_id", project.ID), slog.Any("error", err))
		return err
	}
	h.logger().Info("story planner output ready", slog.String("job_id", job.ID), slog.String("project_id", project.ID), slog.String("story_plan_id", plan.ID), slog.Int("story_plan_version", plan.Version), slog.Int("moment_count", len(plan.Moments)), slog.Int("caption_count", len(plan.Captions)), slog.Int("video_moment_count", videoMomentCount(plan.Moments)), slog.String("caption_style_preset", storyPlanCaptionStylePreset(plan)))
	if err := h.Store.SaveStoryPlan(ctx, job.UserID, plan); err != nil {
		h.logger().Error("save story plan failed", slog.String("job_id", job.ID), slog.String("project_id", project.ID), slog.String("story_plan_id", plan.ID), slog.Int("caption_count", len(plan.Captions)), slog.Any("error", err))
		return err
	}
	h.logger().Info("story plan saved", slog.String("job_id", job.ID), slog.String("project_id", project.ID), slog.String("story_plan_id", plan.ID), slog.Int("caption_count", len(plan.Captions)))
	return nil
}

func (h StoryPlanHandler) storyPlanRequest(project models.StoryProject, configuration map[string]string, photoAnalyses []models.PhotoAnalysis, videoAnalyses []models.VideoAnalysis, clusters []models.MediaCluster) ai.Request {
	version, _ := strconv.Atoi(strings.TrimSpace(configuration["story_plan_version"]))
	duration := defaultStoryDuration(project)
	if seconds, _ := strconv.Atoi(strings.TrimSpace(configuration["target_duration_seconds"])); seconds > 0 {
		duration = time.Duration(seconds) * time.Second
	}
	selectedMediaIDs := splitStoryPlanConfigList(configuration["selected_media_ids"])
	if len(selectedMediaIDs) == 0 {
		selectedMediaIDs = append([]string(nil), project.MediaIDs...)
	}
	instructions := strings.TrimSpace(configuration["user_instructions"])
	if instructions == "" {
		instructions = project.Context
	}
	style := models.VisualStyle(strings.TrimSpace(configuration["visual_style"]))
	if style == "" {
		style = models.VisualStyleCinematic
	}
	subtitlesEnabled := boolPointerFromConfig(configuration["subtitles_enabled"])
	return ai.Request{
		ProjectID:           project.ID,
		Version:             version,
		DesiredDuration:     duration,
		StoryType:           storyTypeFromContext(project.Context),
		VisualStyle:         style,
		UserInstructions:    instructions,
		SelectedMediaIDs:    selectedMediaIDs,
		PreferredMediaOrder: splitStoryPlanConfigList(configuration["preferred_media_order"]),
		ExcludedMediaIDs:    splitStoryPlanConfigList(configuration["excluded_media_ids"]),
		ExcludedMomentIDs:   splitStoryPlanConfigList(configuration["excluded_moment_ids"]),
		SubtitlesEnabled:    subtitlesEnabled,
		CaptionStylePreset:  strings.TrimSpace(configuration["caption_style_preset"]),
		PhotoAnalyses:       photoAnalyses,
		VideoAnalyses:       videoAnalyses,
		MediaClusters:       clusters,
	}
}

func boolPointerFromConfig(value string) *bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "1", "yes", "on":
		result := true
		return &result
	case "false", "0", "no", "off":
		result := false
		return &result
	default:
		return nil
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

func storyPlanCaptionStylePreset(plan models.StoryPlan) string {
	if plan.CaptionStyle == nil {
		return "unset"
	}
	return valueOrUnset(plan.CaptionStyle.Preset)
}

func splitStoryPlanConfigList(value string) []string {
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if _, ok := seen[part]; ok {
			continue
		}
		seen[part] = struct{}{}
		result = append(result, part)
	}
	return result
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

func (h StoryPlanHandler) logger() *slog.Logger {
	if h.Logger != nil {
		return h.Logger
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, nil))
}

func videoAnalysesWithSpeech(videoAnalyses []models.VideoAnalysis) int {
	count := 0
	for _, analysis := range videoAnalyses {
		if analysis.Transcript.HasSpeech && len(analysis.Transcript.Segments) > 0 {
			count++
		}
	}
	return count
}

func totalTranscriptSegments(videoAnalyses []models.VideoAnalysis) int {
	total := 0
	for _, analysis := range videoAnalyses {
		total += len(analysis.Transcript.Segments)
	}
	return total
}

func videoMomentCount(moments []models.StoryMoment) int {
	count := 0
	for _, moment := range moments {
		if moment.MediaType == models.MediaTypeVideo {
			count++
		}
	}
	return count
}
