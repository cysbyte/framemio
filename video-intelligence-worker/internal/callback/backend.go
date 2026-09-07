package callback

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/framemio/video-intelligence-worker/internal/domain"
)

type BackendReporter struct {
	BaseURL       string
	InternalToken string
	HTTPClient    *http.Client
	Logger        *slog.Logger
	RetryAttempts int
	RetryBackoff  time.Duration
}

const (
	completionCallbackAttempts = 4
	completionCallbackBackoff  = 250 * time.Millisecond
)

type completionRequest struct {
	Status              string  `json:"status"`
	Progress            float64 `json:"progress"`
	ArtifactS3Key       string  `json:"artifact_s3_key,omitempty"`
	ArtifactContentType string  `json:"artifact_content_type,omitempty"`
}

func (r BackendReporter) CompleteJob(ctx context.Context, job domain.ProcessingJob, artifactS3Key, artifactContentType string) error {
	baseURL := strings.TrimRight(strings.TrimSpace(r.BaseURL), "/")
	if baseURL == "" || strings.TrimSpace(r.InternalToken) == "" {
		return fmt.Errorf("backend callback requires base url and internal token")
	}
	if strings.TrimSpace(artifactContentType) == "" {
		artifactContentType = "application/json"
	}
	body, err := json.Marshal(completionRequest{
		Status:              "COMPLETED",
		Progress:            1,
		ArtifactS3Key:       artifactS3Key,
		ArtifactContentType: artifactContentType,
	})
	if err != nil {
		return err
	}
	client := r.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	attempts := r.RetryAttempts
	if attempts <= 0 {
		attempts = completionCallbackAttempts
	}
	backoff := r.RetryBackoff
	if backoff <= 0 {
		backoff = completionCallbackBackoff
	}
	started := time.Now()
	for attempt := 1; attempt <= attempts; attempt++ {
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/internal/jobs/"+url.PathEscape(job.ID)+"/status", bytes.NewReader(body))
		if err != nil {
			return err
		}
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-FrameMio-Internal-Token", r.InternalToken)
		attemptStarted := time.Now()
		response, err := client.Do(request)
		if err == nil {
			response.Body.Close()
			if response.StatusCode >= 200 && response.StatusCode < 300 {
				r.logger().Info("backend completion callback succeeded",
					slog.String("job_id", job.ID), slog.String("project_id", job.ProjectID),
					slog.Int("attempt", attempt), slog.Int("status_code", response.StatusCode),
					slog.Int64("attempt_duration_ms", time.Since(attemptStarted).Milliseconds()),
					slog.Int64("total_duration_ms", time.Since(started).Milliseconds()),
				)
				return nil
			}
			if !retryableStatus(response.StatusCode) {
				return fmt.Errorf("job completion callback returned %s", response.Status)
			}
			err = fmt.Errorf("job completion callback returned %s", response.Status)
		}
		if attempt == attempts {
			return fmt.Errorf("post job completion callback after %d attempts: %w", attempt, err)
		}
		r.logger().Warn("backend completion callback retrying",
			slog.String("job_id", job.ID), slog.String("project_id", job.ProjectID),
			slog.Int("attempt", attempt), slog.Int("next_attempt", attempt+1),
			slog.Int64("attempt_duration_ms", time.Since(attemptStarted).Milliseconds()),
			slog.Any("error", err),
		)
		if err := sleep(ctx, backoff*time.Duration(1<<(attempt-1))); err != nil {
			return err
		}
	}
	return fmt.Errorf("post job completion callback failed")
}

func (r BackendReporter) logger() *slog.Logger {
	if r.Logger != nil {
		return r.Logger
	}
	return slog.Default()
}

func retryableStatus(statusCode int) bool {
	return statusCode == http.StatusRequestTimeout || statusCode == http.StatusTooManyRequests || statusCode >= 500
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
