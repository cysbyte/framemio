package understanding

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/framemio/video-intelligence-worker/internal/domain"
)

func TestServiceCachesByProviderModelAndPreservesPreviousAnalysis(t *testing.T) {
	t.Parallel()
	repository := newMemoryRepository()
	artifacts := &memoryArtifacts{}
	providerA := &fakeProvider{name: "provider-a", model: "model-a"}
	serviceA := newService(providerA, repository, artifacts)
	request := testRequest()

	first, err := serviceA.Analyze(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if first.Reused || first.Analysis.SchemaVersion != domain.VideoAnalysisSchemaV1 || first.Analysis.InputVersion != "video-evidence-v1" || first.Analysis.UnderstandingVersion != "video-understanding-v1" {
		t.Fatalf("unexpected first analysis: %+v", first)
	}
	if first.Analysis.DurationMicros != request.Evidence.Metadata.DurationMicros || len(first.Analysis.Scenes) != 2 ||
		len(first.Analysis.Scenes[0].RepresentativeFrames) != 1 || len(first.Analysis.Transcript.Segments) != 1 ||
		len(first.Analysis.Segments) != 2 || first.Analysis.ProcessingStatus != domain.VideoAnalysisStatusCompleted {
		t.Fatalf("persistent analysis is incomplete: %+v", first.Analysis)
	}
	if providerA.calls != 1 || artifacts.calls != len(request.Evidence.Frames) {
		t.Fatalf("unexpected provider or presign calls: provider=%d presign=%d", providerA.calls, artifacts.calls)
	}
	encodedInput, err := json.Marshal(providerA.lastInput)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encodedInput), "source_s3_key") || strings.Contains(string(encodedInput), "source.mov") {
		t.Fatalf("provider input contains original video reference: %s", encodedInput)
	}
	if first.Analysis.Segments[0].VisualQuality >= first.Analysis.Segments[0].MemoryValue {
		t.Fatal("technical quality and memory value were not represented independently")
	}

	cached, err := serviceA.Analyze(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if !cached.Reused || cached.Analysis.ID != first.Analysis.ID || providerA.calls != 1 || artifacts.calls != len(request.Evidence.Frames) {
		t.Fatalf("completed analysis was not reused: %+v", cached)
	}

	providerB := &fakeProvider{name: "provider-b", model: "model-b"}
	serviceB := newService(providerB, repository, artifacts)
	secondModel, err := serviceB.Analyze(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if secondModel.Analysis.ID == first.Analysis.ID || secondModel.Analysis.Model != "model-b" || repository.count() != 2 {
		t.Fatalf("different model destroyed or reused previous analysis: %+v", secondModel.Analysis)
	}

	request.Force = true
	request.RunID = "analysis-job-force-1"
	forced, err := serviceA.Analyze(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if forced.Reused || forced.Analysis.ID == first.Analysis.ID || forced.Analysis.AnalysisVersion == first.Analysis.AnalysisVersion || providerA.calls != 2 || repository.count() != 3 {
		t.Fatalf("forced same-model analysis did not create an immutable version: %+v", forced)
	}
	forcedRetry, err := serviceA.Analyze(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if forcedRetry.Analysis.ID != forced.Analysis.ID || repository.count() != 3 {
		t.Fatalf("retry of the same forced job created a duplicate version: %+v", forcedRetry)
	}
}

func TestValidateResponseV1RequiresGroundedCompleteCandidates(t *testing.T) {
	t.Parallel()
	input := testInput()
	validated, err := ValidateResponseV1(validResponse(input, "valid"), input)
	if err != nil {
		t.Fatal(err)
	}
	if len(validated.Candidates) != len(input.Segments) || validated.Candidates[0].MemoryValueScore != 0.98 || validated.Candidates[0].VisualQualityScore != 0.35 {
		t.Fatalf("valid response changed score semantics: %+v", validated)
	}

	tests := []struct {
		name   string
		mutate func(*domain.VideoUnderstandingResponseV1)
	}{
		{name: "wrong schema", mutate: func(value *domain.VideoUnderstandingResponseV1) { value.SchemaVersion = "video-analysis.v2" }},
		{name: "missing candidate", mutate: func(value *domain.VideoUnderstandingResponseV1) { value.Candidates = value.Candidates[:1] }},
		{name: "invented timestamp", mutate: func(value *domain.VideoUnderstandingResponseV1) { value.Candidates[0].StartMicros++ }},
		{name: "unknown frame", mutate: func(value *domain.VideoUnderstandingResponseV1) {
			value.Candidates[0].FrameIDs = []string{"invented-frame"}
		}},
		{name: "ungrounded visual claim", mutate: func(value *domain.VideoUnderstandingResponseV1) { value.Candidates[0].FrameIDs = nil }},
		{name: "ungrounded speech", mutate: func(value *domain.VideoUnderstandingResponseV1) { value.Candidates[0].TranscriptSegmentIndexes = nil }},
		{name: "ungrounded useful candidate", mutate: func(value *domain.VideoUnderstandingResponseV1) {
			value.Candidates[1].Scenery = nil
			value.Candidates[1].FrameIDs = nil
			value.Candidates[1].RecommendedUsage = domain.RecommendedUsageSupporting
		}},
		{name: "invalid score", mutate: func(value *domain.VideoUnderstandingResponseV1) { value.Candidates[0].MemoryValueScore = 1.1 }},
		{name: "invalid usage", mutate: func(value *domain.VideoUnderstandingResponseV1) { value.Candidates[0].RecommendedUsage = "viral_clip" }},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			response := validResponse(input, test.name)
			test.mutate(&response)
			if _, err := ValidateResponseV1(response, input); !errors.Is(err, domain.ErrInvalidModelOutput) {
				t.Fatalf("validation error = %v", err)
			}
		})
	}
}

func TestServiceRetriesInvalidResponseAndDoesNotStoreIt(t *testing.T) {
	t.Parallel()
	repository := newMemoryRepository()
	provider := &fakeProvider{name: "provider-a", model: "model-a", invalidSchema: true}
	service := newService(provider, repository, &memoryArtifacts{})
	service.RetryAttempts = 2
	if _, err := service.Analyze(context.Background(), testRequest()); err == nil {
		t.Fatal("expected invalid provider response to fail")
	}
	if provider.calls != 2 || repository.count() != 0 {
		t.Fatalf("invalid response was not retried safely: calls=%d records=%d", provider.calls, repository.count())
	}
}

func newService(provider VideoUnderstandingProvider, repository Repository, artifacts ArtifactStore) *Service {
	return &Service{
		Provider: provider, Repository: repository, Artifacts: artifacts,
		RetryAttempts: 2, RetryDelay: time.Nanosecond, FrameURLExpiry: time.Hour,
		Now: func() time.Time { return time.Date(2026, time.August, 19, 14, 0, 0, 0, time.UTC) },
	}
}

func testRequest() Request {
	evidence := testEvidence()
	return Request{
		CacheRequest: CacheRequest{
			UserID: "user-1", MediaID: evidence.MediaID, SourceETag: evidence.SourceETag,
			InputVersion: evidence.AnalysisVersion, UnderstandingVersion: "video-understanding-v1",
		},
		ProjectID: evidence.ProjectID, EvidenceS3Key: "users/user-1/video-analysis/media-1/evidence.json", Evidence: evidence,
	}
}

func testEvidence() domain.VideoEvidence {
	return domain.VideoEvidence{
		SchemaVersion: 1, AnalysisVersion: "video-evidence-v1", MediaID: "media-1", ProjectID: "project-1",
		SourceS3Key: "users/user-1/media/media-1/source.mov", SourceETag: "etag-1",
		Metadata: domain.VideoMetadata{DurationMicros: 8_000_000, Width: 1920, Height: 1080, FPS: 30, VideoCodec: "hevc", HasAudio: true},
		Scenes: []domain.VideoScene{
			{ID: "scene-0001", StartMicros: 0, EndMicros: 4_000_000, ChangeScore: 0.8},
			{ID: "scene-0002", StartMicros: 4_000_000, EndMicros: 8_000_000, ChangeScore: 0.6},
		},
		Segments: []domain.CandidateSegment{
			{ID: "segment-0001", SceneID: "scene-0001", StartMicros: 0, EndMicros: 4_000_000},
			{ID: "segment-0002", SceneID: "scene-0002", StartMicros: 4_000_000, EndMicros: 8_000_000},
		},
		Frames: []domain.SelectedFrame{
			{ID: "frame-0001", MediaID: "media-1", SceneID: "scene-0001", SegmentID: "segment-0001", TimestampMicros: 2_000_000, S3Key: "frames/1.jpg", QualityScore: 0.35},
			{ID: "frame-0002", MediaID: "media-1", SceneID: "scene-0002", SegmentID: "segment-0002", TimestampMicros: 6_000_000, S3Key: "frames/2.jpg", QualityScore: 0.95},
		},
		Transcript: domain.Transcript{Language: "en", HasSpeech: true, Segments: []domain.TranscriptSegment{{
			StartMicros: 1_000_000, EndMicros: 2_500_000, Text: "That was so funny.", Confidence: 0.92,
		}}},
		Quality: domain.QualitySignals{OverallScore: 0.65, AverageSharpness: 0.5, AverageBrightness: 0.55},
	}
}

func testInput() domain.VideoUnderstandingInputV1 {
	evidence := testEvidence()
	return domain.VideoUnderstandingInputV1{
		SchemaVersion: domain.VideoUnderstandingInputSchemaV1,
		InputVersion:  evidence.AnalysisVersion, AnalysisVersion: "video-understanding-v1",
		Objective: Objective, GroundingRules: append([]string(nil), groundingRules...),
		MediaID: evidence.MediaID, ProjectID: evidence.ProjectID, Metadata: evidence.Metadata,
		Scenes: evidence.Scenes, Segments: evidence.Segments,
		Frames: []domain.VideoUnderstandingFrameV1{
			{ID: "frame-0001", SceneID: "scene-0001", SegmentID: "segment-0001", TimestampMicros: 2_000_000, URL: "https://example.test/frames/1.jpg"},
			{ID: "frame-0002", SceneID: "scene-0002", SegmentID: "segment-0002", TimestampMicros: 6_000_000, URL: "https://example.test/frames/2.jpg"},
		},
		Transcript: evidence.Transcript, Quality: evidence.Quality,
	}
}

func validResponse(input domain.VideoUnderstandingInputV1, reasonSuffix string) domain.VideoUnderstandingResponseV1 {
	return domain.VideoUnderstandingResponseV1{
		SchemaVersion: domain.VideoAnalysisSchemaV1,
		Candidates: []domain.VideoSegmentAnalysisV1{
			{
				SegmentID: "segment-0001", SceneID: "scene-0001", StartMicros: 0, EndMicros: 4_000_000,
				Description: "Friends laugh together after a joke", ImportanceScore: 0.95,
				VisualQualityScore: 0.35, MemoryValueScore: 0.98, EmotionalValueScore: 0.96, StoryRelevanceScore: 0.94,
				EmotionalTone: "joy", RecommendedUsage: domain.RecommendedUsageHero,
				Reason: "The personal laughter matters more than the slight camera shake " + reasonSuffix,
				People: []string{"friends"}, Activities: []string{"laughing"}, Interactions: []string{"shared joke"},
				Expressions: []string{"smiling"}, Speech: []string{"That was so funny."},
				FrameIDs: []string{"frame-0001"}, TranscriptSegmentIndexes: []int{0},
			},
			{
				SegmentID: "segment-0002", SceneID: "scene-0002", StartMicros: 4_000_000, EndMicros: 8_000_000,
				Description: "A technically clean view of an empty street", ImportanceScore: 0.25,
				VisualQualityScore: 0.95, MemoryValueScore: 0.15, EmotionalValueScore: 0.1, StoryRelevanceScore: 0.2,
				RecommendedUsage: domain.RecommendedUsageContext,
				Reason:           "Useful only as brief location context despite strong image quality " + reasonSuffix,
				Scenery:          []string{"empty street"}, FrameIDs: []string{"frame-0002"},
			},
		},
	}
}

type memoryRepository struct {
	mu      sync.Mutex
	records map[string]domain.VideoAnalysis
}

func newMemoryRepository() *memoryRepository {
	return &memoryRepository{records: make(map[string]domain.VideoAnalysis)}
}

func (r *memoryRepository) FindLatestVideoAnalysis(_ context.Context, userID, videoID, inputVersion, understandingVersion, provider, model, sourceETag string) (domain.VideoAnalysis, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var latest domain.VideoAnalysis
	for _, value := range r.records {
		if value.UserID == userID && value.VideoID == videoID && value.InputVersion == inputVersion &&
			value.UnderstandingVersion == understandingVersion && value.Provider == provider && value.Model == model &&
			value.SourceETag == sourceETag && (latest.ID == "" || value.CreatedAt.After(latest.CreatedAt)) {
			latest = value
		}
	}
	if latest.ID == "" {
		return domain.VideoAnalysis{}, domain.ErrNotFound
	}
	return latest, nil
}

func (r *memoryRepository) SaveVideoAnalysis(_ context.Context, analysis domain.VideoAnalysis) (domain.VideoAnalysis, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := analysisKey(analysis.UserID, analysis.VideoID, analysis.AnalysisVersion)
	if existing, ok := r.records[key]; ok {
		return existing, nil
	}
	r.records[key] = analysis
	return analysis, nil
}

func (r *memoryRepository) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.records)
}

func analysisKey(values ...string) string { return strings.Join(values, ":") }

type memoryArtifacts struct {
	mu    sync.Mutex
	calls int
}

func (a *memoryArtifacts) PresignGet(_ context.Context, key string, _ time.Duration) (string, error) {
	a.mu.Lock()
	a.calls++
	a.mu.Unlock()
	if key == "" {
		return "", errors.New("missing frame key")
	}
	return "https://evidence.example.test/" + key, nil
}

type fakeProvider struct {
	name          string
	model         string
	calls         int
	invalidSchema bool
	lastInput     domain.VideoUnderstandingInputV1
}

func (p *fakeProvider) Understand(_ context.Context, input domain.VideoUnderstandingInputV1) (domain.VideoUnderstandingResponseV1, error) {
	p.calls++
	p.lastInput = input
	response := validResponse(input, fmt.Sprintf("provider call %d", p.calls))
	if p.invalidSchema {
		response.SchemaVersion = "invalid"
	}
	return response, nil
}

func (p *fakeProvider) Name() string  { return p.name }
func (p *fakeProvider) Model() string { return p.model }
