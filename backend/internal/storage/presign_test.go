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
		bucket:             "framemio-media",
		partSizeBytes:      8 * 1024 * 1024,
		multipartThreshold: 8 * 1024 * 1024,
		expiry:             2 * time.Hour,
		client:             client,
		presigner:          s3.NewPresignClient(client),
	}
}

func TestPresignedSingleUploadSignsOnlyHost(t *testing.T) {
	presigned, err := newTestS3Storage().presignPutObjectUpload(context.Background(), "uploads/photo.jpg")
	if err != nil {
		t.Fatalf("presign single upload: %v", err)
	}
	if got := signedHeaders(t, presigned.URL); got != "host" {
		t.Errorf("X-Amz-SignedHeaders = %q, want host; iOS background uploads may rewrite content headers", got)
	}
}

func TestPresignedMultipartUploadPartSignsOnlyHost(t *testing.T) {
	presigned, err := newTestS3Storage().presignUploadPart(context.Background(), "uploads/video.mov", "multipart-upload-id", 1)
	if err != nil {
		t.Fatalf("presign multipart upload part: %v", err)
	}
	if got := signedHeaders(t, presigned.URL); got != "host" {
		t.Errorf("X-Amz-SignedHeaders = %q, want host; iOS background uploads may rewrite content headers", got)
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

func TestDownloadBaseURLUsesCloudFront(t *testing.T) {
	storage := newTestS3Storage()
	if err := storage.SetDownloadBaseURL("https://d3j31q5bw6z6w5.cloudfront.net"); err != nil {
		t.Fatalf("set download base url: %v", err)
	}
	raw, err := storage.PresignedDownloadURL(context.Background(), "users/user_123/renders/story final.mp4", time.Hour)
	if err != nil {
		t.Fatalf("download url: %v", err)
	}
	if got, want := raw, "https://d3j31q5bw6z6w5.cloudfront.net/users/user_123/renders/story%20final.mp4"; got != want {
		t.Errorf("download url = %q, want %q", got, want)
	}
	if strings.Contains(raw, "X-Amz-Signature") {
		t.Errorf("cdn download url should not include s3 presign query: %q", raw)
	}
}

func TestDownloadBaseURLRejectsNonHTTPS(t *testing.T) {
	if err := newTestS3Storage().SetDownloadBaseURL("http://d3j31q5bw6z6w5.cloudfront.net"); err == nil {
		t.Fatal("expected non-https download base url to be rejected")
	}
}

func signedHeaders(t *testing.T, raw string) string {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse presigned url: %v", err)
	}
	return parsed.Query().Get("X-Amz-SignedHeaders")
}
