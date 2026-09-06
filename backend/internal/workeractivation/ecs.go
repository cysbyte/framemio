package workeractivation

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"
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
	videoServiceName     string
	renderTaskDefinition string
	renderContainerName  string
	renderServiceName    string
	storyTaskDefinition  string
	storyContainerName   string
	storyServiceName     string
	serviceScaleOutCount int32
	allowedJobTypes      map[models.JobType]struct{}
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
	VideoServiceName     string
	RenderTaskDefinition string
	RenderContainerName  string
	RenderServiceName    string
	StoryTaskDefinition  string
	StoryContainerName   string
	StoryServiceName     string
	ServiceScaleOutCount int32
	AllowedJobTypes      []models.JobType
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
	serviceScaleOutCount := config.ServiceScaleOutCount
	if serviceScaleOutCount <= 0 {
		serviceScaleOutCount = 1
	}
	return &ECSRunTaskActivator{
		client:               ecs.NewFromConfig(awsConfiguration),
		cluster:              strings.TrimSpace(config.Cluster),
		subnets:              compact(config.Subnets),
		securityGroups:       compact(config.SecurityGroups),
		assignPublicIP:       assignPublicIP,
		videoTaskDefinition:  strings.TrimSpace(config.VideoTaskDefinition),
		videoContainerName:   valueOrDefault(config.VideoContainerName, "Main"),
		videoServiceName:     strings.TrimSpace(config.VideoServiceName),
		renderTaskDefinition: strings.TrimSpace(config.RenderTaskDefinition),
		renderContainerName:  valueOrDefault(config.RenderContainerName, "Main"),
		renderServiceName:    strings.TrimSpace(config.RenderServiceName),
		storyTaskDefinition:  strings.TrimSpace(config.StoryTaskDefinition),
		storyContainerName:   valueOrDefault(config.StoryContainerName, "Main"),
		storyServiceName:     strings.TrimSpace(config.StoryServiceName),
		serviceScaleOutCount: serviceScaleOutCount,
		allowedJobTypes:      allowedJobTypes(config.AllowedJobTypes),
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
		VideoServiceName:     os.Getenv("VIDEO_WORKER_SERVICE_NAME"),
		RenderTaskDefinition: os.Getenv("RENDER_WORKER_TASK_DEFINITION"),
		RenderContainerName:  os.Getenv("RENDER_WORKER_CONTAINER_NAME"),
		RenderServiceName:    os.Getenv("RENDER_WORKER_SERVICE_NAME"),
		StoryTaskDefinition:  firstEnv("STORY_WORKER_TASK_DEFINITION", "BACKEND_WORKER_TASK_DEFINITION"),
		StoryContainerName:   firstEnv("STORY_WORKER_CONTAINER_NAME", "BACKEND_WORKER_CONTAINER_NAME"),
		StoryServiceName:     firstEnv("STORY_WORKER_SERVICE_NAME", "BACKEND_WORKER_SERVICE_NAME"),
		ServiceScaleOutCount: int32(readInt(os.Getenv("ECS_WORKER_SERVICE_SCALE_OUT_COUNT"), 1)),
		AllowedJobTypes:      readJobTypes(os.Getenv("ECS_WORKER_RUN_TASK_JOB_TYPES")),
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
	serviceScaleErr := a.scaleServiceForJob(ctx, message)
	taskDefinition, containerName, reason, ok := a.workerFor(message.JobType)
	if !ok {
		a.logger.Info("worker activation skipped", slog.String("job_id", message.JobID), slog.String("job_type", string(message.JobType)), slog.String("reason", "unsupported_job_type"))
		return serviceScaleErr
	}
	if !a.jobTypeAllowed(message.JobType) {
		a.logger.Info("worker run-task activation skipped", slog.String("job_id", message.JobID), slog.String("job_type", string(message.JobType)), slog.String("reason", "job_type_not_allowed"))
		return serviceScaleErr
	}
	if taskDefinition == "" {
		return fmt.Errorf("%s task definition is not configured", reason)
	}
	overrideEnv := workerOverrideEnvironment(message)
	a.logger.Info("starting one-off ECS worker task", slog.String("job_id", message.JobID), slog.String("job_type", string(message.JobType)), slog.String("reason", reason), slog.String("cluster", a.cluster), slog.String("task_definition", taskDefinition), slog.String("container", containerName), slog.Int("subnet_count", len(a.subnets)), slog.Int("security_group_count", len(a.securityGroups)), slog.String("assign_public_ip", string(a.assignPublicIP)))
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
				Name:        aws.String(containerName),
				Environment: overrideEnv,
			}},
		},
		StartedBy: aws.String("framemio-" + reason),
	}
	output, err := a.client.RunTask(ctx, input)
	if err != nil {
		a.logger.Error("one-off ECS worker task start failed", slog.String("job_id", message.JobID), slog.String("job_type", string(message.JobType)), slog.String("reason", reason), slog.Any("error", err))
		return fmt.Errorf("run %s task: %w", reason, err)
	}
	if len(output.Failures) > 0 {
		for index, failure := range output.Failures {
			a.logger.Error("one-off ECS worker task start returned failure", slog.String("job_id", message.JobID), slog.String("job_type", string(message.JobType)), slog.String("reason", reason), slog.Int("failure_index", index), slog.String("failure_arn", aws.ToString(failure.Arn)), slog.String("failure_reason", aws.ToString(failure.Reason)), slog.String("failure_detail", aws.ToString(failure.Detail)))
		}
		return fmt.Errorf("run %s task returned %d failure(s)", reason, len(output.Failures))
	}
	taskARNs := make([]string, 0, len(output.Tasks))
	for _, task := range output.Tasks {
		taskARNs = append(taskARNs, aws.ToString(task.TaskArn))
	}
	a.logger.Info("started one-off ECS worker task", slog.String("job_id", message.JobID), slog.String("job_type", string(message.JobType)), slog.String("reason", reason), slog.Int("tasks", len(output.Tasks)), slog.Any("task_arns", taskARNs))
	return serviceScaleErr
}

func workerOverrideEnvironment(message queue.JobMessage) []types.KeyValuePair {
	environment := []types.KeyValuePair{
		{Name: aws.String("JOB_ID"), Value: aws.String(message.JobID)},
		{Name: aws.String("JOB_TYPE"), Value: aws.String(string(message.JobType))},
		{Name: aws.String("WORKER_RUN_ONCE"), Value: aws.String("true")},
	}
	if message.JobType == models.JobTypeStoryPlan {
		environment = append(environment,
			types.KeyValuePair{Name: aws.String("WORKER_RUN_ONCE_WAIT_TIMEOUT"), Value: aws.String(valueOrDefault(firstEnv("STORY_WORKER_RUN_ONCE_WAIT_TIMEOUT", "WORKER_RUN_ONCE_WAIT_TIMEOUT"), "3m"))},
			types.KeyValuePair{Name: aws.String("SQS_VISIBILITY_TIMEOUT"), Value: aws.String(valueOrDefault(firstEnv("STORY_WORKER_SQS_VISIBILITY_TIMEOUT", "SQS_VISIBILITY_TIMEOUT"), "3m"))},
		)
	}
	return environment
}

func (a *ECSRunTaskActivator) scaleServiceForJob(ctx context.Context, message queue.JobMessage) error {
	serviceName, reason, ok := a.serviceFor(message.JobType)
	if !ok {
		return nil
	}
	if serviceName == "" {
		a.logger.Info("worker service scale skipped", slog.String("job_id", message.JobID), slog.String("job_type", string(message.JobType)), slog.String("reason", reason), slog.String("skip_reason", "service_not_configured"))
		return nil
	}
	desiredCount := a.serviceScaleOutCount
	if desiredCount <= 0 {
		desiredCount = 1
	}
	a.logger.Info("scaling worker service for job", slog.String("job_id", message.JobID), slog.String("job_type", string(message.JobType)), slog.String("reason", reason), slog.String("cluster", a.cluster), slog.String("service", serviceName), slog.Int("desired_count", int(desiredCount)))
	_, err := a.client.UpdateService(ctx, &ecs.UpdateServiceInput{
		Cluster:      aws.String(a.cluster),
		Service:      aws.String(serviceName),
		DesiredCount: aws.Int32(desiredCount),
	})
	if err != nil {
		a.logger.Warn("worker service scale failed", slog.String("job_id", message.JobID), slog.String("job_type", string(message.JobType)), slog.String("reason", reason), slog.String("service", serviceName), slog.Any("error", err))
		return fmt.Errorf("scale %s service: %w", reason, err)
	}
	a.logger.Info("worker service scale requested", slog.String("job_id", message.JobID), slog.String("job_type", string(message.JobType)), slog.String("reason", reason), slog.String("service", serviceName), slog.Int("desired_count", int(desiredCount)))
	return nil
}

func (a *ECSRunTaskActivator) serviceFor(jobType models.JobType) (string, string, bool) {
	switch jobType {
	case models.JobTypeMediaPreprocess, models.JobTypeTranscribe, models.JobTypeVideoUnderstand:
		return a.videoServiceName, "video-worker", true
	case models.JobTypeStoryPlan:
		return a.storyServiceName, "story-worker", true
	case models.JobTypeVideoRender:
		return a.renderServiceName, "render-worker", true
	default:
		return "", "", false
	}
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

func (a *ECSRunTaskActivator) jobTypeAllowed(jobType models.JobType) bool {
	if len(a.allowedJobTypes) == 0 {
		return true
	}
	normalized, ok := models.NormalizeJobType(jobType)
	if !ok {
		return false
	}
	_, ok = a.allowedJobTypes[normalized]
	return ok
}

func allowedJobTypes(jobTypes []models.JobType) map[models.JobType]struct{} {
	if len(jobTypes) == 0 {
		return nil
	}
	result := make(map[models.JobType]struct{}, len(jobTypes))
	for _, jobType := range jobTypes {
		normalized, ok := models.NormalizeJobType(jobType)
		if ok {
			result[normalized] = struct{}{}
		}
	}
	return result
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

func readJobTypes(value string) []models.JobType {
	values := compact(strings.Split(value, ","))
	result := make([]models.JobType, 0, len(values))
	for _, value := range values {
		if jobType, ok := models.NormalizeJobType(models.JobType(value)); ok {
			result = append(result, jobType)
		}
	}
	return result
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

func readInt(value string, fallback int) int {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return fallback
	}
	return parsed
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
