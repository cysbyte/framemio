package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	analysisvalidation "github.com/framemio/video-intelligence-worker/internal/analysis"
	"github.com/framemio/video-intelligence-worker/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository interface {
	LoadJobContext(ctx context.Context, message domain.JobMessage) (domain.JobContext, error)
	FindLatestVideoAnalysis(ctx context.Context, userID, videoID, inputVersion, understandingVersion, provider, model, sourceETag string) (domain.VideoAnalysis, error)
	GetVideoAnalysis(ctx context.Context, userID, videoID, analysisVersion string) (domain.VideoAnalysis, error)
	SaveVideoAnalysis(ctx context.Context, analysis domain.VideoAnalysis) (domain.VideoAnalysis, error)
	GetTranscript(ctx context.Context, userID, mediaID, version, sourceETag string) (domain.TranscriptionRecord, error)
	SaveTranscript(ctx context.Context, transcript domain.TranscriptionRecord) (domain.TranscriptionRecord, error)
	GetPreprocessing(ctx context.Context, userID, projectID, mediaID, version, sourceETag string) (domain.PreprocessingArtifact, error)
	SavePreprocessing(ctx context.Context, artifact domain.PreprocessingArtifact) (domain.PreprocessingArtifact, error)
	StartJobAttempt(ctx context.Context, jobID string) (domain.ProcessingJob, error)
	UpdateJob(ctx context.Context, jobID, status string, progress float64, errorCode, errorMessage, artifactS3Key string) (domain.ProcessingJob, error)
	SetProjectStatus(ctx context.Context, userID, projectID, status string) error
}

type Postgres struct {
	pool *pgxpool.Pool
}

const migrationSQL = `
	CREATE TABLE IF NOT EXISTS framemio_domain_records (
		kind TEXT NOT NULL,
		id TEXT NOT NULL,
		user_id TEXT NOT NULL,
		project_id TEXT NOT NULL DEFAULT '',
		media_id TEXT NOT NULL DEFAULT '',
		logical_key TEXT NOT NULL DEFAULT '',
		data JSONB NOT NULL,
		created_at TIMESTAMPTZ NOT NULL,
		updated_at TIMESTAMPTZ NOT NULL,
		PRIMARY KEY (kind, id)
	);
	CREATE UNIQUE INDEX IF NOT EXISTS framemio_domain_records_logical_key
	ON framemio_domain_records (kind, user_id, logical_key)
	WHERE logical_key <> '';
	CREATE INDEX IF NOT EXISTS framemio_video_analysis_lookup
	ON framemio_domain_records (kind, user_id, media_id, logical_key, created_at DESC)
	WHERE kind IN ('video_preprocessing', 'video_analysis');
	CREATE INDEX IF NOT EXISTS framemio_video_analysis_video_id
	ON framemio_domain_records (media_id)
	WHERE kind = 'video_analysis';
	CREATE INDEX IF NOT EXISTS framemio_video_analysis_version
	ON framemio_domain_records ((data->>'analysis_version'))
	WHERE kind = 'video_analysis';
	CREATE INDEX IF NOT EXISTS framemio_video_analysis_project_id
	ON framemio_domain_records (project_id)
	WHERE kind = 'video_analysis';
	CREATE INDEX IF NOT EXISTS framemio_video_analysis_processing_status
	ON framemio_domain_records ((data->>'processing_status'))
	WHERE kind = 'video_analysis';
	CREATE INDEX IF NOT EXISTS framemio_video_analysis_latest
	ON framemio_domain_records (user_id, media_id, created_at DESC)
	WHERE kind = 'video_analysis';
	CREATE OR REPLACE FUNCTION framemio_reject_video_analysis_update()
	RETURNS TRIGGER AS $$
	BEGIN
		IF OLD.kind = 'video_analysis' THEN
			RAISE EXCEPTION 'video analysis records are immutable';
		END IF;
		RETURN NEW;
	END;
	$$ LANGUAGE plpgsql;
	DO $$
	BEGIN
		IF NOT EXISTS (SELECT 1 FROM pg_trigger WHERE tgname = 'framemio_video_analysis_immutable') THEN
			CREATE TRIGGER framemio_video_analysis_immutable
			BEFORE UPDATE ON framemio_domain_records
			FOR EACH ROW EXECUTE FUNCTION framemio_reject_video_analysis_update();
		END IF;
	END;
	$$;
	CREATE INDEX IF NOT EXISTS framemio_transcript_lookup
	ON framemio_domain_records (kind, user_id, media_id, logical_key, created_at DESC)
	WHERE kind = 'transcript'`

func NewPostgres(ctx context.Context, databaseURL string) (*Postgres, error) {
	pool, err := newPostgresPool(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("connect postgres: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return &Postgres{pool: pool}, nil
}

func (s *Postgres) Close() { s.pool.Close() }

func (s *Postgres) Migrate(ctx context.Context) error {
	connection, err := s.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire migration connection: %w", err)
	}
	defer connection.Release()

	if _, err := connection.Exec(ctx, "SELECT pg_advisory_lock(450876624497)"); err != nil {
		return fmt.Errorf("acquire video analysis migration lock: %w", err)
	}
	defer connection.Exec(context.Background(), "SELECT pg_advisory_unlock(450876624497)")

	_, err = connection.Exec(ctx, migrationSQL)
	if err != nil {
		return fmt.Errorf("create video analysis index: %w", err)
	}
	return nil
}

func (s *Postgres) GetTranscript(ctx context.Context, userID, mediaID, version, sourceETag string) (domain.TranscriptionRecord, error) {
	var transcript domain.TranscriptionRecord
	if err := s.getLogical(ctx, "transcript", userID, logicalKey(mediaID, version, sourceETag), &transcript); err != nil {
		return domain.TranscriptionRecord{}, err
	}
	return transcript, nil
}

func (s *Postgres) SaveTranscript(ctx context.Context, transcript domain.TranscriptionRecord) (domain.TranscriptionRecord, error) {
	key := logicalKey(transcript.MediaID, transcript.Version, transcript.SourceETag)
	inserted, err := s.insert(ctx, "transcript", transcript.ID, transcript.UserID, transcript.ProjectID, transcript.MediaID, key, transcript)
	if err != nil {
		return domain.TranscriptionRecord{}, err
	}
	if inserted {
		return transcript, nil
	}
	return s.GetTranscript(ctx, transcript.UserID, transcript.MediaID, transcript.Version, transcript.SourceETag)
}

func (s *Postgres) LoadJobContext(ctx context.Context, message domain.JobMessage) (domain.JobContext, error) {
	var result domain.JobContext
	if err := s.getByID(ctx, "job", message.JobID, &result.Job); err != nil {
		return domain.JobContext{}, err
	}
	if result.Job.ProjectID != message.ProjectID || result.Job.MediaID != message.MediaID || !isVideoAnalysisJobType(result.Job.Type) || !isVideoAnalysisJobType(message.JobType) {
		return domain.JobContext{}, domain.Permanent(fmt.Errorf("job identifiers do not match queue message"))
	}
	if err := s.getOwned(ctx, "media", result.Job.UserID, message.MediaID, &result.Media); err != nil {
		return domain.JobContext{}, err
	}
	if err := s.getOwned(ctx, "project", result.Job.UserID, message.ProjectID, &result.Project); err != nil {
		return domain.JobContext{}, err
	}
	if result.Media.UserID != result.Project.UserID || result.Media.UserID != result.Job.UserID {
		return domain.JobContext{}, domain.Permanent(fmt.Errorf("job records have inconsistent ownership"))
	}
	mediaBelongsToProject := false
	for _, mediaID := range result.Project.MediaIDs {
		if mediaID == result.Media.ID {
			mediaBelongsToProject = true
			break
		}
	}
	if !mediaBelongsToProject {
		return domain.JobContext{}, domain.Permanent(fmt.Errorf("media does not belong to project"))
	}
	return result, nil
}

func (s *Postgres) FindLatestVideoAnalysis(ctx context.Context, userID, videoID, inputVersion, understandingVersion, provider, model, sourceETag string) (domain.VideoAnalysis, error) {
	var data []byte
	err := s.pool.QueryRow(ctx, `
		SELECT data
		FROM framemio_domain_records
		WHERE kind='video_analysis' AND user_id=$1 AND media_id=$2
		  AND data->>'input_version'=$3
		  AND data->>'understanding_version'=$4
		  AND data->>'provider'=$5
		  AND data->>'model'=$6
		  AND COALESCE(data->>'source_etag','')=$7
		  AND data->>'processing_status'=$8
		ORDER BY created_at DESC
		LIMIT 1`, userID, videoID, inputVersion, understandingVersion, provider, model, sourceETag, domain.VideoAnalysisStatusCompleted).Scan(&data)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.VideoAnalysis{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.VideoAnalysis{}, err
	}
	return decodeVideoAnalysis(data)
}

func (s *Postgres) GetVideoAnalysis(ctx context.Context, userID, videoID, analysisVersion string) (domain.VideoAnalysis, error) {
	var analysis domain.VideoAnalysis
	if err := s.getLogical(ctx, "video_analysis", userID, logicalKey(videoID, analysisVersion), &analysis); err != nil {
		return domain.VideoAnalysis{}, err
	}
	if err := analysisvalidation.Validate(analysis); err != nil {
		return domain.VideoAnalysis{}, fmt.Errorf("validate stored video analysis: %w", err)
	}
	return analysis, nil
}

func (s *Postgres) SaveVideoAnalysis(ctx context.Context, analysis domain.VideoAnalysis) (domain.VideoAnalysis, error) {
	if err := analysisvalidation.Validate(analysis); err != nil {
		return domain.VideoAnalysis{}, fmt.Errorf("validate video analysis: %w", err)
	}
	key := logicalKey(analysis.VideoID, analysis.AnalysisVersion)
	inserted, err := s.insert(ctx, "video_analysis", analysis.ID, analysis.UserID, analysis.ProjectID, analysis.VideoID, key, analysis)
	if err != nil {
		return domain.VideoAnalysis{}, err
	}
	if inserted {
		return analysis, nil
	}
	return s.GetVideoAnalysis(ctx, analysis.UserID, analysis.VideoID, analysis.AnalysisVersion)
}

func decodeVideoAnalysis(data []byte) (domain.VideoAnalysis, error) {
	analysis, err := analysisvalidation.Decode(data)
	if err != nil {
		return domain.VideoAnalysis{}, fmt.Errorf("decode video_analysis: %w", err)
	}
	return analysis, nil
}

func (s *Postgres) GetPreprocessing(ctx context.Context, userID, projectID, mediaID, version, sourceETag string) (domain.PreprocessingArtifact, error) {
	var artifact domain.PreprocessingArtifact
	if err := s.getLogical(ctx, "video_preprocessing", userID, logicalKey(projectID, mediaID, version, sourceETag), &artifact); err != nil {
		return domain.PreprocessingArtifact{}, err
	}
	return artifact, nil
}

func (s *Postgres) SavePreprocessing(ctx context.Context, artifact domain.PreprocessingArtifact) (domain.PreprocessingArtifact, error) {
	key := logicalKey(artifact.ProjectID, artifact.MediaID, artifact.AnalysisVersion, artifact.SourceETag)
	inserted, err := s.insert(ctx, "video_preprocessing", artifact.ID, artifact.UserID, artifact.ProjectID, artifact.MediaID, key, artifact)
	if err != nil {
		return domain.PreprocessingArtifact{}, err
	}
	if inserted {
		return artifact, nil
	}
	return s.GetPreprocessing(ctx, artifact.UserID, artifact.ProjectID, artifact.MediaID, artifact.AnalysisVersion, artifact.SourceETag)
}

func (s *Postgres) StartJobAttempt(ctx context.Context, jobID string) (domain.ProcessingJob, error) {
	return s.mutateJob(ctx, jobID, func(job *domain.ProcessingJob, now time.Time) error {
		if isTerminalJobStatus(job.Status) {
			return domain.Permanent(fmt.Errorf("terminal job cannot start another attempt"))
		}
		job.Status = "RUNNING"
		job.Attempt++
		job.Progress = max(job.Progress, 0.02)
		job.ErrorCode = ""
		job.ErrorMessage = ""
		job.UpdatedAt = now
		return nil
	})
}

func (s *Postgres) UpdateJob(ctx context.Context, jobID, status string, progress float64, errorCode, errorMessage, artifactS3Key string) (domain.ProcessingJob, error) {
	return s.mutateJob(ctx, jobID, func(job *domain.ProcessingJob, now time.Time) error {
		if isTerminalJobStatus(job.Status) {
			if job.Status != status {
				return domain.Permanent(fmt.Errorf("terminal job cannot transition from %s to %s", job.Status, status))
			}
			return nil
		}
		job.Status = status
		job.Progress = max(job.Progress, min(1, max(0, progress)))
		job.ErrorCode = errorCode
		job.ErrorMessage = errorMessage
		if artifactS3Key != "" {
			job.ArtifactS3Key = artifactS3Key
			job.ArtifactType = "application/json"
		}
		job.UpdatedAt = now
		if status == "COMPLETED" {
			job.Progress = 1
			job.CompletedAt = &now
		} else if status == "FAILED" {
			job.CompletedAt = &now
		}
		return nil
	})
}

func (s *Postgres) mutateJob(ctx context.Context, jobID string, mutation func(*domain.ProcessingJob, time.Time) error) (domain.ProcessingJob, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.ProcessingJob{}, fmt.Errorf("begin job update: %w", err)
	}
	defer tx.Rollback(ctx)
	var data []byte
	if err := tx.QueryRow(ctx, `SELECT data FROM framemio_domain_records WHERE kind='job' AND id=$1 FOR UPDATE`, jobID).Scan(&data); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ProcessingJob{}, domain.ErrNotFound
		}
		return domain.ProcessingJob{}, err
	}
	var job domain.ProcessingJob
	if err := json.Unmarshal(data, &job); err != nil {
		return domain.ProcessingJob{}, fmt.Errorf("decode job: %w", err)
	}
	now := time.Now().UTC()
	if err := mutation(&job, now); err != nil {
		return domain.ProcessingJob{}, err
	}
	encoded, err := json.Marshal(job)
	if err != nil {
		return domain.ProcessingJob{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE framemio_domain_records SET data=$1,updated_at=$2 WHERE kind='job' AND id=$3`, jsonbParam(encoded), now, jobID); err != nil {
		return domain.ProcessingJob{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.ProcessingJob{}, fmt.Errorf("commit job update: %w", err)
	}
	return job, nil
}

func (s *Postgres) SetProjectStatus(ctx context.Context, userID, projectID, status string) error {
	var project domain.StoryProject
	if err := s.getOwned(ctx, "project", userID, projectID, &project); err != nil {
		return err
	}
	project.Status = status
	project.UpdatedAt = time.Now().UTC()
	data, err := json.Marshal(project)
	if err != nil {
		return err
	}
	result, err := s.pool.Exec(ctx, `UPDATE framemio_domain_records SET data=$1,updated_at=$2 WHERE kind='project' AND id=$3 AND user_id=$4`, jsonbParam(data), project.UpdatedAt, projectID, userID)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return domain.ErrNotFound
	}
	return nil
}

func (s *Postgres) getByID(ctx context.Context, kind, id string, destination any) error {
	var data []byte
	if err := s.pool.QueryRow(ctx, `SELECT data FROM framemio_domain_records WHERE kind=$1 AND id=$2`, kind, id).Scan(&data); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrNotFound
		}
		return err
	}
	return decode(kind, data, destination)
}

func (s *Postgres) getOwned(ctx context.Context, kind, userID, id string, destination any) error {
	var data []byte
	if err := s.pool.QueryRow(ctx, `SELECT data FROM framemio_domain_records WHERE kind=$1 AND user_id=$2 AND id=$3`, kind, userID, id).Scan(&data); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrNotFound
		}
		return err
	}
	return decode(kind, data, destination)
}

func (s *Postgres) getLogical(ctx context.Context, kind, userID, key string, destination any) error {
	var data []byte
	if err := s.pool.QueryRow(ctx, `SELECT data FROM framemio_domain_records WHERE kind=$1 AND user_id=$2 AND logical_key=$3`, kind, userID, key).Scan(&data); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrNotFound
		}
		return err
	}
	return decode(kind, data, destination)
}

func (s *Postgres) insert(ctx context.Context, kind, id, userID, projectID, mediaID, key string, value any) (bool, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return false, fmt.Errorf("encode %s: %w", kind, err)
	}
	now := time.Now().UTC()
	result, err := s.pool.Exec(ctx, `
		INSERT INTO framemio_domain_records
		(kind,id,user_id,project_id,media_id,logical_key,data,created_at,updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$8)
		ON CONFLICT DO NOTHING`, kind, id, userID, projectID, mediaID, key, jsonbParam(data), now)
	if err != nil {
		return false, fmt.Errorf("insert %s: %w", kind, err)
	}
	return result.RowsAffected() == 1, nil
}

func decode(kind string, data []byte, destination any) error {
	if err := json.Unmarshal(data, destination); err != nil {
		return fmt.Errorf("decode %s: %w", kind, err)
	}
	return nil
}

func jsonbParam(data []byte) string {
	return string(data)
}

func logicalKey(values ...string) string {
	hash := sha256.New()
	for _, value := range values {
		_, _ = fmt.Fprintf(hash, "%d:", len(value))
		_, _ = hash.Write([]byte(value))
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func isVideoAnalysisJobType(jobType string) bool {
	switch jobType {
	case "analysis", "MEDIA_PREPROCESS", "TRANSCRIBE", "VIDEO_UNDERSTAND":
		return true
	default:
		return false
	}
}

func isTerminalJobStatus(status string) bool {
	switch status {
	case "COMPLETED", "FAILED", "CANCELLED", "succeeded", "failed", "cancelled", "canceled":
		return true
	default:
		return false
	}
}
