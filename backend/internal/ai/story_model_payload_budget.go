package ai

import (
	"encoding/json"
	"strings"

	"github.com/framemio/backend/internal/models"
)

const (
	storyModelMaxProviderRequestBytes int64 = 1_000_000
	storyModelMaxUserPromptBytes            = 900_000
)

type storyPromptBudgetProfile struct {
	analyses           int
	scenes             int
	framesPerScene     int
	videoSegments      int
	transcriptSegments int
	listItems          int
	textLength         int
	planMoments        int
	planBRoll          int
	planCaptions       int
}

var storyPromptBudgetProfiles = []storyPromptBudgetProfile{
	{analyses: 160, scenes: 60, framesPerScene: 2, videoSegments: 80, transcriptSegments: 60, listItems: 24, textLength: 2000, planMoments: 240, planBRoll: 120, planCaptions: 240},
	{analyses: 120, scenes: 40, framesPerScene: 1, videoSegments: 60, transcriptSegments: 40, listItems: 16, textLength: 1200, planMoments: 180, planBRoll: 80, planCaptions: 180},
	{analyses: 80, scenes: 24, framesPerScene: 1, videoSegments: 40, transcriptSegments: 24, listItems: 12, textLength: 800, planMoments: 120, planBRoll: 48, planCaptions: 120},
	{analyses: 50, scenes: 12, framesPerScene: 0, videoSegments: 24, transcriptSegments: 12, listItems: 8, textLength: 500, planMoments: 80, planBRoll: 32, planCaptions: 80},
	{analyses: 30, scenes: 6, framesPerScene: 0, videoSegments: 12, transcriptSegments: 6, listItems: 6, textLength: 320, planMoments: 48, planBRoll: 16, planCaptions: 48},
	{analyses: 16, scenes: 0, framesPerScene: 0, videoSegments: 6, transcriptSegments: 3, listItems: 4, textLength: 220, planMoments: 32, planBRoll: 8, planCaptions: 32},
	{analyses: 8, scenes: 0, framesPerScene: 0, videoSegments: 3, transcriptSegments: 0, listItems: 3, textLength: 160, planMoments: 16, planBRoll: 4, planCaptions: 16},
	{analyses: 4, scenes: 0, framesPerScene: 0, videoSegments: 1, transcriptSegments: 0, listItems: 2, textLength: 120, planMoments: 8, planBRoll: 0, planCaptions: 8},
}

func compactStoryPlannerInputForPrompt(input StoryPlannerInput, maxBytes int) StoryPlannerInput {
	if maxBytes <= 0 || storyPlannerPromptFits(input, maxBytes) {
		return input
	}
	for _, profile := range storyPromptBudgetProfiles {
		candidate := cloneJSON(input)
		applyStoryPlannerBudgetProfile(&candidate, profile)
		if storyPlannerPromptFits(candidate, maxBytes) {
			return candidate
		}
	}
	candidate := cloneJSON(input)
	applyStoryPlannerBudgetProfile(&candidate, storyPromptBudgetProfile{analyses: 1, videoSegments: 0, transcriptSegments: 0, listItems: 1, textLength: 80})
	return candidate
}

func compactStoryRevisionInputForPrompt(input StoryRevisionInput, maxBytes int) StoryRevisionInput {
	if maxBytes <= 0 || storyRevisionPromptFits(input, maxBytes) {
		return input
	}
	for _, profile := range storyPromptBudgetProfiles {
		candidate := cloneJSON(input)
		applyStoryRevisionBudgetProfile(&candidate, profile)
		if storyRevisionPromptFits(candidate, maxBytes) {
			return candidate
		}
	}
	candidate := cloneJSON(input)
	applyStoryRevisionBudgetProfile(&candidate, storyPromptBudgetProfile{analyses: 1, videoSegments: 0, transcriptSegments: 0, listItems: 1, textLength: 80, planMoments: 4, planBRoll: 0, planCaptions: 4})
	return candidate
}

func storyPlannerPromptFits(input StoryPlannerInput, maxBytes int) bool {
	payload, err := json.Marshal(input)
	if err != nil {
		return false
	}
	return len([]byte(storyPlannerPromptWithPayload(payload))) <= maxBytes
}

func storyRevisionPromptFits(input StoryRevisionInput, maxBytes int) bool {
	payload, err := json.Marshal(input)
	if err != nil {
		return false
	}
	return len([]byte(storyRevisionPromptWithPayload(payload))) <= maxBytes
}

func applyStoryPlannerBudgetProfile(input *StoryPlannerInput, profile storyPromptBudgetProfile) {
	input.Project.UserInstructions = compactPromptText(input.Project.UserInstructions, profile.textLength)
	input.Media.SelectedMediaIDs = compactStringList(input.Media.SelectedMediaIDs, profile.analyses, profile.textLength)
	input.Media.PreferredMediaOrder = compactStringList(input.Media.PreferredMediaOrder, profile.analyses, profile.textLength)
	input.Media.ExcludedMediaIDs = compactStringList(input.Media.ExcludedMediaIDs, profile.analyses, profile.textLength)
	input.Media.ExcludedMomentIDs = compactStringList(input.Media.ExcludedMomentIDs, profile.analyses, profile.textLength)
	input.Media.PhotoAnalyses = input.Media.PhotoAnalyses[:min(len(input.Media.PhotoAnalyses), profile.analyses)]
	for index := range input.Media.PhotoAnalyses {
		photo := &input.Media.PhotoAnalyses[index]
		photo.Location = compactPromptText(photo.Location, profile.textLength)
		photo.People = compactStringList(photo.People, profile.listItems, profile.textLength)
		photo.Labels = compactStringList(photo.Labels, profile.listItems, profile.textLength)
		photo.Caption = compactPromptText(photo.Caption, profile.textLength)
		photo.Description = compactPromptText(photo.Description, profile.textLength)
	}
	input.Media.VideoAnalyses = input.Media.VideoAnalyses[:min(len(input.Media.VideoAnalyses), profile.analyses)]
	for index := range input.Media.VideoAnalyses {
		video := &input.Media.VideoAnalyses[index]
		video.Scenes = video.Scenes[:min(len(video.Scenes), profile.scenes)]
		for sceneIndex := range video.Scenes {
			video.Scenes[sceneIndex].RepresentativeFrames = video.Scenes[sceneIndex].RepresentativeFrames[:min(len(video.Scenes[sceneIndex].RepresentativeFrames), profile.framesPerScene)]
		}
		video.Transcript.Text = compactPromptText(video.Transcript.Text, profile.textLength)
		video.Transcript.Segments = video.Transcript.Segments[:min(len(video.Transcript.Segments), profile.transcriptSegments)]
		for segmentIndex := range video.Transcript.Segments {
			video.Transcript.Segments[segmentIndex].Text = compactPromptText(video.Transcript.Segments[segmentIndex].Text, profile.textLength)
		}
		video.Segments = video.Segments[:min(len(video.Segments), profile.videoSegments)]
		for segmentIndex := range video.Segments {
			compactVideoSegment(&video.Segments[segmentIndex], profile)
		}
		video.DetectedPeople = compactStringList(video.DetectedPeople, profile.listItems, profile.textLength)
		video.DetectedActivities = compactStringList(video.DetectedActivities, profile.listItems, profile.textLength)
		video.DetectedLocations = compactStringList(video.DetectedLocations, profile.listItems, profile.textLength)
	}
	input.Media.MediaClusters = input.Media.MediaClusters[:min(len(input.Media.MediaClusters), profile.analyses)]
	for index := range input.Media.MediaClusters {
		cluster := &input.Media.MediaClusters[index]
		cluster.Label = compactPromptText(cluster.Label, profile.textLength)
		cluster.MediaIDs = compactStringList(cluster.MediaIDs, profile.listItems, profile.textLength)
		cluster.Location = compactPromptText(cluster.Location, profile.textLength)
	}
}

func applyStoryRevisionBudgetProfile(input *StoryRevisionInput, profile storyPromptBudgetProfile) {
	input.Story.Context = compactPromptText(input.Story.Context, profile.textLength)
	input.UserInstruction = compactPromptText(input.UserInstruction, profile.textLength)
	input.ProductSafetyPolicy = compactStringList(input.ProductSafetyPolicy, profile.listItems, profile.textLength)
	input.Constraints.AllowedMediaIDs = compactStringList(input.Constraints.AllowedMediaIDs, profile.analyses, profile.textLength)
	input.MediaContext.ProjectMediaIDs = compactStringList(input.MediaContext.ProjectMediaIDs, profile.analyses, profile.textLength)
	input.MediaContext.SelectedMediaIDs = compactStringList(input.MediaContext.SelectedMediaIDs, profile.analyses, profile.textLength)
	input.MediaContext.PreferredMediaOrder = compactStringList(input.MediaContext.PreferredMediaOrder, profile.analyses, profile.textLength)
	input.MediaContext.ExcludedMediaIDs = compactStringList(input.MediaContext.ExcludedMediaIDs, profile.analyses, profile.textLength)
	compactStoryPlanForPrompt(&input.CurrentStoryPlan, profile)
	input.MediaContext.PhotoAnalyses = input.MediaContext.PhotoAnalyses[:min(len(input.MediaContext.PhotoAnalyses), profile.analyses)]
	for index := range input.MediaContext.PhotoAnalyses {
		photo := &input.MediaContext.PhotoAnalyses[index]
		photo.Location = compactPromptText(photo.Location, profile.textLength)
		photo.People = compactStringList(photo.People, profile.listItems, profile.textLength)
		photo.Labels = compactStringList(photo.Labels, profile.listItems, profile.textLength)
		photo.Caption = compactPromptText(photo.Caption, profile.textLength)
		photo.Description = compactPromptText(photo.Description, profile.textLength)
	}
	input.MediaContext.VideoAnalyses = input.MediaContext.VideoAnalyses[:min(len(input.MediaContext.VideoAnalyses), profile.analyses)]
	for index := range input.MediaContext.VideoAnalyses {
		video := &input.MediaContext.VideoAnalyses[index]
		video.Provider = ""
		video.Model = ""
		video.SourceETag = ""
		video.Scenes = video.Scenes[:min(len(video.Scenes), profile.scenes)]
		for sceneIndex := range video.Scenes {
			video.Scenes[sceneIndex].RepresentativeFrames = video.Scenes[sceneIndex].RepresentativeFrames[:min(len(video.Scenes[sceneIndex].RepresentativeFrames), profile.framesPerScene)]
			for frameIndex := range video.Scenes[sceneIndex].RepresentativeFrames {
				video.Scenes[sceneIndex].RepresentativeFrames[frameIndex].S3Key = ""
			}
		}
		video.Transcript.Text = compactPromptText(video.Transcript.Text, profile.textLength)
		video.Transcript.Segments = video.Transcript.Segments[:min(len(video.Transcript.Segments), profile.transcriptSegments)]
		for segmentIndex := range video.Transcript.Segments {
			video.Transcript.Segments[segmentIndex].Text = compactPromptText(video.Transcript.Segments[segmentIndex].Text, profile.textLength)
		}
		video.Segments = video.Segments[:min(len(video.Segments), profile.videoSegments)]
		for segmentIndex := range video.Segments {
			compactVideoSegment(&video.Segments[segmentIndex], profile)
		}
		video.DetectedPeople = compactStringList(video.DetectedPeople, profile.listItems, profile.textLength)
		video.DetectedActivities = compactStringList(video.DetectedActivities, profile.listItems, profile.textLength)
		video.DetectedLocations = compactStringList(video.DetectedLocations, profile.listItems, profile.textLength)
	}
	input.MediaContext.MediaClusters = input.MediaContext.MediaClusters[:min(len(input.MediaContext.MediaClusters), profile.analyses)]
	for index := range input.MediaContext.MediaClusters {
		cluster := &input.MediaContext.MediaClusters[index]
		cluster.Label = compactPromptText(cluster.Label, profile.textLength)
		cluster.MediaIDs = compactStringList(cluster.MediaIDs, profile.listItems, profile.textLength)
		cluster.Location = compactPromptText(cluster.Location, profile.textLength)
	}
}

func compactStoryPlanForPrompt(plan *models.StoryPlan, profile storyPromptBudgetProfile) {
	plan.TitleText = compactPromptText(plan.TitleText, profile.textLength)
	plan.EndingText = compactPromptText(plan.EndingText, profile.textLength)
	plan.UserInstructions = compactPromptText(plan.UserInstructions, profile.textLength)
	plan.Moments = plan.Moments[:min(len(plan.Moments), profile.planMoments)]
	for index := range plan.Moments {
		moment := &plan.Moments[index]
		moment.Narrative = compactPromptText(moment.Narrative, profile.textLength)
		moment.Caption = compactPromptText(moment.Caption, profile.textLength)
		moment.Reason = compactPromptText(moment.Reason, profile.textLength)
		if moment.Typography != nil {
			moment.Typography.Title = compactPromptText(moment.Typography.Title, profile.textLength)
			moment.Typography.Subtitle = compactPromptText(moment.Typography.Subtitle, profile.textLength)
			moment.Typography.Reason = compactPromptText(moment.Typography.Reason, profile.textLength)
		}
	}
	plan.BRoll = plan.BRoll[:min(len(plan.BRoll), profile.planBRoll)]
	for index := range plan.BRoll {
		plan.BRoll[index].Reason = compactPromptText(plan.BRoll[index].Reason, profile.textLength)
	}
	plan.Captions = plan.Captions[:min(len(plan.Captions), profile.planCaptions)]
	for index := range plan.Captions {
		plan.Captions[index].Text = compactPromptText(plan.Captions[index].Text, profile.textLength)
	}
}

func compactVideoSegment(segment *models.VideoSegment, profile storyPromptBudgetProfile) {
	segment.Description = compactPromptText(segment.Description, profile.textLength)
	segment.Reasons = compactStringList(segment.Reasons, profile.listItems, profile.textLength)
	segment.RecommendedUsage = compactPromptText(segment.RecommendedUsage, profile.textLength)
	segment.People = compactStringList(segment.People, profile.listItems, profile.textLength)
	segment.Activities = compactStringList(segment.Activities, profile.listItems, profile.textLength)
	segment.Interactions = compactStringList(segment.Interactions, profile.listItems, profile.textLength)
	segment.Expressions = compactStringList(segment.Expressions, profile.listItems, profile.textLength)
	segment.Objects = compactStringList(segment.Objects, profile.listItems, profile.textLength)
	segment.Food = compactStringList(segment.Food, profile.listItems, profile.textLength)
	segment.Locations = compactStringList(segment.Locations, profile.listItems, profile.textLength)
	segment.Scenery = compactStringList(segment.Scenery, profile.listItems, profile.textLength)
	segment.Events = compactStringList(segment.Events, profile.listItems, profile.textLength)
	segment.InterestingActions = compactStringList(segment.InterestingActions, profile.listItems, profile.textLength)
	segment.Speech = compactStringList(segment.Speech, profile.listItems, profile.textLength)
	segment.MeaningfulAudio = compactStringList(segment.MeaningfulAudio, profile.listItems, profile.textLength)
	segment.Labels = compactStringList(segment.Labels, profile.listItems, profile.textLength)
}

func compactStringList(values []string, limit, textLength int) []string {
	if len(values) == 0 || limit <= 0 {
		return nil
	}
	values = values[:min(len(values), limit)]
	result := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := compactPromptText(value, textLength); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func compactPromptText(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 || value == "" {
		return ""
	}
	return truncateText(value, limit)
}

func cloneJSON[T any](value T) T {
	data, err := json.Marshal(value)
	if err != nil {
		return value
	}
	var cloned T
	if err := json.Unmarshal(data, &cloned); err != nil {
		return value
	}
	return cloned
}
