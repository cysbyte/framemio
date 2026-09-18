package store

import (
	"context"
	"testing"
	"time"

	"github.com/framemio/backend/internal/models"
)

func TestMemoryStorePersistsAllVideoProjectTypes(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	memoryStore := NewMemoryStore()
	now := time.Date(2026, time.September, 10, 10, 0, 0, 0, time.UTC)

	tests := []struct {
		projectType models.ProjectType
		projectID   string
	}{
		{models.ProjectTypeStory, "project-story"},
		{models.ProjectTypePromo, "project-promo"},
		{models.ProjectTypeReel, "project-reel"},
	}

	for _, test := range tests {
		test := test
		t.Run(string(test.projectType), func(t *testing.T) {
			t.Parallel()
			media := readyMediaForSourceTest("user-1", "media-"+string(test.projectType), "", now)
			if err := memoryStore.CreateMediaUpload(ctx, media, models.UploadSession{
				ID: "upload-" + media.ID, UserID: media.UserID, MediaID: media.ID, S3Key: media.S3Key,
				Strategy: models.UploadStrategySingle, CreatedAt: now,
			}); err != nil {
				t.Fatal(err)
			}

			project := models.VideoProject{
				ID: test.projectID, UserID: "user-1", Type: test.projectType, Title: string(test.projectType),
				MediaIDs: []string{media.ID}, Status: models.ProjectStatusDraft, CreatedAt: now, UpdatedAt: now,
			}
			created, existing, err := memoryStore.CreateProject(ctx, project, test.projectID+"-key")
			if err != nil {
				t.Fatal(err)
			}
			if existing || created.Type != test.projectType {
				t.Fatalf("created project = %+v, existing = %t", created, existing)
			}

			loaded, err := memoryStore.GetProject(ctx, "user-1", test.projectID)
			if err != nil {
				t.Fatal(err)
			}
			if loaded.Type != test.projectType {
				t.Fatalf("loaded type = %q, want %q", loaded.Type, test.projectType)
			}

			loaded.Title = "updated " + string(test.projectType)
			updated, err := memoryStore.UpdateProject(ctx, loaded)
			if err != nil {
				t.Fatal(err)
			}
			if updated.Title != loaded.Title || updated.CreatedAt != now || updated.UpdatedAt.IsZero() {
				t.Fatalf("updated project did not preserve lifecycle fields: %+v", updated)
			}
		})
	}
}

func TestMemoryStoreLoadsLegacyProjectAsStory(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	memoryStore := NewMemoryStore()
	now := time.Date(2026, time.September, 10, 10, 0, 0, 0, time.UTC)
	media := readyMediaForSourceTest("user-legacy", "media-legacy", "", now)
	if err := memoryStore.CreateMediaUpload(ctx, media, models.UploadSession{
		ID: "upload-legacy", UserID: media.UserID, MediaID: media.ID, S3Key: media.S3Key,
		Strategy: models.UploadStrategySingle, CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	created, _, err := memoryStore.CreateProject(ctx, models.VideoProject{
		ID: "project-legacy", UserID: media.UserID, MediaIDs: []string{media.ID}, CreatedAt: now, UpdatedAt: now,
	}, "legacy-key")
	if err != nil {
		t.Fatal(err)
	}
	if created.Type != models.ProjectTypeStory {
		t.Fatalf("legacy project type = %q, want story", created.Type)
	}
	loaded, err := memoryStore.GetProject(ctx, media.UserID, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Type != models.ProjectTypeStory {
		t.Fatalf("loaded legacy project type = %q, want story", loaded.Type)
	}
}
