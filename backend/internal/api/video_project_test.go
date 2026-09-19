package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/framemio/backend/internal/models"
	"github.com/framemio/backend/internal/queue"
	"github.com/framemio/backend/internal/store"
)

func TestGenericProjectAPICreatesAllVideoProjectTypes(t *testing.T) {
	t.Parallel()

	memoryStore := store.NewMemoryStore()
	handler := newTestServer(memoryStore, queue.NewMemoryPublisher()).Handler()
	if _, _, err := memoryStore.EnsureCreditGrant(context.Background(), models.CreditGrant{
		UserID:      "user-1",
		Source:      models.CreditGrantSourceCreditPurchase,
		Amount:      200,
		Remaining:   200,
		ReferenceID: "generic-project-test-credit-grant",
	}); err != nil {
		t.Fatal(err)
	}
	createReadyIOSMediaInStore(t, memoryStore, "asset-generic-story", models.MediaTypePhoto, 1024)
	createReadyIOSMediaInStore(t, memoryStore, "asset-generic-promo", models.MediaTypePhoto, 1024)
	createReadyIOSMediaInStore(t, memoryStore, "asset-generic-reel", models.MediaTypePhoto, 1024)

	tests := []struct {
		name        string
		projectType models.ProjectType
		assetID     string
		config      map[string]any
	}{
		{
			name: "story", projectType: models.ProjectTypeStory, assetID: "asset-generic-story",
			config: map[string]any{"story": map[string]any{"emotional_tone": "warm"}},
		},
		{
			name: "promo", projectType: models.ProjectTypePromo, assetID: "asset-generic-promo",
			config: map[string]any{"promo": map[string]any{"call_to_action": "Try it now", "desired_duration_seconds": 15}},
		},
		{
			name: "reel", projectType: models.ProjectTypeReel, assetID: "asset-generic-reel",
			config: map[string]any{"reel": map[string]any{"social_platform": "TikTok", "target_duration_seconds": 30, "style": "dynamic", "aspect_ratio": "16:9"}},
		},
	}

	projects := make(map[models.ProjectType]models.VideoProject, len(tests))
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			response := performJSON(t, handler, http.MethodPost, "/projects", map[string]any{
				"type":          test.projectType,
				"title":         test.name + " project",
				"context":       "Create a " + test.name,
				"media_ids":     []string{"media-" + test.assetID},
				"configuration": test.config,
			}, "generic-"+test.name, true, nil)
			if response.Code != http.StatusCreated {
				t.Fatalf("create status = %d, body = %s", response.Code, response.Body.String())
			}
			var project models.VideoProject
			decodeResponse(t, response, &project)
			if project.Type != test.projectType {
				t.Fatalf("project type = %q, want %q", project.Type, test.projectType)
			}
			if test.projectType == models.ProjectTypeReel {
				if project.Configuration.Reel == nil || project.Configuration.Reel.SocialPlatform != "tiktok" || project.Configuration.Reel.TargetDurationSecs != 30 || project.Configuration.Reel.Style != "dynamic" {
					t.Fatalf("reel configuration was not normalized: %+v", project.Configuration.Reel)
				}
				if project.Configuration.Composition.AspectRatio != "9:16" || project.Configuration.Reel.AspectRatio != "9:16" {
					t.Fatalf("reel aspect ratio was not forced vertical: %+v", project.Configuration)
				}
			}
			projects[test.projectType] = project

			getResponse := performJSON(t, handler, http.MethodGet, "/projects/"+project.ID, nil, "", true, nil)
			if getResponse.Code != http.StatusOK {
				t.Fatalf("get status = %d, body = %s", getResponse.Code, getResponse.Body.String())
			}
			var loaded models.VideoProject
			decodeResponse(t, getResponse, &loaded)
			if loaded.Type != test.projectType || loaded.ID != project.ID {
				t.Fatalf("loaded project = %+v", loaded)
			}
		})
	}

	promo := projects[models.ProjectTypePromo]
	updatedResponse := performJSON(t, handler, http.MethodPatch, "/projects/"+promo.ID, map[string]any{
		"title": "Updated promo",
	}, "", true, nil)
	if updatedResponse.Code != http.StatusOK {
		t.Fatalf("update status = %d, body = %s", updatedResponse.Code, updatedResponse.Body.String())
	}
	var updated models.VideoProject
	decodeResponse(t, updatedResponse, &updated)
	if updated.Type != models.ProjectTypePromo || updated.Title != "Updated promo" {
		t.Fatalf("updated project = %+v", updated)
	}

	typeChange := performJSON(t, handler, http.MethodPatch, "/projects/"+promo.ID, map[string]any{
		"type": models.ProjectTypeReel,
	}, "", true, nil)
	if typeChange.Code != http.StatusConflict {
		t.Fatalf("type change status = %d, body = %s", typeChange.Code, typeChange.Body.String())
	}
}

func TestGenericProjectCreationReservesCreditsForPromoAndReel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		projectType models.ProjectType
		mediaID     string
	}{
		{name: "promo", projectType: models.ProjectTypePromo, mediaID: "asset-credit-promo"},
		{name: "reel", projectType: models.ProjectTypeReel, mediaID: "asset-credit-reel"},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			memoryStore := store.NewMemoryStore()
			server := newTestServer(memoryStore, queue.NewMemoryPublisher())
			handler := server.Handler()
			createReadyIOSMediaInStore(t, memoryStore, test.mediaID, models.MediaTypePhoto, 1024)
			body := map[string]any{
				"type":      test.projectType,
				"title":     test.name + " project",
				"context":   "credit test",
				"media_ids": []string{"media-" + test.mediaID},
			}

			first := performJSON(t, handler, http.MethodPost, "/projects", body, "credit-"+test.name, true, nil)
			if first.Code != http.StatusCreated {
				t.Fatalf("create status = %d, body = %s", first.Code, first.Body.String())
			}
			var project models.VideoProject
			decodeResponse(t, first, &project)
			if project.CreditOperationKey != string(models.CreditActionCreateStory) {
				t.Fatalf("credit operation key = %q", project.CreditOperationKey)
			}

			balance, err := memoryStore.GetCreditBalance(context.Background(), "user-1")
			if err != nil {
				t.Fatal(err)
			}
			if balance.Available != 60 || balance.Reserved != 40 {
				t.Fatalf("balance after %s creation = %+v, want 60 available and 40 reserved", test.name, balance)
			}

			second := performJSON(t, handler, http.MethodPost, "/projects", body, "credit-"+test.name, true, nil)
			if second.Code != http.StatusOK {
				t.Fatalf("duplicate create status = %d, body = %s", second.Code, second.Body.String())
			}
			var duplicate models.VideoProject
			decodeResponse(t, second, &duplicate)
			if duplicate.ID != project.ID {
				t.Fatalf("duplicate project id = %q, want %q", duplicate.ID, project.ID)
			}
			balance, err = memoryStore.GetCreditBalance(context.Background(), "user-1")
			if err != nil {
				t.Fatal(err)
			}
			if balance.Available != 60 || balance.Reserved != 40 {
				t.Fatalf("balance after duplicate %s creation = %+v, reservation was duplicated", test.name, balance)
			}
		})
	}
}

func TestGenericProjectCreationRejectsInsufficientCredits(t *testing.T) {
	t.Parallel()

	memoryStore := store.NewMemoryStore()
	publisher := queue.NewMemoryPublisher()
	server := newTestServer(memoryStore, publisher)
	server.Credits.Policy.InitialGrant = 20
	handler := server.Handler()
	createReadyIOSMediaInStore(t, memoryStore, "asset-credit-insufficient", models.MediaTypePhoto, 1024)

	response := performJSON(t, handler, http.MethodPost, "/projects", map[string]any{
		"type":      models.ProjectTypePromo,
		"title":     "Insufficient promo",
		"context":   "credit test",
		"media_ids": []string{"media-asset-credit-insufficient"},
	}, "credit-insufficient", true, nil)
	if response.Code != http.StatusPaymentRequired {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"error":"insufficient_credits"`) {
		t.Fatalf("expected insufficient_credits response, body = %s", response.Body.String())
	}
}
