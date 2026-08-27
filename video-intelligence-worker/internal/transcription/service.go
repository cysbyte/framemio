package transcription

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/framemio/video-intelligence-worker/internal/domain"
)

const timestampToleranceMicros int64 = 250_000

type MediaReference struct {
	URL            string `json:"url"`
	MediaType      string `json:"media_type"`
	DurationMicros int64  `json:"duration_micros"`
}

type ProviderRequest struct {
	MediaID      string         `json:"media_id"`
	Reference    MediaReference `json:"reference"`
	LanguageHint string         `json:"language_hint,omitempty"`
}

type ProviderResult struct {
	Transcript  domain.Transcript
	RawArtifact json.RawMessage
}

type TranscriptionProvider interface {
	Transcribe(ctx context.Context, request ProviderRequest) (ProviderResult, error)
	Name() string
	Model() string
}

type Repository interface {
	GetTranscript(ctx context.Context, userID, mediaID, version, sourceETag string) (domain.TranscriptionRecord, error)
	SaveTranscript(ctx context.Context, transcript domain.TranscriptionRecord) (domain.TranscriptionRecord, error)
}

type ArtifactStore interface {
	PresignGet(ctx context.Context, key string, expiry time.Duration) (string, error)
	PutJSON(ctx context.Context, key string, value any) error
}

type Request struct {
	UserID         string
	ProjectID      string
	MediaID        string
	SourceETag     string
	SourceS3Key    string
	MediaType      string
	DurationMicros int64
	Version        string
	LanguageHint   string
}

type Result struct {
	Record domain.TranscriptionRecord
	Reused bool
}

type Transcriber interface {
	Transcribe(ctx context.Context, request Request) (Result, error)
}

type Service struct {
	Provider                  TranscriptionProvider
	Repository                Repository
	Artifacts                 ArtifactStore
	RetryAttempts             int
	RetryDelay                time.Duration
	ReferenceExpiry           time.Duration
	RawArtifactThresholdBytes int
	Now                       func() time.Time
}

func (s *Service) Transcribe(ctx context.Context, request Request) (Result, error) {
	if err := validateRequest(request); err != nil {
		return Result{}, domain.Permanent(err)
	}
	if s.Provider == nil || s.Repository == nil || s.Artifacts == nil {
		return Result{}, domain.Permanent(errors.New("transcription service dependencies are required"))
	}
	if cached, err := s.Repository.GetTranscript(ctx, request.UserID, request.MediaID, request.Version, request.SourceETag); err == nil {
		if validCachedRecord(cached, request) {
			normalized, validationErr := Validate(cached.Transcript, request.DurationMicros)
			if validationErr != nil {
				return Result{}, fmt.Errorf("cached transcript is invalid: %w", validationErr)
			}
			cached.Transcript = normalized
			return Result{Record: cached, Reused: true}, nil
		}
		return Result{}, fmt.Errorf("cached transcript does not match its lookup key")
	} else if !errors.Is(err, domain.ErrNotFound) {
		return Result{}, err
	}
	referenceURL, err := s.Artifacts.PresignGet(ctx, request.SourceS3Key, s.referenceExpiry())
	if err != nil {
		return Result{}, fmt.Errorf("presign transcription source: %w", err)
	}
	providerRequest := ProviderRequest{
		MediaID: request.MediaID,
		Reference: MediaReference{
			URL: referenceURL, MediaType: request.MediaType, DurationMicros: request.DurationMicros,
		},
		LanguageHint: strings.TrimSpace(request.LanguageHint),
	}
	var providerResult ProviderResult
	err = s.retry(ctx, func() error {
		var providerErr error
		providerResult, providerErr = s.Provider.Transcribe(ctx, providerRequest)
		if providerErr != nil {
			return providerErr
		}
		providerResult.Transcript, providerErr = Validate(providerResult.Transcript, request.DurationMicros)
		return providerErr
	})
	if err != nil {
		return Result{}, fmt.Errorf("transcription provider %s failed: %w", s.Provider.Name(), err)
	}
	rawArtifactKey := ""
	if len(providerResult.RawArtifact) >= s.rawArtifactThreshold() {
		if !json.Valid(providerResult.RawArtifact) {
			return Result{}, fmt.Errorf("%w: provider raw artifact is not valid JSON", domain.ErrInvalidTranscript)
		}
		rawArtifactKey = rawArtifactS3Key(request)
		if err := s.retry(ctx, func() error {
			return s.Artifacts.PutJSON(ctx, rawArtifactKey, providerResult.RawArtifact)
		}); err != nil {
			return Result{}, fmt.Errorf("store raw transcription artifact: %w", err)
		}
	}
	now := s.now().UTC()
	record := domain.TranscriptionRecord{
		ID: domain.NewID("transcript"), UserID: request.UserID, ProjectID: request.ProjectID,
		MediaID: request.MediaID, SourceETag: request.SourceETag, Version: request.Version,
		Provider: strings.TrimSpace(s.Provider.Name()), ProviderModel: strings.TrimSpace(s.Provider.Model()),
		Language: providerResult.Transcript.Language, HasSpeech: providerResult.Transcript.HasSpeech,
		SegmentCount: len(providerResult.Transcript.Segments), DurationMicros: request.DurationMicros,
		Transcript: providerResult.Transcript, RawArtifactS3Key: rawArtifactKey, CreatedAt: now, UpdatedAt: now,
	}
	saved, err := s.Repository.SaveTranscript(ctx, record)
	if err != nil {
		return Result{}, err
	}
	return Result{Record: saved, Reused: saved.ID != record.ID}, nil
}

func Validate(transcript domain.Transcript, durationMicros int64) (domain.Transcript, error) {
	if durationMicros <= 0 {
		return domain.Transcript{}, fmt.Errorf("%w: media duration must be positive", domain.ErrInvalidTranscript)
	}
	transcript.Language = strings.TrimSpace(transcript.Language)
	transcript.Text = strings.TrimSpace(transcript.Text)
	for index := range transcript.Segments {
		segment := &transcript.Segments[index]
		segment.Text = strings.TrimSpace(segment.Text)
		if segment.EndMicros > durationMicros && segment.EndMicros <= durationMicros+timestampToleranceMicros {
			segment.EndMicros = durationMicros
		}
		if segment.StartMicros < 0 || segment.EndMicros <= segment.StartMicros || segment.EndMicros > durationMicros || segment.Text == "" {
			return domain.Transcript{}, fmt.Errorf("%w: segment %d has invalid timestamps or text", domain.ErrInvalidTranscript, index)
		}
		if segment.Confidence < 0 || segment.Confidence > 1 {
			return domain.Transcript{}, fmt.Errorf("%w: segment %d confidence is outside zero to one", domain.ErrInvalidTranscript, index)
		}
	}
	sort.SliceStable(transcript.Segments, func(left, right int) bool {
		if transcript.Segments[left].StartMicros == transcript.Segments[right].StartMicros {
			return transcript.Segments[left].EndMicros < transcript.Segments[right].EndMicros
		}
		return transcript.Segments[left].StartMicros < transcript.Segments[right].StartMicros
	})
	if len(transcript.Segments) == 0 {
		transcript.Text = ""
		transcript.HasSpeech = false
		return transcript, nil
	}
	transcript.HasSpeech = true
	if transcript.Text == "" {
		parts := make([]string, 0, len(transcript.Segments))
		for _, segment := range transcript.Segments {
			parts = append(parts, segment.Text)
		}
		transcript.Text = strings.Join(parts, " ")
	}
	return transcript, nil
}

func (s *Service) retry(ctx context.Context, operation func() error) error {
	attempts := s.RetryAttempts
	if attempts <= 0 {
		attempts = 3
	}
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := operation(); err == nil {
			return nil
		} else {
			if domain.IsPermanent(err) {
				return err
			}
			lastErr = err
		}
		if attempt+1 == attempts {
			break
		}
		delay := s.RetryDelay
		if delay <= 0 {
			delay = 250 * time.Millisecond
		}
		delay *= time.Duration(1 << attempt)
		if delay > 5*time.Second {
			delay = 5 * time.Second
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return lastErr
}

func validateRequest(request Request) error {
	if strings.TrimSpace(request.UserID) == "" || strings.TrimSpace(request.ProjectID) == "" || strings.TrimSpace(request.MediaID) == "" {
		return errors.New("transcription request requires user, project, and media identifiers")
	}
	if strings.TrimSpace(request.SourceS3Key) == "" || strings.TrimSpace(request.Version) == "" || request.DurationMicros <= 0 {
		return errors.New("transcription request requires source key, version, and positive duration")
	}
	if request.MediaType != "audio" && request.MediaType != "video" {
		return errors.New("transcription media type must be audio or video")
	}
	return nil
}

func validCachedRecord(record domain.TranscriptionRecord, request Request) bool {
	return record.ID != "" && record.UserID == request.UserID && record.MediaID == request.MediaID &&
		record.SourceETag == request.SourceETag && record.Version == request.Version && record.DurationMicros == request.DurationMicros &&
		record.SegmentCount == len(record.Transcript.Segments) && record.HasSpeech == record.Transcript.HasSpeech
}

func rawArtifactS3Key(request Request) string {
	hash := sha256.Sum256([]byte(request.SourceETag))
	return fmt.Sprintf("users/%s/transcriptions/%s/%s/%s/provider-response.json",
		safeComponent(request.UserID), safeComponent(request.MediaID), safeComponent(request.Version), hex.EncodeToString(hash[:8]))
}

func safeComponent(value string) string {
	value = strings.TrimSpace(value)
	var result strings.Builder
	for _, character := range value {
		if unicode.IsLetter(character) || unicode.IsDigit(character) || character == '-' || character == '_' {
			result.WriteRune(character)
		} else {
			result.WriteByte('_')
		}
	}
	if result.Len() == 0 {
		return "unknown"
	}
	return result.String()
}

func (s *Service) rawArtifactThreshold() int {
	if s.RawArtifactThresholdBytes > 0 {
		return s.RawArtifactThresholdBytes
	}
	return 256 << 10
}

func (s *Service) referenceExpiry() time.Duration {
	if s.ReferenceExpiry > 0 {
		return s.ReferenceExpiry
	}
	return 30 * time.Minute
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}
