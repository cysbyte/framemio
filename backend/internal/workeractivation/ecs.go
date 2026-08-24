package workeractivation

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/aws/aws-sdk-go-v2/service/ecs/types"
	"github.com/framemio/backend/internal/models"
	"github.com/framemio/backend/internal/queue"
)

type Activator interface {
	Activate(ctx context.Context, message queue.JobMessage) error
}

type ECSRunTaskActivator struct {
	client               *ecs.Client
	cluster              string
	subnets              []string
	securityGroups       []string
	assignPublicIP       types.AssignPublicIp
	videoTaskDefinition  string
	videoContainerName   string
	renderTaskDefinition string
	renderContainerName  string
	storyTaskDefinition  string
	storyContainerName   string
	logger               *slog.Logger
}

type ECSRunTaskConfig struct {
	Region               string
	Cluster              string
	Subnets              []string
	SecurityGroups       []string
	AssignPublicIP       bool
	VideoTaskDefinition  string
	VideoContainerName   string
	RenderTaskDefinition string
	RenderContainerName  string
	StoryTaskDefinition  string
	StoryContainerName   string
	Logger               *slog.Logger
}

func NewECSRunTaskActivator(ctx context.Context, config ECSRunTaskConfig) (*ECSRunTaskActivator, error) {
	if strings.TrimSpace(config.Cluster) == "" {
		return nil, fmt.Errorf("ECS cluster is required")
	}
	if len(config.Subnets) == 0 {
		return nil, fmt.Errorf("ECS worker subnets are required")
	}
	if len(config.SecurityGroups) == 0 {
		return nil, fmt.Errorf("ECS worker security groups are required")
	}
	options := make([]func(*awsconfig.LoadOptions) error, 0, 1)
	if strings.TrimSpace(config.Region) != "" {
		options = append(options, awsconfig.WithRegion(config.Region))
	}
	awsConfiguration, err := awsconfig.LoadDefaultConfig(ctx, options...)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}
	assignPublicIP := types.AssignPublicIpDisabled
	if config.AssignPublicIP {
		assignPublicIP = types.AssignPublicIpEnabled
	}
	logger := config.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &ECSRunTaskActivator{
		client:               ecs.NewFromConfig(awsConfiguration),
		cluster:              strings.TrimSpace(config.Cluster),
		subnets:              compact(config.Subnets),
		securityGroups:       compact(config.SecurityGroups),
		assignPublicIP:       assignPublicIP,
		videoTaskDefinition:  strings.TrimSpace(config.VideoTaskDefinition),
		videoContainerName:   valueOrDefault(config.VideoContainerName, "Main"),
		renderTaskDefinition: strings.TrimSpace(config.RenderTaskDefinition),
		renderContainerName:  valueOrDefault(config.RenderContainerName, "Main"),
		storyTaskDefinition:  strings.TrimSpace(config.StoryTaskDefinition),
		storyContainerName:   valueOrDefault(config.StoryContainerName, "Main"),
		logger:               logger,
	}, nil
}

func NewECSRunTaskActivatorFromEnv(ctx context.Context, logger *slog.Logger) (*ECSRunTaskActivator, bool, error) {
	if !readBool(os.Getenv("ECS_WORKER_RUN_TASK_ENABLED"), false) {
		return nil, false, nil
	}
	activator, err := NewECSRunTaskActivator(ctx, ECSRunTaskConfig{
		Region:               firstEnv("ECS_WORKER_REGION", "AWS_REGION"),
		Cluster:              firstEnv("ECS_WORKER_CLUSTER", "ECS_CLUSTER"),
		Subnets:              readList(firstEnv("ECS_WORKER_SUBNETS", "ECS_SUBNETS")),
		SecurityGroups:       readList(firstEnv("ECS_WORKER_SECURITY_GROUPS", "ECS_SECURITY_GROUPS")),
		AssignPublicIP:       readBool(firstEnv("ECS_WORKER_ASSIGN_PUBLIC_IP", "ECS_ASSIGN_PUBLIC_IP"), true),
		VideoTaskDefinition:  os.Getenv("VIDEO_WORKER_TASK_DEFINITION"),
		VideoContainerName:   os.Getenv("VIDEO_WORKER_CONTAINER_NAME"),
		RenderTaskDefinition: os.Getenv("RENDER_WORKER_TASK_DEFINITION"),
		RenderContainerName:  os.Getenv("RENDER_WORKER_CONTAINER_NAME"),
		StoryTaskDefinition:  firstEnv("STORY_WORKER_TASK_DEFINITION", "BACKEND_WORKER_TASK_DEFINITION"),
		StoryContainerName:   firstEnv("STORY_WORKER_CONTAINER_NAME", "BACKEND_WORKER_CONTAINER_NAME"),
		Logger:               logger,
	})
	if err != nil {
		return nil, true, err
	}
	return activator, true, nil
}

func (a *ECSRunTaskActivator) Activate(ctx context.Context, message queue.JobMessage) error {
	if a == nil {
		return nil
	}
	taskDefinition, containerName, reason, ok := a.workerFor(message.JobType)
	if !ok {
		return nil
	}
	if taskDefinition == "" {
		return fmt.Errorf("%s task definition is not configured", reason)
	}
	input := &ecs.RunTaskInput{
		Cluster:        aws.String(a.cluster),
		TaskDefinition: aws.String(taskDefinition),
		CapacityProviderStrategy: []types.CapacityProviderStrategyItem{{
			CapacityProvider: aws.String("FARGATE"),
			Weight:           1,
		}},
		Count: aws.Int32(1),
		NetworkConfiguration: &types.NetworkConfiguration{
			AwsvpcConfiguration: &types.AwsVpcConfiguration{
				Subnets:        a.subnets,
				SecurityGroups: a.securityGroups,
				AssignPublicIp: a.assignPublicIP,
			},
		},
		Overrides: &types.TaskOverride{
			ContainerOverrides: []types.ContainerOverride{{
				Name: aws.String(containerName),
				Environment: []types.KeyValuePair{
					{Name: aws.String("JOB_ID"), Value: aws.String(message.JobID)},
					{Name: aws.String("JOB_TYPE"), Value: aws.String(string(message.JobType))},
					{Name: aws.String("WORKER_RUN_ONCE"), Value: aws.String("true")},
				},
			}},
		},
		StartedBy: aws.String("framemio-" + reason),
		Tags: []types.Tag{
			{Key: aws.String("product"), Value: aws.String("framemio")},
			{Key: aws.String("jobId"), Value: aws.String(message.JobID)},
			{Key: aws.String("jobType"), Value: aws.String(string(message.JobType))},
			{Key: aws.String("reason"), Value: aws.String(reason)},
		},
	}
	output, err := a.client.RunTask(ctx, input)
	if err != nil {
		return fmt.Errorf("run %s task: %w", reason, err)
	}
	if len(output.Failures) > 0 {
		return fmt.Errorf("run %s task returned %d failure(s)", reason, len(output.Failures))
	}
	a.logger.Info("started one-off ECS worker task", slog.String("job_id", message.JobID), slog.String("job_type", string(message.JobType)), slog.String("reason", reason), slog.Int("tasks", len(output.Tasks)))
	return nil
}

func (a *ECSRunTaskActivator) workerFor(jobType models.JobType) (string, string, string, bool) {
	switch jobType {
	case models.JobTypeMediaPreprocess, models.JobTypeTranscribe, models.JobTypeVideoUnderstand:
		return a.videoTaskDefinition, a.videoContainerName, "video-worker", true
	case models.JobTypeStoryPlan:
		return a.storyTaskDefinition, a.storyContainerName, "story-worker", true
	case models.JobTypeVideoRender:
		return a.renderTaskDefinition, a.renderContainerName, "render-worker", true
	default:
		return "", "", "", false
	}
}

func firstEnv(names ...string) string {
	for _, name := range names {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			return value
		}
	}
	return ""
}

func readList(value string) []string {
	return compact(strings.Split(value, ","))
}

func compact(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func readBool(value string, fallback bool) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "enabled":
		return true
	case "0", "false", "no", "disabled":
		return false
	default:
		return fallback
	}
}

func valueOrDefault(value, fallback string) string {
	if trimmed := strings.TrimSpace(value); trimmed != "" {
		return trimmed
	}
	return fallback
}
