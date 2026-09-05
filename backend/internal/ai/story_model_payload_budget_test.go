package ai

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/framemio/backend/internal/models"
)

func TestStoryPlannerPromptStaysUnderOneMegabyte(t *testing.T) {
	t.Parallel()
	prompt, err := boundedStoryPlannerUserPrompt(storyPlannerInputFromRequest(hugeStoryPlannerRequest()), storyModelMaxUserPromptBytes)
	if err != nil {
		t.Fatal(err)
	}
	if len([]byte(prompt)) > storyModelMaxUserPromptBytes {
		t.Fatalf("planner prompt = %d bytes, want <= %d", len([]byte(prompt)), storyModelMaxUserPromptBytes)
	}
}

func TestStoryRevisionPromptStaysUnderOneMegabyte(t *testing.T) {
	t.Parallel()
	prompt, err := boundedStoryRevisionUserPrompt(hugeStoryRevisionInput(), storyModelMaxUserPromptBytes)
	if err != nil {
		t.Fatal(err)
	}
	if len([]byte(prompt)) > storyModelMaxUserPromptBytes {
		t.Fatalf("revision prompt = %d bytes, want <= %d", len([]byte(prompt)), storyModelMaxUserPromptBytes)
	}
}

func TestStoryModelProviderRequestLimitRejectsOversizedBody(t *testing.T) {
	t.Parallel()
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("provider request should not be sent when local request body exceeds the byte limit")
		return nil, nil
	})}
	_, err := postTypographyJSONWithRequestLimit(context.Background(), client, "https://planner.test/v1/chat/completions", "test-key", map[string]string{"payload": strings.Repeat("x", 128)}, 64, 1024)
	if err == nil || !strings.Contains(err.Error(), "provider request exceeds") {
		t.Fatalf("expected request limit error, got %v", err)
	}
}

func hugeStoryPlannerRequest() Request {
	request := storyPlannerProviderRequest()
	largeText := strings.Repeat("beach family sunset dinner mountain speech transcript detail ", 80)
	request.UserInstructions = strings.Repeat("make it cinematic and emotional ", 300)
	request.SelectedMediaIDs = manyIDs("photo-gpt", 90)
	request.PreferredMediaOrder = manyIDs("photo-gpt", 90)
	request.PhotoAnalyses = make([]models.PhotoAnalysis, 0, 90)
	for index := 0; index < 90; index++ {
		photoID := "photo-gpt"
		if index > 0 {
			photoID = "photo-extra"
		}
		request.PhotoAnalyses = append(request.PhotoAnalyses, models.PhotoAnalysis{
			ID:              "analysis-photo",
			PhotoID:         photoID,
			ProjectID:       request.ProjectID,
			AnalysisVersion: "photo-v1",
			Location:        largeText,
			People:          repeatedStrings("person "+largeText, 80),
			Labels:          repeatedStrings("label "+largeText, 80),
			Caption:         largeText,
			Description:     largeText,
			StoryRelevance:  0.8,
		})
	}
	request.VideoAnalyses = []models.VideoAnalysis{hugeVideoAnalysis("video-gpt", request.ProjectID, largeText)}
	request.MediaClusters = []models.MediaCluster{{ID: "cluster-1", ProjectID: request.ProjectID, Label: largeText, MediaIDs: manyIDs("photo-gpt", 90), Location: largeText}}
	return request
}

func hugeStoryRevisionInput() StoryRevisionInput {
	input := revisionFixtureInput(strings.Repeat("make each moment longer and more emotional ", 300))
	largeText := strings.Repeat("beach family sunset dinner mountain speech transcript detail ", 80)
	input.Story.Context = largeText
	input.ProductSafetyPolicy = repeatedStrings(largeText, 80)
	input.CurrentStoryPlan.Moments = make([]models.StoryMoment, 0, 90)
	input.CurrentStoryPlan.Captions = make([]models.StoryCaption, 0, 90)
	for index := 0; index < 90; index++ {
		mediaID := "media-beach"
		input.CurrentStoryPlan.Moments = append(input.CurrentStoryPlan.Moments, models.StoryMoment{MomentID: "moment", AssetID: mediaID, MediaID: mediaID, MediaType: models.MediaTypePhoto, Narrative: largeText, Caption: largeText, DurationMS: 3000, CropIntent: "fit", Transition: "cut", Reason: largeText})
		input.CurrentStoryPlan.Captions = append(input.CurrentStoryPlan.Captions, models.StoryCaption{MomentID: "moment", Text: largeText, StartMS: 0, EndMS: 3000})
	}
	input.MediaContext.ProjectMediaIDs = manyIDs("media-beach", 90)
	input.MediaContext.SelectedMediaIDs = manyIDs("media-beach", 90)
	input.MediaContext.PreferredMediaOrder = manyIDs("media-beach", 90)
	input.MediaContext.PhotoAnalyses = make([]models.PhotoAnalysis, 0, 90)
	for index := 0; index < 90; index++ {
		input.MediaContext.PhotoAnalyses = append(input.MediaContext.PhotoAnalyses, models.PhotoAnalysis{PhotoID: "media-beach", ProjectID: input.Story.ID, Location: largeText, People: repeatedStrings(largeText, 80), Labels: repeatedStrings(largeText, 80), Caption: largeText, Description: largeText, StoryRelevance: 0.8})
	}
	input.MediaContext.VideoAnalyses = []models.VideoAnalysis{hugeVideoAnalysis("media-video", input.Story.ID, largeText)}
	input.MediaContext.MediaClusters = []models.MediaCluster{{ID: "cluster-1", ProjectID: input.Story.ID, Label: largeText, MediaIDs: manyIDs("media-beach", 90), Location: largeText}}
	input.Constraints.AllowedMediaIDs = manyIDs("media-beach", 90)
	return input
}

func hugeVideoAnalysis(videoID, projectID, text string) models.VideoAnalysis {
	analysis := models.VideoAnalysis{
		ID:              "analysis-video",
		VideoID:         videoID,
		ProjectID:       projectID,
		AnalysisVersion: "video-v1",
		Provider:        "provider",
		Model:           "model",
		SourceETag:      strings.Repeat("etag", 100),
		DurationMicros:  600_000_000,
		Transcript:      models.VideoTranscript{Language: "en", Text: text, HasSpeech: true, Segments: make([]models.VideoTranscriptSegment, 0, 80)},
		Segments:        make([]models.VideoSegment, 0, 80),
	}
	for index := 0; index < 50; index++ {
		analysis.Scenes = append(analysis.Scenes, models.VideoScene{SceneID: "scene", StartMicros: int64(index) * 1_000_000, EndMicros: int64(index+1) * 1_000_000, RepresentativeFrames: []models.Frame{{FrameID: "frame", TimestampMicros: int64(index) * 1_000_000, S3Key: text}}})
	}
	for index := 0; index < 80; index++ {
		analysis.Transcript.Segments = append(analysis.Transcript.Segments, models.VideoTranscriptSegment{StartMicros: int64(index) * 1_000_000, EndMicros: int64(index+1) * 1_000_000, Text: text, Confidence: 0.9})
	}
	for index := 0; index < 80; index++ {
		analysis.Segments = append(analysis.Segments, models.VideoSegment{SegmentID: "segment", SceneID: "scene", StartMicros: int64(index) * 1_000_000, EndMicros: int64(index+1) * 1_000_000, Description: text, Reasons: repeatedStrings(text, 12), RecommendedUsage: text, People: repeatedStrings(text, 12), Activities: repeatedStrings(text, 12), Objects: repeatedStrings(text, 12), Locations: repeatedStrings(text, 12), Scenery: repeatedStrings(text, 12), Events: repeatedStrings(text, 12), Speech: repeatedStrings(text, 12), Labels: repeatedStrings(text, 12), StoryRelevance: 0.8})
	}
	analysis.DetectedPeople = repeatedStrings(text, 24)
	analysis.DetectedActivities = repeatedStrings(text, 24)
	analysis.DetectedLocations = repeatedStrings(text, 24)
	return analysis
}

func manyIDs(first string, count int) []string {
	ids := make([]string, 0, count)
	for index := 0; index < count; index++ {
		if index == 0 {
			ids = append(ids, first)
		} else {
			ids = append(ids, first+"-extra")
		}
	}
	return ids
}

func repeatedStrings(value string, count int) []string {
	values := make([]string, count)
	for index := range values {
		values[index] = value
	}
	return values
}
