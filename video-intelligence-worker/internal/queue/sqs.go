package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/framemio/video-intelligence-worker/internal/domain"
)

type Handler interface {
	Process(ctx context.Context, message domain.JobMessage) error
	MarkFailed(ctx context.Context, message domain.JobMessage, processingError error) error
}

type Consumer struct {
	client            *sqs.Client
	queueURL          string
	visibilityTimeout time.Duration
	maxAttempts       int
	logger            *slog.Logger
}

func NewConsumer(ctx context.Context, queueURL, region string, visibilityTimeout time.Duration, maxAttempts int, logger *slog.Logger) (*Consumer, error) {
	options := make([]func(*awsconfig.LoadOptions) error, 0, 1)
	if region != "" {
		options = append(options, awsconfig.WithRegion(region))
	}
	configuration, err := awsconfig.LoadDefaultConfig(ctx, options...)
	if err != nil {
		return nil, fmt.Errorf("load aws configuration: %w", err)
	}
	if visibilityTimeout < 30*time.Second {
		visibilityTimeout = 5 * time.Minute
	}
	if maxAttempts <= 0 {
		maxAttempts = 5
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Consumer{
		client: sqs.NewFromConfig(configuration), queueURL: queueURL,
		visibilityTimeout: visibilityTimeout, maxAttempts: maxAttempts, logger: logger,
	}, nil
}

func (c *Consumer) Run(ctx context.Context, handler Handler) error {
	for {
		if err := ctx.Err(); err != nil {
			return nil
		}
		result, err := c.receive(ctx, 1)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return nil
			}
			c.logger.Error("receive video analysis job", slog.Any("error", err))
			if err := sleep(ctx, time.Second); err != nil {
				return nil
			}
			continue
		}
		if len(result.Messages) > 0 {
			c.logger.Info("received video analysis messages", slog.Int("count", len(result.Messages)))
		}
		for _, message := range result.Messages {
			c.handleMessage(ctx, handler, message)
		}
	}
}

func (c *Consumer) RunOnce(ctx context.Context, handler Handler, targetJobID string, waitTimeout time.Duration) error {
	if waitTimeout <= 0 {
		waitTimeout = 2 * time.Minute
	}
	c.logger.Info("video worker run-once polling started", slog.String("target_job_id", targetJobID), slog.Duration("wait_timeout", waitTimeout))
	idleDeadline := time.Now().Add(waitTimeout)
	for {
		receiveContext := ctx
		var cancel context.CancelFunc
		if targetJobID != "" {
			remaining := time.Until(idleDeadline)
			if remaining <= 0 {
				c.logger.Info("no matching video analysis job found", slog.String("target_job_id", targetJobID))
				return nil
			}
			receiveContext, cancel = context.WithTimeout(ctx, remaining)
		}
		result, err := c.receive(receiveContext, 10)
		if cancel != nil {
			cancel()
		}
		if err != nil {
			if targetJobID != "" && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) && ctx.Err() == nil {
				c.logger.Info("no matching video analysis job found", slog.String("target_job_id", targetJobID))
				return nil
			}
			if errors.Is(err, context.Canceled) && ctx.Err() != nil {
				return nil
			}
			return err
		}
		if len(result.Messages) > 0 {
			c.logger.Info("video worker run-once received messages", slog.String("target_job_id", targetJobID), slog.Int("count", len(result.Messages)))
		}
		handledMessage := false
		for _, message := range result.Messages {
			jobMessage, ok := c.decodeMessage(ctx, message)
			if !ok {
				continue
			}
			if !isVideoAnalysisJobType(jobMessage.JobType) {
				c.logger.Info("release non-video-analysis message", slog.String("job_id", jobMessage.JobID), slog.String("job_type", jobMessage.JobType))
				c.release(ctx, message)
				continue
			}
			if targetJobID != "" && jobMessage.JobID != targetJobID {
				c.logger.Info("handling available video analysis message", slog.String("target_job_id", targetJobID), slog.String("job_id", jobMessage.JobID), slog.String("job_type", jobMessage.JobType))
			}
			c.handleMessage(ctx, handler, message)
			handledMessage = true
			if targetJobID == "" || jobMessage.JobID == targetJobID {
				c.logger.Info("video worker run-once target handled", slog.String("target_job_id", targetJobID), slog.String("job_id", jobMessage.JobID), slog.String("job_type", jobMessage.JobType))
				return nil
			}
			c.logger.Info("video worker run-once continuing after available job", slog.String("target_job_id", targetJobID), slog.String("handled_job_id", jobMessage.JobID), slog.String("handled_job_type", jobMessage.JobType))
		}
		if targetJobID != "" && handledMessage {
			idleDeadline = time.Now().Add(waitTimeout)
		}
	}
}

func (c *Consumer) receive(ctx context.Context, maxMessages int32) (*sqs.ReceiveMessageOutput, error) {
	return c.client.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
		QueueUrl: aws.String(c.queueURL), MaxNumberOfMessages: maxMessages, WaitTimeSeconds: 20,
		VisibilityTimeout:           int32(c.visibilityTimeout.Seconds()),
		MessageSystemAttributeNames: []types.MessageSystemAttributeName{types.MessageSystemAttributeNameApproximateReceiveCount},
	})
}

func (c *Consumer) handleMessage(ctx context.Context, handler Handler, received types.Message) {
	if received.Body == nil || received.ReceiptHandle == nil {
		return
	}
	message, ok := c.decodeMessage(ctx, received)
	if !ok {
		return
	}
	if !isVideoAnalysisJobType(message.JobType) {
		c.logger.Info("release non-video-analysis message", slog.String("job_id", message.JobID), slog.String("project_id", message.ProjectID), slog.String("job_type", message.JobType))
		c.release(ctx, received)
		return
	}
	receiveCount := 1
	if value := received.Attributes[string(types.MessageSystemAttributeNameApproximateReceiveCount)]; value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 {
			receiveCount = parsed
		}
	}
	c.logger.Info("handling video analysis message",
		slog.String("job_id", message.JobID), slog.String("project_id", message.ProjectID),
		slog.String("media_id", message.MediaID), slog.String("job_type", message.JobType), slog.Int("receive_count", receiveCount),
	)
	processingContext, cancel := context.WithCancel(ctx)
	heartbeatDone := make(chan struct{})
	go c.extendVisibility(processingContext, *received.ReceiptHandle, heartbeatDone)
	err := handler.Process(processingContext, message)
	cancel()
	<-heartbeatDone
	if err == nil {
		c.logger.Info("video analysis message handled", slog.String("job_id", message.JobID), slog.String("media_id", message.MediaID))
		c.delete(ctx, *received.ReceiptHandle)
		return
	}
	if errors.Is(err, context.Canceled) && ctx.Err() != nil {
		return
	}
	c.logger.Error("video analysis job failed",
		slog.String("job_id", message.JobID), slog.String("media_id", message.MediaID),
		slog.Int("receive_count", receiveCount), slog.Any("error", err),
	)
	if errors.Is(err, domain.ErrNotFound) {
		c.logger.Warn("delete stale video analysis message", slog.String("job_id", message.JobID), slog.String("project_id", message.ProjectID), slog.String("media_id", message.MediaID), slog.String("job_type", message.JobType), slog.Int("receive_count", receiveCount))
		c.delete(ctx, *received.ReceiptHandle)
		return
	}
	if domain.IsPermanent(err) || (c.maxAttempts > 0 && receiveCount >= c.maxAttempts) {
		if receiveCount >= c.maxAttempts && !domain.IsPermanent(err) {
			c.logger.Error("video analysis max receive count exceeded", slog.String("job_id", message.JobID), slog.String("media_id", message.MediaID), slog.Int("receive_count", receiveCount), slog.Int("max_attempts", c.maxAttempts))
		}
		if failErr := handler.MarkFailed(ctx, message, err); failErr != nil {
			if errors.Is(failErr, domain.ErrNotFound) {
				c.logger.Warn("delete stale video analysis message after mark failed", slog.String("job_id", message.JobID), slog.String("project_id", message.ProjectID), slog.String("media_id", message.MediaID), slog.String("job_type", message.JobType), slog.Int("receive_count", receiveCount))
				c.delete(ctx, *received.ReceiptHandle)
				return
			}
			c.logger.Error("record terminal video analysis failure", slog.String("job_id", message.JobID), slog.Any("error", failErr))
			return
		}
		c.delete(ctx, *received.ReceiptHandle)
		return
	}
	backoffAttempt := receiveCount
	if c.maxAttempts > 0 {
		backoffAttempt = min(backoffAttempt, c.maxAttempts)
	}
	backoff := time.Duration(math.Pow(2, float64(backoffAttempt-1))) * 15 * time.Second
	backoff = min(backoff, c.visibilityTimeout)
	_, changeErr := c.client.ChangeMessageVisibility(ctx, &sqs.ChangeMessageVisibilityInput{
		QueueUrl: aws.String(c.queueURL), ReceiptHandle: received.ReceiptHandle, VisibilityTimeout: int32(backoff.Seconds()),
	})
	if changeErr != nil {
		c.logger.Error("set video analysis retry visibility", slog.String("job_id", message.JobID), slog.Any("error", changeErr))
	}
}

func (c *Consumer) extendVisibility(ctx context.Context, receiptHandle string, done chan<- struct{}) {
	defer close(done)
	interval := c.visibilityTimeout / 2
	if interval < 15*time.Second {
		interval = 15 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_, err := c.client.ChangeMessageVisibility(ctx, &sqs.ChangeMessageVisibilityInput{
				QueueUrl: aws.String(c.queueURL), ReceiptHandle: aws.String(receiptHandle),
				VisibilityTimeout: int32(c.visibilityTimeout.Seconds()),
			})
			if err != nil && ctx.Err() == nil {
				c.logger.Warn("extend video analysis visibility", slog.Any("error", err))
			}
		}
	}
}

func (c *Consumer) decodeMessage(ctx context.Context, received types.Message) (domain.JobMessage, bool) {
	if received.Body == nil || received.ReceiptHandle == nil {
		return domain.JobMessage{}, false
	}
	var message domain.JobMessage
	if err := json.Unmarshal([]byte(*received.Body), &message); err != nil {
		c.logger.Error("discard invalid video analysis message", slog.Any("error", err))
		c.delete(ctx, *received.ReceiptHandle)
		return domain.JobMessage{}, false
	}
	return message, true
}

func (c *Consumer) release(ctx context.Context, received types.Message) {
	if received.ReceiptHandle == nil {
		return
	}
	_, err := c.client.ChangeMessageVisibility(ctx, &sqs.ChangeMessageVisibilityInput{
		QueueUrl: aws.String(c.queueURL), ReceiptHandle: received.ReceiptHandle, VisibilityTimeout: 0,
	})
	if err != nil && ctx.Err() == nil {
		c.logger.Warn("release unmatched video analysis message", slog.Any("error", err))
	}
}

func (c *Consumer) delete(ctx context.Context, receiptHandle string) {
	if _, err := c.client.DeleteMessage(ctx, &sqs.DeleteMessageInput{QueueUrl: aws.String(c.queueURL), ReceiptHandle: aws.String(receiptHandle)}); err != nil {
		c.logger.Error("delete video analysis message", slog.Any("error", err))
	}
}

func isVideoAnalysisJobType(jobType string) bool {
	switch jobType {
	case "analysis", "MEDIA_PREPROCESS", "TRANSCRIBE", "VIDEO_UNDERSTAND":
		return true
	default:
		return false
	}
}

func sleep(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
