package callback

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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
}

type completionRequest struct {
	Status              string  `json:"status"`
	Progress            float64 `json:"progress"`
	ArtifactS3Key       string  `json:"artifact_s3_key,omitempty"`
	ArtifactContentType string  `json:"artifact_content_type,omitempty"`
}

func (r BackendReporter) CompleteJob(ctx context.Context, job domain.ProcessingJob, artifactS3Key string) error {
	baseURL := strings.TrimRight(strings.TrimSpace(r.BaseURL), "/")
	if baseURL == "" || strings.TrimSpace(r.InternalToken) == "" {
		return fmt.Errorf("backend callback requires base url and internal token")
	}
	body, err := json.Marshal(completionRequest{
		Status:              "COMPLETED",
		Progress:            1,
		ArtifactS3Key:       artifactS3Key,
		ArtifactContentType: "application/json",
	})
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/internal/jobs/"+url.PathEscape(job.ID)+"/status", bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-FrameMio-Internal-Token", r.InternalToken)
	client := r.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("post job completion callback: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("job completion callback returned %s", response.Status)
	}
	return nil
}
