package understanding

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"sort"
	"strings"
	"time"

	analysisvalidation "github.com/framemio/video-intelligence-worker/internal/analysis"
	"github.com/framemio/video-intelligence-worker/internal/domain"
)

const Objective = "Identify the moments in this everyday iPhone video that are meaningful, interesting, visually useful, or emotionally valuable for a personal memory video."

var groundingRules = []string{
	"Return exactly one candidate analysis for every supplied candidate segment.",
	"Copy segment IDs, scene IDs, and timestamp bounds exactly from the supplied evidence.",
	"Never invent people, actions, expressions, objects, food, locations, events, speech, audio, or timestamps.",
	"Ground visual claims in supplied frame IDs and speech claims in supplied transcript segment indexes.",
	"Keep technical visual quality separate from memory value, emotional value, importance, and story relevance.",
	"A technically imperfect personal interaction may have high memory value.",
	"When evidence is insufficient, recommend exclusion and state that limitation rather than guessing.",
}

var requiredCandidateFieldsV1 = []string{
	"segment_id", "scene_id", "start_micros", "end_micros", "description",
	"importance_score", "visual_quality_score", "memory_value_score", "emotional_value_score",
	"story_relevance_score", "recommended_usage", "reason",
}

var requiredSceneFieldsV1 = []string{"segment_ids", "description", "reason"}

type VideoUnderstandingProvider interface {
	Understand(ctx context.Context, input domain.VideoUnderstandingInputV1) (domain.VideoUnderstandingResponseV1, error)
	Name() string
	Model() string
}

// VideoFileUnderstandingProvider is implemented by providers that need the
// original video in addition to the deterministic evidence envelope.
type VideoFileUnderstandingProvider interface {
	UnderstandVideo(ctx context.Context, input domain.VideoUnderstandingInputV1, videoURL string) (domain.VideoUnderstandingResponseV1, error)
}

type VideoSceneDetectionProvider interface {
	DetectScenes(ctx context.Context, input domain.VideoUnderstandingInputV1) (domain.VideoSceneDetectionResponseV1, error)
	Name() string
	Model() string
}

type SemanticSceneProvider interface {
	VideoSceneDetectionProvider
	VideoUnderstandingProvider
}

type Repository interface {
	FindLatestVideoAnalysis(ctx context.Context, userID, videoID, inputVersion, understandingVersion, provider, model, sourceETag string) (domain.VideoAnalysis, error)
	SaveVideoAnalysis(ctx context.Context, analysis domain.VideoAnalysis) (domain.VideoAnalysis, error)
}

type ArtifactStore interface {
	PresignGet(ctx context.Context, key string, expiry time.Duration) (string, error)
}

type CacheRequest struct {
	UserID               string
	MediaID              string
	SourceETag           string
	InputVersion         string
	UnderstandingVersion string
}

type Request struct {
	CacheRequest
	ProjectID     string
	EvidenceS3Key string
	Evidence      domain.VideoEvidence
	RunID         string
	Force         bool
}

type Result struct {
	Analysis domain.VideoAnalysis
	Reused   bool
}

type Analyzer interface {
	FindCached(ctx context.Context, request CacheRequest) (Result, error)
	Analyze(ctx context.Context, request Request) (Result, error)
}

type Service struct {
	Provider       VideoUnderstandingProvider
	Repository     Repository
	Artifacts      ArtifactStore
	RetryAttempts  int
	RetryDelay     time.Duration
	FrameURLExpiry time.Duration
	BatchMaxScenes int
	BatchMaxFrames int
	Logger         *slog.Logger
	Now            func() time.Time
}

type sceneDetectionResult struct {
	input    domain.VideoUnderstandingInputV1
	evidence domain.VideoEvidence
	err      error
}

type understandingBatch struct {
	input             domain.VideoUnderstandingInputV1
	transcriptIndexes []int
	frameCount        int
	sceneCount        int
}

func DecodeResponseV1(data []byte) (domain.VideoUnderstandingResponseV1, error) {
	var response domain.VideoUnderstandingResponseV1
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&response); err != nil {
		return domain.VideoUnderstandingResponseV1{}, fmt.Errorf("decode %s response: %w", domain.VideoAnalysisSchemaV1, err)
	}
	if err := ensureJSONEnd(decoder); err != nil {
		return domain.VideoUnderstandingResponseV1{}, err
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(data, &document); err != nil {
		return domain.VideoUnderstandingResponseV1{}, err
	}
	if missingJSONField(document, "schema_version") || missingJSONField(document, "candidates") {
		return domain.VideoUnderstandingResponseV1{}, fmt.Errorf("%w: schema_version and candidates are required", domain.ErrInvalidModelOutput)
	}
	var candidates []map[string]json.RawMessage
	if err := json.Unmarshal(document["candidates"], &candidates); err != nil {
		return domain.VideoUnderstandingResponseV1{}, fmt.Errorf("%w: candidates must be an array", domain.ErrInvalidModelOutput)
	}
	for index, candidate := range candidates {
		for _, field := range requiredCandidateFieldsV1 {
			if missingJSONField(candidate, field) {
				return domain.VideoUnderstandingResponseV1{}, fmt.Errorf("%w: candidate %d is missing required field %s", domain.ErrInvalidModelOutput, index, field)
			}
		}
	}
	return response, nil
}

func DecodeSceneResponseV1(data []byte) (domain.VideoSceneDetectionResponseV1, error) {
	var response domain.VideoSceneDetectionResponseV1
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&response); err != nil {
		return domain.VideoSceneDetectionResponseV1{}, fmt.Errorf("decode %s response: %w", domain.VideoSceneAnalysisSchemaV1, err)
	}
	if err := ensureJSONEnd(decoder); err != nil {
		return domain.VideoSceneDetectionResponseV1{}, err
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(data, &document); err != nil {
		return domain.VideoSceneDetectionResponseV1{}, err
	}
	if missingJSONField(document, "schema_version") || missingJSONField(document, "scenes") {
		return domain.VideoSceneDetectionResponseV1{}, fmt.Errorf("%w: schema_version and scenes are required", domain.ErrInvalidModelOutput)
	}
	var scenes []map[string]json.RawMessage
	if err := json.Unmarshal(document["scenes"], &scenes); err != nil {
		return domain.VideoSceneDetectionResponseV1{}, fmt.Errorf("%w: scenes must be an array", domain.ErrInvalidModelOutput)
	}
	for index, scene := range scenes {
		for _, field := range requiredSceneFieldsV1 {
			if missingJSONField(scene, field) {
				return domain.VideoSceneDetectionResponseV1{}, fmt.Errorf("%w: scene %d is missing required field %s", domain.ErrInvalidModelOutput, index, field)
			}
		}
	}
	return response, nil
}

func ValidateSceneResponseV1(response domain.VideoSceneDetectionResponseV1, input domain.VideoUnderstandingInputV1) (domain.VideoSceneDetectionResponseV1, error) {
	if response.SchemaVersion != domain.VideoSceneAnalysisSchemaV1 {
		return domain.VideoSceneDetectionResponseV1{}, fmt.Errorf("%w: schema_version must be %s", domain.ErrInvalidModelOutput, domain.VideoSceneAnalysisSchemaV1)
	}
	if len(response.Scenes) == 0 {
		return domain.VideoSceneDetectionResponseV1{}, fmt.Errorf("%w: scenes are required", domain.ErrInvalidModelOutput)
	}
	segmentIndex := make(map[string]int, len(input.Segments))
	segmentByID := make(map[string]domain.CandidateSegment, len(input.Segments))
	frameByID := make(map[string]domain.VideoUnderstandingFrameV1, len(input.Frames))
	for index, segment := range input.Segments {
		segmentIndex[segment.ID] = index
		segmentByID[segment.ID] = segment
	}
	for _, frame := range input.Frames {
		frameByID[frame.ID] = frame
	}
	assigned := make(map[string]struct{}, len(input.Segments))
	for sceneIndex, sceneGroup := range response.Scenes {
		if len(sceneGroup.SegmentIDs) == 0 {
			return domain.VideoSceneDetectionResponseV1{}, fmt.Errorf("%w: scene %d requires segment_ids", domain.ErrInvalidModelOutput, sceneIndex)
		}
		if strings.TrimSpace(sceneGroup.Description) == "" || strings.TrimSpace(sceneGroup.Reason) == "" {
			return domain.VideoSceneDetectionResponseV1{}, fmt.Errorf("%w: scene %d requires description and reason", domain.ErrInvalidModelOutput, sceneIndex)
		}
		positions := make([]int, 0, len(sceneGroup.SegmentIDs))
		seenInScene := make(map[string]struct{}, len(sceneGroup.SegmentIDs))
		for _, segmentID := range sceneGroup.SegmentIDs {
			segment, exists := segmentByID[segmentID]
			if !exists {
				return domain.VideoSceneDetectionResponseV1{}, fmt.Errorf("%w: scene %d references unknown segment %s", domain.ErrInvalidModelOutput, sceneIndex, segmentID)
			}
			if _, duplicate := assigned[segmentID]; duplicate {
				return domain.VideoSceneDetectionResponseV1{}, fmt.Errorf("%w: segment %s appears in more than one scene", domain.ErrInvalidModelOutput, segmentID)
			}
			if _, duplicate := seenInScene[segmentID]; duplicate {
				return domain.VideoSceneDetectionResponseV1{}, fmt.Errorf("%w: scene %d duplicates segment %s", domain.ErrInvalidModelOutput, sceneIndex, segmentID)
			}
			seenInScene[segmentID] = struct{}{}
			positions = append(positions, segmentIndex[segmentID])
			_ = segment
		}
		sort.Ints(positions)
		for index := 1; index < len(positions); index++ {
			if positions[index] != positions[index-1]+1 {
				return domain.VideoSceneDetectionResponseV1{}, fmt.Errorf("%w: scene %d segment_ids must be contiguous", domain.ErrInvalidModelOutput, sceneIndex)
			}
		}
		for _, segmentID := range sceneGroup.SegmentIDs {
			assigned[segmentID] = struct{}{}
		}
		for _, frameID := range sceneGroup.FrameIDs {
			frame, exists := frameByID[frameID]
			if !exists {
				return domain.VideoSceneDetectionResponseV1{}, fmt.Errorf("%w: scene %d references unknown frame %s", domain.ErrInvalidModelOutput, sceneIndex, frameID)
			}
			if _, exists := segmentByID[frame.SegmentID]; !exists {
				return domain.VideoSceneDetectionResponseV1{}, fmt.Errorf("%w: scene %d references frame %s from unknown segment", domain.ErrInvalidModelOutput, sceneIndex, frameID)
			}
		}
	}
	if len(assigned) != len(input.Segments) {
		return domain.VideoSceneDetectionResponseV1{}, fmt.Errorf("%w: scene detection must cover every segment", domain.ErrInvalidModelOutput)
	}
	return response, nil
}

func remapForSceneDetection(input domain.VideoUnderstandingInputV1, evidence domain.VideoEvidence, response domain.VideoSceneDetectionResponseV1) (domain.VideoUnderstandingInputV1, domain.VideoEvidence, error) {
	validated, err := ValidateSceneResponseV1(response, input)
	if err != nil {
		return domain.VideoUnderstandingInputV1{}, domain.VideoEvidence{}, err
	}
	segmentIndex := make(map[string]int, len(input.Segments))
	for index, segment := range input.Segments {
		segmentIndex[segment.ID] = index
	}
	remappedInput := input
	remappedEvidence := evidence
	remappedScenes := make([]domain.VideoScene, 0, len(validated.Scenes))
	remappedSegments := make([]domain.CandidateSegment, len(input.Segments))
	remappedFrames := make([]domain.VideoUnderstandingFrameV1, len(input.Frames))
	remappedEvidenceFrames := make([]domain.SelectedFrame, len(evidence.Frames))
	for index, sceneGroup := range validated.Scenes {
		sceneID := fmt.Sprintf("scene-%04d", index+1)
		positions := make([]int, 0, len(sceneGroup.SegmentIDs))
		for _, segmentID := range sceneGroup.SegmentIDs {
			positions = append(positions, segmentIndex[segmentID])
		}
		sort.Ints(positions)
		start := input.Segments[positions[0]].StartMicros
		end := input.Segments[positions[len(positions)-1]].EndMicros
		representativeFrames := make([]domain.Frame, 0)
		for _, position := range positions {
			segment := input.Segments[position]
			remappedSegment := segment
			remappedSegment.SceneID = sceneID
			remappedSegments[position] = remappedSegment
			for frameIndex, frame := range input.Frames {
				if frame.SegmentID != segment.ID {
					continue
				}
				remappedFrame := frame
				remappedFrame.SceneID = sceneID
				remappedFrames[frameIndex] = remappedFrame
				representativeFrames = append(representativeFrames, domain.Frame{FrameID: frame.ID, TimestampMicros: frame.TimestampMicros, S3Key: frame.URL})
			}
			for frameIndex, frame := range evidence.Frames {
				if frame.SegmentID != segment.ID {
					continue
				}
				remappedEvidenceFrame := frame
				remappedEvidenceFrame.SceneID = sceneID
				remappedEvidenceFrames[frameIndex] = remappedEvidenceFrame
			}
		}
		sort.Slice(representativeFrames, func(left, right int) bool {
			return representativeFrames[left].TimestampMicros < representativeFrames[right].TimestampMicros
		})
		_ = representativeFrames
		remappedScenes = append(remappedScenes, domain.VideoScene{ID: sceneID, StartMicros: start, EndMicros: end, ChangeScore: 1})
	}
	remappedInput.Scenes = remappedScenes
	remappedInput.Segments = remappedSegments
	remappedInput.Frames = remappedFrames
	if len(remappedScenes) > 0 {
		durationMinutes := math.Max(float64(remappedInput.Metadata.DurationMicros)/60_000_000, 1.0/60)
		remappedInput.Quality.SceneChangesPM = float64(max(0, len(remappedScenes)-1)) / durationMinutes
		if len(remappedScenes) == 1 {
			remappedInput.Quality.LikelyStatic = true
		}
	}
	remappedEvidence.Scenes = remappedScenes
	remappedEvidence.Segments = remappedSegments
	remappedEvidence.Frames = remappedEvidenceFrames
	return remappedInput, remappedEvidence, nil
}

func (s *Service) FindCached(ctx context.Context, request CacheRequest) (Result, error) {
	if err := s.validateDependencies(); err != nil {
		return Result{}, domain.Permanent(err)
	}
	if err := validateCacheRequest(request); err != nil {
		return Result{}, domain.Permanent(err)
	}
	analysis, err := s.Repository.FindLatestVideoAnalysis(
		ctx, request.UserID, request.MediaID, request.InputVersion, request.UnderstandingVersion,
		s.Provider.Name(), s.Provider.Model(), request.SourceETag,
	)
	if err != nil {
		return Result{}, err
	}
	return Result{Analysis: analysis, Reused: true}, nil
}

func (s *Service) Analyze(ctx context.Context, request Request) (Result, error) {
	if err := s.validateDependencies(); err != nil {
		return Result{}, domain.Permanent(err)
	}
	if err := validateRequest(request); err != nil {
		return Result{}, domain.Permanent(err)
	}
	if !request.Force {
		if cached, err := s.FindCached(ctx, request.CacheRequest); err == nil {
			return cached, nil
		} else if !errors.Is(err, domain.ErrNotFound) {
			return Result{}, err
		}
	}
	input, err := s.makeInput(ctx, request)
	if err != nil {
		return Result{}, err
	}
	var sourceAwareProvider VideoFileUnderstandingProvider
	if candidate, ok := s.Provider.(VideoFileUnderstandingProvider); ok {
		sourceAwareProvider = candidate
	}
	var sourceVideoURL string
	if sourceAwareProvider != nil {
		sourceVideoURL, err = s.Artifacts.PresignGet(ctx, request.Evidence.SourceS3Key, s.frameURLExpiry())
		if err != nil {
			return Result{}, fmt.Errorf("presign understanding source video: %w", err)
		}
	}

	batches, err := buildUnderstandingBatches(input, s.batchMaxScenes(), s.batchMaxFrames())
	if err != nil {
		return Result{}, domain.Permanent(fmt.Errorf("build video understanding batches: %w", err))
	}
	s.logger().Info("video_understanding_batches_planned",
		slog.String("job_id", request.RunID),
		slog.String("project_id", request.ProjectID),
		slog.String("media_id", request.MediaID),
		slog.String("provider", s.Provider.Name()),
		slog.String("model", s.Provider.Model()),
		slog.Int("batch_count", len(batches)),
		slog.Int("candidate_count", len(input.Segments)),
		slog.Int("frame_count", len(input.Frames)),
		slog.Int("max_scenes_per_batch", s.batchMaxScenes()),
		slog.Int("max_frames_per_batch", s.batchMaxFrames()),
	)

	response := domain.VideoUnderstandingResponseV1{
		SchemaVersion: domain.VideoAnalysisSchemaV1,
		Candidates:    make([]domain.VideoSegmentAnalysisV1, 0, len(input.Segments)),
	}
	for batchIndex, batch := range batches {
		batchResponse := domain.VideoUnderstandingResponseV1{}
		batchNumber := batchIndex + 1
		if err := s.retry(ctx, request, batch.input, batchNumber, len(batches), func() error {
			var providerErr error
			if sourceAwareProvider != nil {
				batchResponse, providerErr = sourceAwareProvider.UnderstandVideo(ctx, batch.input, sourceVideoURL)
			} else {
				batchResponse, providerErr = s.Provider.Understand(ctx, batch.input)
			}
			if providerErr != nil {
				return providerErr
			}
			batchResponse, providerErr = ValidateResponseV1(batchResponse, batch.input)
			if providerErr != nil {
				return providerErr
			}
			return remapTranscriptIndexes(&batchResponse, batch.transcriptIndexes)
		}); err != nil {
			wrapped := fmt.Errorf("video understanding batch %d/%d provider %s failed: %w", batchNumber, len(batches), s.Provider.Name(), err)
			if errors.Is(err, domain.ErrInvalidModelOutput) {
				return Result{}, domain.Permanent(wrapped)
			}
			return Result{}, wrapped
		}
		response.Candidates = append(response.Candidates, batchResponse.Candidates...)
		s.logger().Info("video_understanding_batch_completed",
			slog.String("job_id", request.RunID),
			slog.String("project_id", request.ProjectID),
			slog.String("media_id", request.MediaID),
			slog.String("provider", s.Provider.Name()),
			slog.String("model", s.Provider.Model()),
			slog.Int("batch_index", batchNumber),
			slog.Int("batch_count", len(batches)),
			slog.Int("candidate_count", len(batch.input.Segments)),
			slog.Int("frame_count", batch.frameCount),
			slog.Int("scene_count", batch.sceneCount),
		)
	}
	response, err = ValidateResponseV1(response, input)
	if err != nil {
		wrapped := fmt.Errorf("validate merged video understanding response: %w", err)
		if errors.Is(err, domain.ErrInvalidModelOutput) {
			return Result{}, domain.Permanent(wrapped)
		}
		return Result{}, wrapped
	}
	analysis := makePersistentAnalysis(request, response, strings.TrimSpace(s.Provider.Name()), strings.TrimSpace(s.Provider.Model()), s.now().UTC())
	if err := analysisvalidation.Validate(analysis); err != nil {
		return Result{}, domain.Permanent(fmt.Errorf("validate persistent video analysis: %w", err))
	}
	saved, err := s.Repository.SaveVideoAnalysis(ctx, analysis)
	if err != nil {
		return Result{}, err
	}
	return Result{Analysis: saved, Reused: saved.ID != analysis.ID}, nil
}

func buildUnderstandingBatches(input domain.VideoUnderstandingInputV1, maxScenes, maxFrames int) ([]understandingBatch, error) {
	if len(input.Segments) == 0 {
		return nil, errors.New("video understanding input has no segments")
	}
	if maxScenes <= 0 {
		maxScenes = 10
	}
	if maxFrames <= 0 {
		maxFrames = 24
	}

	framesBySegment := make(map[string]int, len(input.Segments))
	for _, frame := range input.Frames {
		framesBySegment[frame.SegmentID]++
	}

	type batchRange struct {
		start int
		end   int
	}
	ranges := make([]batchRange, 0)
	start := 0
	currentScenes := make(map[string]struct{})
	currentFrames := 0
	for index, segment := range input.Segments {
		_, sceneAlreadyIncluded := currentScenes[segment.SceneID]
		projectedScenes := len(currentScenes)
		if !sceneAlreadyIncluded {
			projectedScenes++
		}
		projectedFrames := currentFrames + framesBySegment[segment.ID]
		if index > start && (projectedScenes > maxScenes || projectedFrames > maxFrames) {
			ranges = append(ranges, batchRange{start: start, end: index})
			start = index
			currentScenes = make(map[string]struct{})
			currentFrames = 0
		}
		currentScenes[segment.SceneID] = struct{}{}
		currentFrames += framesBySegment[segment.ID]
	}
	ranges = append(ranges, batchRange{start: start, end: len(input.Segments)})

	batches := make([]understandingBatch, 0, len(ranges))
	for _, item := range ranges {
		batchInput, transcriptIndexes, frameCount, sceneCount := makeUnderstandingBatchInput(input, item.start, item.end)
		batches = append(batches, understandingBatch{
			input:             batchInput,
			transcriptIndexes: transcriptIndexes,
			frameCount:        frameCount,
			sceneCount:        sceneCount,
		})
	}
	return batches, nil
}

func makeUnderstandingBatchInput(input domain.VideoUnderstandingInputV1, start, end int) (domain.VideoUnderstandingInputV1, []int, int, int) {
	segments := append([]domain.CandidateSegment(nil), input.Segments[start:end]...)
	segmentIDs := make(map[string]struct{}, len(segments))
	sceneIDs := make(map[string]struct{}, len(segments))
	for _, segment := range segments {
		segmentIDs[segment.ID] = struct{}{}
		sceneIDs[segment.SceneID] = struct{}{}
	}

	frames := make([]domain.VideoUnderstandingFrameV1, 0)
	for _, frame := range input.Frames {
		if _, exists := segmentIDs[frame.SegmentID]; exists {
			frames = append(frames, frame)
		}
	}

	transcriptIndexes := make([]int, 0)
	transcriptSegments := make([]domain.TranscriptSegment, 0)
	for index, transcriptSegment := range input.Transcript.Segments {
		if transcriptOverlapsSegments(transcriptSegment, segments) {
			transcriptIndexes = append(transcriptIndexes, index)
			transcriptSegments = append(transcriptSegments, transcriptSegment)
		}
	}
	transcript := input.Transcript
	transcript.Segments = transcriptSegments
	transcript.HasSpeech = input.Transcript.HasSpeech && len(transcriptSegments) > 0
	if len(transcriptSegments) > 0 {
		text := make([]string, 0, len(transcriptSegments))
		for _, segment := range transcriptSegments {
			text = append(text, segment.Text)
		}
		transcript.Text = strings.Join(text, " ")
	} else {
		transcript.Text = ""
	}

	scenes := make([]domain.VideoScene, 0, len(sceneIDs))
	for _, scene := range input.Scenes {
		if _, exists := sceneIDs[scene.ID]; exists {
			scenes = append(scenes, scene)
		}
	}
	batchInput := input
	batchInput.Scenes = scenes
	batchInput.Segments = segments
	batchInput.Frames = frames
	batchInput.Transcript = transcript
	return batchInput, transcriptIndexes, len(frames), len(sceneIDs)
}

func transcriptOverlapsSegments(transcript domain.TranscriptSegment, segments []domain.CandidateSegment) bool {
	for _, segment := range segments {
		if transcript.EndMicros > segment.StartMicros && transcript.StartMicros < segment.EndMicros {
			return true
		}
	}
	return false
}

func remapTranscriptIndexes(response *domain.VideoUnderstandingResponseV1, transcriptIndexes []int) error {
	for candidateIndex := range response.Candidates {
		candidate := &response.Candidates[candidateIndex]
		remapped := make([]int, 0, len(candidate.TranscriptSegmentIndexes))
		for _, localIndex := range candidate.TranscriptSegmentIndexes {
			if localIndex < 0 || localIndex >= len(transcriptIndexes) {
				return fmt.Errorf("%w: batch transcript index %d is out of range", domain.ErrInvalidModelOutput, localIndex)
			}
			remapped = append(remapped, transcriptIndexes[localIndex])
		}
		candidate.TranscriptSegmentIndexes = remapped
	}
	return nil
}

func makePersistentAnalysis(request Request, response domain.VideoUnderstandingResponseV1, provider, model string, createdAt time.Time) domain.VideoAnalysis {
	version := persistentAnalysisVersion(request, provider, model)
	framesByScene := make(map[string][]domain.Frame, len(request.Evidence.Scenes))
	for _, frame := range request.Evidence.Frames {
		framesByScene[frame.SceneID] = append(framesByScene[frame.SceneID], domain.Frame{
			FrameID: frame.ID, TimestampMicros: frame.TimestampMicros, S3Key: frame.S3Key,
		})
	}
	scenes := make([]domain.Scene, 0, len(request.Evidence.Scenes))
	for _, evidenceScene := range request.Evidence.Scenes {
		frames := framesByScene[evidenceScene.ID]
		sort.Slice(frames, func(left, right int) bool { return frames[left].TimestampMicros < frames[right].TimestampMicros })
		scenes = append(scenes, domain.Scene{
			SceneID: evidenceScene.ID, StartMicros: evidenceScene.StartMicros, EndMicros: evidenceScene.EndMicros,
			RepresentativeFrames: frames,
		})
	}
	segments := make([]domain.Segment, 0, len(response.Candidates))
	people := make([]string, 0)
	activities := make([]string, 0)
	locations := make([]string, 0)
	for _, candidate := range response.Candidates {
		segments = append(segments, domain.Segment{
			SegmentID: candidate.SegmentID, SceneID: candidate.SceneID,
			StartMicros: candidate.StartMicros, EndMicros: candidate.EndMicros,
			Description: candidate.Description, Importance: candidate.ImportanceScore,
			VisualQuality: candidate.VisualQualityScore, MemoryValue: candidate.MemoryValueScore,
			EmotionalValue: candidate.EmotionalValueScore, StoryRelevance: candidate.StoryRelevanceScore,
			Recommended:      candidate.RecommendedUsage != domain.RecommendedUsageExclude,
			RecommendedUsage: candidate.RecommendedUsage, Reasons: []string{candidate.Reason},
			People: candidate.People, Activities: candidate.Activities, Interactions: candidate.Interactions,
			Expressions: candidate.Expressions, Objects: candidate.Objects, Food: candidate.Food,
			Locations: candidate.Locations, Scenery: candidate.Scenery, Events: candidate.Events,
			InterestingActions: candidate.InterestingActions, Speech: candidate.Speech,
			MeaningfulAudio: candidate.MeaningfulAudio,
			Labels:          normalizedLabels(append(append(append(append([]string{}, candidate.Objects...), candidate.Food...), candidate.Scenery...), candidate.Events...)),
		})
		people = append(people, candidate.People...)
		activities = append(activities, candidate.Activities...)
		locations = append(locations, candidate.Locations...)
	}
	return domain.VideoAnalysis{
		ID:            "video_analysis_" + strings.TrimPrefix(version, "va_")[:24],
		SchemaVersion: domain.VideoAnalysisSchemaV1,
		UserID:        request.UserID, ProjectID: request.ProjectID, VideoID: request.MediaID,
		AnalysisVersion: version, InputVersion: request.InputVersion, UnderstandingVersion: request.UnderstandingVersion,
		Provider: provider, Model: model, SourceETag: request.SourceETag, EvidenceS3Key: request.EvidenceS3Key,
		ProcessingStatus: domain.VideoAnalysisStatusCompleted, DurationMicros: request.Evidence.Metadata.DurationMicros,
		Scenes: scenes, Transcript: request.Evidence.Transcript, Segments: segments,
		DetectedPeople: normalizedLabels(people), DetectedActivities: normalizedLabels(activities),
		DetectedLocations: normalizedLabels(locations), QualitySignals: request.Evidence.Quality, CreatedAt: createdAt,
	}
}

func persistentAnalysisVersion(request Request, provider, model string) string {
	discriminator := "canonical"
	if request.Force {
		discriminator = "forced:" + strings.TrimSpace(request.RunID)
	}
	hash := sha256.New()
	for _, value := range []string{
		request.UserID, request.MediaID, request.SourceETag, request.InputVersion,
		request.UnderstandingVersion, provider, model, discriminator,
	} {
		_, _ = fmt.Fprintf(hash, "%d:", len(value))
		_, _ = hash.Write([]byte(value))
	}
	return "va_" + hex.EncodeToString(hash.Sum(nil))
}

func normalizedLabels(values []string) []string {
	byKey := make(map[string]string, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, exists := byKey[key]; !exists {
			byKey[key] = value
		}
	}
	keys := make([]string, 0, len(byKey))
	for key := range byKey {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]string, 0, len(keys))
	for _, key := range keys {
		result = append(result, byKey[key])
	}
	return result
}

func (s *Service) makeInput(ctx context.Context, request Request) (domain.VideoUnderstandingInputV1, error) {
	frames := make([]domain.VideoUnderstandingFrameV1, 0, len(request.Evidence.Frames))
	for _, frame := range request.Evidence.Frames {
		url, err := s.Artifacts.PresignGet(ctx, frame.S3Key, s.frameURLExpiry())
		if err != nil {
			return domain.VideoUnderstandingInputV1{}, fmt.Errorf("presign understanding frame %s: %w", frame.ID, err)
		}
		frames = append(frames, domain.VideoUnderstandingFrameV1{
			ID: frame.ID, SceneID: frame.SceneID, SegmentID: frame.SegmentID,
			TimestampMicros: frame.TimestampMicros, URL: url,
			Brightness: frame.Brightness, Contrast: frame.Contrast, Sharpness: frame.Sharpness,
			QualityScore: frame.QualityScore, SelectionReason: frame.SelectionReason,
		})
	}
	return domain.VideoUnderstandingInputV1{
		SchemaVersion: domain.VideoUnderstandingInputSchemaV1,
		InputVersion:  request.InputVersion, AnalysisVersion: request.UnderstandingVersion,
		Objective: Objective, GroundingRules: append([]string(nil), groundingRules...),
		MediaID: request.MediaID, ProjectID: request.ProjectID,
		Metadata: request.Evidence.Metadata, Scenes: request.Evidence.Scenes, Segments: request.Evidence.Segments,
		Frames: frames, Transcript: request.Evidence.Transcript, Quality: request.Evidence.Quality,
	}, nil
}

func ValidateResponseV1(response domain.VideoUnderstandingResponseV1, input domain.VideoUnderstandingInputV1) (domain.VideoUnderstandingResponseV1, error) {
	if response.SchemaVersion != domain.VideoAnalysisSchemaV1 {
		return domain.VideoUnderstandingResponseV1{}, fmt.Errorf("%w: schema_version must be %s", domain.ErrInvalidModelOutput, domain.VideoAnalysisSchemaV1)
	}
	if len(response.Candidates) != len(input.Segments) {
		return domain.VideoUnderstandingResponseV1{}, fmt.Errorf("%w: expected %d candidate analyses, received %d", domain.ErrInvalidModelOutput, len(input.Segments), len(response.Candidates))
	}
	segments := make(map[string]domain.CandidateSegment, len(input.Segments))
	for _, segment := range input.Segments {
		segments[segment.ID] = segment
	}
	frames := make(map[string]domain.VideoUnderstandingFrameV1, len(input.Frames))
	for _, frame := range input.Frames {
		frames[frame.ID] = frame
	}
	seen := make(map[string]struct{}, len(response.Candidates))
	for index := range response.Candidates {
		candidate := &response.Candidates[index]
		segment, exists := segments[candidate.SegmentID]
		if !exists {
			return domain.VideoUnderstandingResponseV1{}, fmt.Errorf("%w: candidate %d references unknown segment", domain.ErrInvalidModelOutput, index)
		}
		if _, duplicate := seen[candidate.SegmentID]; duplicate {
			return domain.VideoUnderstandingResponseV1{}, fmt.Errorf("%w: segment %s was analyzed more than once", domain.ErrInvalidModelOutput, candidate.SegmentID)
		}
		seen[candidate.SegmentID] = struct{}{}
		if candidate.SceneID != segment.SceneID || candidate.StartMicros != segment.StartMicros || candidate.EndMicros != segment.EndMicros {
			return domain.VideoUnderstandingResponseV1{}, fmt.Errorf("%w: candidate %s invented scene or timestamp bounds", domain.ErrInvalidModelOutput, candidate.SegmentID)
		}
		candidate.Description = strings.TrimSpace(candidate.Description)
		candidate.Reason = strings.TrimSpace(candidate.Reason)
		candidate.EmotionalTone = strings.TrimSpace(candidate.EmotionalTone)
		if candidate.Description == "" || candidate.Reason == "" {
			return domain.VideoUnderstandingResponseV1{}, fmt.Errorf("%w: candidate %s requires description and reason", domain.ErrInvalidModelOutput, candidate.SegmentID)
		}
		if !validScore(candidate.ImportanceScore) || !validScore(candidate.VisualQualityScore) ||
			!validScore(candidate.MemoryValueScore) || !validScore(candidate.EmotionalValueScore) || !validScore(candidate.StoryRelevanceScore) {
			return domain.VideoUnderstandingResponseV1{}, fmt.Errorf("%w: candidate %s has score outside zero to one", domain.ErrInvalidModelOutput, candidate.SegmentID)
		}
		if !validRecommendedUsage(candidate.RecommendedUsage) {
			return domain.VideoUnderstandingResponseV1{}, fmt.Errorf("%w: candidate %s has invalid recommended_usage", domain.ErrInvalidModelOutput, candidate.SegmentID)
		}
		var err error
		candidate.People, err = validatedLabels(candidate.People, "people", candidate.SegmentID)
		if err != nil {
			return domain.VideoUnderstandingResponseV1{}, err
		}
		candidate.Activities, err = validatedLabels(candidate.Activities, "activities", candidate.SegmentID)
		if err != nil {
			return domain.VideoUnderstandingResponseV1{}, err
		}
		candidate.Interactions, err = validatedLabels(candidate.Interactions, "interactions", candidate.SegmentID)
		if err != nil {
			return domain.VideoUnderstandingResponseV1{}, err
		}
		candidate.Expressions, err = validatedLabels(candidate.Expressions, "expressions", candidate.SegmentID)
		if err != nil {
			return domain.VideoUnderstandingResponseV1{}, err
		}
		candidate.Objects, err = validatedLabels(candidate.Objects, "objects", candidate.SegmentID)
		if err != nil {
			return domain.VideoUnderstandingResponseV1{}, err
		}
		candidate.Food, err = validatedLabels(candidate.Food, "food", candidate.SegmentID)
		if err != nil {
			return domain.VideoUnderstandingResponseV1{}, err
		}
		candidate.Locations, err = validatedLabels(candidate.Locations, "locations", candidate.SegmentID)
		if err != nil {
			return domain.VideoUnderstandingResponseV1{}, err
		}
		candidate.Scenery, err = validatedLabels(candidate.Scenery, "scenery", candidate.SegmentID)
		if err != nil {
			return domain.VideoUnderstandingResponseV1{}, err
		}
		candidate.Events, err = validatedLabels(candidate.Events, "events", candidate.SegmentID)
		if err != nil {
			return domain.VideoUnderstandingResponseV1{}, err
		}
		candidate.InterestingActions, err = validatedLabels(candidate.InterestingActions, "interesting_actions", candidate.SegmentID)
		if err != nil {
			return domain.VideoUnderstandingResponseV1{}, err
		}
		candidate.Speech, err = validatedLabels(candidate.Speech, "speech", candidate.SegmentID)
		if err != nil {
			return domain.VideoUnderstandingResponseV1{}, err
		}
		candidate.MeaningfulAudio, err = validatedLabels(candidate.MeaningfulAudio, "meaningful_audio", candidate.SegmentID)
		if err != nil {
			return domain.VideoUnderstandingResponseV1{}, err
		}
		candidate.FrameIDs = validateFrameReferences(candidate.FrameIDs, candidate.SegmentID, frames)
		candidate.TranscriptSegmentIndexes, err = validateTranscriptReferences(candidate.TranscriptSegmentIndexes, segment, input.Transcript.Segments)
		if err != nil {
			return domain.VideoUnderstandingResponseV1{}, err
		}
		repairUngroundedCandidate(candidate)
		if hasVisualClaims(*candidate) && len(candidate.FrameIDs) == 0 {
			return domain.VideoUnderstandingResponseV1{}, fmt.Errorf("%w: candidate %s has ungrounded visual claims", domain.ErrInvalidModelOutput, candidate.SegmentID)
		}
		if (len(candidate.Speech) > 0 || len(candidate.MeaningfulAudio) > 0) && len(candidate.TranscriptSegmentIndexes) == 0 {
			return domain.VideoUnderstandingResponseV1{}, fmt.Errorf("%w: candidate %s has ungrounded speech or audio claims", domain.ErrInvalidModelOutput, candidate.SegmentID)
		}
		if candidate.RecommendedUsage != domain.RecommendedUsageExclude && len(candidate.FrameIDs) == 0 && len(candidate.TranscriptSegmentIndexes) == 0 {
			return domain.VideoUnderstandingResponseV1{}, fmt.Errorf("%w: candidate %s recommends usage without grounding evidence", domain.ErrInvalidModelOutput, candidate.SegmentID)
		}
		if candidate.RecommendedUsage == domain.RecommendedUsageAudioOnly && len(candidate.TranscriptSegmentIndexes) == 0 {
			return domain.VideoUnderstandingResponseV1{}, fmt.Errorf("%w: audio-only candidate %s lacks transcript grounding", domain.ErrInvalidModelOutput, candidate.SegmentID)
		}
	}
	sort.Slice(response.Candidates, func(left, right int) bool {
		return response.Candidates[left].StartMicros < response.Candidates[right].StartMicros
	})
	return response, nil
}

func validateFrameReferences(values []string, segmentID string, frames map[string]domain.VideoUnderstandingFrameV1) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		frame, exists := frames[value]
		if value == "" || !exists || frame.SegmentID != segmentID {
			continue
		}
		if _, duplicate := seen[value]; duplicate {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func repairUngroundedCandidate(candidate *domain.VideoSegmentAnalysisV1) {
	visualGrounded := len(candidate.FrameIDs) > 0
	audioGrounded := len(candidate.TranscriptSegmentIndexes) > 0
	if !visualGrounded {
		candidate.People = nil
		candidate.Activities = nil
		candidate.Interactions = nil
		candidate.Expressions = nil
		candidate.Objects = nil
		candidate.Food = nil
		candidate.Locations = nil
		candidate.Scenery = nil
		candidate.Events = nil
		candidate.InterestingActions = nil
	}
	if !audioGrounded {
		candidate.Speech = nil
		candidate.MeaningfulAudio = nil
	}
	if visualGrounded || audioGrounded {
		return
	}
	candidate.RecommendedUsage = domain.RecommendedUsageExclude
	candidate.Description = "Segment excluded because no grounded evidence matched this segment."
	candidate.Reason = "The model did not return valid frame or transcript evidence for this segment."
	candidate.ImportanceScore = min(candidate.ImportanceScore, 0.05)
	candidate.MemoryValueScore = min(candidate.MemoryValueScore, 0.05)
	candidate.EmotionalValueScore = min(candidate.EmotionalValueScore, 0.05)
	candidate.StoryRelevanceScore = min(candidate.StoryRelevanceScore, 0.05)
}

func validateTranscriptReferences(values []int, segment domain.CandidateSegment, transcript []domain.TranscriptSegment) ([]int, error) {
	seen := make(map[int]struct{}, len(values))
	result := make([]int, 0, len(values))
	for _, value := range values {
		if value < 0 || value >= len(transcript) {
			return nil, fmt.Errorf("%w: segment %s references an unknown transcript segment", domain.ErrInvalidModelOutput, segment.ID)
		}
		item := transcript[value]
		if item.EndMicros <= segment.StartMicros || item.StartMicros >= segment.EndMicros {
			return nil, fmt.Errorf("%w: segment %s references non-overlapping transcript evidence", domain.ErrInvalidModelOutput, segment.ID)
		}
		if _, duplicate := seen[value]; duplicate {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Ints(result)
	return result, nil
}

func validatedLabels(values []string, field, segmentID string) ([]string, error) {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, min(len(values), 25))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			return nil, fmt.Errorf("%w: segment %s contains an empty %s label", domain.ErrInvalidModelOutput, segmentID, field)
		}
		key := strings.ToLower(value)
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, value)
		if len(result) == 25 {
			break
		}
	}
	return result, nil
}

func hasVisualClaims(candidate domain.VideoSegmentAnalysisV1) bool {
	return len(candidate.People)+len(candidate.Activities)+len(candidate.Interactions)+len(candidate.Expressions)+
		len(candidate.Objects)+len(candidate.Food)+len(candidate.Locations)+len(candidate.Scenery)+
		len(candidate.Events)+len(candidate.InterestingActions) > 0
}

func validScore(value float64) bool { return value >= 0 && value <= 1 }

func validRecommendedUsage(value string) bool {
	switch value {
	case domain.RecommendedUsageHero, domain.RecommendedUsageSupporting, domain.RecommendedUsageContext,
		domain.RecommendedUsageAudioOnly, domain.RecommendedUsageExclude:
		return true
	default:
		return false
	}
}

func validateCacheRequest(request CacheRequest) error {
	if strings.TrimSpace(request.UserID) == "" || strings.TrimSpace(request.MediaID) == "" || strings.TrimSpace(request.SourceETag) == "" ||
		strings.TrimSpace(request.InputVersion) == "" || strings.TrimSpace(request.UnderstandingVersion) == "" {
		return errors.New("video understanding cache request is incomplete")
	}
	return nil
}

func validateRequest(request Request) error {
	if err := validateCacheRequest(request.CacheRequest); err != nil {
		return err
	}
	if strings.TrimSpace(request.ProjectID) == "" || strings.TrimSpace(request.EvidenceS3Key) == "" {
		return errors.New("video understanding request requires project and evidence references")
	}
	if request.Force && strings.TrimSpace(request.RunID) == "" {
		return errors.New("forced video understanding requires a stable run ID")
	}
	if request.Evidence.MediaID != request.MediaID || request.Evidence.ProjectID != request.ProjectID ||
		request.Evidence.AnalysisVersion != request.InputVersion || request.Evidence.SourceETag != request.SourceETag {
		return errors.New("video understanding evidence does not match request identifiers or input version")
	}
	if request.Evidence.Metadata.DurationMicros <= 0 || len(request.Evidence.Segments) == 0 {
		return errors.New("video understanding evidence requires duration and candidate segments")
	}
	return validateEvidence(request.Evidence)
}

func validateEvidence(evidence domain.VideoEvidence) error {
	if evidence.SchemaVersion != 1 || strings.TrimSpace(evidence.SourceS3Key) == "" {
		return errors.New("video understanding evidence has an unsupported schema or missing source reference")
	}
	scenes := make(map[string]domain.VideoScene, len(evidence.Scenes))
	for _, scene := range evidence.Scenes {
		if scene.ID == "" || scene.StartMicros < 0 || scene.EndMicros <= scene.StartMicros || scene.EndMicros > evidence.Metadata.DurationMicros {
			return errors.New("video understanding evidence contains an invalid scene")
		}
		if _, duplicate := scenes[scene.ID]; duplicate {
			return errors.New("video understanding evidence contains duplicate scene IDs")
		}
		scenes[scene.ID] = scene
	}
	segments := make(map[string]domain.CandidateSegment, len(evidence.Segments))
	for _, segment := range evidence.Segments {
		scene, exists := scenes[segment.SceneID]
		if segment.ID == "" || !exists || segment.StartMicros < scene.StartMicros || segment.EndMicros > scene.EndMicros || segment.EndMicros <= segment.StartMicros {
			return errors.New("video understanding evidence contains an invalid candidate segment")
		}
		if _, duplicate := segments[segment.ID]; duplicate {
			return errors.New("video understanding evidence contains duplicate segment IDs")
		}
		segments[segment.ID] = segment
	}
	frameIDs := make(map[string]struct{}, len(evidence.Frames))
	for _, frame := range evidence.Frames {
		segment, exists := segments[frame.SegmentID]
		if frame.ID == "" || frame.S3Key == "" || !exists || frame.SceneID != segment.SceneID ||
			frame.TimestampMicros < segment.StartMicros || frame.TimestampMicros >= segment.EndMicros {
			return errors.New("video understanding evidence contains an invalid selected frame")
		}
		if _, duplicate := frameIDs[frame.ID]; duplicate {
			return errors.New("video understanding evidence contains duplicate frame IDs")
		}
		frameIDs[frame.ID] = struct{}{}
	}
	for _, segment := range evidence.Transcript.Segments {
		if segment.StartMicros < 0 || segment.EndMicros <= segment.StartMicros || segment.EndMicros > evidence.Metadata.DurationMicros || strings.TrimSpace(segment.Text) == "" {
			return errors.New("video understanding evidence contains an invalid transcript segment")
		}
	}
	return nil
}

func (s *Service) validateDependencies() error {
	if s.Provider == nil || s.Repository == nil || s.Artifacts == nil {
		return errors.New("video understanding service dependencies are required")
	}
	if strings.TrimSpace(s.Provider.Name()) == "" || strings.TrimSpace(s.Provider.Model()) == "" {
		return errors.New("video understanding provider name and model are required")
	}
	return nil
}

func (s *Service) retry(ctx context.Context, request Request, input domain.VideoUnderstandingInputV1, batchIndex, batchCount int, operation func() error) error {
	attempts := s.RetryAttempts
	if attempts <= 0 {
		attempts = 3
	}
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		attemptNumber := attempt + 1
		started := time.Now()
		s.logger().Info("video_understanding_provider_attempt_started",
			slog.String("job_id", request.RunID),
			slog.String("project_id", request.ProjectID),
			slog.String("media_id", request.MediaID),
			slog.String("provider", s.Provider.Name()),
			slog.String("model", s.Provider.Model()),
			slog.Int("attempt", attemptNumber),
			slog.Int("max_attempts", attempts),
			slog.Int("batch_index", batchIndex),
			slog.Int("batch_count", batchCount),
			slog.Int("candidate_count", len(input.Segments)),
			slog.Int("scene_count", len(input.Scenes)),
			slog.Int("frame_count", len(input.Frames)),
			slog.Int("transcript_segment_count", len(input.Transcript.Segments)),
		)
		if err := operation(); err == nil {
			s.logger().Info("video_understanding_provider_attempt_completed",
				slog.String("job_id", request.RunID),
				slog.String("project_id", request.ProjectID),
				slog.String("media_id", request.MediaID),
				slog.String("provider", s.Provider.Name()),
				slog.String("model", s.Provider.Model()),
				slog.Int("attempt", attemptNumber),
				slog.Int("batch_index", batchIndex),
				slog.Int("batch_count", batchCount),
				slog.Int64("latency_ms", time.Since(started).Milliseconds()),
			)
			return nil
		} else {
			lastErr = err
			s.logger().Warn("video_understanding_provider_attempt_failed",
				slog.String("job_id", request.RunID),
				slog.String("project_id", request.ProjectID),
				slog.String("media_id", request.MediaID),
				slog.String("provider", s.Provider.Name()),
				slog.String("model", s.Provider.Model()),
				slog.Int("attempt", attemptNumber),
				slog.Int("max_attempts", attempts),
				slog.Int64("latency_ms", time.Since(started).Milliseconds()),
				slog.Any("error", err),
			)
		}
		if attempt+1 == attempts {
			break
		}
		delay := s.RetryDelay
		if delay <= 0 {
			delay = 250 * time.Millisecond
		}
		delay *= time.Duration(1 << attempt)
		if delay > 5*time.Second {
			delay = 5 * time.Second
		}
		s.logger().Info("video_understanding_provider_retry_scheduled",
			slog.String("job_id", request.RunID),
			slog.String("project_id", request.ProjectID),
			slog.String("media_id", request.MediaID),
			slog.Int("next_attempt", attemptNumber+1),
			slog.Int("batch_index", batchIndex),
			slog.Int("batch_count", batchCount),
			slog.Int64("delay_ms", delay.Milliseconds()),
		)
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return lastErr
}

func (s *Service) logger() *slog.Logger {
	if s.Logger != nil {
		return s.Logger
	}
	return slog.Default()
}

func (s *Service) frameURLExpiry() time.Duration {
	if s.FrameURLExpiry > 0 {
		return s.FrameURLExpiry
	}
	return 30 * time.Minute
}

func (s *Service) batchMaxScenes() int {
	if s.BatchMaxScenes > 0 {
		return s.BatchMaxScenes
	}
	return 10
}

func (s *Service) batchMaxFrames() int {
	if s.BatchMaxFrames > 0 {
		return s.BatchMaxFrames
	}
	return 24
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func missingJSONField(document map[string]json.RawMessage, name string) bool {
	value, exists := document[name]
	return !exists || len(bytes.TrimSpace(value)) == 0 || bytes.Equal(bytes.TrimSpace(value), []byte("null"))
}

func ensureJSONEnd(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("%w: response contains multiple JSON values", domain.ErrInvalidModelOutput)
		}
		return fmt.Errorf("%w: response has invalid trailing data: %v", domain.ErrInvalidModelOutput, err)
	}
	return nil
}
