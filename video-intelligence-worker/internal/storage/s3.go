package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type SourceInfo struct {
	SizeBytes int64
	ETag      string
}

type ObjectStore interface {
	Head(ctx context.Context, key string) (SourceInfo, error)
	DownloadFile(ctx context.Context, key, destinationPath string) error
	UploadFile(ctx context.Context, key, sourcePath, contentType string) error
	PutJSON(ctx context.Context, key string, value any) error
	GetJSON(ctx context.Context, key string, destination any) error
	PresignGet(ctx context.Context, key string, expiry time.Duration) (string, error)
}

type S3 struct {
	bucket    string
	client    *s3.Client
	presigner *s3.PresignClient
}

func NewS3(ctx context.Context, bucket, region string) (*S3, error) {
	options := make([]func(*config.LoadOptions) error, 0, 1)
	if region != "" {
		options = append(options, config.WithRegion(region))
	}
	configuration, err := config.LoadDefaultConfig(ctx, options...)
	if err != nil {
		return nil, fmt.Errorf("load aws configuration: %w", err)
	}
	client := s3.NewFromConfig(configuration)
	return &S3{bucket: bucket, client: client, presigner: s3.NewPresignClient(client)}, nil
}

func (s *S3) Head(ctx context.Context, key string) (SourceInfo, error) {
	result, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	if err != nil {
		return SourceInfo{}, fmt.Errorf("head s3 object %s: %w", key, err)
	}
	return SourceInfo{SizeBytes: aws.ToInt64(result.ContentLength), ETag: aws.ToString(result.ETag)}, nil
}

func (s *S3) DownloadFile(ctx context.Context, key, destinationPath string) error {
	result, err := s.client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	if err != nil {
		return fmt.Errorf("get s3 object %s: %w", key, err)
	}
	defer result.Body.Close()
	if err := os.MkdirAll(filepath.Dir(destinationPath), 0o700); err != nil {
		return err
	}
	temporaryPath := destinationPath + ".partial"
	file, err := os.OpenFile(temporaryPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(file, result.Body)
	closeErr := file.Close()
	if copyErr != nil {
		_ = os.Remove(temporaryPath)
		return fmt.Errorf("download s3 object %s: %w", key, copyErr)
	}
	if closeErr != nil {
		_ = os.Remove(temporaryPath)
		return closeErr
	}
	if err := os.Rename(temporaryPath, destinationPath); err != nil {
		_ = os.Remove(temporaryPath)
		return err
	}
	return nil
}

func (s *S3) UploadFile(ctx context.Context, key, sourcePath, contentType string) error {
	file, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil {
		return err
	}
	_, err = s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(s.bucket), Key: aws.String(key), Body: file,
		ContentType: aws.String(contentType), ContentLength: aws.Int64(stat.Size()),
	})
	if err != nil {
		return fmt.Errorf("put s3 object %s: %w", key, err)
	}
	return nil
}

func (s *S3) PutJSON(ctx context.Context, key string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp("", "framemio-evidence-*.json")
	if err != nil {
		return err
	}
	path := temporary.Name()
	defer os.Remove(path)
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return s.UploadFile(ctx, key, path, "application/json")
}

func (s *S3) GetJSON(ctx context.Context, key string, destination any) error {
	result, err := s.client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	if err != nil {
		return fmt.Errorf("get s3 JSON %s: %w", key, err)
	}
	defer result.Body.Close()
	decoder := json.NewDecoder(io.LimitReader(result.Body, 20<<20))
	if err := decoder.Decode(destination); err != nil {
		return fmt.Errorf("decode s3 JSON %s: %w", key, err)
	}
	return nil
}

func (s *S3) PresignGet(ctx context.Context, key string, expiry time.Duration) (string, error) {
	result, err := s.presigner.PresignGetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)}, func(options *s3.PresignOptions) {
		options.Expires = expiry
	})
	if err != nil {
		return "", fmt.Errorf("presign s3 object %s: %w", key, err)
	}
	return result.URL, nil
}
