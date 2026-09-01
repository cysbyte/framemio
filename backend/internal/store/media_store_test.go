package store

import (
	"context"
	"testing"
	"time"

	"github.com/framemio/backend/internal/models"
)

func TestMemoryStoreAddProjectMediaSkipsDuplicateSourceAsset(t *testing.T) {
	ctx := context.Background()
	memoryStore := NewMemoryStore()
	now := time.Date(2026, time.September, 1, 12, 0, 0, 0, time.UTC)
	userID := "user-duplicate-source"

	original := readyMediaForSourceTest(userID, "media-original", "photo-local-id", now)
	duplicate := readyMediaForSourceTest(userID, "media-duplicate", "photo-local-id", now)
	fresh := readyMediaForSourceTest(userID, "media-fresh", "other-photo-local-id", now)

	for _, media := range []models.MediaAsset{original, duplicate, fresh} {
		session := models.UploadSession{
			ID:        "upload-" + media.ID,
			UserID:    userID,
			MediaID:   media.ID,
			S3Key:     media.S3Key,
			Strategy:  models.UploadStrategySingle,
			CreatedAt: now,
		}
		if err := memoryStore.CreateMediaUpload(ctx, media, session); err != nil {
			t.Fatalf("create media %s: %v", media.ID, err)
		}
	}

	project := models.StoryProject{
		ID:        "project-duplicate-source",
		UserID:    userID,
		Title:     "Duplicate Source Test",
		MediaIDs:  []string{original.ID},
		Status:    models.ProjectStatusReady,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if _, _, err := memoryStore.CreateProject(ctx, project, "project-duplicate-source-key"); err != nil {
		t.Fatalf("create project: %v", err)
	}

	updated, err := memoryStore.AddProjectMedia(ctx, userID, project.ID, []string{duplicate.ID, fresh.ID})
	if err != nil {
		t.Fatalf("add project media: %v", err)
	}

	if got, want := updated.MediaIDs, []string{original.ID, fresh.ID}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("media IDs = %#v, want %#v", got, want)
	}
}

func readyMediaForSourceTest(userID, mediaID, sourceAssetID string, now time.Time) models.MediaAsset {
	return models.MediaAsset{
		ID:            mediaID,
		UserID:        userID,
		Type:          models.MediaTypePhoto,
		Status:        models.MediaStatusReady,
		FileName:      mediaID + ".jpg",
		ContentType:   "image/jpeg",
		FileSizeBytes: 1024,
		PixelWidth:    1200,
		PixelHeight:   900,
		S3Key:         "users/" + userID + "/media/" + mediaID,
		UploadID:      "upload-" + mediaID,
		SourceAssetID: sourceAssetID,
		ETag:          "etag-" + mediaID,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
}
