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

func TestWorkerOverrideEnvironmentAddsStoryTimeouts(t *testing.T) {
	environment := workerOverrideEnvironment(queue.JobMessage{JobID: "job-story", JobType: models.JobTypeStoryPlan})
	values := make(map[string]string, len(environment))
	for _, entry := range environment {
		values[*entry.Name] = *entry.Value
	}
	if values["JOB_ID"] != "job-story" || values["JOB_TYPE"] != string(models.JobTypeStoryPlan) || values["WORKER_RUN_ONCE"] != "true" {
		t.Fatalf("missing base override env: %+v", values)
	}
	if values["WORKER_RUN_ONCE_WAIT_TIMEOUT"] != "3m" || values["SQS_VISIBILITY_TIMEOUT"] != "3m" {
		t.Fatalf("missing story timeout override env: %+v", values)
	}
}

func TestWorkerOverrideEnvironmentUsesConfiguredStoryTimeouts(t *testing.T) {
	t.Setenv("STORY_WORKER_RUN_ONCE_WAIT_TIMEOUT", "4m")
	t.Setenv("STORY_WORKER_SQS_VISIBILITY_TIMEOUT", "4m")
	environment := workerOverrideEnvironment(queue.JobMessage{JobID: "job-story", JobType: models.JobTypeStoryPlan})
	values := make(map[string]string, len(environment))
	for _, entry := range environment {
		values[*entry.Name] = *entry.Value
	}
	if values["WORKER_RUN_ONCE_WAIT_TIMEOUT"] != "4m" || values["SQS_VISIBILITY_TIMEOUT"] != "4m" {
		t.Fatalf("story timeout overrides should use env values: %+v", values)
	}
}

func TestWorkerOverrideEnvironmentKeepsNonStoryOverridesMinimal(t *testing.T) {
	environment := workerOverrideEnvironment(queue.JobMessage{JobID: "job-render", JobType: models.JobTypeVideoRender})
	values := make(map[string]string, len(environment))
	for _, entry := range environment {
		values[*entry.Name] = *entry.Value
	}
	if _, ok := values["WORKER_RUN_ONCE_WAIT_TIMEOUT"]; ok {
		t.Fatalf("render override should not force story wait timeout: %+v", values)
	}
	if _, ok := values["SQS_VISIBILITY_TIMEOUT"]; ok {
		t.Fatalf("render override should not force story visibility timeout: %+v", values)
	}
}

func TestECSRunTaskActivatorRoutesWorkerServices(t *testing.T) {
	activator := &ECSRunTaskActivator{
		videoServiceName:  "video-service",
		renderServiceName: "render-service",
		storyServiceName:  "story-service",
	}

	tests := []struct {
		jobType models.JobType
		want    string
	}{
		{models.JobTypeMediaPreprocess, "video-service"},
		{models.JobTypeTranscribe, "video-service"},
		{models.JobTypeVideoUnderstand, "video-service"},
		{models.JobTypeVideoRender, "render-service"},
		{models.JobTypeStoryPlan, "story-service"},
	}

	for _, test := range tests {
		got, _, ok := activator.serviceFor(test.jobType)
		if !ok {
			t.Fatalf("serviceFor(%s) ok = false, want true", test.jobType)
		}
		if got != test.want {
			t.Fatalf("serviceFor(%s) = %q, want %q", test.jobType, got, test.want)
		}
	}
}

func TestReadIntUsesFallbackForInvalidValues(t *testing.T) {
	if got := readInt(" 3 ", 1); got != 3 {
		t.Fatalf("readInt valid = %d, want 3", got)
	}
	if got := readInt("", 1); got != 1 {
		t.Fatalf("readInt empty = %d, want fallback 1", got)
	}
	if got := readInt("not-a-number", 2); got != 2 {
		t.Fatalf("readInt invalid = %d, want fallback 2", got)
	}
}
