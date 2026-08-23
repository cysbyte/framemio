package storage

import (
	"context"
	"fmt"
	"math"
	"path"
	"sort"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/framemio/backend/internal/models"
)

type UploadAuthorizationRequest struct {
	UserID        string
	MediaID       string
	FileName      string
	ContentType   string
	FileSizeBytes int64
	Optimization  string
}

type UploadRefreshRequest struct {
	ContentType   string
	FileSizeBytes int64
}

type ObjectStorage interface {
	AuthorizeUpload(ctx context.Context, request UploadAuthorizationRequest) (models.UploadSession, error)
	RefreshUpload(ctx context.Context, session models.UploadSession, request UploadRefreshRequest) (models.UploadSession, error)
	CompleteUpload(ctx context.Context, session models.UploadSession, parts []models.CompletedUploadPart) (string, error)
	AbortUpload(ctx context.Context, session models.UploadSession) error
	ObjectExists(ctx context.Context, key string) (bool, error)
}

type S3ObjectStorage struct {
	bucket             string
	partSizeBytes      int64
	multipartThreshold int64
	expiry             time.Duration
	client             *s3.Client
	presigner          *s3.PresignClient
}

func NewS3ObjectStorage(ctx context.Context, bucket string, partSizeBytes int64, multipartThreshold int64, expiry time.Duration) (*S3ObjectStorage, error) {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}
	if partSizeBytes < 5*1024*1024 {
		partSizeBytes = 8 * 1024 * 1024
	}
	if multipartThreshold < partSizeBytes {
		multipartThreshold = partSizeBytes
	}
	if expiry <= 0 {
		expiry = 15 * time.Minute
	}
	client := s3.NewFromConfig(cfg)
	return &S3ObjectStorage{
		bucket:             bucket,
		partSizeBytes:      partSizeBytes,
		multipartThreshold: multipartThreshold,
		expiry:             expiry,
		client:             client,
		presigner:          s3.NewPresignClient(client),
	}, nil
}

func (s *S3ObjectStorage) AuthorizeUpload(ctx context.Context, request UploadAuthorizationRequest) (models.UploadSession, error) {
	now := time.Now().UTC()
	expiresAt := now.Add(s.expiry)
	fileName := path.Base(request.FileName)
	key := fmt.Sprintf("users/%s/media/%s/%s", request.UserID, request.MediaID, fileName)
	if request.FileName == "" {
		key = fmt.Sprintf("users/%s/media/%s", request.UserID, request.MediaID)
	}
	if request.FileSizeBytes < s.multipartThreshold {
		presigned, err := s.presigner.PresignPutObject(ctx, &s3.PutObjectInput{
			Bucket:        aws.String(s.bucket),
			Key:           aws.String(key),
			ContentType:   aws.String(request.ContentType),
			ContentLength: aws.Int64(request.FileSizeBytes),
		}, func(options *s3.PresignOptions) {
			options.Expires = s.expiry
		})
		if err != nil {
			return models.UploadSession{}, fmt.Errorf("presign s3 object: %w", err)
		}
		return models.UploadSession{
			ID:            models.NewID("upload"),
			UserID:        request.UserID,
			MediaID:       request.MediaID,
			S3Key:         key,
			Strategy:      models.UploadStrategySingle,
			PartSizeBytes: request.FileSizeBytes,
			Parts:         []models.UploadPart{{PartNumber: 1, OffsetBytes: 0, LengthBytes: request.FileSizeBytes, UploadURL: presigned.URL, ExpiresAt: expiresAt}},
			ExpiresAt:     expiresAt,
			CreatedAt:     now,
		}, nil
	}

	partCount := int(math.Ceil(float64(request.FileSizeBytes) / float64(s.partSizeBytes)))
	if partCount > 10_000 {
		return models.UploadSession{}, fmt.Errorf("multipart upload exceeds S3 part limit")
	}
	created, err := s.client.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(key),
		ContentType: aws.String(request.ContentType),
	})
	if err != nil {
		return models.UploadSession{}, fmt.Errorf("create s3 multipart upload: %w", err)
	}
	if created.UploadId == nil || *created.UploadId == "" {
		return models.UploadSession{}, fmt.Errorf("create s3 multipart upload: missing upload id")
	}
	parts := make([]models.UploadPart, 0, partCount)
	for number := 1; number <= partCount; number++ {
		offset := int64(number-1) * s.partSizeBytes
		length := s.partSizeBytes
		if remaining := request.FileSizeBytes - offset; remaining < length {
			length = remaining
		}
		part, err := s.presigner.PresignUploadPart(ctx, &s3.UploadPartInput{
			Bucket:        aws.String(s.bucket),
			Key:           aws.String(key),
			UploadId:      created.UploadId,
			PartNumber:    aws.Int32(int32(number)),
			ContentLength: aws.Int64(length),
		}, func(options *s3.PresignOptions) {
			options.Expires = s.expiry
		})
		if err != nil {
			_, _ = s.client.AbortMultipartUpload(ctx, &s3.AbortMultipartUploadInput{
				Bucket: aws.String(s.bucket), Key: aws.String(key), UploadId: created.UploadId,
			})
			return models.UploadSession{}, fmt.Errorf("presign s3 part %d: %w", number, err)
		}
		parts = append(parts, models.UploadPart{PartNumber: number, OffsetBytes: offset, LengthBytes: length, UploadURL: part.URL, ExpiresAt: expiresAt})
	}
	return models.UploadSession{
		ID:                models.NewID("upload"),
		UserID:            request.UserID,
		MediaID:           request.MediaID,
		S3Key:             key,
		Strategy:          models.UploadStrategyMultipart,
		MultipartUploadID: *created.UploadId,
		PartSizeBytes:     s.partSizeBytes,
		Parts:             parts,
		ExpiresAt:         expiresAt,
		CreatedAt:         now,
	}, nil
}

func (s *S3ObjectStorage) RefreshUpload(ctx context.Context, session models.UploadSession, request UploadRefreshRequest) (models.UploadSession, error) {
	now := time.Now().UTC()
	expiresAt := now.Add(s.expiry)
	refreshed := session
	refreshed.ExpiresAt = expiresAt
	if refreshed.Strategy == models.UploadStrategySingle {
		presigned, err := s.presigner.PresignPutObject(ctx, &s3.PutObjectInput{
			Bucket:        aws.String(s.bucket),
			Key:           aws.String(refreshed.S3Key),
			ContentType:   aws.String(request.ContentType),
			ContentLength: aws.Int64(request.FileSizeBytes),
		}, func(options *s3.PresignOptions) {
			options.Expires = s.expiry
		})
		if err != nil {
			return models.UploadSession{}, fmt.Errorf("refresh s3 object upload: %w", err)
		}
		if len(refreshed.Parts) == 0 {
			refreshed.Parts = []models.UploadPart{{PartNumber: 1, OffsetBytes: 0, LengthBytes: request.FileSizeBytes}}
		}
		refreshed.Parts[0].UploadURL = presigned.URL
		refreshed.Parts[0].ExpiresAt = expiresAt
		return refreshed, nil
	}

	if refreshed.MultipartUploadID == "" {
		return models.UploadSession{}, fmt.Errorf("refresh multipart upload: missing upload id")
	}
	for index := range refreshed.Parts {
		part := &refreshed.Parts[index]
		presigned, err := s.presigner.PresignUploadPart(ctx, &s3.UploadPartInput{
			Bucket:        aws.String(s.bucket),
			Key:           aws.String(refreshed.S3Key),
			UploadId:      aws.String(refreshed.MultipartUploadID),
			PartNumber:    aws.Int32(int32(part.PartNumber)),
			ContentLength: aws.Int64(part.LengthBytes),
		}, func(options *s3.PresignOptions) {
			options.Expires = s.expiry
		})
		if err != nil {
			return models.UploadSession{}, fmt.Errorf("refresh s3 part %d: %w", part.PartNumber, err)
		}
		part.UploadURL = presigned.URL
		part.ExpiresAt = expiresAt
	}
	return refreshed, nil
}

func (s *S3ObjectStorage) CompleteUpload(ctx context.Context, session models.UploadSession, parts []models.CompletedUploadPart) (string, error) {
	if session.Strategy == models.UploadStrategySingle {
		head, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(session.S3Key)})
		if err != nil {
			return "", fmt.Errorf("verify s3 object: %w", err)
		}
		if aws.ToInt64(head.ContentLength) != sessionSize(session) {
			return "", fmt.Errorf("uploaded object size does not match authorization")
		}
		return aws.ToString(head.ETag), nil
	}
	if session.MultipartUploadID == "" || len(parts) == 0 {
		return "", fmt.Errorf("multipart completion requires upload id and parts")
	}
	if len(parts) != len(session.Parts) {
		return "", fmt.Errorf("multipart completion requires every authorized part")
	}
	authorized := make(map[int]struct{}, len(session.Parts))
	for _, part := range session.Parts {
		authorized[part.PartNumber] = struct{}{}
	}
	seen := make(map[int]struct{}, len(parts))
	sort.Slice(parts, func(left, right int) bool { return parts[left].PartNumber < parts[right].PartNumber })
	completed := make([]types.CompletedPart, 0, len(parts))
	for _, part := range parts {
		if part.PartNumber <= 0 || part.ETag == "" {
			return "", fmt.Errorf("invalid completed upload part")
		}
		if _, ok := authorized[part.PartNumber]; !ok {
			return "", fmt.Errorf("completed upload part was not authorized")
		}
		if _, duplicate := seen[part.PartNumber]; duplicate {
			return "", fmt.Errorf("duplicate completed upload part")
		}
		seen[part.PartNumber] = struct{}{}
		completed = append(completed, types.CompletedPart{PartNumber: aws.Int32(int32(part.PartNumber)), ETag: aws.String(part.ETag)})
	}
	result, err := s.client.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{
		Bucket:          aws.String(s.bucket),
		Key:             aws.String(session.S3Key),
		UploadId:        aws.String(session.MultipartUploadID),
		MultipartUpload: &types.CompletedMultipartUpload{Parts: completed},
	})
	if err != nil {
		return "", fmt.Errorf("complete s3 multipart upload: %w", err)
	}
	head, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(session.S3Key)})
	if err != nil {
		return "", fmt.Errorf("verify completed s3 object: %w", err)
	}
	if aws.ToInt64(head.ContentLength) != sessionSize(session) {
		return "", fmt.Errorf("completed object size does not match authorization")
	}
	return aws.ToString(result.ETag), nil
}

func sessionSize(session models.UploadSession) int64 {
	var total int64
	for _, part := range session.Parts {
		total += part.LengthBytes
	}
	return total
}

func (s *S3ObjectStorage) AbortUpload(ctx context.Context, session models.UploadSession) error {
	if session.Strategy != models.UploadStrategyMultipart || session.MultipartUploadID == "" {
		return nil
	}
	_, err := s.client.AbortMultipartUpload(ctx, &s3.AbortMultipartUploadInput{
		Bucket: aws.String(s.bucket), Key: aws.String(session.S3Key), UploadId: aws.String(session.MultipartUploadID),
	})
	if err != nil {
		return fmt.Errorf("abort s3 multipart upload: %w", err)
	}
	return nil
}

func (s *S3ObjectStorage) ObjectExists(ctx context.Context, key string) (bool, error) {
	_, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	if err != nil {
		return false, err
	}
	return true, nil
}

type StaticObjectStorage struct {
	BaseURL            string
	PartSizeBytes      int64
	MultipartThreshold int64
}

func (s StaticObjectStorage) AuthorizeUpload(_ context.Context, request UploadAuthorizationRequest) (models.UploadSession, error) {
	partSize := s.PartSizeBytes
	if partSize < 5*1024*1024 {
		partSize = 8 * 1024 * 1024
	}
	threshold := s.MultipartThreshold
	if threshold < partSize {
		threshold = partSize
	}
	now := time.Now().UTC()
	expiresAt := now.Add(15 * time.Minute)
	key := fmt.Sprintf("users/%s/media/%s", request.UserID, request.MediaID)
	strategy := models.UploadStrategySingle
	if request.FileSizeBytes >= threshold {
		strategy = models.UploadStrategyMultipart
	}
	partCount := int(math.Ceil(float64(request.FileSizeBytes) / float64(partSize)))
	parts := make([]models.UploadPart, 0, partCount)
	for number := 1; number <= partCount; number++ {
		offset := int64(number-1) * partSize
		length := partSize
		if remaining := request.FileSizeBytes - offset; remaining < length {
			length = remaining
		}
		parts = append(parts, models.UploadPart{PartNumber: number, OffsetBytes: offset, LengthBytes: length, UploadURL: fmt.Sprintf("%s/%s/part/%d", s.BaseURL, request.MediaID, number), ExpiresAt: expiresAt})
	}
	multipartUploadID := ""
	if strategy == models.UploadStrategyMultipart {
		multipartUploadID = models.NewID("multipart")
	}
	return models.UploadSession{ID: models.NewID("upload"), UserID: request.UserID, MediaID: request.MediaID, S3Key: key, Strategy: strategy, MultipartUploadID: multipartUploadID, PartSizeBytes: partSize, Parts: parts, ExpiresAt: expiresAt, CreatedAt: now}, nil
}

func (s StaticObjectStorage) RefreshUpload(_ context.Context, session models.UploadSession, request UploadRefreshRequest) (models.UploadSession, error) {
	expiresAt := time.Now().UTC().Add(15 * time.Minute)
	refreshed := session
	refreshed.ExpiresAt = expiresAt
	if len(refreshed.Parts) == 0 && refreshed.Strategy == models.UploadStrategySingle {
		refreshed.Parts = []models.UploadPart{{PartNumber: 1, OffsetBytes: 0, LengthBytes: request.FileSizeBytes}}
	}
	for index := range refreshed.Parts {
		refreshed.Parts[index].UploadURL = fmt.Sprintf("%s/%s/part/%d?refresh=%d", s.BaseURL, refreshed.MediaID, refreshed.Parts[index].PartNumber, expiresAt.Unix())
		refreshed.Parts[index].ExpiresAt = expiresAt
	}
	return refreshed, nil
}

func (s StaticObjectStorage) CompleteUpload(_ context.Context, session models.UploadSession, parts []models.CompletedUploadPart) (string, error) {
	if session.Strategy == models.UploadStrategyMultipart {
		if len(parts) != len(session.Parts) {
			return "", fmt.Errorf("multipart completion requires every authorized part")
		}
	}
	return "mock-etag", nil
}

func (s StaticObjectStorage) AbortUpload(_ context.Context, _ models.UploadSession) error { return nil }

func (s StaticObjectStorage) ObjectExists(_ context.Context, _ string) (bool, error) {
	return true, nil
}
