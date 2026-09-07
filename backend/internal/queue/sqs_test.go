package queue

import (
	"context"
	"testing"

	"github.com/framemio/backend/internal/models"
)

func TestMemoryPublisherDeleteProjectMessages(t *testing.T) {
	t.Parallel()
	publisher := NewMemoryPublisher()
	messages := []JobMessage{
		{JobID: "job-1", ProjectID: "project-delete", JobType: models.JobTypeStoryPlan},
		{JobID: "job-2", ProjectID: "project-keep", JobType: models.JobTypeVideoRender},
		{JobID: "job-3", ProjectID: "project-delete", JobType: models.JobTypeVideoRender},
	}
	for _, message := range messages {
		if err := publisher.Publish(context.Background(), message); err != nil {
			t.Fatal(err)
		}
	}

	deleted, err := publisher.DeleteProjectMessages(context.Background(), "project-delete", []string{"job-1"}, 100)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 1 {
		t.Fatalf("deleted = %d, want 1", deleted)
	}
	remaining := publisher.Messages()
	if len(remaining) != 2 || remaining[0].JobID != "job-2" || remaining[1].JobID != "job-3" {
		t.Fatalf("unexpected remaining messages: %+v", remaining)
	}
}

func TestRouteForJobTypeUsesSpecificQueues(t *testing.T) {
	t.Parallel()
	queues := QueueURLs{Processing: "processing", Story: "story", Video: "video", Render: "render"}
	tests := []struct {
		jobType models.JobType
		kind    QueueKind
		url     string
	}{
		{models.JobTypeMediaPreprocess, QueueKindVideo, "video"},
		{models.JobTypeTranscribe, QueueKindVideo, "video"},
		{models.JobTypeVideoUnderstand, QueueKindVideo, "video"},
		{models.JobTypeStoryPlan, QueueKindStory, "story"},
		{models.JobTypeStoryValidate, QueueKindStory, "story"},
		{models.JobTypeVideoRender, QueueKindRender, "render"},
	}
	for _, test := range tests {
		test := test
		t.Run(string(test.jobType), func(t *testing.T) {
			t.Parallel()
			route, err := RouteForJobType(test.jobType, queues)
			if err != nil {
				t.Fatal(err)
			}
			if route.Kind != test.kind || route.URL != test.url || route.UsedFallback {
				t.Fatalf("route = %+v, want kind=%s url=%s fallback=false", route, test.kind, test.url)
			}
		})
	}
}

func TestRouteForJobTypeFallsBackToProcessingQueue(t *testing.T) {
	t.Parallel()
	route, err := RouteForJobType(models.JobTypeVideoRender, QueueURLs{Processing: "processing"})
	if err != nil {
		t.Fatal(err)
	}
	if route.Kind != QueueKindProcessing || route.URL != "processing" || !route.UsedFallback {
		t.Fatalf("route = %+v, want processing fallback", route)
	}
}

func TestRouteForJobTypeRoutesUnknownToStoryQueue(t *testing.T) {
	t.Parallel()
	route, err := RouteForJobType(models.JobType("STORY_REVISE"), QueueURLs{Story: "story", Processing: "processing"})
	if err != nil {
		t.Fatal(err)
	}
	if route.Kind != QueueKindStory || route.URL != "story" || route.UsedFallback {
		t.Fatalf("route = %+v, want story queue", route)
	}
}

func TestRouteForJobTypeSupportsAliases(t *testing.T) {
	t.Parallel()
	route, err := RouteForJobType(models.JobType("render"), QueueURLs{Render: "render"})
	if err != nil {
		t.Fatal(err)
	}
	if route.Kind != QueueKindRender || route.URL != "render" {
		t.Fatalf("route = %+v, want render queue", route)
	}
}
