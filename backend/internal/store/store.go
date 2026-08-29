package store

import (
	"context"
	"errors"
	"time"

	"github.com/framemio/backend/internal/models"
)

var (
	ErrNotFound            = errors.New("record not found")
	ErrConflict            = errors.New("record conflict")
	ErrInsufficientCredits = errors.New("insufficient credits")
	ErrAlreadyReady        = errors.New("media is already ready")
	ErrInvalidOwner        = errors.New("record does not belong to user")
)

type Store interface {
	EnsureUser(ctx context.Context, user models.User) (models.User, error)
	GetMedia(ctx context.Context, userID, mediaID string) (models.MediaAsset, error)
	CreateMediaUpload(ctx context.Context, media models.MediaAsset, session models.UploadSession) error
	UpdateUploadSession(ctx context.Context, session models.UploadSession) error
	GetUploadSessionByIdempotency(ctx context.Context, userID, key string) (models.UploadSession, error)
	GetReadyMediaByUploadIdempotencyPrefix(ctx context.Context, userID, prefix string) (models.MediaAsset, error)
	GetUploadSession(ctx context.Context, userID, uploadID string) (models.UploadSession, error)
	MarkMediaReady(ctx context.Context, userID, mediaID, uploadID, s3Key, etag string) (models.MediaAsset, error)
	CreateProject(ctx context.Context, project models.StoryProject, idempotencyKey string) (models.StoryProject, bool, error)
	AddProjectMedia(ctx context.Context, userID, projectID string, mediaIDs []string) (models.StoryProject, error)
	GetProject(ctx context.Context, userID, projectID string) (models.StoryProject, error)
	ListProjects(ctx context.Context, userID string) ([]models.StoryProject, error)
	DeleteProject(ctx context.Context, userID, projectID string) error
	SetProjectStatus(ctx context.Context, userID, projectID string, status models.ProjectStatus) error
	SetProjectCreditOperationKey(ctx context.Context, userID, projectID, creditOperationKey string) error
	CreateJob(ctx context.Context, job models.ProcessingJob, idempotencyKey string) (models.ProcessingJob, bool, error)
	GetJob(ctx context.Context, userID, jobID string) (models.ProcessingJob, error)
	GetJobByID(ctx context.Context, jobID string) (models.ProcessingJob, error)
	UpdateJob(ctx context.Context, jobID string, status models.JobStatus, progress float64, errorCode, errorMessage, artifactS3Key, artifactType string) (models.ProcessingJob, error)
	RetryJob(ctx context.Context, jobID string, nextRunAt time.Time, errorCode, errorMessage string) (models.ProcessingJob, error)
	ListProjectJobs(ctx context.Context, userID, projectID string) ([]models.ProcessingJob, error)
	SaveAnalysis(ctx context.Context, userID string, analysis models.MediaAnalysis) error
	SaveVideoAnalysis(ctx context.Context, userID string, analysis models.VideoAnalysis) error
	GetVideoAnalysis(ctx context.Context, userID, videoID, analysisVersion string) (models.VideoAnalysis, error)
	ListProjectAnalyses(ctx context.Context, userID, projectID string) ([]models.MediaAnalysis, []models.VideoAnalysis, error)
	SaveStoryPlan(ctx context.Context, userID string, plan models.StoryPlan) error
	GetStoryPlan(ctx context.Context, userID, projectID string) (models.StoryPlan, error)
	EnsureCreditGrant(ctx context.Context, grant models.CreditGrant) (models.CreditGrant, bool, error)
	GetCreditBalance(ctx context.Context, userID string) (models.CreditBalance, error)
	ReserveCredits(ctx context.Context, reservation models.CreditReservation) (models.CreditReservation, bool, error)
	GetCreditReservation(ctx context.Context, userID, reservationID string) (models.CreditReservation, error)
	GetCreditReservationByReference(ctx context.Context, userID string, action models.CreditAction, referenceType, referenceID string) (models.CreditReservation, error)
	CommitCreditReservation(ctx context.Context, userID, reservationID string) (models.CreditReservation, error)
	ReleaseCreditReservation(ctx context.Context, userID, reservationID string) (models.CreditReservation, error)
	ListCreditLedger(ctx context.Context, userID string) ([]models.CreditLedgerEntry, error)
}
