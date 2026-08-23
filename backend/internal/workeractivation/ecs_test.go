package workeractivation

import (
	"context"
	"testing"

	"github.com/framemio/backend/internal/models"
	"github.com/framemio/backend/internal/queue"
)

func TestNilECSRunTaskActivatorIsNoop(t *testing.T) {
	var activator *ECSRunTaskActivator
	if err := activator.Activate(context.Background(), queue.JobMessage{JobType: models.JobTypeVideoUnderstand}); err != nil {
		t.Fatalf("nil activator returned error: %v", err)
	}
}
