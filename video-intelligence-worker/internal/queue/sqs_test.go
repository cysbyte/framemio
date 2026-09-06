package queue

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/framemio/video-intelligence-worker/internal/domain"
)

func TestIsVideoAnalysisJobType(t *testing.T) {
	t.Parallel()

	accepted := []string{"analysis", "MEDIA_PREPROCESS", "TRANSCRIBE", "VIDEO_UNDERSTAND"}
	for _, jobType := range accepted {
		jobType := jobType
		t.Run(jobType, func(t *testing.T) {
			t.Parallel()
			if !isVideoAnalysisJobType(jobType) {
				t.Fatalf("expected %s to be owned by video intelligence worker", jobType)
			}
		})
	}

	released := []string{"STORY_PLAN", "STORY_VALIDATE", "VIDEO_RENDER", "", "RENDER"}
	for _, jobType := range released {
		jobType := jobType
		t.Run("release_"+jobType, func(t *testing.T) {
			t.Parallel()
			if isVideoAnalysisJobType(jobType) {
				t.Fatalf("expected %s to be released for another worker", jobType)
			}
		})
	}
}

func TestRetryVisibilityUsesShortExponentialBackoff(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name              string
		receiveCount      int
		maxAttempts       int
		visibilityTimeout time.Duration
		want              time.Duration
	}{
		{name: "first attempt", receiveCount: 1, maxAttempts: 5, visibilityTimeout: 2 * time.Minute, want: 15 * time.Second},
		{name: "second attempt", receiveCount: 2, maxAttempts: 5, visibilityTimeout: 2 * time.Minute, want: 30 * time.Second},
		{name: "capped by max attempts", receiveCount: 5, maxAttempts: 3, visibilityTimeout: 2 * time.Minute, want: time.Minute},
		{name: "capped by visibility", receiveCount: 5, maxAttempts: 5, visibilityTimeout: 45 * time.Second, want: 45 * time.Second},
		{name: "invalid receive count", receiveCount: 0, maxAttempts: 5, visibilityTimeout: 2 * time.Minute, want: 15 * time.Second},
	}

	for _, tt := range cases {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := retryVisibility(tt.receiveCount, tt.maxAttempts, tt.visibilityTimeout); got != tt.want {
				t.Fatalf("retryVisibility() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestHandleMessageReleasesCanceledContextForQuickRetry(t *testing.T) {
	client := &fakeSQSClient{}
	consumer := &Consumer{
		client:            client,
		queueURL:          "https://sqs.example.test/video-analysis",
		visibilityTimeout: 2 * time.Minute,
		maxAttempts:       5,
		logger:            slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	message := domain.JobMessage{JobID: "job-video-1", ProjectID: "project-1", MediaID: "media-1", JobType: "VIDEO_UNDERSTAND"}
	body, err := json.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	receiptHandle := "receipt-1"
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	consumer.handleMessage(ctx, canceledHandler{}, types.Message{
		Body:          aws.String(string(body)),
		ReceiptHandle: aws.String(receiptHandle),
		Attributes: map[string]string{
			string(types.MessageSystemAttributeNameApproximateReceiveCount): "2",
		},
	})

	if len(client.visibilityChanges) != 1 {
		t.Fatalf("visibility change count = %d, want 1", len(client.visibilityChanges))
	}
	if got := client.visibilityChanges[0]; got != int32(shutdownRetryVisibility.Seconds()) {
		t.Fatalf("visibility timeout = %d, want %d", got, int32(shutdownRetryVisibility.Seconds()))
	}
	if client.deleteCount != 0 {
		t.Fatalf("delete count = %d, want 0", client.deleteCount)
	}
}

type canceledHandler struct{}

func (canceledHandler) Process(ctx context.Context, _ domain.JobMessage) error {
	return ctx.Err()
}

func (canceledHandler) MarkFailed(context.Context, domain.JobMessage, error) error {
	return nil
}

type fakeSQSClient struct {
	visibilityChanges []int32
	deleteCount       int
}

func (f *fakeSQSClient) ReceiveMessage(context.Context, *sqs.ReceiveMessageInput, ...func(*sqs.Options)) (*sqs.ReceiveMessageOutput, error) {
	return &sqs.ReceiveMessageOutput{}, nil
}

func (f *fakeSQSClient) DeleteMessage(context.Context, *sqs.DeleteMessageInput, ...func(*sqs.Options)) (*sqs.DeleteMessageOutput, error) {
	f.deleteCount++
	return &sqs.DeleteMessageOutput{}, nil
}

func (f *fakeSQSClient) ChangeMessageVisibility(_ context.Context, input *sqs.ChangeMessageVisibilityInput, _ ...func(*sqs.Options)) (*sqs.ChangeMessageVisibilityOutput, error) {
	f.visibilityChanges = append(f.visibilityChanges, input.VisibilityTimeout)
	return &sqs.ChangeMessageVisibilityOutput{}, nil
}
