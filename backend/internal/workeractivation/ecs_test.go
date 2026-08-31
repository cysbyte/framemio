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

func TestReadBoolAcceptsEnabledValues(t *testing.T) {
	for _, value := range []string{"1", "true", "yes", "enabled"} {
		if !readBool(value, false) {
			t.Fatalf("readBool(%q) = false, want true", value)
		}
	}
}

func TestECSRunTaskActivatorAllowsAllJobTypesByDefault(t *testing.T) {
	activator := &ECSRunTaskActivator{}
	if !activator.jobTypeAllowed(models.JobTypeVideoRender) {
		t.Fatal("jobTypeAllowed(VIDEO_RENDER) = false, want true")
	}
}

func TestECSRunTaskActivatorRestrictsAllowedJobTypes(t *testing.T) {
	activator := &ECSRunTaskActivator{allowedJobTypes: allowedJobTypes([]models.JobType{models.JobTypeStoryPlan})}
	if !activator.jobTypeAllowed(models.JobTypeStoryPlan) {
		t.Fatal("jobTypeAllowed(STORY_PLAN) = false, want true")
	}
	if activator.jobTypeAllowed(models.JobTypeVideoRender) {
		t.Fatal("jobTypeAllowed(VIDEO_RENDER) = true, want false")
	}
}

func TestReadJobTypesNormalizesValues(t *testing.T) {
	jobTypes := readJobTypes(" story_plan, render,invalid ")
	if len(jobTypes) != 2 {
		t.Fatalf("len(readJobTypes) = %d, want 2", len(jobTypes))
	}
	if jobTypes[0] != models.JobTypeStoryPlan || jobTypes[1] != models.JobTypeVideoRender {
		t.Fatalf("readJobTypes returned %#v", jobTypes)
	}
}
