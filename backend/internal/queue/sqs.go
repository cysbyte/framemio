package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/framemio/backend/internal/models"
)

type JobMessage struct {
	JobID         string            `json:"jobId"`
	ProjectID     string            `json:"projectId"`
	MediaID       string            `json:"mediaId,omitempty"`
	JobType       models.JobType    `json:"jobType"`
	Configuration map[string]string `json:"configuration,omitempty"`
}

type Publisher interface {
	Publish(ctx context.Context, message JobMessage) error
}

type ProjectMessageCleaner interface {
	DeleteProjectMessages(ctx context.Context, projectID string, jobIDs []string, maxMessages int) (int, error)
}

type ReceivedJobMessage struct {
	Message                 JobMessage `json:"message"`
	ReceiptHandle           string     `json:"-"`
	ApproximateReceiveCount int        `json:"approximate_receive_count"`
}

type Consumer interface {
	Receive(ctx context.Context, maxMessages int32, waitTime time.Duration, visibilityTimeout time.Duration) ([]ReceivedJobMessage, error)
	Delete(ctx context.Context, receiptHandle string) error
	ExtendVisibility(ctx context.Context, receiptHandle string, visibilityTimeout time.Duration) error
	MoveToDeadLetter(ctx context.Context, received ReceivedJobMessage, reason string) error
}

type SQSPublisher struct {
	client   *sqs.Client
	queueURL string
}

type QueueKind string

const (
	QueueKindProcessing QueueKind = "shared_fallback"
	QueueKindStory      QueueKind = "story"
	QueueKindVideo      QueueKind = "video"
	QueueKindRender     QueueKind = "render"
)

type QueueURLs struct {
	Processing string
	Story      string
	Video      string
	Render     string
}

type RoutingSQSPublisher struct {
	client *sqs.Client
	queues QueueURLs
	logger *slog.Logger
}

type QueueRoute struct {
	Kind         QueueKind
	URL          string
	UsedFallback bool
}

func NewSQSPublisher(ctx context.Context, queueURL string) (*SQSPublisher, error) {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}
	return &SQSPublisher{client: sqs.NewFromConfig(cfg), queueURL: queueURL}, nil
}

func NewRoutingSQSPublisher(ctx context.Context, queues QueueURLs, logger *slog.Logger) (*RoutingSQSPublisher, error) {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &RoutingSQSPublisher{client: sqs.NewFromConfig(cfg), queues: normalizeQueueURLs(queues), logger: logger}, nil
}

func (p *SQSPublisher) Publish(ctx context.Context, message JobMessage) error {
	if err := validateJobMessage(message); err != nil {
		return err
	}
	return sendJobMessage(ctx, p.client, p.queueURL, message)
}

func (p *RoutingSQSPublisher) Publish(ctx context.Context, message JobMessage) error {
	if err := validateJobMessage(message); err != nil {
		return err
	}
	route, err := RouteForJobType(message.JobType, p.queues)
	if err != nil {
		return err
	}
	p.logger.Info("publishing job message",
		slog.String("job_id", message.JobID),
		slog.String("job_type", string(message.JobType)),
		slog.String("project_id", message.ProjectID),
		slog.String("media_id", message.MediaID),
		slog.String("queue_kind", string(route.Kind)),
		slog.String("queue_name", queueName(route.URL)),
		slog.Bool("fallback_used", route.UsedFallback),
	)
	started := time.Now()
	if err := sendJobMessage(ctx, p.client, route.URL, message); err != nil {
		p.logger.Error("queue publish failed",
			slog.String("job_id", message.JobID), slog.String("job_type", string(message.JobType)),
			slog.String("project_id", message.ProjectID), slog.String("queue_kind", string(route.Kind)),
			slog.String("queue_name", queueName(route.URL)), slog.Bool("fallback_used", route.UsedFallback),
			slog.Int64("publish_duration_ms", time.Since(started).Milliseconds()), slog.Any("error", err),
		)
		return err
	}
	p.logger.Info("queue publish succeeded",
		slog.String("job_id", message.JobID), slog.String("job_type", string(message.JobType)),
		slog.String("project_id", message.ProjectID), slog.String("queue_kind", string(route.Kind)),
		slog.String("queue_name", queueName(route.URL)), slog.Bool("fallback_used", route.UsedFallback),
		slog.Int64("publish_duration_ms", time.Since(started).Milliseconds()),
	)
	return nil
}

func queueName(queueURL string) string {
	trimmed := strings.TrimRight(strings.TrimSpace(queueURL), "/")
	if index := strings.LastIndex(trimmed, "/"); index >= 0 {
		return trimmed[index+1:]
	}
	return trimmed
}

func sendJobMessage(ctx context.Context, client *sqs.Client, queueURL string, message JobMessage) error {
	body, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("encode job message: %w", err)
	}
	if len(body) > 64*1024 {
		return fmt.Errorf("job message exceeds 64 KiB safety limit")
	}
	input := &sqs.SendMessageInput{
		QueueUrl:    aws.String(queueURL),
		MessageBody: aws.String(string(body)),
	}
	if strings.HasSuffix(queueURL, ".fifo") {
		input.MessageDeduplicationId = aws.String(message.JobID)
		input.MessageGroupId = aws.String(message.ProjectID)
	}
	_, err = client.SendMessage(ctx, input)
	if err != nil {
		return fmt.Errorf("publish job message: %w", err)
	}
	return nil
}

func (p *SQSPublisher) DeleteProjectMessages(ctx context.Context, projectID string, jobIDs []string, maxMessages int) (int, error) {
	return deleteProjectMessages(ctx, p.client, p.queueURL, projectID, jobIDs, maxMessages)
}

func (p *RoutingSQSPublisher) DeleteProjectMessages(ctx context.Context, projectID string, jobIDs []string, maxMessages int) (int, error) {
	deleted := 0
	for _, queueURL := range uniqueQueueURLs(p.queues) {
		count, err := deleteProjectMessages(ctx, p.client, queueURL, projectID, jobIDs, maxMessages)
		deleted += count
		if err != nil {
			return deleted, err
		}
	}
	return deleted, nil
}

func RouteForJobType(jobType models.JobType, queues QueueURLs) (QueueRoute, error) {
	queues = normalizeQueueURLs(queues)
	kind, ok := QueueKindForJobType(jobType)
	if !ok {
		kind = QueueKindStory
	}
	if queueURL := queueURLForKind(kind, queues); queueURL != "" {
		return QueueRoute{Kind: kind, URL: queueURL}, nil
	}
	if queues.Processing != "" {
		return QueueRoute{Kind: QueueKindProcessing, URL: queues.Processing, UsedFallback: true}, nil
	}
	return QueueRoute{}, fmt.Errorf("queue URL is not configured for %s jobs", kind)
}

func QueueKindForJobType(jobType models.JobType) (QueueKind, bool) {
	normalized, ok := models.NormalizeJobType(jobType)
	if !ok {
		return "", false
	}
	switch normalized {
	case models.JobTypeMediaPreprocess, models.JobTypeTranscribe, models.JobTypeVideoUnderstand:
		return QueueKindVideo, true
	case models.JobTypeStoryPlan, models.JobTypeStoryValidate:
		return QueueKindStory, true
	case models.JobTypeVideoRender:
		return QueueKindRender, true
	default:
		return "", false
	}
}

func MissingRouteKinds(queues QueueURLs) []QueueKind {
	queues = normalizeQueueURLs(queues)
	missing := make([]QueueKind, 0, 3)
	for _, kind := range []QueueKind{QueueKindStory, QueueKindVideo, QueueKindRender} {
		if queueURLForKind(kind, queues) == "" && queues.Processing == "" {
			missing = append(missing, kind)
		}
	}
	return missing
}

func queueURLForKind(kind QueueKind, queues QueueURLs) string {
	switch kind {
	case QueueKindStory:
		return queues.Story
	case QueueKindVideo:
		return queues.Video
	case QueueKindRender:
		return queues.Render
	case QueueKindProcessing:
		return queues.Processing
	default:
		return ""
	}
}

func normalizeQueueURLs(queues QueueURLs) QueueURLs {
	return QueueURLs{
		Processing: strings.TrimSpace(queues.Processing),
		Story:      strings.TrimSpace(queues.Story),
		Video:      strings.TrimSpace(queues.Video),
		Render:     strings.TrimSpace(queues.Render),
	}
}

func uniqueQueueURLs(queues QueueURLs) []string {
	queues = normalizeQueueURLs(queues)
	seen := make(map[string]struct{}, 4)
	result := make([]string, 0, 4)
	for _, queueURL := range []string{queues.Story, queues.Video, queues.Render, queues.Processing} {
		if queueURL == "" {
			continue
		}
		if _, ok := seen[queueURL]; ok {
			continue
		}
		seen[queueURL] = struct{}{}
		result = append(result, queueURL)
	}
	return result
}

type SQSConsumer struct {
	client        *sqs.Client
	queueURL      string
	deadLetterURL string
}

func NewSQSConsumer(ctx context.Context, queueURL, deadLetterURL string) (*SQSConsumer, error) {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}
	return &SQSConsumer{client: sqs.NewFromConfig(cfg), queueURL: queueURL, deadLetterURL: deadLetterURL}, nil
}

func (c *SQSConsumer) Receive(ctx context.Context, maxMessages int32, waitTime time.Duration, visibilityTimeout time.Duration) ([]ReceivedJobMessage, error) {
	if maxMessages <= 0 || maxMessages > 10 {
		maxMessages = 10
	}
	input := &sqs.ReceiveMessageInput{
		QueueUrl:              aws.String(c.queueURL),
		MaxNumberOfMessages:   maxMessages,
		WaitTimeSeconds:       int32(waitTime / time.Second),
		VisibilityTimeout:     int32(visibilityTimeout / time.Second),
		MessageAttributeNames: []string{"All"},
		AttributeNames:        []types.QueueAttributeName{types.QueueAttributeName("ApproximateReceiveCount")},
	}
	output, err := c.client.ReceiveMessage(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("receive job messages: %w", err)
	}
	received := make([]ReceivedJobMessage, 0, len(output.Messages))
	for _, message := range output.Messages {
		var jobMessage JobMessage
		if err := json.Unmarshal([]byte(aws.ToString(message.Body)), &jobMessage); err != nil {
			continue
		}
		if err := validateJobMessage(jobMessage); err != nil {
			continue
		}
		count, _ := strconv.Atoi(message.Attributes[string(types.QueueAttributeName("ApproximateReceiveCount"))])
		received = append(received, ReceivedJobMessage{Message: jobMessage, ReceiptHandle: aws.ToString(message.ReceiptHandle), ApproximateReceiveCount: count})
	}
	return received, nil
}

func (c *SQSConsumer) Delete(ctx context.Context, receiptHandle string) error {
	if strings.TrimSpace(receiptHandle) == "" {
		return fmt.Errorf("receipt handle is required")
	}
	_, err := c.client.DeleteMessage(ctx, &sqs.DeleteMessageInput{QueueUrl: aws.String(c.queueURL), ReceiptHandle: aws.String(receiptHandle)})
	if err != nil {
		return fmt.Errorf("delete job message: %w", err)
	}
	return nil
}

func (c *SQSConsumer) DeleteProjectMessages(ctx context.Context, projectID string, jobIDs []string, maxMessages int) (int, error) {
	return deleteProjectMessages(ctx, c.client, c.queueURL, projectID, jobIDs, maxMessages)
}

func (c *SQSConsumer) ExtendVisibility(ctx context.Context, receiptHandle string, visibilityTimeout time.Duration) error {
	if strings.TrimSpace(receiptHandle) == "" {
		return fmt.Errorf("receipt handle is required")
	}
	_, err := c.client.ChangeMessageVisibility(ctx, &sqs.ChangeMessageVisibilityInput{QueueUrl: aws.String(c.queueURL), ReceiptHandle: aws.String(receiptHandle), VisibilityTimeout: int32(visibilityTimeout / time.Second)})
	if err != nil {
		return fmt.Errorf("extend job visibility: %w", err)
	}
	return nil
}

func (c *SQSConsumer) MoveToDeadLetter(ctx context.Context, received ReceivedJobMessage, reason string) error {
	if c.deadLetterURL == "" {
		return fmt.Errorf("dead-letter queue is not configured")
	}
	body, err := json.Marshal(map[string]any{"message": received.Message, "reason": reason, "receiveCount": received.ApproximateReceiveCount})
	if err != nil {
		return err
	}
	if len(body) > 64*1024 {
		return fmt.Errorf("dead-letter message exceeds 64 KiB safety limit")
	}
	_, err = c.client.SendMessage(ctx, &sqs.SendMessageInput{QueueUrl: aws.String(c.deadLetterURL), MessageBody: aws.String(string(body))})
	if err != nil {
		return fmt.Errorf("publish dead-letter message: %w", err)
	}
	return c.Delete(ctx, received.ReceiptHandle)
}

func deleteProjectMessages(ctx context.Context, client *sqs.Client, queueURL, projectID string, jobIDs []string, maxMessages int) (int, error) {
	projectID = strings.TrimSpace(projectID)
	if client == nil || strings.TrimSpace(queueURL) == "" || projectID == "" {
		return 0, fmt.Errorf("queue client, url, and project id are required")
	}
	if maxMessages <= 0 {
		maxMessages = 50
	}
	jobSet := make(map[string]struct{}, len(jobIDs))
	for _, jobID := range jobIDs {
		jobID = strings.TrimSpace(jobID)
		if jobID != "" {
			jobSet[jobID] = struct{}{}
		}
	}
	deleted := 0
	scanned := 0
	for scanned < maxMessages {
		batchSize := min(10, maxMessages-scanned)
		output, err := client.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
			QueueUrl:            aws.String(queueURL),
			MaxNumberOfMessages: int32(batchSize),
			WaitTimeSeconds:     0,
			VisibilityTimeout:   1,
		})
		if err != nil {
			return deleted, fmt.Errorf("receive project queue messages: %w", err)
		}
		if len(output.Messages) == 0 {
			return deleted, nil
		}
		scanned += len(output.Messages)
		for _, raw := range output.Messages {
			receiptHandle := aws.ToString(raw.ReceiptHandle)
			if receiptHandle == "" {
				continue
			}
			var message JobMessage
			if err := json.Unmarshal([]byte(aws.ToString(raw.Body)), &message); err != nil || !matchesProjectMessage(message, projectID, jobSet) {
				_, _ = client.ChangeMessageVisibility(ctx, &sqs.ChangeMessageVisibilityInput{QueueUrl: aws.String(queueURL), ReceiptHandle: raw.ReceiptHandle, VisibilityTimeout: 0})
				continue
			}
			if _, err := client.DeleteMessage(ctx, &sqs.DeleteMessageInput{QueueUrl: aws.String(queueURL), ReceiptHandle: raw.ReceiptHandle}); err != nil {
				return deleted, fmt.Errorf("delete project queue message: %w", err)
			}
			deleted++
		}
	}
	return deleted, nil
}

func matchesProjectMessage(message JobMessage, projectID string, jobSet map[string]struct{}) bool {
	if strings.TrimSpace(message.ProjectID) != projectID {
		return false
	}
	if len(jobSet) == 0 {
		return true
	}
	_, ok := jobSet[strings.TrimSpace(message.JobID)]
	return ok
}

type MemoryPublisher struct {
	mu       sync.Mutex
	messages []JobMessage
	seen     map[string]struct{}
}

func NewMemoryPublisher() *MemoryPublisher { return &MemoryPublisher{seen: make(map[string]struct{})} }

func (p *MemoryPublisher) Publish(_ context.Context, message JobMessage) error {
	if err := validateJobMessage(message); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.seen[message.JobID]; ok {
		return nil
	}
	p.seen[message.JobID] = struct{}{}
	p.messages = append(p.messages, message)
	return nil
}

func validateJobMessage(message JobMessage) error {
	if message.JobID == "" || message.ProjectID == "" {
		return fmt.Errorf("job and project identifiers are required")
	}
	switch message.JobType {
	case models.JobTypeMediaPreprocess, models.JobTypeTranscribe, models.JobTypeVideoUnderstand, models.JobTypeStoryPlan, models.JobTypeStoryValidate, models.JobTypeVideoRender:
	default:
		return fmt.Errorf("unsupported job type")
	}
	if len(message.Configuration) > 32 {
		return fmt.Errorf("job configuration is too large")
	}
	for key, value := range message.Configuration {
		if len(key) > 100 || len(value) > 500 {
			return fmt.Errorf("job configuration value is too large")
		}
	}
	return nil
}

func (p *MemoryPublisher) Messages() []JobMessage {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]JobMessage(nil), p.messages...)
}

func (p *MemoryPublisher) DeleteProjectMessages(_ context.Context, projectID string, jobIDs []string, _ int) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	jobSet := make(map[string]struct{}, len(jobIDs))
	for _, jobID := range jobIDs {
		jobID = strings.TrimSpace(jobID)
		if jobID != "" {
			jobSet[jobID] = struct{}{}
		}
	}
	kept := p.messages[:0]
	deleted := 0
	for _, message := range p.messages {
		if matchesProjectMessage(message, projectID, jobSet) {
			delete(p.seen, message.JobID)
			deleted++
			continue
		}
		kept = append(kept, message)
	}
	p.messages = kept
	return deleted, nil
}
