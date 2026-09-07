package taskprotection

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultProtectionMinutes = 120
	defaultRequestTimeout    = 5 * time.Second
)

type Client struct {
	AgentURI          string
	ProtectionMinutes int
	HTTPClient        *http.Client
}

func NewFromEnv() Client {
	return Client{
		AgentURI:          strings.TrimSpace(os.Getenv("ECS_AGENT_URI")),
		ProtectionMinutes: envInt("ECS_TASK_PROTECTION_MINUTES", defaultProtectionMinutes),
	}
}

func (c Client) Protect(ctx context.Context) (bool, error) {
	if strings.TrimSpace(c.AgentURI) == "" {
		return false, nil
	}
	minutes := c.ProtectionMinutes
	if minutes <= 0 {
		minutes = defaultProtectionMinutes
	}
	return true, c.update(ctx, taskProtectionRequest{ProtectionEnabled: true, ExpiresInMinutes: minutes})
}

func (c Client) Unprotect(ctx context.Context) error {
	if strings.TrimSpace(c.AgentURI) == "" {
		return nil
	}
	return c.update(ctx, taskProtectionRequest{ProtectionEnabled: false})
}

type taskProtectionRequest struct {
	ProtectionEnabled bool `json:"ProtectionEnabled"`
	ExpiresInMinutes  int  `json:"ExpiresInMinutes,omitempty"`
}

func (c Client) update(ctx context.Context, payload taskProtectionRequest) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	requestContext, cancel := context.WithTimeout(ctx, defaultRequestTimeout)
	defer cancel()
	url := strings.TrimRight(strings.TrimSpace(c.AgentURI), "/") + "/task-protection/v1/state"
	request, err := http.NewRequestWithContext(requestContext, http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	client := c.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		text, _ := io.ReadAll(io.LimitReader(response.Body, 500))
		return fmt.Errorf("ecs task protection returned %d: %s", response.StatusCode, strings.TrimSpace(string(text)))
	}
	return nil
}

func envInt(name string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}
