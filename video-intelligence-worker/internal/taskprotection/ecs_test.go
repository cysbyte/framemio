package taskprotection

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientEnablesAndDisablesTaskProtection(t *testing.T) {
	t.Parallel()
	requests := make([]taskProtectionRequest, 0, 2)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPut || request.URL.Path != "/task-protection/v1/state" {
			t.Fatalf("unexpected request %s %s", request.Method, request.URL.Path)
		}
		var payload taskProtectionRequest
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		requests = append(requests, payload)
		response.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := Client{AgentURI: server.URL, ProtectionMinutes: 30, HTTPClient: server.Client()}
	protected, err := client.Protect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !protected {
		t.Fatal("expected ECS task protection to be enabled")
	}
	if err := client.Unprotect(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(requests) != 2 || !requests[0].ProtectionEnabled || requests[0].ExpiresInMinutes != 30 || requests[1].ProtectionEnabled {
		t.Fatalf("unexpected task protection requests: %+v", requests)
	}
}

func TestClientReportsUnavailableOutsideECS(t *testing.T) {
	t.Parallel()
	protected, err := (Client{}).Protect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if protected {
		t.Fatal("expected no task protection outside ECS")
	}
	if err := (Client{}).Unprotect(context.Background()); err != nil {
		t.Fatal(err)
	}
}
