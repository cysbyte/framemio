package pipeline

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/framemio/video-intelligence-worker/internal/domain"
	"github.com/framemio/video-intelligence-worker/internal/storage"
	"github.com/framemio/video-intelligence-worker/internal/store"
	"github.com/framemio/video-intelligence-worker/internal/transcription"
	"github.com/framemio/video-intelligence-worker/internal/understanding"
)

type MediaToolchain interface {
	Probe(ctx context.Context, sourcePath string) (domain.VideoMetadata, error)
	DetectScenes(ctx context.Context, sourcePath string, threshold float64, maxBoundaries int) ([]domain.SceneBoundary, error)
	ExtractFrame(ctx context.Context, sourcePath string, timestampMicros int64, destinationPath string) error
	ExtractAudio(ctx context.Context, sourcePath, destinationPath string) error
}

type Config struct {
	WorkRoot              string
	AnalysisVersion       string
	SceneThreshold        float64
	MaxSceneBoundaries    int
	Planner               PlannerConfig
	DuplicateHashDistance int
	StageRetries          int
	EnableTranscription   bool
	TranscriptionVersion  string
	UnderstandingVersion  string
	EvidenceURLExpiry     time.Duration
}

type Result struct {
	Analysis domain.VideoAnalysis
	Reused   bool
}

type CompletionReporter interface {
	CompleteJob(ctx context.Context, job domain.ProcessingJob, artifactS3Key string) error
}

type Processor struct {
	Config        Config
	Repository    store.Repository
	Objects       storage.ObjectStore
	Tools         MediaToolchain
	Transcription transcription.Transcriber
	Understanding understanding.Analyzer
	Completion    CompletionReporter
	Logger        *slog.Logger
	Now           func() time.Time
}

func (p *Processor) Process(ctx context.Context, message domain.JobMessage) (Result, error) {
	if err := validateMessage(message); err != nil {
		return Result{}, domain.Permanent(err)
	}
	jobContext, err := p.Repository.LoadJobContext(ctx, message)
	if err != nil {
		p.logger().Error("load video job context failed", slog.String("job_id", message.JobID), slog.String("project_id", message.ProjectID), slog.String("media_id", message.MediaID), slog.Any("error", err))
		return Result{}, err
	}
	p.logger().Info("loaded video job context", slog.String("job_id", jobContext.Job.ID), slog.String("project_id", jobContext.Project.ID), slog.String("media_id", jobContext.Media.ID), slog.String("media_status", jobContext.Media.Status), slog.Int64("file_size_bytes", jobContext.Media.FileSizeBytes), slog.String("s3_key", jobContext.Media.S3Key))
	if jobContext.Media.Type != "video" || jobContext.Media.Status != "ready" {
		return Result{}, domain.Permanent(fmt.Errorf("%w: media %s is %s/%s", domain.ErrUnsupportedMedia, jobContext.Media.ID, jobContext.Media.Type, jobContext.Media.Status))
	}
	if p.Understanding == nil {
		return Result{}, domain.Permanent(fmt.Errorf("video understanding service is required"))
	}
	inputVersion := p.inputVersion(message)
	analysisVersion := p.understandingVersion(message)
	force := parseBool(message.Configuration["force_reanalyze"])
	sourceInfo, err := p.Objects.Head(ctx, jobContext.Media.S3Key)
	if err != nil {
		p.logger().Error("head source video failed", slog.String("job_id", jobContext.Job.ID), slog.String("media_id", jobContext.Media.ID), slog.String("s3_key", jobContext.Media.S3Key), slog.Any("error", err))
		return Result{}, err
	}
	p.logger().Info("source video found", slog.String("job_id", jobContext.Job.ID), slog.String("media_id", jobContext.Media.ID), slog.Int64("source_size_bytes", sourceInfo.SizeBytes), slog.String("source_etag", sourceInfo.ETag))
	sourceETag := sourceInfo.ETag
	if sourceETag == "" {
		sourceETag = jobContext.Media.ETag
	}
	if jobContext.Media.FileSizeBytes > 0 && sourceInfo.SizeBytes > 0 && jobContext.Media.FileSizeBytes != sourceInfo.SizeBytes {
		return Result{}, domain.Permanent(fmt.Errorf("source object size does not match media record"))
	}
	if !force {
		cached, lookupErr := p.Understanding.FindCached(ctx, understanding.CacheRequest{
			UserID: jobContext.Job.UserID, MediaID: jobContext.Media.ID, SourceETag: sourceETag,
			InputVersion: inputVersion, UnderstandingVersion: analysisVersion,
		})
		if lookupErr == nil {
			if err := p.completeJob(ctx, jobContext.Job, cached.Analysis.EvidenceS3Key); err != nil {
				return Result{}, err
			}
			return Result{Analysis: cached.Analysis, Reused: true}, nil
		}
		if !errors.Is(lookupErr, domain.ErrNotFound) {
			return Result{}, lookupErr
		}
	}
	if _, err := p.Repository.StartJobAttempt(ctx, jobContext.Job.ID); err != nil {
		p.logger().Error("start video job attempt failed", slog.String("job_id", jobContext.Job.ID), slog.Any("error", err))
		return Result{}, err
	}
	p.logger().Info("started video job attempt", slog.String("job_id", jobContext.Job.ID), slog.Int("attempt", jobContext.Job.Attempt+1))
	evidence, artifact, err := p.loadOrPreprocess(ctx, jobContext, inputVersion, sourceETag)
	if err != nil {
		p.logger().Error("video preprocess failed", slog.String("job_id", jobContext.Job.ID), slog.String("media_id", jobContext.Media.ID), slog.Any("error", err))
		return Result{}, err
	}
	p.logger().Info("video preprocess ready", slog.String("job_id", jobContext.Job.ID), slog.String("media_id", jobContext.Media.ID), slog.Int("frame_count", artifact.FrameCount), slog.String("evidence_s3_key", artifact.EvidenceS3Key))
	if _, err := p.Repository.UpdateJob(ctx, jobContext.Job.ID, "RUNNING", 0.78, "", "", artifact.EvidenceS3Key); err != nil {
		return Result{}, err
	}
	p.logger().Info("starting video understanding", slog.String("job_id", jobContext.Job.ID), slog.String("media_id", jobContext.Media.ID), slog.String("evidence_s3_key", artifact.EvidenceS3Key))
	understandingResult, err := p.Understanding.Analyze(ctx, understanding.Request{
		CacheRequest: understanding.CacheRequest{
			UserID: jobContext.Job.UserID, MediaID: jobContext.Media.ID, SourceETag: sourceETag,
			InputVersion: inputVersion, UnderstandingVersion: analysisVersion,
		},
		ProjectID: jobContext.Project.ID, EvidenceS3Key: artifact.EvidenceS3Key, Evidence: evidence,
		RunID: jobContext.Job.ID, Force: force,
	})
	if err != nil {
		p.logger().Error("video understanding failed", slog.String("job_id", jobContext.Job.ID), slog.String("media_id", jobContext.Media.ID), slog.Any("error", err))
		return Result{}, fmt.Errorf("understand video evidence: %w", err)
	}
	if _, err := p.Repository.UpdateJob(ctx, jobContext.Job.ID, "RUNNING", 0.92, "", "", artifact.EvidenceS3Key); err != nil {
		return Result{}, err
	}
	analysis := understandingResult.Analysis
	if err := p.completeJob(ctx, jobContext.Job, analysis.EvidenceS3Key); err != nil {
		return Result{}, err
	}
	return Result{Analysis: analysis, Reused: understandingResult.Reused}, nil
}

func (p *Processor) completeJob(ctx context.Context, job domain.ProcessingJob, artifactS3Key string) error {
	if p.Completion == nil {
		return fmt.Errorf("backend completion callback is required")
	}
	if err := p.Completion.CompleteJob(ctx, job, artifactS3Key); err != nil {
		p.logger().Error("complete video job callback failed", slog.String("job_id", job.ID), slog.String("project_id", job.ProjectID), slog.String("media_id", job.MediaID), slog.Any("error", err))
		return err
	}
	p.logger().Info("completed video job through backend callback", slog.String("job_id", job.ID), slog.String("project_id", job.ProjectID), slog.String("media_id", job.MediaID))
	return nil
}

func (p *Processor) MarkFailed(ctx context.Context, message domain.JobMessage, err error) error {
	code := "video_analysis_failed"
	if domain.IsPermanent(err) {
		code = "video_analysis_invalid"
	}
	jobContext, loadErr := p.Repository.LoadJobContext(ctx, message)
	if loadErr != nil {
		return loadErr
	}
	if _, updateErr := p.Repository.UpdateJob(ctx, message.JobID, "FAILED", jobContext.Job.Progress, code, truncate(err.Error(), 2000), ""); updateErr != nil {
		return updateErr
	}
	return p.Repository.SetProjectStatus(ctx, jobContext.Job.UserID, jobContext.Project.ID, "failed")
}

func (p *Processor) loadOrPreprocess(ctx context.Context, jobContext domain.JobContext, version, sourceETag string) (domain.VideoEvidence, domain.PreprocessingArtifact, error) {
	artifact, err := p.Repository.GetPreprocessing(ctx, jobContext.Job.UserID, jobContext.Media.ID, version, sourceETag)
	if err == nil {
		var evidence domain.VideoEvidence
		if loadErr := p.Objects.GetJSON(ctx, artifact.EvidenceS3Key, &evidence); loadErr == nil && validCachedEvidence(evidence, jobContext, version, sourceETag) {
			return evidence, artifact, nil
		}
		p.logger().Warn("cached preprocessing artifact is unavailable; rebuilding", slog.String("media_id", jobContext.Media.ID), slog.String("evidence_s3_key", artifact.EvidenceS3Key))
	} else if !errors.Is(err, domain.ErrNotFound) {
		return domain.VideoEvidence{}, domain.PreprocessingArtifact{}, err
	}
	workspace, err := os.MkdirTemp(p.Config.WorkRoot, "job-"+safeComponent(jobContext.Job.ID)+"-")
	if err != nil {
		return domain.VideoEvidence{}, domain.PreprocessingArtifact{}, err
	}
	defer os.RemoveAll(workspace)
	sourcePath := filepath.Join(workspace, "source"+sourceExtension(jobContext.Media.FileName))
	if err := retry(ctx, p.Config.StageRetries, func() error {
		return p.Objects.DownloadFile(ctx, jobContext.Media.S3Key, sourcePath)
	}); err != nil {
		return domain.VideoEvidence{}, domain.PreprocessingArtifact{}, fmt.Errorf("download source video: %w", err)
	}
	if _, err := p.Repository.UpdateJob(ctx, jobContext.Job.ID, "RUNNING", 0.12, "", "", ""); err != nil {
		return domain.VideoEvidence{}, domain.PreprocessingArtifact{}, err
	}
	metadata, err := p.Tools.Probe(ctx, sourcePath)
	if err != nil {
		p.logger().Error("probe source video failed", slog.String("job_id", jobContext.Job.ID), slog.String("media_id", jobContext.Media.ID), slog.Any("error", err))
		return domain.VideoEvidence{}, domain.PreprocessingArtifact{}, domain.Permanent(fmt.Errorf("probe source video: %w", err))
	}
	p.logger().Info("probed source video", slog.String("job_id", jobContext.Job.ID), slog.String("media_id", jobContext.Media.ID), slog.Int64("duration_micros", metadata.DurationMicros), slog.Bool("has_audio", metadata.HasAudio))
	boundaries, err := p.Tools.DetectScenes(ctx, sourcePath, p.Config.SceneThreshold, p.Config.MaxSceneBoundaries)
	if err != nil {
		p.logger().Error("detect video scenes failed", slog.String("job_id", jobContext.Job.ID), slog.String("media_id", jobContext.Media.ID), slog.Any("error", err))
		return domain.VideoEvidence{}, domain.PreprocessingArtifact{}, fmt.Errorf("detect video scenes: %w", err)
	}
	scenes := BuildScenes(metadata, boundaries, p.Config.Planner.MinSceneDuration)
	segments := BuildSegments(scenes, p.Config.Planner.MaxSegmentDuration)
	candidates := PlanFrameCandidates(metadata, scenes, segments, p.Config.Planner)
	p.logger().Info("planned video frames", slog.String("job_id", jobContext.Job.ID), slog.String("media_id", jobContext.Media.ID), slog.Int("scene_count", len(scenes)), slog.Int("segment_count", len(segments)), slog.Int("candidate_count", len(candidates)))
	if _, err := p.Repository.UpdateJob(ctx, jobContext.Job.ID, "RUNNING", 0.28, "", "", ""); err != nil {
		return domain.VideoEvidence{}, domain.PreprocessingArtifact{}, err
	}
	analyzedFrames := make([]AnalyzedFrame, 0, len(candidates))
	for index, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return domain.VideoEvidence{}, domain.PreprocessingArtifact{}, err
		}
		framePath := filepath.Join(workspace, fmt.Sprintf("candidate-%05d.jpg", index+1))
		if err := retry(ctx, p.Config.StageRetries, func() error {
			return p.Tools.ExtractFrame(ctx, sourcePath, candidate.TimestampMicros, framePath)
		}); err != nil {
			p.logger().Warn("skip failed frame candidate", slog.String("candidate_id", candidate.ID), slog.Any("error", err))
			continue
		}
		frame, err := InspectFrame(framePath)
		if err != nil {
			p.logger().Warn("skip unreadable frame candidate", slog.String("candidate_id", candidate.ID), slog.Any("error", err))
			continue
		}
		frame.MediaID = jobContext.Media.ID
		analyzedFrames = append(analyzedFrames, AnalyzedFrame{Candidate: candidate, Frame: frame})
	}
	selectedFrames := SelectDistinctFrames(analyzedFrames, MaxSelectedFrames(metadata, p.Config.Planner), p.Config.DuplicateHashDistance)
	if len(selectedFrames) == 0 {
		return domain.VideoEvidence{}, domain.PreprocessingArtifact{}, domain.Permanent(fmt.Errorf("no usable representative frames were extracted"))
	}
	prefix := artifactPrefix(jobContext.Job.UserID, jobContext.Media.ID, version, sourceETag)
	p.logger().Info("selected representative frames", slog.String("job_id", jobContext.Job.ID), slog.String("media_id", jobContext.Media.ID), slog.Int("selected_frame_count", len(selectedFrames)))
	for index := range selectedFrames {
		key := fmt.Sprintf("%s/frames/%s.jpg", prefix, selectedFrames[index].ID)
		if err := retry(ctx, p.Config.StageRetries, func() error {
			return p.Objects.UploadFile(ctx, key, selectedFrames[index].LocalPath, "image/jpeg")
		}); err != nil {
			return domain.VideoEvidence{}, domain.PreprocessingArtifact{}, fmt.Errorf("upload representative frame: %w", err)
		}
		selectedFrames[index].S3Key = key
	}
	if _, err := p.Repository.UpdateJob(ctx, jobContext.Job.ID, "RUNNING", 0.55, "", "", ""); err != nil {
		return domain.VideoEvidence{}, domain.PreprocessingArtifact{}, err
	}
	p.logger().Info("video frame evidence ready", slog.String("job_id", jobContext.Job.ID), slog.String("media_id", jobContext.Media.ID), slog.Int("frame_count", len(selectedFrames)))
	transcript, audioKey, err := p.extractTranscript(ctx, workspace, sourcePath, prefix, jobContext, sourceETag, metadata)
	if err != nil {
		p.logger().Warn("video transcription unavailable; continuing with visual evidence", slog.String("job_id", jobContext.Job.ID), slog.String("media_id", jobContext.Media.ID), slog.Any("error", err))
		transcript = domain.Transcript{}
		audioKey = ""
	}
	quality := ComputeQuality(metadata, scenes, selectedFrames)
	now := p.now().UTC()
	evidence := domain.VideoEvidence{
		SchemaVersion: 1, AnalysisVersion: version, MediaID: jobContext.Media.ID, ProjectID: jobContext.Project.ID,
		SourceS3Key: jobContext.Media.S3Key, SourceETag: sourceETag, Metadata: metadata, Scenes: scenes,
		Segments: segments, Frames: selectedFrames, Transcript: transcript, Quality: quality, AudioS3Key: audioKey, CreatedAt: now,
	}
	evidenceKey := prefix + "/evidence.json"
	if err := retry(ctx, p.Config.StageRetries, func() error { return p.Objects.PutJSON(ctx, evidenceKey, evidence) }); err != nil {
		return domain.VideoEvidence{}, domain.PreprocessingArtifact{}, fmt.Errorf("store preprocessing evidence: %w", err)
	}
	artifact = domain.PreprocessingArtifact{
		ID: domain.NewID("preprocessing"), UserID: jobContext.Job.UserID, ProjectID: jobContext.Project.ID,
		MediaID: jobContext.Media.ID, AnalysisVersion: version, SourceETag: sourceETag, EvidenceS3Key: evidenceKey,
		FrameCount: len(selectedFrames), Metadata: metadata, Quality: quality, CreatedAt: now,
	}
	artifact, err = p.Repository.SavePreprocessing(ctx, artifact)
	if err != nil {
		return domain.VideoEvidence{}, domain.PreprocessingArtifact{}, err
	}
	return evidence, artifact, nil
}

func (p *Processor) extractTranscript(ctx context.Context, workspace, sourcePath, prefix string, jobContext domain.JobContext, sourceETag string, metadata domain.VideoMetadata) (domain.Transcript, string, error) {
	if !p.Config.EnableTranscription || !metadata.HasAudio {
		return domain.Transcript{}, "", nil
	}
	if cached, err := p.Repository.GetTranscript(ctx, jobContext.Job.UserID, jobContext.Media.ID, p.Config.TranscriptionVersion, sourceETag); err == nil {
		normalized, validationErr := transcription.Validate(cached.Transcript, metadata.DurationMicros)
		if validationErr == nil {
			p.logger().Info("reusing cached video transcript", slog.String("job_id", jobContext.Job.ID), slog.String("media_id", jobContext.Media.ID), slog.Int("segment_count", len(normalized.Segments)))
			return normalized, "", nil
		}
		p.logger().Warn("cached video transcript invalid; retranscribing", slog.String("job_id", jobContext.Job.ID), slog.String("media_id", jobContext.Media.ID), slog.Any("error", validationErr))
	} else if !errors.Is(err, domain.ErrNotFound) {
		return domain.Transcript{}, "", err
	}
	p.logger().Info("starting video transcription", slog.String("job_id", jobContext.Job.ID), slog.String("media_id", jobContext.Media.ID))
	if p.Transcription == nil {
		return domain.Transcript{}, "", domain.Permanent(fmt.Errorf("transcription service is required when transcription is enabled"))
	}
	audioPath := filepath.Join(workspace, "speech.flac")
	if err := retry(ctx, p.Config.StageRetries, func() error { return p.Tools.ExtractAudio(ctx, sourcePath, audioPath) }); err != nil {
		return domain.Transcript{}, "", fmt.Errorf("extract speech audio: %w", err)
	}
	audioKey := prefix + "/audio/speech.flac"
	if err := retry(ctx, p.Config.StageRetries, func() error { return p.Objects.UploadFile(ctx, audioKey, audioPath, "audio/flac") }); err != nil {
		return domain.Transcript{}, "", fmt.Errorf("upload speech audio: %w", err)
	}
	result, err := p.Transcription.Transcribe(ctx, transcription.Request{
		UserID: jobContext.Job.UserID, ProjectID: jobContext.Project.ID, MediaID: jobContext.Media.ID,
		SourceETag: sourceETag, SourceS3Key: audioKey, SourceLocalPath: audioPath, MediaType: "audio",
		DurationMicros: metadata.DurationMicros, Version: p.Config.TranscriptionVersion,
	})
	if err != nil {
		return domain.Transcript{}, "", fmt.Errorf("transcribe video audio: %w", err)
	}
	p.logger().Info("video transcription ready", slog.String("job_id", jobContext.Job.ID), slog.String("media_id", jobContext.Media.ID), slog.Bool("has_speech", result.Record.Transcript.HasSpeech), slog.Int("segment_count", len(result.Record.Transcript.Segments)))
	return result.Record.Transcript, audioKey, nil
}

func (p *Processor) inputVersion(message domain.JobMessage) string {
	if value := strings.TrimSpace(message.Configuration["input_version"]); value != "" {
		return value
	}
	if value := strings.TrimSpace(message.Configuration["analysis_version"]); value != "" {
		return value
	}
	return p.Config.AnalysisVersion
}

func (p *Processor) understandingVersion(message domain.JobMessage) string {
	if value := strings.TrimSpace(message.Configuration["understanding_version"]); value != "" {
		return value
	}
	return p.Config.UnderstandingVersion
}

func (p *Processor) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now()
}

func (p *Processor) logger() *slog.Logger {
	if p.Logger != nil {
		return p.Logger
	}
	return slog.Default()
}

func validateMessage(message domain.JobMessage) error {
	if message.JobID == "" || message.ProjectID == "" || message.MediaID == "" || !isVideoAnalysisJobType(message.JobType) {
		return fmt.Errorf("analysis message requires jobId, projectId, mediaId, and a video analysis job type")
	}
	return nil
}

func isVideoAnalysisJobType(jobType string) bool {
	switch jobType {
	case "analysis", "MEDIA_PREPROCESS", "TRANSCRIBE", "VIDEO_UNDERSTAND":
		return true
	default:
		return false
	}
}

func validCachedEvidence(evidence domain.VideoEvidence, jobContext domain.JobContext, version, sourceETag string) bool {
	return evidence.SchemaVersion == 1 && evidence.MediaID == jobContext.Media.ID && evidence.ProjectID == jobContext.Project.ID &&
		evidence.AnalysisVersion == version && evidence.SourceETag == sourceETag && len(evidence.Frames) > 0
}

func retry(ctx context.Context, attempts int, operation func() error) error {
	if attempts <= 0 {
		attempts = 1
	}
	var lastError error
	for attempt := 1; attempt <= attempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := operation(); err == nil {
			return nil
		} else {
			lastError = err
		}
		if attempt < attempts {
			delay := time.Duration(1<<(attempt-1)) * 250 * time.Millisecond
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
	}
	return lastError
}

func artifactPrefix(userID, mediaID, version, sourceETag string) string {
	hash := sha256.Sum256([]byte(sourceETag))
	return fmt.Sprintf("users/%s/video-analysis/%s/%s/%s", safeComponent(userID), safeComponent(mediaID), safeComponent(version), hex.EncodeToString(hash[:8]))
}

func safeComponent(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	var builder strings.Builder
	for _, character := range value {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '-' || character == '_' {
			builder.WriteRune(character)
		} else {
			builder.WriteByte('-')
		}
	}
	return strings.Trim(builder.String(), "-")
}

func sourceExtension(fileName string) string {
	extension := strings.ToLower(filepath.Ext(fileName))
	if len(extension) > 10 || extension == "" {
		return ".mov"
	}
	return extension
}

func parseBool(value string) bool {
	parsed, _ := strconv.ParseBool(value)
	return parsed
}

func truncate(value string, maximum int) string {
	if len(value) <= maximum {
		return value
	}
	return value[:maximum]
}
