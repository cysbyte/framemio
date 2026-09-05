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
