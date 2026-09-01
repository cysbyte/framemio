package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/framemio/backend/internal/models"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresStore struct {
	pool *pgxpool.Pool
}

func NewPostgresStore(ctx context.Context, dsn string) (*PostgresStore, error) {
	pool, err := newPostgresPool(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("connect postgres: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return &PostgresStore{pool: pool}, nil
}

func (s *PostgresStore) Close() { s.pool.Close() }

func (s *PostgresStore) Migrate(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, domainRecordsMigration)
	if err != nil {
		return fmt.Errorf("migrate postgres: %w", err)
	}
	return nil
}

type record struct {
	Kind       string
	ID         string
	UserID     string
	ProjectID  string
	MediaID    string
	LogicalKey string
	Data       []byte
}

func (s *PostgresStore) get(ctx context.Context, kind, userID, id string, destination any) error {
	var data []byte
	err := s.pool.QueryRow(ctx, `
		SELECT data FROM framemio_domain_records
		WHERE kind = $1 AND id = $2 AND user_id = $3`, kind, id, userID).Scan(&data)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("get %s: %w", kind, err)
	}
	if err := json.Unmarshal(data, destination); err != nil {
		return fmt.Errorf("decode %s: %w", kind, err)
	}
	return nil
}

func (s *PostgresStore) getByLogical(ctx context.Context, kind, userID, logicalKey string, destination any) error {
	var data []byte
	err := s.pool.QueryRow(ctx, `
		SELECT data FROM framemio_domain_records
		WHERE kind = $1 AND user_id = $2 AND logical_key = $3`, kind, userID, logicalKey).Scan(&data)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("get %s by key: %w", kind, err)
	}
	if err := json.Unmarshal(data, destination); err != nil {
		return fmt.Errorf("decode %s: %w", kind, err)
	}
	return nil
}

func postgresLogicalKey(parts ...string) string {
	return strings.Join(parts, ":")
}

// videoAnalysisLogicalKey must stay byte-identical to logicalKey in
// video-intelligence-worker/internal/store/postgres.go. VideoAnalysis rows are
// written by that worker and read back here, so the two services have to derive
// the same logical_key or every lookup misses and the StoryPlan validator
// rejects plans whose analysis is present. The length prefix keeps the digest
// unambiguous for values that could otherwise concatenate the same way.
func videoAnalysisLogicalKey(values ...string) string {
	hash := sha256.New()
	for _, value := range values {
		_, _ = fmt.Fprintf(hash, "%d:", len(value))
		_, _ = hash.Write([]byte(value))
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func jsonbParam(data []byte) string {
	return string(data)
}

func (s *PostgresStore) put(ctx context.Context, kind, id, userID, projectID, mediaID, logicalKey string, value any) (bool, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return false, fmt.Errorf("encode %s: %w", kind, err)
	}
	now := time.Now().UTC()
	result, err := s.pool.Exec(ctx, `
		INSERT INTO framemio_domain_records
		(kind, id, user_id, project_id, media_id, logical_key, data, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$8)
		ON CONFLICT DO NOTHING`,
		kind, id, userID, projectID, mediaID, logicalKey, jsonbParam(data), now)
	if err != nil {
		return false, fmt.Errorf("put %s: %w", kind, err)
	}
	return result.RowsAffected() == 1, nil
}

func (s *PostgresStore) EnsureUser(ctx context.Context, user models.User) (models.User, error) {
	data, err := json.Marshal(user)
	if err != nil {
		return models.User{}, err
	}
	now := time.Now().UTC()
	_, err = s.pool.Exec(ctx, `
		INSERT INTO framemio_domain_records
		(kind,id,user_id,logical_key,data,created_at,updated_at)
		VALUES ('user',$1,$1,$1,$2,$3,$3)
		ON CONFLICT DO NOTHING`, user.ID, jsonbParam(data), now)
	if err != nil {
		return models.User{}, fmt.Errorf("ensure user: %w", err)
	}
	var existing models.User
	if err := s.get(ctx, "user", user.ID, user.ID, &existing); err != nil {
		return models.User{}, err
	}
	return existing, nil
}

func (s *PostgresStore) GetMedia(ctx context.Context, userID, mediaID string) (models.MediaAsset, error) {
	var media models.MediaAsset
	if err := s.get(ctx, "media", userID, mediaID, &media); err != nil {
		return models.MediaAsset{}, err
	}
	return media, nil
}

func (s *PostgresStore) CreateMediaUpload(ctx context.Context, media models.MediaAsset, session models.UploadSession) error {
	mediaData, err := json.Marshal(media)
	if err != nil {
		return fmt.Errorf("encode media: %w", err)
	}
	sessionData, err := json.Marshal(session)
	if err != nil {
		return fmt.Errorf("encode upload: %w", err)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin media upload transaction: %w", err)
	}
	defer tx.Rollback(ctx)
	now := time.Now().UTC()
	mediaResult, err := tx.Exec(ctx, `
		INSERT INTO framemio_domain_records
		(kind,id,user_id,project_id,media_id,logical_key,data,created_at,updated_at)
		VALUES ('media',$1,$2,'',$1,'',$3,$4,$4)
		ON CONFLICT DO NOTHING`, media.ID, media.UserID, jsonbParam(mediaData), now)
	if err != nil {
		return fmt.Errorf("insert media: %w", err)
	}
	if mediaResult.RowsAffected() != 1 {
		return ErrConflict
	}
	uploadResult, err := tx.Exec(ctx, `
		INSERT INTO framemio_domain_records
		(kind,id,user_id,project_id,media_id,logical_key,data,created_at,updated_at)
		VALUES ('upload',$1,$2,'',$3,$4,$5,$6,$6)
		ON CONFLICT DO NOTHING`, session.ID, session.UserID, session.MediaID, session.IdempotencyKey, jsonbParam(sessionData), now)
	if err != nil {
		return fmt.Errorf("insert upload: %w", err)
	}
	if uploadResult.RowsAffected() != 1 {
		return ErrConflict
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit media upload: %w", err)
	}
	return nil
}

func (s *PostgresStore) UpdateUploadSession(ctx context.Context, session models.UploadSession) error {
	data, err := json.Marshal(session)
	if err != nil {
		return fmt.Errorf("encode upload: %w", err)
	}
	result, err := s.pool.Exec(ctx, `UPDATE framemio_domain_records SET data=$1,updated_at=$2 WHERE kind='upload' AND id=$3 AND user_id=$4`, jsonbParam(data), time.Now().UTC(), session.ID, session.UserID)
	if err != nil {
		return fmt.Errorf("update upload: %w", err)
	}
	if result.RowsAffected() != 1 {
		return ErrNotFound
	}
	return nil
}

func (s *PostgresStore) GetUploadSessionByIdempotency(ctx context.Context, userID, key string) (models.UploadSession, error) {
	var session models.UploadSession
	if err := s.getByLogical(ctx, "upload", userID, key, &session); err != nil {
		return models.UploadSession{}, err
	}
	return session, nil
}

func (s *PostgresStore) GetReadyMediaByUploadIdempotencyPrefix(ctx context.Context, userID, prefix string) (models.MediaAsset, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT media.data
		FROM framemio_domain_records upload
		JOIN framemio_domain_records media
			ON media.kind = 'media'
			AND media.user_id = upload.user_id
			AND media.id = upload.media_id
		WHERE upload.kind = 'upload'
			AND upload.user_id = $1
			AND left(upload.logical_key, length($2)) = $2
		ORDER BY media.updated_at DESC`, userID, prefix)
	if err != nil {
		return models.MediaAsset{}, fmt.Errorf("get media by upload idempotency prefix: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var data []byte
		if err := rows.Scan(&data); err != nil {
			return models.MediaAsset{}, fmt.Errorf("scan media by upload idempotency prefix: %w", err)
		}
		var media models.MediaAsset
		if err := json.Unmarshal(data, &media); err != nil {
			return models.MediaAsset{}, fmt.Errorf("decode media by upload idempotency prefix: %w", err)
		}
		if media.UserID == userID && media.Status == models.MediaStatusReady {
			return media, nil
		}
	}
	if err := rows.Err(); err != nil {
		return models.MediaAsset{}, fmt.Errorf("iterate media by upload idempotency prefix: %w", err)
	}
	return models.MediaAsset{}, ErrNotFound
}

func (s *PostgresStore) GetUploadSession(ctx context.Context, userID, uploadID string) (models.UploadSession, error) {
	var session models.UploadSession
	if err := s.get(ctx, "upload", userID, uploadID, &session); err != nil {
		return models.UploadSession{}, err
	}
	return session, nil
}

func (s *PostgresStore) MarkMediaReady(ctx context.Context, userID, mediaID, uploadID, s3Key, etag string) (models.MediaAsset, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return models.MediaAsset{}, fmt.Errorf("begin upload completion: %w", err)
	}
	defer tx.Rollback(ctx)
	var mediaData, sessionData []byte
	if err := tx.QueryRow(ctx, `SELECT data FROM framemio_domain_records WHERE kind='media' AND id=$1 AND user_id=$2 FOR UPDATE`, mediaID, userID).Scan(&mediaData); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return models.MediaAsset{}, ErrNotFound
		}
		return models.MediaAsset{}, err
	}
	if err := tx.QueryRow(ctx, `SELECT data FROM framemio_domain_records WHERE kind='upload' AND id=$1 AND user_id=$2 FOR UPDATE`, uploadID, userID).Scan(&sessionData); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return models.MediaAsset{}, ErrNotFound
		}
		return models.MediaAsset{}, err
	}
	var media models.MediaAsset
	var session models.UploadSession
	if err := json.Unmarshal(mediaData, &media); err != nil {
		return models.MediaAsset{}, fmt.Errorf("decode media: %w", err)
	}
	if err := json.Unmarshal(sessionData, &session); err != nil {
		return models.MediaAsset{}, fmt.Errorf("decode upload: %w", err)
	}
	if session.MediaID != mediaID || session.S3Key != s3Key {
		return models.MediaAsset{}, ErrConflict
	}
	if media.Status == models.MediaStatusReady {
		if err := tx.Commit(ctx); err != nil {
			return models.MediaAsset{}, err
		}
		return media, nil
	}
	now := time.Now().UTC()
	media.Status = models.MediaStatusReady
	media.ETag = etag
	media.UpdatedAt = now
	session.CompletedAt = &now
	mediaData, err = json.Marshal(media)
	if err != nil {
		return models.MediaAsset{}, err
	}
	sessionData, err = json.Marshal(session)
	if err != nil {
		return models.MediaAsset{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE framemio_domain_records SET data=$1,updated_at=$2 WHERE kind='media' AND id=$3 AND user_id=$4`, jsonbParam(mediaData), now, mediaID, userID); err != nil {
		return models.MediaAsset{}, fmt.Errorf("mark media ready: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE framemio_domain_records SET data=$1,updated_at=$2 WHERE kind='upload' AND id=$3 AND user_id=$4`, jsonbParam(sessionData), now, uploadID, userID); err != nil {
		return models.MediaAsset{}, fmt.Errorf("complete upload: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return models.MediaAsset{}, fmt.Errorf("commit upload completion: %w", err)
	}
	return media, nil
}

func (s *PostgresStore) CreateProject(ctx context.Context, project models.StoryProject, idempotencyKey string) (models.StoryProject, bool, error) {
	if idempotencyKey != "" {
		var existing models.StoryProject
		if err := s.getByLogical(ctx, "project", project.UserID, idempotencyKey, &existing); err == nil {
			return existing, true, nil
		}
	}
	for _, mediaID := range project.MediaIDs {
		media, err := s.GetMedia(ctx, project.UserID, mediaID)
		if err != nil || media.Status != models.MediaStatusReady {
			return models.StoryProject{}, false, ErrConflict
		}
	}
	inserted, err := s.put(ctx, "project", project.ID, project.UserID, project.ID, "", idempotencyKey, project)
	if err != nil {
		return models.StoryProject{}, false, err
	}
	if !inserted {
		var existing models.StoryProject
		if lookupErr := s.getByLogical(ctx, "project", project.UserID, idempotencyKey, &existing); lookupErr == nil {
			return existing, true, nil
		}
		return models.StoryProject{}, false, ErrConflict
	}
	return project, false, nil
}

func (s *PostgresStore) AddProjectMedia(ctx context.Context, userID, projectID string, mediaIDs []string) (models.StoryProject, error) {
	project, err := s.GetProject(ctx, userID, projectID)
	if err != nil {
		return models.StoryProject{}, err
	}
	existing := make(map[string]struct{}, len(project.MediaIDs))
	existingSourceAssets := make(map[string]struct{}, len(project.MediaIDs))
	for _, mediaID := range project.MediaIDs {
		existing[mediaID] = struct{}{}
		if media, err := s.GetMedia(ctx, userID, mediaID); err == nil {
			if sourceAssetID := strings.TrimSpace(media.SourceAssetID); sourceAssetID != "" {
				existingSourceAssets[sourceAssetID] = struct{}{}
			}
		}
	}
	changed := false
	for _, mediaID := range mediaIDs {
		mediaID = strings.TrimSpace(mediaID)
		if mediaID == "" {
			continue
		}
		media, err := s.GetMedia(ctx, userID, mediaID)
		if err != nil {
			return models.StoryProject{}, err
		}
		if media.Status != models.MediaStatusReady {
			return models.StoryProject{}, ErrConflict
		}
		if _, ok := existing[mediaID]; ok {
			continue
		}
		if sourceAssetID := strings.TrimSpace(media.SourceAssetID); sourceAssetID != "" {
			if _, ok := existingSourceAssets[sourceAssetID]; ok {
				continue
			}
			existingSourceAssets[sourceAssetID] = struct{}{}
		}
		project.MediaIDs = append(project.MediaIDs, mediaID)
		existing[mediaID] = struct{}{}
		changed = true
	}
	if !changed {
		return project, nil
	}
	project.UpdatedAt = time.Now().UTC()
	data, err := json.Marshal(project)
	if err != nil {
		return models.StoryProject{}, err
	}
	result, err := s.pool.Exec(ctx, `UPDATE framemio_domain_records SET data=$1,updated_at=$2 WHERE kind='project' AND id=$3 AND user_id=$4`, jsonbParam(data), project.UpdatedAt, projectID, userID)
	if err != nil {
		return models.StoryProject{}, fmt.Errorf("add project media: %w", err)
	}
	if result.RowsAffected() == 0 {
		return models.StoryProject{}, ErrNotFound
	}
	return project, nil
}

func (s *PostgresStore) GetProject(ctx context.Context, userID, projectID string) (models.StoryProject, error) {
	var project models.StoryProject
	if err := s.get(ctx, "project", userID, projectID, &project); err != nil {
		return models.StoryProject{}, err
	}
	return project, nil
}

func (s *PostgresStore) ListProjects(ctx context.Context, userID string) ([]models.StoryProject, error) {
	rows, err := s.pool.Query(ctx, `SELECT data FROM framemio_domain_records WHERE kind='project' AND user_id=$1 ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	defer rows.Close()
	projects := make([]models.StoryProject, 0)
	for rows.Next() {
		var data []byte
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		var project models.StoryProject
		if err := json.Unmarshal(data, &project); err != nil {
			return nil, fmt.Errorf("decode project: %w", err)
		}
		projects = append(projects, project)
	}
	return projects, rows.Err()
}

func (s *PostgresStore) DeleteProject(ctx context.Context, userID, projectID string) error {
	if _, err := s.GetProject(ctx, userID, projectID); err != nil {
		return err
	}
	_, err := s.pool.Exec(ctx, `
		DELETE FROM framemio_domain_records
		WHERE user_id=$1 AND (
			(kind='project' AND id=$2) OR
			(project_id=$2 AND kind IN ('analysis','video_analysis','story_plan','job'))
		)`, userID, projectID)
	if err != nil {
		return fmt.Errorf("delete project: %w", err)
	}
	return nil
}

func (s *PostgresStore) SetProjectStatus(ctx context.Context, userID, projectID string, status models.ProjectStatus) error {
	var project models.StoryProject
	if err := s.get(ctx, "project", userID, projectID, &project); err != nil {
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
		return ErrNotFound
	}
	return nil
}

func (s *PostgresStore) SetProjectCreditOperationKey(ctx context.Context, userID, projectID, creditOperationKey string) error {
	var project models.StoryProject
	if err := s.get(ctx, "project", userID, projectID, &project); err != nil {
		return err
	}
	project.CreditOperationKey = creditOperationKey
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
		return ErrNotFound
	}
	return nil
}

func (s *PostgresStore) CreateJob(ctx context.Context, job models.ProcessingJob, idempotencyKey string) (models.ProcessingJob, bool, error) {
	if idempotencyKey != "" {
		var existing models.ProcessingJob
		if err := s.getByLogical(ctx, "job", job.UserID, idempotencyKey, &existing); err == nil {
			return existing, true, nil
		}
	}
	inserted, err := s.put(ctx, "job", job.ID, job.UserID, job.ProjectID, job.MediaID, idempotencyKey, job)
	if err != nil {
		return models.ProcessingJob{}, false, err
	}
	if !inserted {
		var existing models.ProcessingJob
		if lookupErr := s.getByLogical(ctx, "job", job.UserID, idempotencyKey, &existing); lookupErr == nil {
			return existing, true, nil
		}
		return models.ProcessingJob{}, false, ErrConflict
	}
	return job, false, nil
}

func (s *PostgresStore) GetJob(ctx context.Context, userID, jobID string) (models.ProcessingJob, error) {
	var job models.ProcessingJob
	if err := s.get(ctx, "job", userID, jobID, &job); err != nil {
		return models.ProcessingJob{}, err
	}
	return job, nil
}

func (s *PostgresStore) GetJobByID(ctx context.Context, jobID string) (models.ProcessingJob, error) {
	var data []byte
	if err := s.pool.QueryRow(ctx, `SELECT data FROM framemio_domain_records WHERE kind='job' AND id=$1`, jobID).Scan(&data); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return models.ProcessingJob{}, ErrNotFound
		}
		return models.ProcessingJob{}, fmt.Errorf("get job: %w", err)
	}
	var job models.ProcessingJob
	if err := json.Unmarshal(data, &job); err != nil {
		return models.ProcessingJob{}, fmt.Errorf("decode job: %w", err)
	}
	return job, nil
}

func (s *PostgresStore) UpdateJob(ctx context.Context, jobID string, status models.JobStatus, progress float64, errorCode, errorMessage, artifactS3Key, artifactType string) (models.ProcessingJob, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return models.ProcessingJob{}, fmt.Errorf("begin job update: %w", err)
	}
	defer tx.Rollback(ctx)
	var data []byte
	if err := tx.QueryRow(ctx, `SELECT data FROM framemio_domain_records WHERE kind='job' AND id=$1 FOR UPDATE`, jobID).Scan(&data); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return models.ProcessingJob{}, ErrNotFound
		}
		return models.ProcessingJob{}, err
	}
	var job models.ProcessingJob
	if err := json.Unmarshal(data, &job); err != nil {
		return models.ProcessingJob{}, err
	}
	if models.IsTerminalJobStatus(job.Status) {
		if job.Status != status {
			return models.ProcessingJob{}, ErrConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return models.ProcessingJob{}, err
		}
		return job, nil
	}
	now := time.Now().UTC()
	if status == models.JobStatusRunning && job.Status != models.JobStatusRunning {
		job.Attempt++
		job.StartedAt = &now
	}
	job.Status = status
	job.Progress = max(job.Progress, max(0, min(1, progress)))
	job.ErrorCode = errorCode
	job.ErrorMessage = errorMessage
	if artifactS3Key != "" {
		job.ArtifactS3Key = artifactS3Key
	}
	if artifactType != "" {
		job.ArtifactType = artifactType
	}
	job.UpdatedAt = now
	if status == models.JobStatusCompleted {
		job.Progress = 1
		job.CompletedAt = &now
		job.VisibilityUntil = nil
		job.NextRunAt = nil
	} else if status == models.JobStatusFailed || status == models.JobStatusCancelled {
		job.CompletedAt = &now
		job.VisibilityUntil = nil
	}
	encoded, err := json.Marshal(job)
	if err != nil {
		return models.ProcessingJob{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE framemio_domain_records SET data=$1,updated_at=$2 WHERE kind='job' AND id=$3`, jsonbParam(encoded), now, jobID); err != nil {
		return models.ProcessingJob{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return models.ProcessingJob{}, fmt.Errorf("commit job update: %w", err)
	}
	return job, nil
}

func (s *PostgresStore) RetryJob(ctx context.Context, jobID string, nextRunAt time.Time, errorCode, errorMessage string) (models.ProcessingJob, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return models.ProcessingJob{}, fmt.Errorf("begin job retry: %w", err)
	}
	defer tx.Rollback(ctx)
	var data []byte
	if err := tx.QueryRow(ctx, `SELECT data FROM framemio_domain_records WHERE kind='job' AND id=$1 FOR UPDATE`, jobID).Scan(&data); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return models.ProcessingJob{}, ErrNotFound
		}
		return models.ProcessingJob{}, err
	}
	var job models.ProcessingJob
	if err := json.Unmarshal(data, &job); err != nil {
		return models.ProcessingJob{}, err
	}
	if job.Status == models.JobStatusCompleted || job.Status == models.JobStatusCancelled || (job.MaxAttempts > 0 && job.Attempt >= job.MaxAttempts) {
		return models.ProcessingJob{}, ErrConflict
	}
	now := time.Now().UTC()
	job.Status = models.JobStatusQueued
	job.ErrorCode = errorCode
	job.ErrorMessage = errorMessage
	job.NextRunAt = &nextRunAt
	job.VisibilityUntil = nil
	job.UpdatedAt = now
	encoded, err := json.Marshal(job)
	if err != nil {
		return models.ProcessingJob{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE framemio_domain_records SET data=$1,updated_at=$2 WHERE kind='job' AND id=$3`, jsonbParam(encoded), now, jobID); err != nil {
		return models.ProcessingJob{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return models.ProcessingJob{}, fmt.Errorf("commit job retry: %w", err)
	}
	return job, nil
}

func (s *PostgresStore) ListProjectJobs(ctx context.Context, userID, projectID string) ([]models.ProcessingJob, error) {
	rows, err := s.pool.Query(ctx, `SELECT data FROM framemio_domain_records WHERE kind='job' AND user_id=$1 AND project_id=$2 ORDER BY created_at`, userID, projectID)
	if err != nil {
		return nil, fmt.Errorf("list jobs: %w", err)
	}
	defer rows.Close()
	jobs := make([]models.ProcessingJob, 0)
	for rows.Next() {
		var data []byte
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		var job models.ProcessingJob
		if err := json.Unmarshal(data, &job); err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

const domainRecordsMigration = `
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
CREATE INDEX IF NOT EXISTS framemio_domain_records_project
    ON framemio_domain_records (kind, user_id, project_id, created_at);`

func (s *PostgresStore) SaveAnalysis(ctx context.Context, userID string, analysis models.MediaAnalysis) error {
	if _, err := s.GetProject(ctx, userID, analysis.ProjectID); err != nil {
		return err
	}
	if _, err := s.GetMedia(ctx, userID, analysis.MediaID); err != nil {
		return err
	}
	_, err := s.put(ctx, "analysis", analysis.ID, userID, analysis.ProjectID, analysis.MediaID, postgresLogicalKey(analysis.ProjectID, analysis.MediaID), analysis)
	return err
}

func (s *PostgresStore) SaveVideoAnalysis(ctx context.Context, userID string, analysis models.VideoAnalysis) error {
	if _, err := s.GetProject(ctx, userID, analysis.ProjectID); err != nil {
		return err
	}
	if media, err := s.GetMedia(ctx, userID, analysis.VideoID); err != nil {
		return err
	} else if media.Type != models.MediaTypeVideo {
		return ErrConflict
	}
	_, err := s.put(ctx, "video_analysis", analysis.ID, userID, analysis.ProjectID, analysis.VideoID, videoAnalysisLogicalKey(analysis.VideoID, analysis.AnalysisVersion), analysis)
	return err
}

func (s *PostgresStore) GetVideoAnalysis(ctx context.Context, userID, videoID, analysisVersion string) (models.VideoAnalysis, error) {
	if _, err := s.GetMedia(ctx, userID, videoID); err != nil {
		return models.VideoAnalysis{}, err
	}
	var data []byte
	if err := s.pool.QueryRow(ctx, `SELECT data FROM framemio_domain_records WHERE kind='video_analysis' AND user_id=$1 AND logical_key=$2`, userID, videoAnalysisLogicalKey(videoID, analysisVersion)).Scan(&data); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return models.VideoAnalysis{}, ErrNotFound
		}
		return models.VideoAnalysis{}, err
	}
	var analysis models.VideoAnalysis
	if err := json.Unmarshal(data, &analysis); err != nil {
		return models.VideoAnalysis{}, fmt.Errorf("decode video analysis: %w", err)
	}
	return analysis, nil
}

func (s *PostgresStore) ListProjectAnalyses(ctx context.Context, userID, projectID string) ([]models.MediaAnalysis, []models.VideoAnalysis, error) {
	project, err := s.GetProject(ctx, userID, projectID)
	if err != nil {
		return nil, nil, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT kind, data
		FROM framemio_domain_records
		WHERE user_id=$1 AND (
			(kind='analysis' AND project_id=$2) OR
			(kind='video_analysis' AND media_id = ANY($3))
		)
		ORDER BY kind, media_id, created_at DESC`, userID, projectID, project.MediaIDs)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	photoAnalyses := make([]models.MediaAnalysis, 0)
	latestVideoByMedia := make(map[string]models.VideoAnalysis)
	for rows.Next() {
		var kind string
		var data []byte
		if err := rows.Scan(&kind, &data); err != nil {
			return nil, nil, err
		}
		switch kind {
		case "analysis":
			var analysis models.MediaAnalysis
			if err := json.Unmarshal(data, &analysis); err != nil {
				return nil, nil, fmt.Errorf("decode media analysis: %w", err)
			}
			photoAnalyses = append(photoAnalyses, analysis)
		case "video_analysis":
			var analysis models.VideoAnalysis
			if err := json.Unmarshal(data, &analysis); err != nil {
				return nil, nil, fmt.Errorf("decode video analysis: %w", err)
			}
			if _, exists := latestVideoByMedia[analysis.VideoID]; !exists {
				latestVideoByMedia[analysis.VideoID] = analysis
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	videoAnalyses := make([]models.VideoAnalysis, 0, len(latestVideoByMedia))
	for _, analysis := range latestVideoByMedia {
		videoAnalyses = append(videoAnalyses, analysis)
	}
	return photoAnalyses, videoAnalyses, nil
}

func (s *PostgresStore) SaveStoryPlan(ctx context.Context, userID string, plan models.StoryPlan) error {
	if _, err := s.GetProject(ctx, userID, plan.ProjectID); err != nil {
		return err
	}
	logicalKey := fmt.Sprintf("%s:%d", plan.ProjectID, plan.Version)
	_, err := s.put(ctx, "story_plan", plan.ID, userID, plan.ProjectID, "", logicalKey, plan)
	return err
}

func (s *PostgresStore) GetStoryPlan(ctx context.Context, userID, projectID string) (models.StoryPlan, error) {
	project, err := s.GetProject(ctx, userID, projectID)
	if err != nil {
		return models.StoryPlan{}, err
	}
	_ = project
	var data []byte
	if err := s.pool.QueryRow(ctx, `SELECT data FROM framemio_domain_records WHERE kind='story_plan' AND user_id=$1 AND project_id=$2 ORDER BY created_at DESC LIMIT 1`, userID, projectID).Scan(&data); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return models.StoryPlan{}, ErrNotFound
		}
		return models.StoryPlan{}, err
	}
	var plan models.StoryPlan
	if err := json.Unmarshal(data, &plan); err != nil {
		return models.StoryPlan{}, fmt.Errorf("decode story plan: %w", err)
	}
	return plan, nil
}
