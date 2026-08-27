package music

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/framemio/backend/internal/models"
	"github.com/framemio/backend/internal/storage"
	"github.com/framemio/backend/internal/store"
)

func TestLocalProviderIsDeterministic(t *testing.T) {
	provider := LocalProvider{}
	request := GenerateRequest{Category: "warm", Prompt: "warm memory", DurationSeconds: 2, Instrumental: true}
	first, err := provider.Generate(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	second, err := provider.Generate(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if string(first.AudioBytes) != string(second.AudioBytes) || first.ProviderGenerationID != second.ProviderGenerationID {
		t.Fatal("local provider must produce deterministic fixture audio")
	}
	if first.ContentType != "audio/wav" || first.FileExtension != "wav" || len(first.AudioBytes) == 0 {
		t.Fatalf("unexpected local provider result: %+v", first)
	}
}

func TestElevenProviderMapsComposeRequest(t *testing.T) {
	var capturedKey string
	var capturedPath string
	var capturedQuery string
	var captured map[string]any
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		capturedPath = request.URL.Path
		capturedQuery = request.URL.RawQuery
		capturedKey = request.Header.Get("xi-api-key")
		if err := json.NewDecoder(request.Body).Decode(&captured); err != nil {
			t.Fatal(err)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"audio/mpeg"}, "Song-Id": []string{"song-123"}},
			Body:       io.NopCloser(strings.NewReader("mp3 bytes")),
			Request:    request,
		}, nil
	})}

	provider := ElevenProvider{APIKey: "secret-key", BaseURL: "https://api.test", Model: "music_v2", HTTPClient: client}
	result, err := provider.Generate(context.Background(), GenerateRequest{Prompt: "calm instrumental", DurationSeconds: 31, Instrumental: true})
	if err != nil {
		t.Fatal(err)
	}
	if capturedPath != "/v1/music" || capturedQuery != "output_format=auto" || capturedKey != "secret-key" {
		t.Fatalf("unexpected request path/query/key: %s %s %s", capturedPath, capturedQuery, capturedKey)
	}
	if captured["prompt"] != "calm instrumental" || captured["model_id"] != "music_v2" || captured["force_instrumental"] != true || captured["music_length_ms"] != float64(31000) {
		t.Fatalf("unexpected request body: %+v", captured)
	}
	if result.Provider != ProviderElevenLabs || result.ProviderGenerationID != "song-123" || result.FileExtension != "mp3" {
		t.Fatalf("unexpected eleven result: %+v", result)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestGenerationServicePersistsReviewTrackWithPendingLicense(t *testing.T) {
	ctx := context.Background()
	memoryStore := store.NewMemoryStore()
	service := GenerationService{Store: memoryStore, ObjectStorage: storage.StaticObjectStorage{BaseURL: "https://objects.example.test"}, Provider: LocalProvider{}, Now: func() time.Time { return time.Date(2026, time.August, 28, 1, 2, 3, 0, time.UTC) }}
	track, err := service.GenerateTrack(ctx, GenerateTrackRequest{Category: "warm"})
	if err != nil {
		t.Fatal(err)
	}
	stored, err := memoryStore.GetMusicTrack(ctx, track.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != models.MusicTrackStatusReview || stored.LicenseMetadata.LicenseStatus != models.MusicLicenseStatusPending {
		t.Fatalf("unexpected track status/license: %+v", stored)
	}
	if stored.ProviderGenerationID == "" || stored.StorageKey == "" || stored.PreviewStorageKey == "" {
		t.Fatalf("missing persisted provider/storage fields: %+v", stored)
	}
}

func TestSelectorPrefersSpeechFriendlyApprovedTrack(t *testing.T) {
	ctx := context.Background()
	memoryStore := store.NewMemoryStore()
	now := time.Date(2026, time.August, 28, 1, 0, 0, 0, time.UTC)
	tracks := []models.MusicTrack{
		{ID: "music-loud", Title: "Loud", Status: models.MusicTrackStatusApproved, DurationSeconds: 60, Moods: []string{"warm"}, Energy: 0.9, Instrumental: true, SpeechFriendly: false, StorageKey: "music/loud.mp3", LicenseMetadata: models.MusicLicenseMetadata{LicenseStatus: models.MusicLicenseStatusVerified}, CreatedAt: now, UpdatedAt: now},
		{ID: "music-soft", Title: "Soft", Status: models.MusicTrackStatusApproved, DurationSeconds: 60, Moods: []string{"warm"}, Energy: 0.35, Instrumental: true, SpeechFriendly: true, StorageKey: "music/soft.mp3", LicenseMetadata: models.MusicLicenseMetadata{LicenseStatus: models.MusicLicenseStatusVerified}, CreatedAt: now, UpdatedAt: now},
	}
	for _, track := range tracks {
		if err := memoryStore.SaveMusicTrack(ctx, track); err != nil {
			t.Fatal(err)
		}
	}
	selector := Selector{Store: memoryStore, RequireVerifiedLicense: true, DefaultVolume: 0.16, DuckedVolume: 0.04}
	audio, err := selector.Select(ctx, SelectionRequest{ProjectID: "project-1", StoryPlanID: "plan-1", Mood: "warm", DurationSeconds: 15, HasSpeech: true, Mode: "auto"})
	if err != nil {
		t.Fatal(err)
	}
	if audio == nil || audio.TrackID != "music-soft" || audio.BackgroundS3Key != "music/soft.mp3" || audio.Ducking == nil || !*audio.Ducking {
		t.Fatalf("unexpected selected audio: %+v", audio)
	}
}

func TestSelectorNonePreservesOriginalAudio(t *testing.T) {
	selector := Selector{}
	audio, err := selector.Select(context.Background(), SelectionRequest{Mode: "none"})
	if err != nil {
		t.Fatal(err)
	}
	if audio.Source != "original" || audio.OriginalAudio == nil || !*audio.OriginalAudio || audio.Ducking == nil || *audio.Ducking {
		t.Fatalf("music none should preserve source audio without ducking: %+v", audio)
	}
}
