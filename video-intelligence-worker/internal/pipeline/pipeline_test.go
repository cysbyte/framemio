package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/framemio/video-intelligence-worker/internal/domain"
	"github.com/framemio/video-intelligence-worker/internal/storage"
	"github.com/framemio/video-intelligence-worker/internal/transcription"
	"github.com/framemio/video-intelligence-worker/internal/understanding"
)

func TestProcessorCachesPreprocessingAndAnalysis(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 19, 10, 0, 0, 0, time.UTC)
	repository := newMemoryRepository(now)
	objects := newMemoryObjects()
	tools := &fakeTools{metadata: domain.VideoMetadata{
		DurationMicros: 12_000_000, Width: 1920, Height: 1080, Rotation: 90, FPS: 29.97,
		VideoCodec: "hevc", AudioCodec: "aac", BitRate: 12_000_000, FileSizeBytes: 1000, HasAudio: true,
	}, boundaries: []domain.SceneBoundary{{TimestampMicros: 4_000_000, Score: 0.6}, {TimestampMicros: 8_000_000, Score: 0.7}}}
	transcriber := &fakeTranscriber{}
	provider := &fakeUnderstandingProvider{failuresRemaining: 1}
	understandingService := &understanding.Service{
		Provider: provider, Repository: repository, Artifacts: objects,
		RetryAttempts: 2, RetryDelay: time.Nanosecond, FrameURLExpiry: time.Hour,
		Now: func() time.Time { return now },
	}
	processor := &Processor{
		Config: Config{
			WorkRoot: t.TempDir(), AnalysisVersion: "video-v1", SceneThreshold: 0.32, MaxSceneBoundaries: 100,
			Planner: PlannerConfig{
				MinSceneDuration: 750 * time.Millisecond, MaxSegmentDuration: 12 * time.Second,
				LongVideoThreshold: 5 * time.Minute, MaxFramesShort: 12, MaxFramesLong: 24,
			},
			DuplicateHashDistance: 4, StageRetries: 2, EnableTranscription: true,
			TranscriptionVersion: "transcription-v1", UnderstandingVersion: "video-understanding-v1",
			EvidenceURLExpiry: time.Hour,
		},
		Repository: repository, Objects: objects, Tools: tools, Transcription: transcriber, Understanding: understandingService,
		Now: func() time.Time { return now },
	}

	first := repository.addJob("job-1", false)
	result, err := processor.Process(context.Background(), first)
	if err != nil {
		t.Fatal(err)
	}
	if result.Reused || result.Analysis.EvidenceS3Key == "" || len(result.Analysis.Segments) == 0 {
		t.Fatalf("unexpected first result: %+v", result)
	}
	initialFrameCalls := tools.frameCalls
	initialAudioCalls := tools.audioCalls
	if initialFrameCalls == 0 || initialAudioCalls != 1 || transcriber.calls != 1 || provider.calls != 2 {
		t.Fatalf("unexpected processing calls: frames=%d audio=%d transcription=%d understanding=%d", initialFrameCalls, initialAudioCalls, transcriber.calls, provider.calls)
	}

	second := repository.addJob("job-2", false)
	reused, err := processor.Process(context.Background(), second)
	if err != nil {
		t.Fatal(err)
	}
	if !reused.Reused || reused.Analysis.ID != result.Analysis.ID {
		t.Fatalf("analysis was not reused: %+v", reused)
	}
	if tools.frameCalls != initialFrameCalls || tools.audioCalls != initialAudioCalls || transcriber.calls != 1 || provider.calls != 2 {
		t.Fatal("analysis reuse repeated preprocessing or provider work")
	}

	third := repository.addJob("job-3", true)
	forced, err := processor.Process(context.Background(), third)
	if err != nil {
		t.Fatal(err)
	}
	if forced.Reused || forced.Analysis.ID == result.Analysis.ID || forced.Analysis.AnalysisVersion == result.Analysis.AnalysisVersion ||
		forced.Analysis.Segments[0].Reasons[0] == result.Analysis.Segments[0].Reasons[0] || repository.analysisCount() != 2 {
		t.Fatalf("forced reanalysis did not append semantic output: before=%+v after=%+v", result.Analysis, forced.Analysis)
	}
	if tools.frameCalls != initialFrameCalls || tools.audioCalls != initialAudioCalls || transcriber.calls != 1 {
		t.Fatal("forced semantic reanalysis did not reuse deterministic preprocessing")
	}
	if provider.calls != 3 {
		t.Fatalf("understanding calls = %d, want 3", provider.calls)
	}
}

type memoryRepository struct {
	mu            sync.Mutex
	now           time.Time
	contexts      map[string]domain.JobContext
	analyses      map[string]domain.VideoAnalysis
	preprocessing map[string]domain.PreprocessingArtifact
	transcripts   map[string]domain.TranscriptionRecord
}

func newMemoryRepository(now time.Time) *memoryRepository {
	return &memoryRepository{
		now: now, contexts: make(map[string]domain.JobContext), analyses: make(map[string]domain.VideoAnalysis),
		preprocessing: make(map[string]domain.PreprocessingArtifact), transcripts: make(map[string]domain.TranscriptionRecord),
	}
}

func (r *memoryRepository) addJob(jobID string, force bool) domain.JobMessage {
	r.mu.Lock()
	defer r.mu.Unlock()
	configuration := map[string]string{"analysis_version": "video-v1"}
	if force {
		configuration["force_reanalyze"] = "true"
	}
	job := domain.ProcessingJob{
		ID: jobID, UserID: "user-1", ProjectID: "project-1", MediaID: "media-1", Type: "analysis",
		Status: "queued", Configuration: configuration, CreatedAt: r.now, UpdatedAt: r.now,
	}
	media := domain.MediaAsset{
		ID: "media-1", UserID: "user-1", Type: "video", Status: "ready", FileName: "IMG_1001.MOV",
		ContentType: "video/quicktime", FileSizeBytes: 1000, S3Key: "users/user-1/media/media-1/source.mov", ETag: "etag-1",
	}
	project := domain.StoryProject{ID: "project-1", UserID: "user-1", MediaIDs: []string{"media-1"}, Status: "analyzing", CreatedAt: r.now, UpdatedAt: r.now}
	r.contexts[jobID] = domain.JobContext{Job: job, Media: media, Project: project}
	return domain.JobMessage{JobID: jobID, ProjectID: project.ID, MediaID: media.ID, JobType: "analysis", Configuration: configuration}
}

func (r *memoryRepository) LoadJobContext(_ context.Context, message domain.JobMessage) (domain.JobContext, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	value, ok := r.contexts[message.JobID]
	if !ok {
		return domain.JobContext{}, domain.ErrNotFound
	}
	return value, nil
}

func (r *memoryRepository) FindLatestVideoAnalysis(_ context.Context, userID, videoID, inputVersion, understandingVersion, provider, model, sourceETag string) (domain.VideoAnalysis, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var latest domain.VideoAnalysis
	for _, value := range r.analyses {
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

func (r *memoryRepository) GetVideoAnalysis(_ context.Context, userID, videoID, analysisVersion string) (domain.VideoAnalysis, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	value, ok := r.analyses[cacheKey(userID, videoID, analysisVersion)]
	if !ok {
		return domain.VideoAnalysis{}, domain.ErrNotFound
	}
	return value, nil
}

func (r *memoryRepository) SaveVideoAnalysis(_ context.Context, analysis domain.VideoAnalysis) (domain.VideoAnalysis, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := cacheKey(analysis.UserID, analysis.VideoID, analysis.AnalysisVersion)
	if existing, ok := r.analyses[key]; ok {
		return existing, nil
	}
	r.analyses[key] = analysis
	return analysis, nil
}

func (r *memoryRepository) analysisCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.analyses)
}

func (r *memoryRepository) GetTranscript(_ context.Context, userID, mediaID, version, sourceETag string) (domain.TranscriptionRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	value, ok := r.transcripts[cacheKey(userID, mediaID, version, sourceETag)]
	if !ok {
		return domain.TranscriptionRecord{}, domain.ErrNotFound
	}
	return value, nil
}

func (r *memoryRepository) SaveTranscript(_ context.Context, transcript domain.TranscriptionRecord) (domain.TranscriptionRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := cacheKey(transcript.UserID, transcript.MediaID, transcript.Version, transcript.SourceETag)
	if existing, ok := r.transcripts[key]; ok {
		return existing, nil
	}
	r.transcripts[key] = transcript
	return transcript, nil
}

func (r *memoryRepository) GetPreprocessing(_ context.Context, userID, mediaID, version, sourceETag string) (domain.PreprocessingArtifact, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	value, ok := r.preprocessing[cacheKey(userID, mediaID, version, sourceETag)]
	if !ok {
		return domain.PreprocessingArtifact{}, domain.ErrNotFound
	}
	return value, nil
}

func (r *memoryRepository) SavePreprocessing(_ context.Context, artifact domain.PreprocessingArtifact) (domain.PreprocessingArtifact, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := cacheKey(artifact.UserID, artifact.MediaID, artifact.AnalysisVersion, artifact.SourceETag)
	if existing, ok := r.preprocessing[key]; ok {
		return existing, nil
	}
	r.preprocessing[key] = artifact
	return artifact, nil
}

func (r *memoryRepository) StartJobAttempt(_ context.Context, jobID string) (domain.ProcessingJob, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	value := r.contexts[jobID]
	value.Job.Status = "running"
	value.Job.Attempt++
	value.Job.Progress = max(value.Job.Progress, 0.02)
	r.contexts[jobID] = value
	return value.Job, nil
}

func (r *memoryRepository) UpdateJob(_ context.Context, jobID, status string, progress float64, errorCode, errorMessage, artifactS3Key string) (domain.ProcessingJob, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	value := r.contexts[jobID]
	value.Job.Status = status
	value.Job.Progress = max(value.Job.Progress, progress)
	value.Job.ErrorCode = errorCode
	value.Job.ErrorMessage = errorMessage
	if artifactS3Key != "" {
		value.Job.ArtifactS3Key = artifactS3Key
	}
	r.contexts[jobID] = value
	return value.Job, nil
}

func (r *memoryRepository) SetProjectStatus(_ context.Context, userID, projectID, status string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for key, value := range r.contexts {
		if value.Project.UserID == userID && value.Project.ID == projectID {
			value.Project.Status = status
			r.contexts[key] = value
		}
	}
	return nil
}

type memoryObjects struct {
	mu      sync.Mutex
	objects map[string][]byte
}

func newMemoryObjects() *memoryObjects {
	return &memoryObjects{objects: map[string][]byte{"users/user-1/media/media-1/source.mov": []byte("source")}}
}

func (s *memoryObjects) Head(_ context.Context, key string) (storage.SourceInfo, error) {
	if key == "users/user-1/media/media-1/source.mov" {
		return storage.SourceInfo{SizeBytes: 1000, ETag: "etag-1"}, nil
	}
	return storage.SourceInfo{}, errors.New("not found")
}

func (s *memoryObjects) DownloadFile(_ context.Context, key, destinationPath string) error {
	s.mu.Lock()
	data, ok := s.objects[key]
	s.mu.Unlock()
	if !ok {
		return errors.New("not found")
	}
	return os.WriteFile(destinationPath, data, 0o600)
}

func (s *memoryObjects) UploadFile(_ context.Context, key, sourcePath, _ string) error {
	data, err := os.ReadFile(sourcePath)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.objects[key] = data
	s.mu.Unlock()
	return nil
}

func (s *memoryObjects) PutJSON(_ context.Context, key string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.objects[key] = data
	s.mu.Unlock()
	return nil
}

func (s *memoryObjects) GetJSON(_ context.Context, key string, destination any) error {
	s.mu.Lock()
	data, ok := s.objects[key]
	s.mu.Unlock()
	if !ok {
		return errors.New("not found")
	}
	return json.Unmarshal(data, destination)
}

func (s *memoryObjects) PresignGet(_ context.Context, key string, _ time.Duration) (string, error) {
	s.mu.Lock()
	_, ok := s.objects[key]
	s.mu.Unlock()
	if !ok {
		return "", errors.New("not found")
	}
	return "https://evidence.example.test/" + key, nil
}

type fakeTools struct {
	metadata   domain.VideoMetadata
	boundaries []domain.SceneBoundary
	frameCalls int
	audioCalls int
}

func (t *fakeTools) Probe(context.Context, string) (domain.VideoMetadata, error) {
	return t.metadata, nil
}
func (t *fakeTools) DetectScenes(context.Context, string, float64, int) ([]domain.SceneBoundary, error) {
	return append([]domain.SceneBoundary(nil), t.boundaries...), nil
}
func (t *fakeTools) ExtractFrame(_ context.Context, _ string, timestampMicros int64, destinationPath string) error {
	t.frameCalls++
	value := image.NewRGBA(image.Rect(0, 0, 96, 64))
	cutoff := int((timestampMicros/250_000)%80) + 8
	for y := 0; y < 64; y++ {
		for x := 0; x < 96; x++ {
			if x < cutoff {
				value.Set(x, y, color.RGBA{R: 230, G: 180, B: 80, A: 255})
			} else {
				value.Set(x, y, color.RGBA{R: 30, G: 90, B: 190, A: 255})
			}
		}
	}
	file, err := os.Create(destinationPath)
	if err != nil {
		return err
	}
	defer file.Close()
	return jpeg.Encode(file, value, &jpeg.Options{Quality: 90})
}
func (t *fakeTools) ExtractAudio(_ context.Context, _, destinationPath string) error {
	t.audioCalls++
	return os.WriteFile(destinationPath, []byte("audio"), 0o600)
}

type fakeTranscriber struct{ calls int }

func (s *fakeTranscriber) Transcribe(_ context.Context, request transcription.Request) (transcription.Result, error) {
	s.calls++
	transcript := domain.Transcript{
		Language: "en", Text: "Happy birthday", HasSpeech: true,
		Segments: []domain.TranscriptSegment{{StartMicros: 1_000_000, EndMicros: 2_000_000, Text: "Happy birthday", Confidence: 0.98}},
	}
	return transcription.Result{Record: domain.TranscriptionRecord{
		ID: "transcript-1", UserID: request.UserID, ProjectID: request.ProjectID, MediaID: request.MediaID,
		SourceETag: request.SourceETag, Version: request.Version, Provider: "fake", ProviderModel: "fake-v1",
		Language: transcript.Language, HasSpeech: true, SegmentCount: 1, DurationMicros: request.DurationMicros, Transcript: transcript,
	}}, nil
}

type fakeUnderstandingProvider struct {
	calls             int
	failuresRemaining int
}

func (m *fakeUnderstandingProvider) Understand(_ context.Context, input domain.VideoUnderstandingInputV1) (domain.VideoUnderstandingResponseV1, error) {
	m.calls++
	if m.failuresRemaining > 0 {
		m.failuresRemaining--
		return domain.VideoUnderstandingResponseV1{}, errors.New("temporary provider error")
	}
	if len(input.Frames) == 0 {
		return domain.VideoUnderstandingResponseV1{}, errors.New("missing frames")
	}
	candidates := make([]domain.VideoSegmentAnalysisV1, 0, len(input.Segments))
	for _, segment := range input.Segments {
		candidate := domain.VideoSegmentAnalysisV1{
			SegmentID: segment.ID, SceneID: segment.SceneID, StartMicros: segment.StartMicros, EndMicros: segment.EndMicros,
			Description: "Available evidence for this candidate segment", ImportanceScore: 0.4,
			VisualQualityScore: 0.6, MemoryValueScore: 0.4, EmotionalValueScore: 0.3, StoryRelevanceScore: 0.4,
			RecommendedUsage: domain.RecommendedUsageExclude, Reason: fmt.Sprintf("Grounded analysis call %d with limited evidence", m.calls),
		}
		for _, frame := range input.Frames {
			if frame.SegmentID == segment.ID {
				candidate.Description = "Friends celebrate together"
				candidate.ImportanceScore = 0.95
				candidate.MemoryValueScore = 0.98
				candidate.EmotionalValueScore = 0.96
				candidate.StoryRelevanceScore = 0.92
				candidate.EmotionalTone = "joy"
				candidate.RecommendedUsage = domain.RecommendedUsageHero
				candidate.People = []string{"friends"}
				candidate.Activities = []string{"celebrating"}
				candidate.FrameIDs = []string{frame.ID}
				break
			}
		}
		for index, transcript := range input.Transcript.Segments {
			if transcript.EndMicros > segment.StartMicros && transcript.StartMicros < segment.EndMicros {
				candidate.Speech = []string{transcript.Text}
				candidate.TranscriptSegmentIndexes = []int{index}
				break
			}
		}
		candidates = append(candidates, candidate)
	}
	return domain.VideoUnderstandingResponseV1{SchemaVersion: domain.VideoAnalysisSchemaV1, Candidates: candidates}, nil
}
func (m *fakeUnderstandingProvider) Name() string  { return "fake-provider" }
func (m *fakeUnderstandingProvider) Model() string { return "fake-model" }

func cacheKey(values ...string) string { return filepath.Join(values...) }
