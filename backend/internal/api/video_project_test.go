package api

import (
	"net/http"
	"testing"

	"github.com/framemio/backend/internal/models"
	"github.com/framemio/backend/internal/queue"
	"github.com/framemio/backend/internal/store"
)

func TestGenericProjectAPICreatesAllVideoProjectTypes(t *testing.T) {
	t.Parallel()

	memoryStore := store.NewMemoryStore()
	handler := newTestServer(memoryStore, queue.NewMemoryPublisher()).Handler()
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
