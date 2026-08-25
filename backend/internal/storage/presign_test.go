package storage

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// newTestS3Storage builds storage with static credentials so presigning, which
// is pure local computation, needs no AWS account or network.
func newTestS3Storage() *S3ObjectStorage {
	client := s3.New(s3.Options{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("AKIAEXAMPLE", "secretexample", ""),
	})
	return &S3ObjectStorage{
		bucket:    "framemio-media",
		expiry:    2 * time.Hour,
		client:    client,
		presigner: s3.NewPresignClient(client),
	}
}

// A media player fetching a presigned URL sends no headers beyond Host. If the
// URL signs anything else, S3 rejects the signature with 403 and the film shows
// as unplayable on the device, so pin the signed header set.
func TestPresignedDownloadURLSignsOnlyHost(t *testing.T) {
	raw, err := newTestS3Storage().PresignedDownloadURL(context.Background(), "renders/story.mp4", time.Hour)
	if err != nil {
		t.Fatalf("presign download url: %v", err)
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse presigned url: %v", err)
	}
	if got := parsed.Query().Get("X-Amz-SignedHeaders"); got != "host" {
		t.Errorf("X-Amz-SignedHeaders = %q, want %q; a player cannot send extra signed headers", got, "host")
	}
	if got := parsed.Query().Get("X-Amz-Signature"); got == "" {
		t.Error("presigned url has no signature")
	}
}

// Guards the specific regression: the SDK's default response checksum
// validation adds x-amz-checksum-mode to the signed headers.
func TestPresignedDownloadURLOmitsChecksumMode(t *testing.T) {
	raw, err := newTestS3Storage().PresignedDownloadURL(context.Background(), "renders/story.mp4", time.Hour)
	if err != nil {
		t.Fatalf("presign download url: %v", err)
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse presigned url: %v", err)
	}
	signed := parsed.Query().Get("X-Amz-SignedHeaders")
	if strings.Contains(signed, "x-amz-checksum-mode") {
		t.Errorf("presigned url signs x-amz-checksum-mode (SignedHeaders=%q)", signed)
	}
}

// The default expiry applies when a caller passes a non-positive duration, so a
// misconfigured call still yields a usable link rather than an instant 403.
func TestPresignedDownloadURLDefaultsExpiryAndRequiresKey(t *testing.T) {
	storage := newTestS3Storage()
	raw, err := storage.PresignedDownloadURL(context.Background(), "renders/story.mp4", 0)
	if err != nil {
		t.Fatalf("presign download url: %v", err)
	}
	parsed, _ := url.Parse(raw)
	if got := parsed.Query().Get("X-Amz-Expires"); got != "7200" {
		t.Errorf("X-Amz-Expires = %q, want 7200 from the default expiry", got)
	}
	if _, err := storage.PresignedDownloadURL(context.Background(), "", time.Hour); err == nil {
		t.Error("expected an error for an empty key")
	}
}

