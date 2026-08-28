package store

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/framemio/backend/internal/models"
)

type MemoryStore struct {
	mu            sync.RWMutex
	users         map[string]models.User
	media         map[string]models.MediaAsset
	uploads       map[string]models.UploadSession
	uploadByIdem  map[string]string
	projects      map[string]models.StoryProject
	projectByIdem map[string]string
	jobs          map[string]models.ProcessingJob
	jobByIdem     map[string]string
	analyses      map[string]models.MediaAnalysis
	videoAnalyses map[string]models.VideoAnalysis
	storyPlans    map[string]models.StoryPlan
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		users:         make(map[string]models.User),
		media:         make(map[string]models.MediaAsset),
		uploads:       make(map[string]models.UploadSession),
		uploadByIdem:  make(map[string]string),
		projects:      make(map[string]models.StoryProject),
		projectByIdem: make(map[string]string),
		jobs:          make(map[string]models.ProcessingJob),
		jobByIdem:     make(map[string]string),
		analyses:      make(map[string]models.MediaAnalysis),
		videoAnalyses: make(map[string]models.VideoAnalysis),
		storyPlans:    make(map[string]models.StoryPlan),
	}
}

func (s *MemoryStore) EnsureUser(_ context.Context, user models.User) (models.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.users[user.ID]; ok {
		return existing, nil
	}
	s.users[user.ID] = user
	return user, nil
}

func (s *MemoryStore) GetMedia(_ context.Context, userID, mediaID string) (models.MediaAsset, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	media, ok := s.media[mediaID]
	if !ok {
		return models.MediaAsset{}, ErrNotFound
	}
	if media.UserID != userID {
		return models.MediaAsset{}, ErrInvalidOwner
	}
	return media, nil
}

func (s *MemoryStore) CreateMediaUpload(_ context.Context, media models.MediaAsset, session models.UploadSession) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if session.IdempotencyKey != "" {
		if _, ok := s.uploadByIdem[session.UserID+"\x00"+session.IdempotencyKey]; ok {
			return ErrConflict
		}
	}
	if _, ok := s.media[media.ID]; ok {
		return ErrConflict
	}
	if _, ok := s.uploads[session.ID]; ok {
		return ErrConflict
	}
	s.media[media.ID] = media
	s.uploads[session.ID] = session
	if session.IdempotencyKey != "" {
		s.uploadByIdem[session.UserID+"\x00"+session.IdempotencyKey] = session.ID
	}
	return nil
}

func (s *MemoryStore) UpdateUploadSession(_ context.Context, session models.UploadSession) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.uploads[session.ID]
	if !ok {
		return ErrNotFound
	}
	if existing.UserID != session.UserID {
		return ErrInvalidOwner
	}
	s.uploads[session.ID] = session
	return nil
}

func (s *MemoryStore) GetUploadSessionByIdempotency(_ context.Context, userID, key string) (models.UploadSession, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	uploadID, ok := s.uploadByIdem[userID+"\x00"+key]
	if !ok {
		return models.UploadSession{}, ErrNotFound
	}
	session, ok := s.uploads[uploadID]
	if !ok {
		return models.UploadSession{}, ErrNotFound
	}
	return session, nil
}

func (s *MemoryStore) GetReadyMediaByUploadIdempotencyPrefix(_ context.Context, userID, prefix string) (models.MediaAsset, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	lookupPrefix := userID + "\x00" + prefix
	var best models.MediaAsset
	found := false
	for key, uploadID := range s.uploadByIdem {
		if !strings.HasPrefix(key, lookupPrefix) {
			continue
		}
		session, ok := s.uploads[uploadID]
		if !ok || session.UserID != userID {
			continue
		}
		media, ok := s.media[session.MediaID]
		if !ok || media.UserID != userID || media.Status != models.MediaStatusReady {
			continue
		}
		if !found || media.UpdatedAt.After(best.UpdatedAt) {
			best = media
			found = true
		}
	}
	if !found {
		return models.MediaAsset{}, ErrNotFound
	}
	return best, nil
}

func (s *MemoryStore) GetUploadSession(_ context.Context, userID, uploadID string) (models.UploadSession, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	session, ok := s.uploads[uploadID]
	if !ok {
		return models.UploadSession{}, ErrNotFound
	}
	if session.UserID != userID {
		return models.UploadSession{}, ErrInvalidOwner
	}
	return session, nil
}

func (s *MemoryStore) MarkMediaReady(_ context.Context, userID, mediaID, uploadID, s3Key, etag string) (models.MediaAsset, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	media, ok := s.media[mediaID]
	if !ok {
		return models.MediaAsset{}, ErrNotFound
	}
	if media.UserID != userID {
		return models.MediaAsset{}, ErrInvalidOwner
	}
	session, ok := s.uploads[uploadID]
	if !ok || session.MediaID != mediaID || session.S3Key != s3Key {
		return models.MediaAsset{}, ErrConflict
	}
	if media.Status == models.MediaStatusReady {
		return media, nil
	}
	now := time.Now().UTC()
	media.Status = models.MediaStatusReady
	media.ETag = etag
	media.UpdatedAt = now
	session.CompletedAt = &now
	s.media[mediaID] = media
	s.uploads[uploadID] = session
	return media, nil
}

func (s *MemoryStore) CreateProject(_ context.Context, project models.StoryProject, idempotencyKey string) (models.StoryProject, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := project.UserID + "\x00" + idempotencyKey
	if idempotencyKey != "" {
		if existingID, ok := s.projectByIdem[key]; ok {
			return s.projects[existingID], true, nil
		}
	}
	for _, mediaID := range project.MediaIDs {
		media, ok := s.media[mediaID]
		if !ok || media.UserID != project.UserID || media.Status != models.MediaStatusReady {
			return models.StoryProject{}, false, ErrConflict
		}
	}
	s.projects[project.ID] = project
	if idempotencyKey != "" {
		s.projectByIdem[key] = project.ID
	}
	return project, false, nil
}

func (s *MemoryStore) AddProjectMedia(_ context.Context, userID, projectID string, mediaIDs []string) (models.StoryProject, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	project, ok := s.projects[projectID]
	if !ok {
		return models.StoryProject{}, ErrNotFound
	}
	if project.UserID != userID {
		return models.StoryProject{}, ErrInvalidOwner
	}
	existing := make(map[string]struct{}, len(project.MediaIDs))
	for _, mediaID := range project.MediaIDs {
		existing[mediaID] = struct{}{}
	}
	changed := false
	for _, mediaID := range mediaIDs {
		mediaID = strings.TrimSpace(mediaID)
		if mediaID == "" {
			continue
		}
		media, ok := s.media[mediaID]
		if !ok {
			return models.StoryProject{}, ErrNotFound
		}
		if media.UserID != userID {
			return models.StoryProject{}, ErrInvalidOwner
		}
		if media.Status != models.MediaStatusReady {
			return models.StoryProject{}, ErrConflict
		}
		if _, ok := existing[mediaID]; ok {
			continue
		}
		project.MediaIDs = append(project.MediaIDs, mediaID)
		existing[mediaID] = struct{}{}
		changed = true
	}
	if changed {
		project.UpdatedAt = time.Now().UTC()
		s.projects[projectID] = project
	}
	return project, nil
}

func (s *MemoryStore) GetProject(_ context.Context, userID, projectID string) (models.StoryProject, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	project, ok := s.projects[projectID]
	if !ok {
		return models.StoryProject{}, ErrNotFound
	}
	if project.UserID != userID {
		return models.StoryProject{}, ErrInvalidOwner
	}
	return project, nil
}

func (s *MemoryStore) ListProjects(_ context.Context, userID string) ([]models.StoryProject, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	projects := make([]models.StoryProject, 0)
	for _, project := range s.projects {
		if project.UserID == userID {
			projects = append(projects, project)
		}
	}
	slices.SortFunc(projects, func(a, b models.StoryProject) int {
		return b.CreatedAt.Compare(a.CreatedAt)
	})
	return projects, nil
}

func (s *MemoryStore) DeleteProject(_ context.Context, userID, projectID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	project, ok := s.projects[projectID]
	if !ok {
		return ErrNotFound
	}
	if project.UserID != userID {
		return ErrInvalidOwner
	}
	delete(s.projects, projectID)
	for key, existingID := range s.projectByIdem {
		if existingID == projectID {
			delete(s.projectByIdem, key)
		}
	}
	for key, job := range s.jobs {
		if job.UserID == userID && job.ProjectID == projectID {
			delete(s.jobs, key)
		}
	}
	for key, existingID := range s.jobByIdem {
		if _, ok := s.jobs[existingID]; !ok {
			delete(s.jobByIdem, key)
		}
	}
	for key, analysis := range s.analyses {
		if analysis.ProjectID == projectID {
			delete(s.analyses, key)
		}
	}
	for key, analysis := range s.videoAnalyses {
		if analysis.ProjectID == projectID {
			delete(s.videoAnalyses, key)
		}
	}
	for key, plan := range s.storyPlans {
		if plan.ProjectID == projectID {
			delete(s.storyPlans, key)
		}
	}
	return nil
}

func (s *MemoryStore) SetProjectStatus(_ context.Context, userID, projectID string, status models.ProjectStatus) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	project, ok := s.projects[projectID]
	if !ok {
		return ErrNotFound
	}
	if project.UserID != userID {
		return ErrInvalidOwner
	}
	project.Status = status
	project.UpdatedAt = time.Now().UTC()
	s.projects[projectID] = project
	return nil
}

func (s *MemoryStore) CreateJob(_ context.Context, job models.ProcessingJob, idempotencyKey string) (models.ProcessingJob, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := job.UserID + "\x00" + idempotencyKey
	if idempotencyKey != "" {
		if existingID, ok := s.jobByIdem[key]; ok {
			return s.jobs[existingID], true, nil
		}
	}
	s.jobs[job.ID] = job
	if idempotencyKey != "" {
		s.jobByIdem[key] = job.ID
	}
	return job, false, nil
}

func (s *MemoryStore) GetJob(_ context.Context, userID, jobID string) (models.ProcessingJob, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	job, ok := s.jobs[jobID]
	if !ok {
		return models.ProcessingJob{}, ErrNotFound
	}
	if job.UserID != userID {
		return models.ProcessingJob{}, ErrInvalidOwner
	}
	return job, nil
}

func (s *MemoryStore) GetJobByID(_ context.Context, jobID string) (models.ProcessingJob, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	job, ok := s.jobs[jobID]
	if !ok {
		return models.ProcessingJob{}, ErrNotFound
	}
	return job, nil
}

func (s *MemoryStore) UpdateJob(_ context.Context, jobID string, status models.JobStatus, progress float64, errorCode, errorMessage, artifactS3Key, artifactType string) (models.ProcessingJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[jobID]
	if !ok {
		return models.ProcessingJob{}, ErrNotFound
	}
	if models.IsTerminalJobStatus(job.Status) {
		if job.Status == status {
			return job, nil
		}
		return models.ProcessingJob{}, ErrConflict
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
	s.jobs[jobID] = job
	return job, nil
}

func (s *MemoryStore) RetryJob(_ context.Context, jobID string, nextRunAt time.Time, errorCode, errorMessage string) (models.ProcessingJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[jobID]
	if !ok {
		return models.ProcessingJob{}, ErrNotFound
	}
	if job.Status == models.JobStatusCompleted || job.Status == models.JobStatusCancelled {
		return models.ProcessingJob{}, ErrConflict
	}
	if job.MaxAttempts > 0 && job.Attempt >= job.MaxAttempts {
		return models.ProcessingJob{}, ErrConflict
	}
	now := time.Now().UTC()
	job.Status = models.JobStatusQueued
	job.ErrorCode = errorCode
	job.ErrorMessage = errorMessage
	job.NextRunAt = &nextRunAt
	job.VisibilityUntil = nil
	job.UpdatedAt = now
	s.jobs[jobID] = job
	return job, nil
}

func (s *MemoryStore) ListProjectJobs(_ context.Context, userID, projectID string) ([]models.ProcessingJob, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	jobs := make([]models.ProcessingJob, 0)
	for _, job := range s.jobs {
		if job.UserID == userID && job.ProjectID == projectID {
			jobs = append(jobs, job)
		}
	}
	return jobs, nil
}

func (s *MemoryStore) SaveAnalysis(_ context.Context, userID string, analysis models.MediaAnalysis) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	project, ok := s.projects[analysis.ProjectID]
	if !ok || project.UserID != userID {
		return ErrNotFound
	}
	media, ok := s.media[analysis.MediaID]
	if !ok || media.UserID != userID {
		return ErrNotFound
	}
	key := analysis.ProjectID + "\x00" + analysis.MediaID
	if _, exists := s.analyses[key]; exists {
		return nil
	}
	s.analyses[key] = analysis
	return nil
}

func (s *MemoryStore) SaveVideoAnalysis(_ context.Context, userID string, analysis models.VideoAnalysis) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	project, ok := s.projects[analysis.ProjectID]
	if !ok || project.UserID != userID {
		return ErrNotFound
	}
	media, ok := s.media[analysis.VideoID]
	if !ok || media.UserID != userID || media.Type != models.MediaTypeVideo {
		return ErrNotFound
	}
	key := analysis.VideoID + "\x00" + analysis.AnalysisVersion
	if _, exists := s.videoAnalyses[key]; exists {
		return nil
	}
	s.videoAnalyses[key] = analysis
	return nil
}

func (s *MemoryStore) GetVideoAnalysis(_ context.Context, userID, videoID, analysisVersion string) (models.VideoAnalysis, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	analysis, ok := s.videoAnalyses[videoID+"\x00"+analysisVersion]
	if !ok {
		return models.VideoAnalysis{}, ErrNotFound
	}
	media, ok := s.media[videoID]
	if !ok || media.UserID != userID || media.Type != models.MediaTypeVideo {
		return models.VideoAnalysis{}, ErrNotFound
	}
	project, ok := s.projects[analysis.ProjectID]
	if !ok || project.UserID != userID {
		return models.VideoAnalysis{}, ErrNotFound
	}
	return analysis, nil
}

func (s *MemoryStore) ListProjectAnalyses(_ context.Context, userID, projectID string) ([]models.MediaAnalysis, []models.VideoAnalysis, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	project, ok := s.projects[projectID]
	if !ok || project.UserID != userID {
		return nil, nil, ErrNotFound
	}
	photoAnalyses := make([]models.MediaAnalysis, 0)
	for _, analysis := range s.analyses {
		if analysis.ProjectID == projectID {
			photoAnalyses = append(photoAnalyses, analysis)
		}
	}
	projectMedia := make(map[string]struct{}, len(project.MediaIDs))
	for _, mediaID := range project.MediaIDs {
		projectMedia[mediaID] = struct{}{}
	}
	latestVideoByMedia := make(map[string]models.VideoAnalysis)
	for _, analysis := range s.videoAnalyses {
		if _, selected := projectMedia[analysis.VideoID]; !selected {
			continue
		}
		current, ok := latestVideoByMedia[analysis.VideoID]
		if !ok || analysis.CreatedAt.After(current.CreatedAt) {
			latestVideoByMedia[analysis.VideoID] = analysis
		}
	}
	videoAnalyses := make([]models.VideoAnalysis, 0, len(latestVideoByMedia))
	for _, analysis := range latestVideoByMedia {
		videoAnalyses = append(videoAnalyses, analysis)
	}
	return photoAnalyses, videoAnalyses, nil
}

func (s *MemoryStore) SaveStoryPlan(_ context.Context, userID string, plan models.StoryPlan) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	project, ok := s.projects[plan.ProjectID]
	if !ok || project.UserID != userID {
		return ErrNotFound
	}
	key := plan.ProjectID + "\x00" + strconv.Itoa(plan.Version)
	if _, exists := s.storyPlans[key]; exists {
		return nil
	}
	s.storyPlans[key] = plan
	return nil
}

func (s *MemoryStore) GetStoryPlan(_ context.Context, userID, projectID string) (models.StoryPlan, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	project, ok := s.projects[projectID]
	if !ok || project.UserID != userID {
		return models.StoryPlan{}, ErrNotFound
	}
	var plan models.StoryPlan
	found := false
	for _, candidate := range s.storyPlans {
		if candidate.ProjectID == projectID && (!found || candidate.Version > plan.Version) {
			plan = candidate
			found = true
		}
	}
	if !found {
		return models.StoryPlan{}, ErrNotFound
	}
	return plan, nil
}
