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

func TestValidateResponseV1RepairsUngroundedCandidates(t *testing.T) {
	t.Parallel()
	input := testInput()
	response := validResponse(input, "repair")
	response.Candidates[0].FrameIDs = []string{"invented-frame", "frame-0002"}
	response.Candidates[0].TranscriptSegmentIndexes = nil

	validated, err := ValidateResponseV1(response, input)
	if err != nil {
		t.Fatal(err)
	}
	candidate := validated.Candidates[0]
	if candidate.RecommendedUsage != domain.RecommendedUsageExclude {
		t.Fatalf("ungrounded candidate usage = %s, want exclude", candidate.RecommendedUsage)
	}
	if len(candidate.FrameIDs) != 0 || len(candidate.TranscriptSegmentIndexes) != 0 {
		t.Fatalf("ungrounded candidate kept invalid evidence: %+v", candidate)
	}
	if len(candidate.People) != 0 || len(candidate.Activities) != 0 || len(candidate.Speech) != 0 {
		t.Fatalf("ungrounded candidate kept unsupported claims: %+v", candidate)
	}
	if candidate.StoryRelevanceScore > 0.05 {
		t.Fatalf("ungrounded candidate score was not capped: %+v", candidate)
	}
}

func TestValidateResponseV1KeepsValidFrameWhenSomeReferencesAreInvalid(t *testing.T) {
	t.Parallel()
	input := testInput()
	response := validResponse(input, "partial repair")
	response.Candidates[0].FrameIDs = []string{"invented-frame", "frame-0002", "frame-0001"}

	validated, err := ValidateResponseV1(response, input)
	if err != nil {
		t.Fatal(err)
	}
	candidate := validated.Candidates[0]
	if got, want := candidate.FrameIDs, []string{"frame-0001"}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("frame ids = %v, want %v", got, want)
	}
	if candidate.RecommendedUsage != domain.RecommendedUsageHero {
		t.Fatalf("grounded candidate usage changed to %s", candidate.RecommendedUsage)
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
		{name: "unknown transcript", mutate: func(value *domain.VideoUnderstandingResponseV1) {
			value.Candidates[0].TranscriptSegmentIndexes = []int{99}
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
	if _, err := service.Analyze(context.Background(), testRequest()); err == nil || !domain.IsPermanent(err) {
		t.Fatalf("expected invalid provider response to fail permanently, got %v", err)
	}
	if provider.calls != 2 || repository.count() != 0 {
		t.Fatalf("invalid response was not retried safely: calls=%d records=%d", provider.calls, repository.count())
	}
}

func TestServicePresignsSourceForSourceAwareProvider(t *testing.T) {
	t.Parallel()
	repository := newMemoryRepository()
	artifacts := &memoryArtifacts{}
	provider := &sourceAwareFakeProvider{name: "gemini-agentic", model: "gemini-3.7-flash"}
	service := newService(provider, repository, artifacts)

	if _, err := service.Analyze(context.Background(), testRequest()); err != nil {
		t.Fatal(err)
	}
	if provider.videoURL != "https://evidence.example.test/users/user-1/media/media-1/source.mov" {
		t.Fatalf("source-aware provider URL = %q", provider.videoURL)
	}
	if artifacts.calls != len(testRequest().Evidence.Frames)+1 {
		t.Fatalf("presign calls = %d, want %d", artifacts.calls, len(testRequest().Evidence.Frames)+1)
	}
}

func TestBuildUnderstandingBatchesPreservesContiguousEvidenceAndTranscriptIndexes(t *testing.T) {
	t.Parallel()
	input := batchTestInput(5)

	batches, err := buildUnderstandingBatches(input, 10, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(batches) != 3 {
		t.Fatalf("batch count = %d, want 3", len(batches))
	}
	wantSegments := [][]string{
		{"segment-0001", "segment-0002"},
		{"segment-0003", "segment-0004"},
		{"segment-0005"},
	}
	wantTranscriptIndexes := [][]int{{0, 1}, {2, 3}, {4}}
	for index, batch := range batches {
		if got := segmentIDs(batch.input.Segments); fmt.Sprint(got) != fmt.Sprint(wantSegments[index]) {
			t.Fatalf("batch %d segments = %v, want %v", index+1, got, wantSegments[index])
		}
		if got := frameIDsFromInput(batch.input.Frames); len(got) != len(batch.input.Segments) {
			t.Fatalf("batch %d frame count = %d, want %d", index+1, len(got), len(batch.input.Segments))
		}
		if fmt.Sprint(batch.transcriptIndexes) != fmt.Sprint(wantTranscriptIndexes[index]) {
			t.Fatalf("batch %d transcript indexes = %v, want %v", index+1, batch.transcriptIndexes, wantTranscriptIndexes[index])
		}
	}
}

func TestServiceRetriesOnlyFailedUnderstandingBatch(t *testing.T) {
	t.Parallel()
	repository := newMemoryRepository()
	provider := &batchRecordingProvider{
		name: "provider-a", model: "model-a", failSegmentID: "segment-0003", failures: 1,
	}
	service := newService(provider, repository, &memoryArtifacts{})
	service.BatchMaxScenes = 10
	service.BatchMaxFrames = 2

	result, err := service.Analyze(context.Background(), batchTestRequest(5))
	if err != nil {
		t.Fatal(err)
	}
	if len(provider.calls) != 4 {
		t.Fatalf("provider call count = %d, want 4 (three batches plus one retry)", len(provider.calls))
	}
	if provider.sceneCalls != 0 {
		t.Fatalf("remote scene detection calls = %d, want 0", provider.sceneCalls)
	}
	wantCalls := [][]string{
		{"segment-0001", "segment-0002"},
		{"segment-0003", "segment-0004"},
		{"segment-0003", "segment-0004"},
		{"segment-0005"},
	}
	for index, call := range provider.calls {
		if fmt.Sprint(call) != fmt.Sprint(wantCalls[index]) {
			t.Fatalf("provider call %d segments = %v, want %v", index+1, call, wantCalls[index])
		}
	}
	if got := segmentIDsFromAnalysis(result.Analysis); fmt.Sprint(got) != fmt.Sprint(segmentIDs(batchTestInput(5).Segments)) {
		t.Fatalf("merged segment IDs = %v", got)
	}
}

func newService(provider VideoUnderstandingProvider, repository Repository, artifacts ArtifactStore) *Service {
	return &Service{
		Provider: provider, Repository: repository, Artifacts: artifacts,
		RetryAttempts: 2, RetryDelay: time.Nanosecond, FrameURLExpiry: time.Hour,
		Now: func() time.Time { return time.Date(2026, time.August, 19, 14, 0, 0, 0, time.UTC) },
	}
}

func batchTestRequest(segmentCount int) Request {
	evidence := batchTestEvidence(segmentCount)
	return Request{
		CacheRequest: CacheRequest{
			UserID: "user-1", MediaID: evidence.MediaID, SourceETag: evidence.SourceETag,
			InputVersion: evidence.AnalysisVersion, UnderstandingVersion: "video-understanding-v1",
		},
		ProjectID: evidence.ProjectID, EvidenceS3Key: "users/user-1/video-analysis/media-1/evidence.json", Evidence: evidence,
	}
}

func batchTestEvidence(segmentCount int) domain.VideoEvidence {
	segments := make([]domain.CandidateSegment, 0, segmentCount)
	frames := make([]domain.SelectedFrame, 0, segmentCount)
	transcriptSegments := make([]domain.TranscriptSegment, 0, segmentCount)
	for index := 0; index < segmentCount; index++ {
		start := int64(index) * 4_000_000
		segmentID := fmt.Sprintf("segment-%04d", index+1)
		sceneID := fmt.Sprintf("scene-%04d", index/2+1)
		segments = append(segments, domain.CandidateSegment{
			ID: segmentID, SceneID: sceneID, StartMicros: start, EndMicros: start + 4_000_000,
		})
		frames = append(frames, domain.SelectedFrame{
			ID: fmt.Sprintf("frame-%04d", index+1), MediaID: "media-1", SceneID: sceneID, SegmentID: segmentID,
			TimestampMicros: start + 2_000_000, S3Key: fmt.Sprintf("frames/%d.jpg", index+1), QualityScore: 0.8,
		})
		transcriptSegments = append(transcriptSegments, domain.TranscriptSegment{
			StartMicros: start + 1_000_000, EndMicros: start + 2_000_000, Text: fmt.Sprintf("line %d", index+1),
		})
	}
	duration := int64(segmentCount) * 4_000_000
	scenes := make([]domain.VideoScene, 0, (segmentCount+1)/2)
	for index := 0; index < segmentCount; index += 2 {
		end := int64(minInt(segmentCount, index+2)) * 4_000_000
		scenes = append(scenes, domain.VideoScene{
			ID: fmt.Sprintf("scene-%04d", index/2+1), StartMicros: int64(index) * 4_000_000, EndMicros: end,
		})
	}
	return domain.VideoEvidence{
		SchemaVersion: 1, AnalysisVersion: "video-evidence-v1", MediaID: "media-1", ProjectID: "project-1",
		SourceS3Key: "users/user-1/media/media-1/source.mov", SourceETag: "etag-batches",
		Metadata: domain.VideoMetadata{DurationMicros: duration, Width: 1920, Height: 1080, FPS: 30, VideoCodec: "hevc"},
		Scenes:   scenes, Segments: segments, Frames: frames,
		Transcript: domain.Transcript{Language: "en", HasSpeech: true, Segments: transcriptSegments},
		Quality:    domain.QualitySignals{OverallScore: 0.8},
	}
}

func batchTestInput(segmentCount int) domain.VideoUnderstandingInputV1 {
	evidence := batchTestEvidence(segmentCount)
	frames := make([]domain.VideoUnderstandingFrameV1, 0, len(evidence.Frames))
	for _, frame := range evidence.Frames {
		frames = append(frames, domain.VideoUnderstandingFrameV1{
			ID: frame.ID, SceneID: frame.SceneID, SegmentID: frame.SegmentID,
			TimestampMicros: frame.TimestampMicros, URL: "https://example.test/" + frame.S3Key,
			QualityScore: frame.QualityScore,
		})
	}
	return domain.VideoUnderstandingInputV1{
		SchemaVersion: domain.VideoUnderstandingInputSchemaV1,
		InputVersion:  evidence.AnalysisVersion, AnalysisVersion: "video-understanding-v1",
		Objective: Objective, GroundingRules: append([]string(nil), groundingRules...),
		MediaID: evidence.MediaID, ProjectID: evidence.ProjectID, Metadata: evidence.Metadata,
		Scenes: evidence.Scenes, Segments: evidence.Segments, Frames: frames,
		Transcript: evidence.Transcript, Quality: evidence.Quality,
	}
}

func segmentIDs(segments []domain.CandidateSegment) []string {
	ids := make([]string, 0, len(segments))
	for _, segment := range segments {
		ids = append(ids, segment.ID)
	}
	return ids
}

func frameIDsFromInput(frames []domain.VideoUnderstandingFrameV1) []string {
	ids := make([]string, 0, len(frames))
	for _, frame := range frames {
		ids = append(ids, frame.ID)
	}
	return ids
}

func segmentIDsFromAnalysis(analysis domain.VideoAnalysis) []string {
	ids := make([]string, 0, len(analysis.Segments))
	for _, segment := range analysis.Segments {
		ids = append(ids, segment.SegmentID)
	}
	return ids
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
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

type batchRecordingProvider struct {
	name          string
	model         string
	failSegmentID string
	failures      int
	calls         [][]string
	sceneCalls    int
}

func (p *batchRecordingProvider) Understand(_ context.Context, input domain.VideoUnderstandingInputV1) (domain.VideoUnderstandingResponseV1, error) {
	ids := segmentIDs(input.Segments)
	p.calls = append(p.calls, append([]string(nil), ids...))
	if p.failures > 0 {
		for _, id := range ids {
			if id == p.failSegmentID {
				p.failures--
				return domain.VideoUnderstandingResponseV1{}, errors.New("transient batch failure")
			}
		}
	}
	return genericValidResponse(input), nil
}

func (p *batchRecordingProvider) Name() string  { return p.name }
func (p *batchRecordingProvider) Model() string { return p.model }

func (p *batchRecordingProvider) DetectScenes(_ context.Context, _ domain.VideoUnderstandingInputV1) (domain.VideoSceneDetectionResponseV1, error) {
	p.sceneCalls++
	return domain.VideoSceneDetectionResponseV1{}, errors.New("remote scene detection should not be called")
}

func genericValidResponse(input domain.VideoUnderstandingInputV1) domain.VideoUnderstandingResponseV1 {
	candidates := make([]domain.VideoSegmentAnalysisV1, 0, len(input.Segments))
	for _, segment := range input.Segments {
		var frameIDs []string
		for _, frame := range input.Frames {
			if frame.SegmentID == segment.ID {
				frameIDs = append(frameIDs, frame.ID)
			}
		}
		candidate := domain.VideoSegmentAnalysisV1{
			SegmentID: segment.ID, SceneID: segment.SceneID, StartMicros: segment.StartMicros, EndMicros: segment.EndMicros,
			Description: "A grounded video moment", ImportanceScore: 0.7, VisualQualityScore: 0.7,
			MemoryValueScore: 0.7, EmotionalValueScore: 0.7, StoryRelevanceScore: 0.7,
			RecommendedUsage: domain.RecommendedUsageSupporting, Reason: "Grounded in supplied evidence.", FrameIDs: frameIDs,
		}
		for index, transcript := range input.Transcript.Segments {
			if transcript.EndMicros > segment.StartMicros && transcript.StartMicros < segment.EndMicros {
				candidate.TranscriptSegmentIndexes = append(candidate.TranscriptSegmentIndexes, index)
			}
		}
		candidates = append(candidates, candidate)
	}
	return domain.VideoUnderstandingResponseV1{SchemaVersion: domain.VideoAnalysisSchemaV1, Candidates: candidates}
}

type sourceAwareFakeProvider struct {
	name     string
	model    string
	videoURL string
}

func (p *sourceAwareFakeProvider) Understand(_ context.Context, _ domain.VideoUnderstandingInputV1) (domain.VideoUnderstandingResponseV1, error) {
	return domain.VideoUnderstandingResponseV1{}, errors.New("source-aware path was not used")
}

func (p *sourceAwareFakeProvider) UnderstandVideo(_ context.Context, input domain.VideoUnderstandingInputV1, videoURL string) (domain.VideoUnderstandingResponseV1, error) {
	p.videoURL = videoURL
	return validResponse(input, "source-aware"), nil
}

func (p *sourceAwareFakeProvider) Name() string  { return p.name }
func (p *sourceAwareFakeProvider) Model() string { return p.model }
