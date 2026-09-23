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
	CreateProject(ctx context.Context, project models.VideoProject, idempotencyKey string) (models.VideoProject, bool, error)
	UpdateProject(ctx context.Context, project models.VideoProject) (models.VideoProject, error)
	AddProjectMedia(ctx context.Context, userID, projectID string, mediaIDs []string) (models.VideoProject, error)
	GetProject(ctx context.Context, userID, projectID string) (models.VideoProject, error)
	ListProjects(ctx context.Context, userID string) ([]models.VideoProject, error)
	DeleteProject(ctx context.Context, userID, projectID string) error
	SetProjectStatus(ctx context.Context, userID, projectID string, status models.ProjectStatus) error
	SetProjectCreditOperationKey(ctx context.Context, userID, projectID, creditOperationKey string) error
	CreateJob(ctx context.Context, job models.ProcessingJob, idempotencyKey string) (models.ProcessingJob, bool, error)
	GetJobByIdempotency(ctx context.Context, userID, idempotencyKey string) (models.ProcessingJob, error)
	GetJob(ctx context.Context, userID, jobID string) (models.ProcessingJob, error)
	GetJobByID(ctx context.Context, jobID string) (models.ProcessingJob, error)
	UpdateJob(ctx context.Context, jobID string, status models.JobStatus, progress float64, errorCode, errorMessage, artifactS3Key, artifactType string) (models.ProcessingJob, error)
	RetryJob(ctx context.Context, jobID string, nextRunAt time.Time, errorCode, errorMessage string) (models.ProcessingJob, error)
	ListProjectJobs(ctx context.Context, userID, projectID string) ([]models.ProcessingJob, error)
	CreateTextVideoGeneration(ctx context.Context, generation models.TextVideoGeneration, idempotencyKey string) (models.TextVideoGeneration, bool, error)
	GetTextVideoGenerationByIdempotency(ctx context.Context, userID, idempotencyKey string) (models.TextVideoGeneration, error)
	GetTextVideoGeneration(ctx context.Context, userID, generationID string) (models.TextVideoGeneration, error)
	ListTextVideoGenerations(ctx context.Context, userID string) ([]models.TextVideoGeneration, error)
	ListActiveTextVideoGenerations(ctx context.Context, limit int) ([]models.TextVideoGeneration, error)
	UpdateTextVideoGeneration(ctx context.Context, generationID string, providerTaskID string, status models.TextVideoStatus, progress float64, videoURL, errorMessage string) (models.TextVideoGeneration, error)
	SaveAnalysis(ctx context.Context, userID string, analysis models.MediaAnalysis) error
	SaveVideoAnalysis(ctx context.Context, userID string, analysis models.VideoAnalysis) error
	GetVideoAnalysis(ctx context.Context, userID, videoID, analysisVersion string) (models.VideoAnalysis, error)
	ListProjectAnalyses(ctx context.Context, userID, projectID string) ([]models.MediaAnalysis, []models.VideoAnalysis, error)
	SaveStoryPlan(ctx context.Context, userID string, plan models.StoryPlan) error
	GetStoryPlan(ctx context.Context, userID, projectID string) (models.StoryPlan, error)
	GetStoryPlanVersion(ctx context.Context, userID, projectID string, version int) (models.StoryPlan, error)
	EnsureCreditGrant(ctx context.Context, grant models.CreditGrant) (models.CreditGrant, bool, error)
	GetCreditBalance(ctx context.Context, userID string) (models.CreditBalance, error)
	ReserveCredits(ctx context.Context, reservation models.CreditReservation) (models.CreditReservation, bool, error)
	GetCreditReservation(ctx context.Context, userID, reservationID string) (models.CreditReservation, error)
	GetCreditReservationByReference(ctx context.Context, userID string, action models.CreditAction, referenceType, referenceID string) (models.CreditReservation, error)
	CommitCreditReservation(ctx context.Context, userID, reservationID string) (models.CreditReservation, error)
	ReleaseCreditReservation(ctx context.Context, userID, reservationID string) (models.CreditReservation, error)
	ListCreditLedger(ctx context.Context, userID string) ([]models.CreditLedgerEntry, error)
}
