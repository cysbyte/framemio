package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/framemio/backend/internal/models"
	"github.com/framemio/backend/internal/storage"
)

type iosCreateUploadSessionRequest struct {
	IdempotencyKey string                 `json:"idempotencyKey"`
	Metadata       iosUploadMediaMetadata `json:"metadata"`
}

type iosUploadMediaMetadata struct {
	AssetID               string           `json:"assetID"`
	MediaType             models.MediaType `json:"mediaType"`
	ContentType           string           `json:"contentType"`
	FileName              string           `json:"fileName"`
	FileSizeBytes         int64            `json:"fileSizeBytes"`
	DurationSeconds       *float64         `json:"duration,omitempty"`
	CreationDate          *time.Time       `json:"creationDate,omitempty"`
	PixelWidth            int              `json:"pixelWidth"`
	PixelHeight           int              `json:"pixelHeight"`
	Orientation           string           `json:"orientation"`
	Optimization          string           `json:"optimization"`
	PreferredPartSizeByte int64            `json:"preferredPartSizeBytes"`
}

type iosUploadSessionResponse struct {
	UploadID            string                  `json:"uploadID"`
	MediaID             string                  `json:"mediaID"`
	S3Key               string                  `json:"s3Key"`
	Strategy            models.UploadStrategy   `json:"strategy"`
	PartSizeBytes       int64                   `json:"partSizeBytes"`
	S3MultipartUploadID *string                 `json:"s3MultipartUploadID,omitempty"`
	ExpiresAt           *time.Time              `json:"expiresAt,omitempty"`
	Parts               []iosUploadPartResponse `json:"parts"`
}

type iosUploadPartResponse struct {
	PartNumber   int    `json:"partNumber"`
	OffsetBytes  int64  `json:"offsetBytes"`
	LengthBytes  int64  `json:"lengthBytes"`
	PresignedURL string `json:"presignedURL"`
	Status       string `json:"status"`
	Attempts     int    `json:"attempts"`
}

func (s *Server) createIOSUploadSession(response http.ResponseWriter, request *http.Request) {
	user, ok := s.authenticate(response, request)
	if !ok {
		return
	}
	var input iosCreateUploadSessionRequest
	if !s.decodeJSON(response, request, &input) {
		return
	}
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	if input.IdempotencyKey == "" || len(input.IdempotencyKey) > 200 {
		s.writeError(response, http.StatusBadRequest, "invalid_idempotency_key", "a valid idempotencyKey is required")
		return
	}
	metadata := input.Metadata
	metadata.FileName = strings.TrimSpace(metadata.FileName)
	metadata.ContentType = strings.TrimSpace(metadata.ContentType)
	if metadata.FileName == "" || metadata.FileSizeBytes <= 0 || metadata.ContentType == "" || !validMediaType(metadata.MediaType) {
		s.writeError(response, http.StatusBadRequest, "invalid_media", "valid upload metadata is required")
		return
	}
	if metadata.PixelWidth < 0 || metadata.PixelHeight < 0 || (metadata.DurationSeconds != nil && *metadata.DurationSeconds < 0) {
		s.writeError(response, http.StatusBadRequest, "invalid_media_metadata", "media dimensions and duration cannot be negative")
		return
	}
	if existing, err := s.Store.GetUploadSessionByIdempotency(request.Context(), user.ID, input.IdempotencyKey); err == nil {
		s.writeJSON(response, http.StatusOK, makeIOSUploadSession(existing))
		return
	}
	mediaID := models.NewID("media")
	session, err := s.ObjectStorage.AuthorizeUpload(request.Context(), storage.UploadAuthorizationRequest{
		UserID: user.ID, MediaID: mediaID, FileName: metadata.FileName, ContentType: metadata.ContentType,
		FileSizeBytes: metadata.FileSizeBytes, Optimization: metadata.Optimization,
	})
	if err != nil {
		s.internalError(response, err, "authorize iOS upload")
		return
	}
	session.IdempotencyKey = input.IdempotencyKey
	now := s.Now().UTC()
	media := models.MediaAsset{
		ID: mediaID, UserID: user.ID, Type: metadata.MediaType, Status: models.MediaStatusUploading,
		FileName: metadata.FileName, ContentType: metadata.ContentType, FileSizeBytes: metadata.FileSizeBytes,
		DurationSeconds: metadata.DurationSeconds, CreationDate: metadata.CreationDate, PixelWidth: metadata.PixelWidth,
		PixelHeight: metadata.PixelHeight, Orientation: metadata.Orientation, S3Key: session.S3Key, UploadID: session.ID,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := s.Store.CreateMediaUpload(request.Context(), media, session); err != nil {
		if abortErr := s.ObjectStorage.AbortUpload(request.Context(), session); abortErr != nil {
			s.Logger.Warn("abort unused iOS upload session", "error", abortErr, "upload_id", session.ID)
		}
		if existing, lookupErr := s.Store.GetUploadSessionByIdempotency(request.Context(), user.ID, input.IdempotencyKey); lookupErr == nil {
			s.writeJSON(response, http.StatusOK, makeIOSUploadSession(existing))
			return
		}
		s.internalError(response, err, "persist iOS upload session")
		return
	}
	s.writeJSON(response, http.StatusCreated, makeIOSUploadSession(session))
}

type iosCompleteMultipartRequest struct {
	UploadID string                      `json:"uploadId"`
	S3Key    string                      `json:"s3Key"`
	Parts    []iosCompletedMultipartPart `json:"parts"`
}

type iosCompletedMultipartPart struct {
	PartNumber int    `json:"partNumber"`
	ETag       string `json:"eTag"`
}

func (s *Server) completeIOSMultipartUpload(response http.ResponseWriter, request *http.Request, uploadID string) {
	user, ok := s.authenticate(response, request)
	if !ok {
		return
	}
	var input iosCompleteMultipartRequest
	if !s.decodeJSON(response, request, &input) {
		return
	}
	if input.UploadID != uploadID || input.S3Key == "" {
		s.writeError(response, http.StatusConflict, "invalid_upload_reference", "upload reference does not match the route")
		return
	}
	session, err := s.Store.GetUploadSession(request.Context(), user.ID, uploadID)
	if err != nil || session.S3Key != input.S3Key || session.Strategy != models.UploadStrategyMultipart {
		s.writeError(response, http.StatusConflict, "invalid_upload_reference", "multipart upload session does not match")
		return
	}
	media, err := s.Store.GetMedia(request.Context(), user.ID, session.MediaID)
	if err != nil {
		s.mapStoreError(response, err)
		return
	}
	if media.Status == models.MediaStatusReady {
		s.writeJSON(response, http.StatusOK, media)
		return
	}
	parts := make([]models.CompletedUploadPart, 0, len(input.Parts))
	for _, part := range input.Parts {
		parts = append(parts, models.CompletedUploadPart{PartNumber: part.PartNumber, ETag: part.ETag})
	}
	etag, err := s.ObjectStorage.CompleteUpload(request.Context(), session, parts)
	if err != nil {
		s.internalError(response, err, "complete iOS multipart upload")
		return
	}
	media, err = s.Store.MarkMediaReady(request.Context(), user.ID, session.MediaID, session.ID, session.S3Key, etag)
	if err != nil {
		s.mapStoreError(response, err)
		return
	}
	s.writeJSON(response, http.StatusOK, media)
}

type iosUploadCompletionNotification struct {
	MediaID   string           `json:"mediaId"`
	UploadID  string           `json:"uploadId"`
	S3Key     string           `json:"s3Key"`
	MediaType models.MediaType `json:"mediaType"`
	Duration  *float64         `json:"duration,omitempty"`
}

func (s *Server) notifyIOSUploadComplete(response http.ResponseWriter, request *http.Request) {
	user, ok := s.authenticate(response, request)
	if !ok {
		return
	}
	var input iosUploadCompletionNotification
	if !s.decodeJSON(response, request, &input) {
		return
	}
	if input.MediaID == "" || input.UploadID == "" || input.S3Key == "" || !validMediaType(input.MediaType) {
		s.writeError(response, http.StatusBadRequest, "invalid_completion", "valid upload completion metadata is required")
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
	if media.Type != input.MediaType {
		s.writeError(response, http.StatusConflict, "media_type_mismatch", "media type does not match the upload session")
		return
	}
	if media.Status == models.MediaStatusReady {
		s.writeJSON(response, http.StatusOK, media)
		return
	}
	etag, err := s.ObjectStorage.CompleteUpload(request.Context(), session, nil)
	if err != nil {
		s.internalError(response, err, "verify iOS object upload")
		return
	}
	media, err = s.Store.MarkMediaReady(request.Context(), user.ID, input.MediaID, input.UploadID, input.S3Key, etag)
	if err != nil {
		s.mapStoreError(response, err)
		return
	}
	s.writeJSON(response, http.StatusOK, media)
}

func makeIOSUploadSession(session models.UploadSession) iosUploadSessionResponse {
	parts := make([]iosUploadPartResponse, 0, len(session.Parts))
	for _, part := range session.Parts {
		parts = append(parts, iosUploadPartResponse{
			PartNumber: part.PartNumber, OffsetBytes: part.OffsetBytes, LengthBytes: part.LengthBytes,
			PresignedURL: part.UploadURL, Status: "pending", Attempts: 0,
		})
	}
	var multipartUploadID *string
	if session.MultipartUploadID != "" {
		multipartUploadID = &session.MultipartUploadID
	}
	expiresAt := session.ExpiresAt
	return iosUploadSessionResponse{
		UploadID: session.ID, MediaID: session.MediaID, S3Key: session.S3Key, Strategy: session.Strategy,
		PartSizeBytes: session.PartSizeBytes, S3MultipartUploadID: multipartUploadID, ExpiresAt: &expiresAt, Parts: parts,
	}
}
