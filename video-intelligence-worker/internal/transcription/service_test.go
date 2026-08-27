package transcription

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/framemio/video-intelligence-worker/internal/domain"
)

func TestServiceCachesCompletedTranscriptAndStoresLargeRawArtifact(t *testing.T) {
	t.Parallel()
	repository := newMemoryRepository()
	artifacts := newMemoryArtifacts()
	provider := &fakeProvider{result: ProviderResult{
		Transcript: domain.Transcript{Language: "en", Segments: []domain.TranscriptSegment{
			{StartMicros: 6_800_000, EndMicros: 8_100_000, Text: "We made it.", Confidence: 0.91},
			{StartMicros: 3_200_000, EndMicros: 6_800_000, Text: "Let's go over there.", Confidence: 0.84},
		}},
		RawArtifact: json.RawMessage(`{"provider_job":"job-1","words":[{"word":"Let's","start":3.2}]}`),
	}}
	service := &Service{
		Provider: provider, Repository: repository, Artifacts: artifacts, RetryAttempts: 2,
		RetryDelay: time.Nanosecond, ReferenceExpiry: time.Hour, RawArtifactThresholdBytes: 1,
		Now: func() time.Time { return time.Date(2026, time.August, 19, 12, 0, 0, 0, time.UTC) },
	}
	request := testRequest(10 * time.Second)
	first, err := service.Transcribe(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if first.Reused || provider.calls != 1 || artifacts.presignCalls != 1 {
		t.Fatalf("unexpected first result: reused=%t provider=%d presign=%d", first.Reused, provider.calls, artifacts.presignCalls)
	}
	if !first.Record.HasSpeech || first.Record.SegmentCount != 2 || first.Record.Transcript.Segments[0].StartMicros != 3_200_000 {
		t.Fatalf("timestamped transcript was not normalized: %+v", first.Record)
	}
	if first.Record.RawArtifactS3Key == "" || !strings.Contains(first.Record.RawArtifactS3Key, "/transcriptions/") {
		t.Fatalf("raw provider artifact was not stored: %+v", first.Record)
	}
	second, err := service.Transcribe(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if !second.Reused || second.Record.ID != first.Record.ID || provider.calls != 1 || artifacts.presignCalls != 1 {
		t.Fatalf("completed transcript was not reused: %+v", second)
	}
}

func TestServiceSupportsLongNoisyAudio(t *testing.T) {
	t.Parallel()
	duration := 4 * time.Hour
	provider := &fakeProvider{result: ProviderResult{Transcript: domain.Transcript{
		Language: "en", Segments: []domain.TranscriptSegment{{
			StartMicros: (3*time.Hour + 58*time.Minute).Microseconds(), EndMicros: (3*time.Hour + 59*time.Minute).Microseconds(),
			Text: "Background conversation", Confidence: 0.14,
		}},
	}}}
	service := &Service{
		Provider: provider, Repository: newMemoryRepository(), Artifacts: newMemoryArtifacts(),
		RetryAttempts: 1, RawArtifactThresholdBytes: 1024,
	}
	request := testRequest(duration)
	request.MediaType = "video"
	result, err := service.Transcribe(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Record.HasSpeech || result.Record.DurationMicros != duration.Microseconds() || result.Record.Transcript.Segments[0].Confidence != 0.14 {
		t.Fatalf("long noisy transcription was not preserved: %+v", result.Record)
	}
	if provider.lastRequest.Reference.MediaType != "video" || provider.lastRequest.Reference.DurationMicros != duration.Microseconds() {
		t.Fatalf("provider did not receive long video reference: %+v", provider.lastRequest)
	}
}

func TestServiceTreatsEmptySegmentsAsNoSpeech(t *testing.T) {
	t.Parallel()
	provider := &fakeProvider{result: ProviderResult{Transcript: domain.Transcript{Language: "en", Text: "   "}}}
	service := &Service{Provider: provider, Repository: newMemoryRepository(), Artifacts: newMemoryArtifacts(), RetryAttempts: 1}
	result, err := service.Transcribe(context.Background(), testRequest(30*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if result.Record.HasSpeech || result.Record.SegmentCount != 0 || result.Record.Transcript.Text != "" {
		t.Fatalf("no-speech audio produced speech content: %+v", result.Record)
	}
}

func TestServiceClampsSmallTimestampOverflow(t *testing.T) {
	t.Parallel()
	provider := &fakeProvider{result: ProviderResult{Transcript: domain.Transcript{Language: "en", Segments: []domain.TranscriptSegment{{
		StartMicros: 9_500_000, EndMicros: 10_080_000, Text: "Right at the end", Confidence: 0.82,
	}}}}}
	service := &Service{Provider: provider, Repository: newMemoryRepository(), Artifacts: newMemoryArtifacts(), RetryAttempts: 1}
	result, err := service.Transcribe(context.Background(), testRequest(10*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if got := result.Record.Transcript.Segments[0].EndMicros; got != 10_000_000 {
		t.Fatalf("segment end = %d, want clamped duration", got)
	}
}

func TestServiceRetriesTransientProviderFailure(t *testing.T) {
	t.Parallel()
	provider := &fakeProvider{
		failuresRemaining: 2,
		result: ProviderResult{Transcript: domain.Transcript{Segments: []domain.TranscriptSegment{{
			StartMicros: 100_000, EndMicros: 900_000, Text: "Recovered", Confidence: 0.7,
		}}},
		},
	}
	service := &Service{
		Provider: provider, Repository: newMemoryRepository(), Artifacts: newMemoryArtifacts(),
		RetryAttempts: 3, RetryDelay: time.Nanosecond,
	}
	if _, err := service.Transcribe(context.Background(), testRequest(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	if provider.calls != 3 {
		t.Fatalf("provider calls = %d, want 3", provider.calls)
	}
}

func TestServiceDoesNotRetryPermanentProviderFailure(t *testing.T) {
	t.Parallel()
	provider := &fakeProvider{err: domain.Permanent(errors.New("unsupported audio format"))}
	service := &Service{
		Provider: provider, Repository: newMemoryRepository(), Artifacts: newMemoryArtifacts(),
		RetryAttempts: 5, RetryDelay: time.Nanosecond,
	}
	if _, err := service.Transcribe(context.Background(), testRequest(2*time.Second)); err == nil || !domain.IsPermanent(err) {
		t.Fatalf("expected permanent transcription error, got %v", err)
	}
	if provider.calls != 1 {
		t.Fatalf("provider calls = %d, want 1", provider.calls)
	}
}

func TestServiceReturnsFailureWithoutCaching(t *testing.T) {
	t.Parallel()
	repository := newMemoryRepository()
	provider := &fakeProvider{failuresRemaining: 3}
	service := &Service{
		Provider: provider, Repository: repository, Artifacts: newMemoryArtifacts(),
		RetryAttempts: 3, RetryDelay: time.Nanosecond,
	}
	request := testRequest(2 * time.Second)
	if _, err := service.Transcribe(context.Background(), request); err == nil {
		t.Fatal("expected transcription failure")
	}
	if provider.calls != 3 {
		t.Fatalf("provider calls = %d, want 3", provider.calls)
	}
	if _, err := repository.GetTranscript(context.Background(), request.UserID, request.MediaID, request.Version, request.SourceETag); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("failed transcript was cached: %v", err)
	}
}

func TestServiceHonorsCancellationBetweenRetries(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	provider := &cancelingProvider{cancel: cancel}
	service := &Service{
		Provider: provider, Repository: newMemoryRepository(), Artifacts: newMemoryArtifacts(),
		RetryAttempts: 5, RetryDelay: time.Second,
	}
	_, err := service.Transcribe(ctx, testRequest(2*time.Second))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error = %v", err)
	}
	if provider.calls != 1 {
		t.Fatalf("provider calls after cancellation = %d, want 1", provider.calls)
	}
}

func testRequest(duration time.Duration) Request {
	return Request{
		UserID: "user-1", ProjectID: "project-1", MediaID: "media-1", SourceETag: "etag-1",
		SourceS3Key: "users/user-1/video-analysis/media-1/audio/speech.flac", MediaType: "audio",
		DurationMicros: duration.Microseconds(), Version: "transcription-v1",
	}
}

type memoryRepository struct {
	mu      sync.Mutex
	records map[string]domain.TranscriptionRecord
}

func newMemoryRepository() *memoryRepository {
	return &memoryRepository{records: make(map[string]domain.TranscriptionRecord)}
}

func (r *memoryRepository) GetTranscript(_ context.Context, userID, mediaID, version, sourceETag string) (domain.TranscriptionRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	record, ok := r.records[strings.Join([]string{userID, mediaID, version, sourceETag}, ":")]
	if !ok {
		return domain.TranscriptionRecord{}, domain.ErrNotFound
	}
	return record, nil
}

func (r *memoryRepository) SaveTranscript(_ context.Context, transcript domain.TranscriptionRecord) (domain.TranscriptionRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := strings.Join([]string{transcript.UserID, transcript.MediaID, transcript.Version, transcript.SourceETag}, ":")
	if existing, ok := r.records[key]; ok {
		return existing, nil
	}
	r.records[key] = transcript
	return transcript, nil
}

type memoryArtifacts struct {
	mu           sync.Mutex
	objects      map[string][]byte
	presignCalls int
}

func newMemoryArtifacts() *memoryArtifacts {
	return &memoryArtifacts{objects: map[string][]byte{
		"users/user-1/video-analysis/media-1/audio/speech.flac": []byte("audio"),
	}}
}

func (a *memoryArtifacts) PresignGet(_ context.Context, key string, _ time.Duration) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.presignCalls++
	if _, ok := a.objects[key]; !ok {
		return "", errors.New("artifact not found")
	}
	return "https://media.example.test/" + key, nil
}

func (a *memoryArtifacts) PutJSON(_ context.Context, key string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	a.mu.Lock()
	a.objects[key] = data
	a.mu.Unlock()
	return nil
}

type fakeProvider struct {
	calls             int
	failuresRemaining int
	err               error
	result            ProviderResult
	lastRequest       ProviderRequest
}

func (p *fakeProvider) Transcribe(_ context.Context, request ProviderRequest) (ProviderResult, error) {
	p.calls++
	p.lastRequest = request
	if p.failuresRemaining > 0 {
		p.failuresRemaining--
		return ProviderResult{}, errors.New("temporary transcription failure")
	}
	if p.err != nil {
		return ProviderResult{}, p.err
	}
	return p.result, nil
}

func (*fakeProvider) Name() string  { return "fake-provider" }
func (*fakeProvider) Model() string { return "fake-model" }

type cancelingProvider struct {
	cancel context.CancelFunc
	calls  int
}

func (p *cancelingProvider) Transcribe(_ context.Context, _ ProviderRequest) (ProviderResult, error) {
	p.calls++
	p.cancel()
	return ProviderResult{}, errors.New("temporary transcription failure")
}

func (*cancelingProvider) Name() string  { return "canceling-provider" }
func (*cancelingProvider) Model() string { return "canceling-model" }
