package api

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/framemio/backend/internal/credits"
	"github.com/framemio/backend/internal/models"
	"github.com/framemio/backend/internal/orchestration"
	"github.com/framemio/backend/internal/queue"
	"github.com/framemio/backend/internal/storage"
	"github.com/framemio/backend/internal/store"
	"github.com/framemio/backend/internal/storyvalidation"
	"github.com/framemio/backend/internal/workeractivation"
)

const maxJSONBodyBytes = 1 << 20

type Server struct {
	Store              store.Store
	Credits            *credits.Service
	ObjectStorage      storage.ObjectStorage
	StoryPlanValidator storyvalidation.Service
	Publisher          queue.Publisher
	Activator          workeractivation.Activator
	Orchestrator       *orchestration.Coordinator
	Authenticator      Authenticator
	Logger             *slog.Logger
	Now                func() time.Time
	InternalToken      string
	ExpensiveLimiter   ExpensiveOperationLimiter
	PromptTranscriber  PromptTranscriber
}

func NewServer(dependencies Server) *Server {
	if dependencies.Logger == nil {
		dependencies.Logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))
	}
	if dependencies.Now == nil {
		dependencies.Now = time.Now
	}
	if dependencies.StoryPlanValidator == nil && dependencies.Store != nil && dependencies.ObjectStorage != nil {
		dependencies.StoryPlanValidator = storyvalidation.New(dependencies.Store, dependencies.ObjectStorage, storyvalidation.DefaultConfig())
	}
	if dependencies.Credits == nil && dependencies.Store != nil {
		dependencies.Credits = credits.New(dependencies.Store, credits.LoadPolicyFromEnv(), dependencies.Now)
	}
	if dependencies.Orchestrator == nil && dependencies.Store != nil && dependencies.Publisher != nil {
		dependencies.Orchestrator = orchestration.New(orchestration.Coordinator{Store: dependencies.Store, Publisher: dependencies.Publisher, Activator: dependencies.Activator, Validator: dependencies.StoryPlanValidator, Logger: dependencies.Logger, Now: dependencies.Now, Credits: dependencies.Credits})
	}
	if dependencies.ExpensiveLimiter == nil {
		dependencies.ExpensiveLimiter = NewInMemoryExpensiveOperationLimiter(ExpensiveOperationLimits{})
	}
	if dependencies.PromptTranscriber == nil {
		dependencies.PromptTranscriber = NewDeepgramPromptTranscriberFromEnv()
	}
	return &dependencies
}

func (s *Server) Handler() http.Handler {
	return s.withLogging(http.HandlerFunc(s.dispatch))
}

func (s *Server) dispatch(response http.ResponseWriter, request *http.Request) {
	if request.URL.Path == "/healthz" {
		if request.Method != http.MethodGet {
			s.writeError(response, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
			return
		}
		s.writeJSON(response, http.StatusOK, map[string]string{"status": "ok"})
		return
	}
	if request.URL.Path == "/v1/auth/anonymous" {
		s.createAnonymousToken(response, request)
		return
	}
	if request.URL.Path == "/v1/auth/refresh" {
		s.refreshAnonymousToken(response, request)
		return
	}
	if strings.HasPrefix(request.URL.Path, "/internal/jobs/") {
		parts := strings.Split(strings.Trim(request.URL.Path, "/"), "/")
		if len(parts) == 4 {
			if request.Method == http.MethodGet && parts[3] == "status" {
				s.getInternalJobStatus(response, request, parts[2])
				return
			}
			if request.Method == http.MethodGet && parts[3] == "render-input" {
				s.getRenderInput(response, request, parts[2])
				return
			}
			if request.Method == http.MethodPost {
				switch parts[3] {
				case "status":
					s.updateJobStatus(response, request, parts[2])
					return
				case "analysis":
					s.saveJobAnalysis(response, request, parts[2])
					return
				case "story-plan":
					s.saveJobStoryPlan(response, request, parts[2])
					return
				}
			}
		}
		s.writeError(response, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}

	user, ok := s.authenticate(response, request)
	if !ok {
		return
	}
	request = request.WithContext(contextWithAuthenticatedUser(request.Context(), user))

	if request.URL.Path == "/media/upload-session" {
		if request.Method == http.MethodPost {
			s.createUploadSession(response, request)
			return
		}
		s.writeError(response, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	if request.URL.Path == "/v1/media/upload-sessions" {
		if request.Method == http.MethodPost {
			s.createIOSUploadSession(response, request)
			return
		}
		s.writeError(response, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	if strings.HasPrefix(request.URL.Path, "/v1/media/upload-sessions/") && strings.HasSuffix(request.URL.Path, "/complete") {
		parts := strings.Split(strings.Trim(request.URL.Path, "/"), "/")
		if request.Method == http.MethodPost && len(parts) == 5 {
			s.completeIOSMultipartUpload(response, request, parts[3])
			return
		}
		s.writeError(response, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	if request.URL.Path == "/v1/media/uploads/complete" {
		if request.Method == http.MethodPost {
			s.notifyIOSUploadComplete(response, request)
			return
		}
		s.writeError(response, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	if request.URL.Path == "/v1/transcriptions/token" {
		s.grantPromptTranscriptionToken(response, request)
		return
	}
	if request.URL.Path == "/v1/transcriptions" {
		s.transcribePromptAudio(response, request)
		return
	}
	if request.URL.Path == "/v1/credit-packs" {
		if request.Method == http.MethodGet {
			s.listCreditPacks(response, request)
			return
		}
		s.writeError(response, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	if request.URL.Path == "/v1/me/credits" {
		if request.Method == http.MethodGet {
			s.getMeCredits(response, request)
			return
		}
		s.writeError(response, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	if request.URL.Path == "/v1/me/credits/history" {
		if request.Method == http.MethodGet {
			s.getMeCreditHistory(response, request)
			return
		}
		s.writeError(response, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	if request.URL.Path == "/v1/me/credits/purchased" {
		if request.Method == http.MethodPost {
			s.grantPurchasedCredits(response, request)
			return
		}
		s.writeError(response, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	if request.URL.Path == "/v1/me/credits/subscription-cycle" {
		if request.Method == http.MethodPost {
			s.grantSubscriptionCycleCredits(response, request)
			return
		}
		s.writeError(response, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	if request.URL.Path == "/v1/credit-quote" {
		if request.Method == http.MethodPost {
			s.quoteCreditAction(response, request)
			return
		}
		s.writeError(response, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	if request.URL.Path == "/v1/stories" {
		switch request.Method {
		case http.MethodGet:
			s.listIOSStories(response, request)
			return
		case http.MethodPost:
			s.createIOSStory(response, request)
			return
		default:
			s.writeError(response, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
			return
		}
	}
	if strings.HasPrefix(request.URL.Path, "/v1/stories/") {
		parts := strings.Split(strings.Trim(request.URL.Path, "/"), "/")
		if len(parts) == 3 {
			switch request.Method {
			case http.MethodGet:
				s.getIOSStory(response, request, parts[2])
				return
			case http.MethodDelete:
				s.deleteIOSStory(response, request, parts[2])
				return
			}
		}
		if len(parts) == 4 && (parts[3] == "versions" || parts[3] == "revisions") && request.Method == http.MethodPost {
			s.createIOSStoryVersion(response, request, parts[2])
			return
		}
		if len(parts) == 4 && parts[3] == "regenerate" && request.Method == http.MethodPost {
			s.regenerateIOSStory(response, request, parts[2])
			return
		}
		s.writeError(response, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	if request.URL.Path == "/media" {
		if request.Method == http.MethodPost {
			s.completeMedia(response, request)
			return
		}
		s.writeError(response, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	if strings.HasPrefix(request.URL.Path, "/media/") {
		if request.Method == http.MethodGet {
			s.getMedia(response, request, strings.TrimPrefix(request.URL.Path, "/media/"))
			return
		}
		s.writeError(response, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	if request.URL.Path == "/projects" {
		if request.Method == http.MethodPost {
			s.createProject(response, request)
			return
		}
		s.writeError(response, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	if strings.HasPrefix(request.URL.Path, "/projects/") {
		parts := strings.Split(strings.Trim(request.URL.Path, "/"), "/")
		if len(parts) == 3 && request.Method == http.MethodPost {
			switch parts[2] {
			case "analyze":
				s.analyzeProject(response, request, parts[1])
				return
			case "render":
				s.renderProject(response, request, parts[1])
				return
			}
		}
		if len(parts) == 3 && parts[2] == "story" && request.Method == http.MethodGet {
			s.getProjectStory(response, request, parts[1])
			return
		}
		if len(parts) == 3 && parts[2] == "status" && request.Method == http.MethodGet {
			s.getProjectStatus(response, request, parts[1])
			return
		}
	}
	s.writeError(response, http.StatusNotFound, "not_found", "route not found")
}

type uploadSessionRequest struct {
	FileName        string           `json:"file_name"`
	ContentType     string           `json:"content_type"`
	FileSizeBytes   int64            `json:"file_size_bytes"`
	MediaType       models.MediaType `json:"media_type"`
	DurationSeconds *float64         `json:"duration,omitempty"`
	CreationDate    *time.Time       `json:"creation_date,omitempty"`
	PixelWidth      int              `json:"pixel_width"`
	PixelHeight     int              `json:"pixel_height"`
	Orientation     string           `json:"orientation,omitempty"`
	Optimization    string           `json:"optimization,omitempty"`
}

type uploadSessionResponse struct {
	Media   models.MediaAsset    `json:"media"`
	Session models.UploadSession `json:"upload_session"`
}

func (s *Server) createUploadSession(response http.ResponseWriter, request *http.Request) {
	user, ok := s.authenticate(response, request)
	if !ok {
		return
	}
	idempotencyKey, ok := requiredIdempotencyKey(response, request)
	if !ok {
		return
	}
	var input uploadSessionRequest
	if !s.decodeJSON(response, request, &input) {
		return
	}
	input.FileName = strings.TrimSpace(input.FileName)
	input.ContentType = strings.TrimSpace(input.ContentType)
	if input.FileName == "" || input.FileSizeBytes <= 0 || input.ContentType == "" || !validMediaType(input.MediaType) {
		s.writeError(response, http.StatusBadRequest, "invalid_media", "file name, positive file size, content type, and valid media type are required")
		return
	}
	if input.PixelWidth < 0 || input.PixelHeight < 0 || (input.DurationSeconds != nil && *input.DurationSeconds < 0) {
		s.writeError(response, http.StatusBadRequest, "invalid_media_metadata", "media dimensions and duration cannot be negative")
		return
	}
	if existing, err := s.Store.GetUploadSessionByIdempotency(request.Context(), user.ID, idempotencyKey); err == nil {
		media, mediaErr := s.Store.GetMedia(request.Context(), user.ID, existing.MediaID)
		if mediaErr == nil {
			s.writeJSON(response, http.StatusOK, uploadSessionResponse{Media: media, Session: existing})
			return
		}
	}
	mediaID := models.NewID("media")
	authorized, err := s.ObjectStorage.AuthorizeUpload(request.Context(), storage.UploadAuthorizationRequest{
		UserID: user.ID, MediaID: mediaID, FileName: input.FileName, ContentType: input.ContentType, FileSizeBytes: input.FileSizeBytes, Optimization: input.Optimization,
	})
	if err != nil {
		s.internalError(response, err, "authorize upload")
		return
	}
	authorized.IdempotencyKey = idempotencyKey
	media := models.MediaAsset{
		ID: mediaID, UserID: user.ID, Type: input.MediaType, Status: models.MediaStatusUploading,
		FileName: input.FileName, ContentType: input.ContentType, FileSizeBytes: input.FileSizeBytes,
		DurationSeconds: input.DurationSeconds, CreationDate: input.CreationDate, PixelWidth: input.PixelWidth,
		PixelHeight: input.PixelHeight, Orientation: input.Orientation, S3Key: authorized.S3Key, UploadID: authorized.ID,
		CreatedAt: s.Now().UTC(), UpdatedAt: s.Now().UTC(),
	}
	if err := s.Store.CreateMediaUpload(request.Context(), media, authorized); err != nil {
		if abortErr := s.ObjectStorage.AbortUpload(request.Context(), authorized); abortErr != nil {
			s.Logger.Warn("abort unused upload session", slog.Any("error", abortErr), slog.String("upload_id", authorized.ID))
		}
		if existing, lookupErr := s.Store.GetUploadSessionByIdempotency(request.Context(), user.ID, idempotencyKey); lookupErr == nil {
			storedMedia, mediaErr := s.Store.GetMedia(request.Context(), user.ID, existing.MediaID)
			if mediaErr == nil {
				s.writeJSON(response, http.StatusOK, uploadSessionResponse{Media: storedMedia, Session: existing})
				return
			}
		}
		s.internalError(response, err, "persist upload session")
		return
	}
	s.writeJSON(response, http.StatusCreated, uploadSessionResponse{Media: media, Session: authorized})
}

type completeMediaRequest struct {
	MediaID   string                       `json:"media_id"`
	UploadID  string                       `json:"upload_id"`
	S3Key     string                       `json:"s3_key"`
	MediaType models.MediaType             `json:"media_type"`
	Duration  *float64                     `json:"duration,omitempty"`
	Parts     []models.CompletedUploadPart `json:"parts,omitempty"`
}

func (s *Server) completeMedia(response http.ResponseWriter, request *http.Request) {
	user, ok := s.authenticate(response, request)
	if !ok {
		return
	}
	var input completeMediaRequest
	if !s.decodeJSON(response, request, &input) {
		return
	}
	if input.MediaID == "" || input.UploadID == "" || input.S3Key == "" {
		s.writeError(response, http.StatusBadRequest, "invalid_completion", "media_id, upload_id, and s3_key are required")
		return
	}
	session, err := s.Store.GetUploadSession(request.Context(), user.ID, input.UploadID)
	if err != nil || session.MediaID != input.MediaID || session.S3Key != input.S3Key {
		s.writeError(response, http.StatusConflict, "invalid_upload_reference", "upload session does not match media")
		return
	}
	media, err := s.Store.GetMedia(request.Context(), user.ID, input.MediaID)
	if err != nil {
		s.mapStoreError(response, err)
		return
	}
	if input.MediaType != "" && input.MediaType != media.Type {
		s.writeError(response, http.StatusConflict, "media_type_mismatch", "media type does not match the upload session")
		return
	}
	if media.Status == models.MediaStatusReady {
		s.writeJSON(response, http.StatusOK, media)
		return
	}
	etag, err := s.ObjectStorage.CompleteUpload(request.Context(), session, input.Parts)
	if err != nil {
		if errors.Is(err, storage.ErrUploadSizeMismatch) {
			s.writeError(response, http.StatusConflict, "stale_upload_size", "uploaded object size does not match the authorized media size; retry upload authorization")
			return
		}
		s.internalError(response, err, "complete object upload")
		return
	}
	media, err = s.Store.MarkMediaReady(request.Context(), user.ID, input.MediaID, input.UploadID, input.S3Key, etag)
	if err != nil {
		s.mapStoreError(response, err)
		return
	}
	s.writeJSON(response, http.StatusOK, media)
}

func (s *Server) getMedia(response http.ResponseWriter, request *http.Request, mediaID string) {
	user, ok := s.authenticate(response, request)
	if !ok {
		return
	}
	if mediaID == "" {
		s.writeError(response, http.StatusBadRequest, "invalid_media_id", "media id is required")
		return
	}
	media, err := s.Store.GetMedia(request.Context(), user.ID, mediaID)
	if err != nil {
		s.mapStoreError(response, err)
		return
	}
	s.writeJSON(response, http.StatusOK, media)
}

type createProjectRequest struct {
	Title    string   `json:"title"`
	Context  string   `json:"context"`
	MediaIDs []string `json:"media_ids"`
}

func (s *Server) createProject(response http.ResponseWriter, request *http.Request) {
	user, ok := s.authenticate(response, request)
	if !ok {
		return
	}
	idempotencyKey, ok := requiredIdempotencyKey(response, request)
	if !ok {
		return
	}
	var input createProjectRequest
	if !s.decodeJSON(response, request, &input) {
		return
	}
	input.MediaIDs = uniqueNonEmpty(input.MediaIDs)
	if len(input.MediaIDs) == 0 {
		s.writeError(response, http.StatusBadRequest, "invalid_project", "at least one media id is required")
		return
	}
	now := s.Now().UTC()
	project := models.StoryProject{ID: models.NewID("project"), UserID: user.ID, Title: input.Title, Context: input.Context, MediaIDs: append([]string(nil), input.MediaIDs...), Status: models.ProjectStatusDraft, CreatedAt: now, UpdatedAt: now}
	created, existing, err := s.Store.CreateProject(request.Context(), project, idempotencyKey)
	if err != nil {
		s.mapStoreError(response, err)
		return
	}
	status := http.StatusCreated
	if existing {
		status = http.StatusOK
	}
	s.writeJSON(response, status, created)
}

type jobsResponse struct {
	JobID  string                 `json:"job_id"`
	JobIDs []string               `json:"job_ids"`
	Jobs   []models.ProcessingJob `json:"jobs"`
}

func (s *Server) analyzeProject(response http.ResponseWriter, request *http.Request, projectID string) {
	user, ok := s.authenticate(response, request)
	if !ok {
		return
	}
	idempotencyKey, ok := requiredIdempotencyKey(response, request)
	if !ok {
		return
	}
	if strings.TrimSpace(projectID) == "" {
		s.writeError(response, http.StatusBadRequest, "invalid_project_id", "project id is required")
		return
	}
	if !s.allowExpensiveOperation(response, user.ID, "analyze_project") {
		return
	}
	if s.Orchestrator == nil {
		s.internalError(response, errors.New("job orchestrator not configured"), "job orchestrator not configured")
		return
	}
	jobs, err := s.Orchestrator.StartAnalysis(request.Context(), user.ID, projectID, idempotencyKey)
	if err != nil {
		s.mapStoreError(response, err)
		return
	}
	responseBody := jobsResponse{Jobs: jobs}
	if len(jobs) > 0 {
		responseBody.JobID = jobs[0].ID
		responseBody.JobIDs = make([]string, 0, len(jobs))
		for _, job := range jobs {
			responseBody.JobIDs = append(responseBody.JobIDs, job.ID)
		}
	}
	s.writeJSON(response, http.StatusAccepted, responseBody)
}

func (s *Server) getProjectStatus(response http.ResponseWriter, request *http.Request, projectID string) {
	user, ok := s.authenticate(response, request)
	if !ok {
		return
	}
	project, err := s.Store.GetProject(request.Context(), user.ID, projectID)
	if err != nil {
		s.mapStoreError(response, err)
		return
	}
	jobs, err := s.Store.ListProjectJobs(request.Context(), user.ID, projectID)
	if err != nil {
		s.internalError(response, err, "list project jobs")
		return
	}
	s.writeJSON(response, http.StatusOK, map[string]any{"project": project, "jobs": jobs})
}

func (s *Server) getProjectStory(response http.ResponseWriter, request *http.Request, projectID string) {
	user, ok := s.authenticate(response, request)
	if !ok {
		return
	}
	plan, err := s.Store.GetStoryPlan(request.Context(), user.ID, projectID)
	if err != nil {
		s.mapStoreError(response, err)
		return
	}
	s.writeJSON(response, http.StatusOK, plan)
}

type renderProjectRequest struct {
	OutputPreset string `json:"output_preset,omitempty"`
}

func (s *Server) getMeCredits(response http.ResponseWriter, request *http.Request) {
	user, ok := s.authenticate(response, request)
	if !ok {
		return
	}
	if s.Credits == nil {
		s.internalError(response, errors.New("credit service not configured"), "credit service not configured")
		return
	}
	balance, err := s.Credits.GetBalance(request.Context(), user.ID)
	if err != nil {
		s.internalError(response, err, "get credit balance")
		return
	}
	s.writeJSON(response, http.StatusOK, balance)
}

func (s *Server) getMeCreditHistory(response http.ResponseWriter, request *http.Request) {
	user, ok := s.authenticate(response, request)
	if !ok {
		return
	}
	if s.Credits == nil {
		s.internalError(response, errors.New("credit service not configured"), "credit service not configured")
		return
	}
	history, err := s.Credits.GetHistory(request.Context(), user.ID)
	if err != nil {
		s.internalError(response, err, "get credit history")
		return
	}
	s.writeJSON(response, http.StatusOK, history)
}

type creditQuoteRequest struct {
	Action    string                  `json:"action"`
	ProjectID string                  `json:"projectId,omitempty"`
	StoryID   string                  `json:"storyId,omitempty"`
	Stats     credits.StoryMediaStats `json:"stats,omitempty"`
}

type grantSubscriptionCycleRequest struct {
	Plan       string     `json:"plan"`
	PurchaseID string     `json:"purchase_id"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
}

type grantPurchasedCreditsRequest struct {
	PackID     string `json:"pack_id"`
	PurchaseID string `json:"purchase_id"`
}

func (s *Server) listCreditPacks(response http.ResponseWriter, request *http.Request) {
	if s.Credits == nil {
		s.internalError(response, errors.New("credit service not configured"), "credit service not configured")
		return
	}
	s.writeJSON(response, http.StatusOK, map[string]any{
		"packs":                              s.Credits.Policy.CreditPacks,
		"dynamic_story_pricing":             s.Credits.Policy.DynamicStoryPricing,
		"initial_credit_grant":               s.Credits.Policy.InitialGrant,
		"monthly_subscription_cycle_credits": s.Credits.Policy.MonthlySubscriptionCycleCredits,
		"yearly_subscription_cycle_credits":  s.Credits.Policy.YearlySubscriptionCycleCredits,
	})
}

func (s *Server) grantSubscriptionCycleCredits(response http.ResponseWriter, request *http.Request) {
	user, ok := s.authenticate(response, request)
	if !ok {
		return
	}
	if s.Credits == nil {
		s.internalError(response, errors.New("credit service not configured"), "credit service not configured")
		return
	}
	var input grantSubscriptionCycleRequest
	if !s.decodeJSON(response, request, &input) {
		return
	}
	plan := strings.ToLower(strings.TrimSpace(input.Plan))
	purchaseID := strings.TrimSpace(input.PurchaseID)
	if purchaseID == "" {
		s.writeError(response, http.StatusBadRequest, "invalid_purchase_id", "purchase_id is required")
		return
	}
	var amount int
	switch plan {
	case "monthly", "com.framemio.pro.monthly":
		amount = s.Credits.Policy.MonthlySubscriptionCycleCredits
	case "yearly", "com.framemio.pro.yearly":
		amount = s.Credits.Policy.YearlySubscriptionCycleCredits
	default:
		s.writeError(response, http.StatusBadRequest, "invalid_plan", "plan must be monthly or yearly")
		return
	}
	beforeBalance, err := s.Credits.GetBalance(request.Context(), user.ID)
	if err != nil {
		s.internalError(response, err, "get credit balance before subscription grant")
		return
	}
	grant, created, err := s.Credits.GrantSubscriptionCycle(request.Context(), user.ID, purchaseID, amount, input.ExpiresAt, map[string]any{"plan": plan})
	if err != nil {
		s.internalError(response, err, "grant subscription cycle credits")
		return
	}
	afterBalance, err := s.Credits.GetBalance(request.Context(), user.ID)
	if err != nil {
		s.internalError(response, err, "get credit balance after subscription grant")
		return
	}
	if created {
		s.Logger.Info("subscription credits granted",
			slog.String("user_id", user.ID),
			slog.String("purchase_id", purchaseID),
			slog.String("plan", plan),
			slog.Int("amount", amount),
			slog.String("grant_id", grant.ID),
			slog.Int("balance_before_available", beforeBalance.Available),
			slog.Int("balance_before_subscription", beforeBalance.SubscriptionCredits),
			slog.Int("balance_before_purchased", beforeBalance.PurchasedCredits),
			slog.Int("balance_after_available", afterBalance.Available),
			slog.Int("balance_after_subscription", afterBalance.SubscriptionCredits),
			slog.Int("balance_after_purchased", afterBalance.PurchasedCredits),
		)
	} else {
		s.Logger.Info("subscription credits grant reused",
			slog.String("user_id", user.ID),
			slog.String("purchase_id", purchaseID),
			slog.String("plan", plan),
			slog.Int("amount", amount),
			slog.String("grant_id", grant.ID),
			slog.Int("balance_before_available", beforeBalance.Available),
			slog.Int("balance_before_subscription", beforeBalance.SubscriptionCredits),
			slog.Int("balance_before_purchased", beforeBalance.PurchasedCredits),
			slog.Int("balance_after_available", afterBalance.Available),
			slog.Int("balance_after_subscription", afterBalance.SubscriptionCredits),
			slog.Int("balance_after_purchased", afterBalance.PurchasedCredits),
		)
	}
	s.writeJSON(response, http.StatusOK, grant)
}

func (s *Server) grantPurchasedCredits(response http.ResponseWriter, request *http.Request) {
	user, ok := s.authenticate(response, request)
	if !ok {
		return
	}
	if s.Credits == nil {
		s.internalError(response, errors.New("credit service not configured"), "credit service not configured")
		return
	}
	var input grantPurchasedCreditsRequest
	if !s.decodeJSON(response, request, &input) {
		return
	}
	packID := strings.TrimSpace(input.PackID)
	purchaseID := strings.TrimSpace(input.PurchaseID)
	if purchaseID == "" {
		s.writeError(response, http.StatusBadRequest, "invalid_purchase_id", "purchase_id is required")
		return
	}
	pack, ok := s.Credits.Policy.CreditPack(packID)
	if !ok || pack.Credits <= 0 {
		s.writeError(response, http.StatusBadRequest, "invalid_credit_pack", "credit pack is invalid")
		return
	}
	beforeBalance, err := s.Credits.GetBalance(request.Context(), user.ID)
	if err != nil {
		s.internalError(response, err, "get credit balance before purchase grant")
		return
	}
	grant, duplicate, err := s.Credits.GrantPurchasedCredits(request.Context(), user.ID, purchaseID, pack.Credits, map[string]any{
		"pack_id":    pack.ID,
		"product_id": pack.ProductID,
	})
	if err != nil {
		s.internalError(response, err, "grant purchased credits")
		return
	}
	balance, err := s.Credits.GetBalance(request.Context(), user.ID)
	if err != nil {
		s.internalError(response, err, "get credit balance after purchase")
		return
	}
	if duplicate {
		s.Logger.Info("purchased credits grant reused",
			slog.String("user_id", user.ID),
			slog.String("purchase_id", purchaseID),
			slog.String("pack_id", pack.ID),
			slog.String("product_id", pack.ProductID),
			slog.Int("amount", pack.Credits),
			slog.String("grant_id", grant.ID),
			slog.Int("balance_before_available", beforeBalance.Available),
			slog.Int("balance_before_subscription", beforeBalance.SubscriptionCredits),
			slog.Int("balance_before_purchased", beforeBalance.PurchasedCredits),
			slog.Int("balance_after_available", balance.Available),
			slog.Int("balance_after_subscription", balance.SubscriptionCredits),
			slog.Int("balance_after_purchased", balance.PurchasedCredits),
		)
	} else {
		s.Logger.Info("purchased credits granted",
			slog.String("user_id", user.ID),
			slog.String("purchase_id", purchaseID),
			slog.String("pack_id", pack.ID),
			slog.String("product_id", pack.ProductID),
			slog.Int("amount", pack.Credits),
			slog.String("grant_id", grant.ID),
			slog.Int("balance_before_available", beforeBalance.Available),
			slog.Int("balance_before_subscription", beforeBalance.SubscriptionCredits),
			slog.Int("balance_before_purchased", beforeBalance.PurchasedCredits),
			slog.Int("balance_after_available", balance.Available),
			slog.Int("balance_after_subscription", balance.SubscriptionCredits),
			slog.Int("balance_after_purchased", balance.PurchasedCredits),
		)
	}
	s.writeJSON(response, http.StatusOK, map[string]any{"grant": grant, "balance": balance})
}

func (s *Server) quoteCreditAction(response http.ResponseWriter, request *http.Request) {
	user, ok := s.authenticate(response, request)
	if !ok {
		return
	}
	if s.Credits == nil {
		s.internalError(response, errors.New("credit service not configured"), "credit service not configured")
		return
	}
	var input creditQuoteRequest
	if !s.decodeJSON(response, request, &input) {
		return
	}
	action, ok := models.NormalizeCreditAction(input.Action)
	if !ok {
		s.writeError(response, http.StatusBadRequest, "invalid_credit_action", "action is invalid")
		return
	}
	quote, err := s.Credits.Quote(request.Context(), user.ID, action, input.Stats)
	if err != nil {
		s.internalError(response, err, "quote credit action")
		return
	}
	s.writeJSON(response, http.StatusOK, quote)
}

func (s *Server) renderProject(response http.ResponseWriter, request *http.Request, projectID string) {
	user, ok := s.authenticate(response, request)
	if !ok {
		return
	}
	idempotencyKey, ok := requiredIdempotencyKey(response, request)
	if !ok {
		return
	}
	if _, err := s.Store.GetProject(request.Context(), user.ID, projectID); err != nil {
		s.mapStoreError(response, err)
		return
	}
	var input renderProjectRequest
	if request.ContentLength != 0 && !s.decodeJSON(response, request, &input) {
		return
	}
	if input.OutputPreset == "" {
		input.OutputPreset = "social"
	}
	input.OutputPreset = strings.TrimSpace(input.OutputPreset)
	if input.OutputPreset == "" || len(input.OutputPreset) > 100 {
		s.writeError(response, http.StatusBadRequest, "invalid_output_preset", "output preset is invalid")
		return
	}
	if !s.allowExpensiveOperation(response, user.ID, "render_project") {
		return
	}
	plan, err := s.Store.GetStoryPlan(request.Context(), user.ID, projectID)
	if err != nil {
		s.writeError(response, http.StatusConflict, "story_not_ready", "story plan is not ready for rendering")
		return
	}
	if s.StoryPlanValidator == nil {
		s.internalError(response, errors.New("story plan validator not configured"), "story plan validator not configured")
		return
	}
	if validationErr := s.StoryPlanValidator.Validate(request.Context(), user.ID, plan); validationErr != nil {
		if structured, ok := storyvalidation.AsValidationError(validationErr); ok {
			s.writeStoryPlanValidationError(response, structured)
			return
		}
		s.internalError(response, validationErr, "validate story plan")
		return
	}
	if s.Orchestrator == nil {
		s.internalError(response, errors.New("job orchestrator not configured"), "job orchestrator not configured")
		return
	}
	stored, err := s.Orchestrator.RecordValidationAndQueueRender(request.Context(), user.ID, projectID, idempotencyKey, plan.Version, input.OutputPreset)
	if err != nil {
		s.internalError(response, err, "create render job")
		return
	}
	if !models.IsTerminalJobStatus(stored.Status) {
		if err := s.Store.SetProjectStatus(request.Context(), user.ID, projectID, models.ProjectStatusRendering); err != nil {
			s.internalError(response, err, "update project status")
			return
		}
	}
	s.writeJSON(response, http.StatusAccepted, jobsResponse{JobID: stored.ID, JobIDs: []string{stored.ID}, Jobs: []models.ProcessingJob{stored}})
}

type renderInputMediaAsset struct {
	ID              string           `json:"id"`
	MediaType       models.MediaType `json:"media_type"`
	S3Key           string           `json:"s3_key"`
	ContentType     string           `json:"content_type"`
	DurationSeconds *float64         `json:"duration_seconds,omitempty"`
	Width           int              `json:"width,omitempty"`
	Height          int              `json:"height,omitempty"`
	FileName        string           `json:"file_name,omitempty"`
	ETag            string           `json:"etag,omitempty"`
}

type renderInputResponse struct {
	JobID           string                  `json:"job_id"`
	ProjectID       string                  `json:"project_id"`
	StoryPlan       models.StoryPlan        `json:"story_plan"`
	MediaAssets     []renderInputMediaAsset `json:"media_assets"`
	OutputS3Key     string                  `json:"output_s3_key"`
	RendererVersion string                  `json:"renderer_version,omitempty"`
}

func (s *Server) getRenderInput(response http.ResponseWriter, request *http.Request, jobID string) {
	if !s.authenticateInternal(response, request) {
		return
	}
	job, err := s.Store.GetJobByID(request.Context(), jobID)
	if err != nil {
		s.mapStoreError(response, err)
		return
	}
	if job.Type != models.JobTypeVideoRender {
		s.writeError(response, http.StatusConflict, "invalid_job_type", "job does not accept render input")
		return
	}
	if job.Status == models.JobStatusFailed || job.Status == models.JobStatusCancelled {
		s.writeError(response, http.StatusConflict, "job_not_renderable", "failed or cancelled render jobs cannot be rendered")
		return
	}
	project, err := s.Store.GetProject(request.Context(), job.UserID, job.ProjectID)
	if err != nil {
		s.mapStoreError(response, err)
		return
	}
	plan, err := s.Store.GetStoryPlan(request.Context(), job.UserID, job.ProjectID)
	if err != nil {
		s.mapStoreError(response, err)
		return
	}
	if expectedVersion, ok := configuredStoryPlanVersion(job.Configuration); ok && expectedVersion != plan.Version {
		s.writeError(response, http.StatusConflict, "story_plan_version_mismatch", "render job targets a different story plan version")
		return
	}
	mediaIDs := renderInputMediaIDs(project.MediaIDs, plan)
	assets := make([]renderInputMediaAsset, 0, len(mediaIDs))
	for _, mediaID := range mediaIDs {
		media, err := s.Store.GetMedia(request.Context(), job.UserID, mediaID)
		if err != nil {
			s.mapStoreError(response, err)
			return
		}
		assets = append(assets, renderInputMediaAsset{
			ID:              media.ID,
			MediaType:       media.Type,
			S3Key:           media.S3Key,
			ContentType:     media.ContentType,
			DurationSeconds: media.DurationSeconds,
			Width:           media.PixelWidth,
			Height:          media.PixelHeight,
			FileName:        media.FileName,
			ETag:            media.ETag,
		})
	}
	outputKey := job.Configuration["output_s3_key"]
	if strings.TrimSpace(outputKey) == "" {
		outputKey = renderOutputKey(job.UserID, job.ProjectID, plan.Version, job.Configuration["output_preset"])
	}
	s.Logger.Info("render input prepared", slog.String("job_id", job.ID), slog.String("project_id", job.ProjectID), slog.String("story_plan_id", plan.ID), slog.Int("story_plan_version", plan.Version), slog.Int("moment_count", len(plan.Moments)), slog.Int("broll_count", len(plan.BRoll)), slog.Int("caption_count", len(plan.Captions)), slog.Int("media_asset_count", len(assets)))
	s.writeJSON(response, http.StatusOK, renderInputResponse{
		JobID:       job.ID,
		ProjectID:   job.ProjectID,
		StoryPlan:   plan,
		MediaAssets: assets,
		OutputS3Key: outputKey,
	})
}

func renderInputMediaIDs(projectMediaIDs []string, plan models.StoryPlan) []string {
	seen := make(map[string]struct{}, len(projectMediaIDs)+len(plan.Moments)+len(plan.BRoll))
	ids := make([]string, 0, len(projectMediaIDs)+len(plan.BRoll))
	add := func(mediaID string) {
		mediaID = strings.TrimSpace(mediaID)
		if mediaID == "" {
			return
		}
		if _, exists := seen[mediaID]; exists {
			return
		}
		seen[mediaID] = struct{}{}
		ids = append(ids, mediaID)
	}
	for _, mediaID := range projectMediaIDs {
		add(mediaID)
	}
	for _, moment := range plan.Moments {
		add(firstNonEmptyString(moment.AssetID, moment.MediaID))
	}
	for _, overlay := range plan.BRoll {
		add(firstNonEmptyString(overlay.AssetID, overlay.MediaID))
	}
	return ids
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func renderOutputKey(userID, projectID string, storyPlanVersion int, outputPreset string) string {
	preset := strings.TrimSpace(outputPreset)
	if preset == "" {
		preset = "social"
	}
	return "users/" + userID + "/projects/" + projectID + "/renders/story-plan-v" + strconv.Itoa(storyPlanVersion) + "-" + preset + ".mp4"
}

func configuredStoryPlanVersion(configuration map[string]string) (int, bool) {
	if configuration == nil || strings.TrimSpace(configuration["story_plan_version"]) == "" {
		return 0, false
	}
	version, err := strconv.Atoi(configuration["story_plan_version"])
	if err != nil || version <= 0 {
		return 0, false
	}
	return version, true
}

type updateJobStatusRequest struct {
	Status              models.JobStatus `json:"status"`
	Progress            float64          `json:"progress"`
	ErrorCode           string           `json:"error_code,omitempty"`
	ErrorMessage        string           `json:"error_message,omitempty"`
	ArtifactS3Key       string           `json:"artifact_s3_key,omitempty"`
	ArtifactContentType string           `json:"artifact_content_type,omitempty"`
}

type internalJobStatusResponse struct {
	JobID     string           `json:"job_id"`
	JobType   models.JobType   `json:"job_type"`
	Status    models.JobStatus `json:"status"`
	ProjectID string           `json:"project_id"`
	MediaID   string           `json:"media_id,omitempty"`
	Attempt   int              `json:"attempt"`
}

func (s *Server) getInternalJobStatus(response http.ResponseWriter, request *http.Request, jobID string) {
	if !s.authenticateInternal(response, request) {
		return
	}
	job, err := s.Store.GetJobByID(request.Context(), jobID)
	if err != nil {
		s.mapStoreError(response, err)
		return
	}
	s.writeJSON(response, http.StatusOK, internalJobStatusResponse{JobID: job.ID, JobType: job.Type, Status: job.Status, ProjectID: job.ProjectID, MediaID: job.MediaID, Attempt: job.Attempt})
}

func (s *Server) updateJobStatus(response http.ResponseWriter, request *http.Request, jobID string) {
	started := time.Now()
	if !s.authenticateInternal(response, request) {
		return
	}
	var input updateJobStatusRequest
	if !s.decodeJSON(response, request, &input) {
		return
	}
	normalizedStatus, validStatus := models.NormalizeJobStatus(input.Status)
	if !validStatus {
		s.writeError(response, http.StatusBadRequest, "invalid_job_status", "job status is invalid")
		return
	}
	input.Status = normalizedStatus
	if input.Progress < 0 || input.Progress > 1 {
		s.writeError(response, http.StatusBadRequest, "invalid_progress", "progress must be between zero and one")
		return
	}
	if len(input.ArtifactS3Key) > 1024 || len(input.ArtifactContentType) > 200 || strings.HasPrefix(input.ArtifactS3Key, "http://") || strings.HasPrefix(input.ArtifactS3Key, "https://") {
		s.writeError(response, http.StatusBadRequest, "invalid_artifact", "artifact must be a bounded S3 object key and content type")
		return
	}
	jobBeforeUpdate, err := s.Store.GetJobByID(request.Context(), jobID)
	if err != nil {
		s.mapStoreError(response, err)
		return
	}
	if jobBeforeUpdate.Type == models.JobTypeVideoRender && input.Status == models.JobStatusCompleted && input.ArtifactS3Key == "" && jobBeforeUpdate.ArtifactS3Key == "" {
		s.writeError(response, http.StatusBadRequest, "artifact_required", "a successful render requires an artifact_s3_key")
		return
	}
	if s.Orchestrator == nil {
		s.internalError(response, errors.New("job orchestrator not configured"), "job orchestrator not configured")
		return
	}
	job, err := s.Orchestrator.UpdateJobStatus(request.Context(), jobID, input.Status, input.Progress, input.ErrorCode, input.ErrorMessage, input.ArtifactS3Key, input.ArtifactContentType)
	if err != nil {
		s.Logger.Error("internal job status callback failed", slog.String("job_id", jobID), slog.String("requested_status", string(input.Status)), slog.Float64("requested_progress", input.Progress), slog.Bool("has_artifact", input.ArtifactS3Key != ""), slog.Int64("callback_duration_ms", time.Since(started).Milliseconds()), slog.Any("error", err))
		s.mapStoreError(response, err)
		return
	}
	s.Logger.Info("internal job status callback completed", slog.String("job_id", job.ID), slog.String("job_type", string(job.Type)), slog.String("project_id", job.ProjectID), slog.String("media_id", job.MediaID), slog.String("status", string(job.Status)), slog.Int("attempt", job.Attempt), slog.Bool("has_artifact", job.ArtifactS3Key != ""), slog.Int64("callback_duration_ms", time.Since(started).Milliseconds()))
	s.writeJSON(response, http.StatusOK, job)
}

type saveAnalysisRequest struct {
	Labels       []string               `json:"labels,omitempty"`
	PeopleCount  int                    `json:"people_count"`
	QualityScore float64                `json:"quality_score,omitempty"`
	Signals      map[string]interface{} `json:"signals,omitempty"`
}

func (s *Server) saveJobAnalysis(response http.ResponseWriter, request *http.Request, jobID string) {
	if !s.authenticateInternal(response, request) {
		return
	}
	var input saveAnalysisRequest
	if !s.decodeJSON(response, request, &input) {
		return
	}
	if input.PeopleCount < 0 || input.QualityScore < 0 || input.QualityScore > 1 {
		s.writeError(response, http.StatusBadRequest, "invalid_analysis", "analysis values are outside their valid ranges")
		return
	}
	job, err := s.getInternalJob(request, jobID)
	if err != nil {
		s.mapStoreError(response, err)
		return
	}
	if job.Status == models.JobStatusFailed || job.Status == models.JobStatusCancelled {
		s.writeError(response, http.StatusConflict, "job_failed", "failed jobs cannot accept analysis results")
		return
	}
	if job.Type != models.JobTypeMediaPreprocess || job.MediaID == "" {
		s.writeError(response, http.StatusConflict, "invalid_job_type", "job does not accept media analysis")
		return
	}
	now := s.Now().UTC()
	analysis := models.MediaAnalysis{ID: models.NewID("analysis"), ProjectID: job.ProjectID, MediaID: job.MediaID, Labels: input.Labels, PeopleCount: input.PeopleCount, QualityScore: input.QualityScore, Signals: input.Signals, CreatedAt: now, UpdatedAt: now}
	if err := s.Store.SaveAnalysis(request.Context(), job.UserID, analysis); err != nil {
		s.mapStoreError(response, err)
		return
	}
	if s.Orchestrator == nil {
		s.internalError(response, errors.New("job orchestrator not configured"), "job orchestrator not configured")
		return
	}
	updated, err := s.Orchestrator.UpdateJobStatus(request.Context(), job.ID, models.JobStatusCompleted, 1, "", "", "", "")
	if err != nil {
		s.mapStoreError(response, err)
		return
	}
	s.writeJSON(response, http.StatusOK, map[string]any{"analysis": analysis, "job": updated})
}

func (s *Server) saveJobStoryPlan(response http.ResponseWriter, request *http.Request, jobID string) {
	if !s.authenticateInternal(response, request) {
		return
	}
	var plan models.StoryPlan
	if !s.decodeJSON(response, request, &plan) {
		return
	}
	job, err := s.getInternalJob(request, jobID)
	if err != nil {
		s.mapStoreError(response, err)
		return
	}
	if job.Status == models.JobStatusFailed || job.Status == models.JobStatusCancelled {
		s.writeError(response, http.StatusConflict, "job_failed", "failed jobs cannot accept a story plan")
		return
	}
	legacyStoryPlanJob := job.Type == models.JobTypeMediaPreprocess
	if job.Type != models.JobTypeStoryPlan && !legacyStoryPlanJob {
		s.writeError(response, http.StatusConflict, "invalid_job_type", "job does not accept a story plan")
		return
	}
	project, err := s.Store.GetProject(request.Context(), job.UserID, job.ProjectID)
	if err != nil {
		s.mapStoreError(response, err)
		return
	}
	plan.ProjectID = project.ID
	if plan.ID == "" {
		plan.ID = models.NewID("plan")
	}
	if plan.Version <= 0 || len(plan.Moments) == 0 || plan.Output.AspectRatio == "" || plan.Output.MaxDurationS <= 0 || plan.Output.Preset == "" || !momentsBelongToProject(plan.Moments, project.MediaIDs) {
		s.writeError(response, http.StatusBadRequest, "invalid_story_plan", "story plan must contain valid project moments and a positive version")
		return
	}
	if plan.CreatedAt.IsZero() {
		plan.CreatedAt = s.Now().UTC()
	}
	s.Logger.Info("internal story plan save requested", slog.String("job_id", job.ID), slog.String("project_id", job.ProjectID), slog.String("story_plan_id", plan.ID), slog.Int("moment_count", len(plan.Moments)), slog.Int("caption_count", len(plan.Captions)))
	if err := s.Store.SaveStoryPlan(request.Context(), job.UserID, plan); err != nil {
		s.mapStoreError(response, err)
		return
	}
	storedPlan, err := s.Store.GetStoryPlan(request.Context(), job.UserID, job.ProjectID)
	if err != nil {
		s.mapStoreError(response, err)
		return
	}
	if legacyStoryPlanJob {
		if _, err := s.Store.UpdateJob(request.Context(), job.ID, models.JobStatusCompleted, 1, "", "", "", ""); err != nil {
			s.mapStoreError(response, err)
			return
		}
		if err := s.Store.SetProjectStatus(request.Context(), job.UserID, job.ProjectID, models.ProjectStatusStoryReady); err != nil {
			s.internalError(response, err, "mark story plan ready")
			return
		}
	} else {
		if s.Orchestrator == nil {
			s.internalError(response, errors.New("job orchestrator not configured"), "job orchestrator not configured")
			return
		}
		if _, err := s.Orchestrator.UpdateJobStatus(request.Context(), job.ID, models.JobStatusCompleted, 1, "", "", "", ""); err != nil {
			s.mapStoreError(response, err)
			return
		}
	}
	s.writeJSON(response, http.StatusOK, storedPlan)
}

func (s *Server) getInternalJob(request *http.Request, jobID string) (models.ProcessingJob, error) {
	return s.Store.GetJobByID(request.Context(), jobID)
}

func (s *Server) allowExpensiveOperation(response http.ResponseWriter, userID, operation string) bool {
	if s.ExpensiveLimiter == nil || s.ExpensiveLimiter.Allow(userID, operation, s.Now().UTC()) {
		return true
	}
	s.writeError(response, http.StatusTooManyRequests, "rate_limited", "too many story operations; please try again later")
	return false
}

func (s *Server) authenticateInternal(response http.ResponseWriter, request *http.Request) bool {
	provided := request.Header.Get("X-FrameMio-Internal-Token")
	if s.InternalToken == "" || len(provided) != len(s.InternalToken) || subtle.ConstantTimeCompare([]byte(provided), []byte(s.InternalToken)) != 1 {
		s.writeError(response, http.StatusUnauthorized, "unauthorized", "internal authentication required")
		return false
	}
	return true
}

func (s *Server) authenticate(response http.ResponseWriter, request *http.Request) (models.User, bool) {
	if user, ok := authenticatedUserFromContext(request.Context()); ok {
		return user, true
	}
	if s.Authenticator == nil {
		s.writeError(response, http.StatusInternalServerError, "auth_not_configured", "authentication is not configured")
		return models.User{}, false
	}
	user, err := s.Authenticator.Authenticate(request.Context(), request)
	if err != nil {
		s.writeError(response, http.StatusUnauthorized, "unauthorized", "authentication required")
		return models.User{}, false
	}
	if s.Store != nil {
		user, err = s.Store.EnsureUser(request.Context(), user)
		if err != nil {
			s.internalError(response, err, "ensure user")
			return models.User{}, false
		}
	}
	if s.Credits != nil {
		if _, _, err := s.Credits.EnsureInitialGrant(request.Context(), user.ID); err != nil {
			s.internalError(response, err, "ensure initial credit grant")
			return models.User{}, false
		}
	}
	return user, true
}

func requiredIdempotencyKey(response http.ResponseWriter, request *http.Request) (string, bool) {
	key := strings.TrimSpace(request.Header.Get("Idempotency-Key"))
	if key == "" {
		writeError(response, http.StatusBadRequest, "idempotency_required", "Idempotency-Key header is required")
		return "", false
	}
	if len(key) > 200 {
		writeError(response, http.StatusBadRequest, "invalid_idempotency_key", "Idempotency-Key is too long")
		return "", false
	}
	return key, true
}

func (s *Server) decodeJSON(response http.ResponseWriter, request *http.Request, destination any) bool {
	request.Body = http.MaxBytesReader(response, request.Body, maxJSONBodyBytes)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		if errors.Is(err, io.EOF) {
			s.writeError(response, http.StatusBadRequest, "invalid_json", "request body is required")
		} else {
			s.writeError(response, http.StatusBadRequest, "invalid_json", "request body is invalid")
		}
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		s.writeError(response, http.StatusBadRequest, "invalid_json", "request body must contain one JSON value")
		return false
	}
	return true
}

func validMediaType(mediaType models.MediaType) bool {
	switch mediaType {
	case models.MediaTypePhoto, models.MediaTypeVideo, models.MediaTypeLivePhoto:
		return true
	default:
		return false
	}
}

func uniqueNonEmpty(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func momentsBelongToProject(moments []models.StoryMoment, mediaIDs []string) bool {
	allowed := make(map[string]struct{}, len(mediaIDs))
	for _, mediaID := range mediaIDs {
		allowed[mediaID] = struct{}{}
	}
	for _, moment := range moments {
		mediaID := moment.AssetID
		if mediaID == "" {
			mediaID = moment.MediaID
		}
		if mediaID == "" || moment.DurationMS <= 0 {
			return false
		}
		if _, exists := allowed[mediaID]; !exists {
			return false
		}
		if moment.MediaType == models.MediaTypeVideo || moment.VideoAnalysisVersion != "" || moment.StartMicros != 0 || moment.EndMicros != 0 {
			if moment.VideoAnalysisVersion == "" || moment.StartMicros < 0 || moment.EndMicros <= moment.StartMicros {
				return false
			}
		}
	}
	return true
}

func (s *Server) mapStoreError(response http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		s.writeError(response, http.StatusNotFound, "not_found", "record not found")
	case errors.Is(err, store.ErrInvalidOwner):
		s.writeError(response, http.StatusNotFound, "not_found", "record not found")
	case errors.Is(err, store.ErrConflict):
		s.writeError(response, http.StatusConflict, "conflict", "record conflict")
	default:
		s.internalError(response, err, "store operation")
	}
}

func (s *Server) internalError(response http.ResponseWriter, err error, operation string) {
	s.Logger.Error(operation, slog.Any("error", err))
	s.writeError(response, http.StatusInternalServerError, "internal_error", "request could not be completed")
}

func (s *Server) writeJSON(response http.ResponseWriter, status int, value any) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	if err := json.NewEncoder(response).Encode(value); err != nil {
		s.Logger.Error("write response", slog.Any("error", err))
	}
}

func (s *Server) writeError(response http.ResponseWriter, status int, code, message string) {
	writeError(response, status, code, message)
}

func (s *Server) writeStoryPlanValidationError(response http.ResponseWriter, validationErr *storyvalidation.ValidationError) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(http.StatusBadRequest)
	_ = json.NewEncoder(response).Encode(map[string]any{
		"error":   "invalid_story_plan",
		"message": "story plan is invalid",
		"details": validationErr.Errors,
	})
}

func writeError(response http.ResponseWriter, status int, code, message string) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(map[string]string{"error": code, "message": message})
}

func (s *Server) withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		started := time.Now()
		requestID := strings.TrimSpace(request.Header.Get("X-Request-ID"))
		if requestID == "" || len(requestID) > 100 {
			requestID = models.NewID("request")
		}
		response.Header().Set("X-Request-ID", requestID)
		tracked := &statusResponseWriter{ResponseWriter: response}
		next.ServeHTTP(tracked, request)
		s.Logger.Info("http request",
			slog.String("request_id", requestID),
			slog.String("method", request.Method),
			slog.String("path", request.URL.Path),
			slog.Int("status", tracked.Status()),
			slog.Duration("duration", time.Since(started)),
		)
	})
}

type statusResponseWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusResponseWriter) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusResponseWriter) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(body)
}

func (w *statusResponseWriter) Status() int {
	if w.status == 0 {
		return http.StatusOK
	}
	return w.status
}
