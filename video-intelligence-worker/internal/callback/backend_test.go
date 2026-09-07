package callback

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/framemio/video-intelligence-worker/internal/domain"
)

func TestBackendReporterRetriesTransientCompletionFailure(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if calls.Add(1) < 3 {
			response.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		response.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	reporter := BackendReporter{
		BaseURL:       server.URL,
		InternalToken: "internal-token",
		RetryAttempts: 3,
		RetryBackoff:  time.Nanosecond,
	}
	err := reporter.CompleteJob(context.Background(), domain.ProcessingJob{ID: "job-retry", ProjectID: "project-retry"}, "analysis/job-retry.json", "application/json")
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 3 {
		t.Fatalf("expected three callback attempts, got %d", calls.Load())
	}
}

func TestBackendReporterDoesNotRetryPermanentCompletionFailure(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		response.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	reporter := BackendReporter{
		BaseURL:       server.URL,
		InternalToken: "internal-token",
		RetryAttempts: 4,
		RetryBackoff:  time.Nanosecond,
	}
	if err := reporter.CompleteJob(context.Background(), domain.ProcessingJob{ID: "job-permanent", ProjectID: "project-permanent"}, "analysis/job-permanent.json", "application/json"); err == nil {
		t.Fatal("expected permanent callback error")
	}
	if calls.Load() != 1 {
		t.Fatalf("expected one callback attempt, got %d", calls.Load())
	}
}
