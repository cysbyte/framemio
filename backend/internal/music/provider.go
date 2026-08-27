package music

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/framemio/backend/internal/models"
	"github.com/framemio/backend/internal/storage"
	"github.com/framemio/backend/internal/store"
)

const (
	ProviderLocal             = "local"
	ProviderElevenLabs        = "elevenlabs"
	DefaultElevenModel        = "music_v2"
	DefaultElevenOutputFormat = "auto"
)

type Provider interface {
	Generate(ctx context.Context, request GenerateRequest) (GenerateResult, error)
}

type GenerateRequest struct {
	Category        string
	Title           string
	Prompt          string
	DurationSeconds int
	Instrumental    bool
	Metadata        TrackMetadata
}

type TrackMetadata struct {
	Moods          []string
	Energy         float64
	Genre          string
	BPM            int
	SpeechFriendly bool
}

type GenerateResult struct {
	AudioBytes           []byte
	ContentType          string
	FileExtension        string
	Provider             string
	ProviderGenerationID string
	Metadata             map[string]interface{}
}

type LocalProvider struct{}

func (LocalProvider) Generate(_ context.Context, request GenerateRequest) (GenerateResult, error) {
	duration := request.DurationSeconds
	if duration <= 0 || duration > 6 {
		duration = 3
	}
	return GenerateResult{
		AudioBytes:           deterministicWAV(request.Prompt, duration),
		ContentType:          "audio/wav",
		FileExtension:        "wav",
		Provider:             ProviderLocal,
		ProviderGenerationID: stableID("local-music", request.Prompt),
		Metadata: map[string]interface{}{
			"deterministic": true,
			"category":      request.Category,
		},
	}, nil
}

type ElevenProvider struct {
	APIKey       string
	Model        string
	OutputFormat string
	BaseURL      string
	HTTPClient   *http.Client
}

func (p ElevenProvider) Generate(ctx context.Context, request GenerateRequest) (GenerateResult, error) {
	apiKey := strings.TrimSpace(p.APIKey)
	if apiKey == "" {
		return GenerateResult{}, errors.New("elevenlabs api key is required")
	}
	baseURL := strings.TrimRight(strings.TrimSpace(p.BaseURL), "/")
	if baseURL == "" {
		baseURL = "https://api.elevenlabs.io"
	}
	model := strings.TrimSpace(p.Model)
	if model == "" {
		model = DefaultElevenModel
	}
	outputFormat := strings.TrimSpace(p.OutputFormat)
	if outputFormat == "" {
		outputFormat = DefaultElevenOutputFormat
	}
	durationMS := request.DurationSeconds * 1000
	if durationMS <= 0 {
		durationMS = 30000
	}
	body := map[string]interface{}{
		"prompt":             request.Prompt,
		"music_length_ms":    durationMS,
		"model_id":           model,
		"force_instrumental": request.Instrumental,
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return GenerateResult{}, err
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/v1/music?output_format="+url.QueryEscape(outputFormat), bytes.NewReader(payload))
	if err != nil {
		return GenerateResult{}, err
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Accept", "audio/mpeg")
	httpRequest.Header.Set("xi-api-key", apiKey)
	client := p.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 3 * time.Minute}
	}
	response, err := client.Do(httpRequest)
	if err != nil {
		return GenerateResult{}, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		limited, _ := io.ReadAll(io.LimitReader(response.Body, 512))
		return GenerateResult{}, fmt.Errorf("eleven music generation failed: status=%d body=%q", response.StatusCode, strings.TrimSpace(string(limited)))
	}
	audio, err := io.ReadAll(io.LimitReader(response.Body, 64<<20))
	if err != nil {
		return GenerateResult{}, err
	}
	if len(audio) == 0 {
		return GenerateResult{}, errors.New("eleven music generation returned empty audio")
	}
	generationID := firstNonEmpty(response.Header.Get("song-id"), response.Header.Get("request-id"))
	if generationID == "" {
		generationID = stableID("eleven-music", request.Prompt, string(audio[:min(len(audio), 64)]))
	}
	return GenerateResult{
		AudioBytes:           audio,
		ContentType:          firstNonEmpty(response.Header.Get("Content-Type"), "audio/mpeg"),
		FileExtension:        "mp3",
		Provider:             ProviderElevenLabs,
		ProviderGenerationID: generationID,
		Metadata: map[string]interface{}{
			"model":        model,
			"outputFormat": outputFormat,
			"category":     request.Category,
			"durationMs":   durationMS,
		},
	}, nil
}

type PromptTemplate struct {
	Category        string
	Title           string
	Prompt          string
	Metadata        TrackMetadata
	DurationSeconds int
}

func DefaultPromptTemplates() []PromptTemplate {
	return []PromptTemplate{
		{Category: "warm", Title: "Golden Memory", Prompt: "Warm nostalgic instrumental background music for a short personal memory video. Soft acoustic guitar and subtle piano, intimate and natural, gentle emotional progression, uplifting but slightly nostalgic, no vocals, no spoken words, no dramatic intro, suitable underneath occasional spoken dialogue.", Metadata: TrackMetadata{Moods: []string{"warm", "nostalgic"}, Energy: 0.42, Genre: "acoustic", BPM: 82, SpeechFriendly: true}, DurationSeconds: 90},
		{Category: "happy", Title: "Small Joys", Prompt: "Happy instrumental background music for everyday family videos, light acoustic rhythm, gentle piano, optimistic and clean, no vocals, no spoken words, suitable under laughter and dialogue.", Metadata: TrackMetadata{Moods: []string{"happy", "playful"}, Energy: 0.58, Genre: "acoustic", BPM: 104, SpeechFriendly: true}, DurationSeconds: 90},
		{Category: "cinematic", Title: "Soft Cinema", Prompt: "Cinematic instrumental background music for a personal memory film, soft piano, warm pads, restrained emotional lift, no vocals, no spoken words, no aggressive drums, clean ending.", Metadata: TrackMetadata{Moods: []string{"cinematic", "emotional"}, Energy: 0.5, Genre: "cinematic", BPM: 76, SpeechFriendly: true}, DurationSeconds: 120},
		{Category: "calm", Title: "Quiet Light", Prompt: "Calm instrumental music for iPhone memory videos, gentle piano and ambient texture, peaceful and unobtrusive, no vocals, no spoken words, low distraction under dialogue.", Metadata: TrackMetadata{Moods: []string{"calm", "warm"}, Energy: 0.28, Genre: "ambient", BPM: 68, SpeechFriendly: true}, DurationSeconds: 90},
		{Category: "playful", Title: "Little Adventure", Prompt: "Playful instrumental background music for short everyday-life videos, light percussion, ukulele-like plucks, cheerful but not childish, no vocals, no spoken words, clean intro and ending.", Metadata: TrackMetadata{Moods: []string{"playful", "happy"}, Energy: 0.66, Genre: "indie", BPM: 112, SpeechFriendly: false}, DurationSeconds: 90},
		{Category: "travel", Title: "Open Roads", Prompt: "Instrumental travel background music for personal trip videos, warm guitar, subtle percussion, sense of movement and discovery, no vocals, no spoken words, suitable for scenic footage.", Metadata: TrackMetadata{Moods: []string{"travel", "warm"}, Energy: 0.6, Genre: "acoustic", BPM: 96, SpeechFriendly: true}, DurationSeconds: 120},
		{Category: "emotional", Title: "Held Close", Prompt: "Emotional instrumental background music for family memory videos, tender piano and strings, sincere and hopeful, no vocals, no spoken words, not overly dramatic, suitable underneath dialogue.", Metadata: TrackMetadata{Moods: []string{"emotional", "nostalgic"}, Energy: 0.38, Genre: "cinematic", BPM: 72, SpeechFriendly: true}, DurationSeconds: 120},
		{Category: "everyday", Title: "Everyday Glow", Prompt: "Instrumental background music for everyday iPhone moments, soft beat, warm keys, natural and friendly, no vocals, no spoken words, low distraction and suitable for memory videos.", Metadata: TrackMetadata{Moods: []string{"everyday", "warm"}, Energy: 0.48, Genre: "lofi", BPM: 88, SpeechFriendly: true}, DurationSeconds: 90},
	}
}

type GenerationService struct {
	Store         store.Store
	ObjectStorage storage.ObjectStorage
	Provider      Provider
	Now           func() time.Time
}

type GenerateTrackRequest struct {
	Category        string
	Title           string
	Prompt          string
	DurationSeconds int
	Instrumental    bool
	Metadata        TrackMetadata
}

func (s GenerationService) GenerateTrack(ctx context.Context, request GenerateTrackRequest) (models.MusicTrack, error) {
	if s.Store == nil || s.ObjectStorage == nil || s.Provider == nil {
		return models.MusicTrack{}, errors.New("music generation service is not configured")
	}
	template, err := s.resolveTemplate(request)
	if err != nil {
		return models.MusicTrack{}, err
	}
	result, err := s.Provider.Generate(ctx, GenerateRequest{
		Category:        template.Category,
		Title:           template.Title,
		Prompt:          template.Prompt,
		DurationSeconds: template.DurationSeconds,
		Instrumental:    true,
		Metadata:        template.Metadata,
	})
	if err != nil {
		return models.MusicTrack{}, err
	}
	if len(result.AudioBytes) == 0 {
		return models.MusicTrack{}, errors.New("music provider returned no audio")
	}
	now := s.now()
	trackID := models.NewID("music")
	ext := strings.Trim(strings.ToLower(result.FileExtension), ".")
	if ext == "" {
		ext = "mp3"
	}
	storageKey := fmt.Sprintf("music/%s/master.%s", trackID, ext)
	if err := s.ObjectStorage.PutObject(ctx, storageKey, result.ContentType, result.AudioBytes); err != nil {
		return models.MusicTrack{}, err
	}
	track := models.MusicTrack{
		ID:                   trackID,
		Title:                template.Title,
		Status:               models.MusicTrackStatusReview,
		DurationSeconds:      float64(template.DurationSeconds),
		BPM:                  template.Metadata.BPM,
		Moods:                cleanStrings(template.Metadata.Moods),
		Energy:               clamp(template.Metadata.Energy, 0, 1),
		Genre:                strings.TrimSpace(template.Metadata.Genre),
		Instrumental:         true,
		SpeechFriendly:       template.Metadata.SpeechFriendly,
		StorageKey:           storageKey,
		PreviewStorageKey:    storageKey,
		Provider:             result.Provider,
		ProviderGenerationID: result.ProviderGenerationID,
		GenerationMetadata:   result.Metadata,
		LicenseMetadata:      models.MusicLicenseMetadata{Provider: result.Provider, LicenseStatus: models.MusicLicenseStatusPending, Notes: "Commercial distribution rights must be verified before production release."},
		CreatedAt:            now,
		UpdatedAt:            now,
	}
	if err := s.Store.SaveMusicTrack(ctx, track); err != nil {
		return models.MusicTrack{}, err
	}
	return track, nil
}

func (s GenerationService) resolveTemplate(request GenerateTrackRequest) (PromptTemplate, error) {
	category := strings.ToLower(strings.TrimSpace(request.Category))
	if category != "" && request.Prompt == "" && request.Title == "" {
		for _, template := range DefaultPromptTemplates() {
			if template.Category == category {
				return template, nil
			}
		}
		return PromptTemplate{}, fmt.Errorf("unknown music category: %s", category)
	}
	title := strings.TrimSpace(request.Title)
	prompt := strings.TrimSpace(request.Prompt)
	if title == "" || prompt == "" {
		return PromptTemplate{}, errors.New("music title and prompt are required")
	}
	duration := request.DurationSeconds
	if duration <= 0 {
		duration = 90
	}
	return PromptTemplate{Category: category, Title: title, Prompt: prompt, Metadata: request.Metadata, DurationSeconds: duration}, nil
}

func (s GenerationService) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func deterministicWAV(seed string, durationSeconds int) []byte {
	const sampleRate = 48000
	const channels = 2
	const bitsPerSample = 16
	samples := sampleRate * durationSeconds
	dataSize := samples * channels * bitsPerSample / 8
	buffer := bytes.NewBuffer(make([]byte, 0, 44+dataSize))
	buffer.WriteString("RIFF")
	_ = binary.Write(buffer, binary.LittleEndian, uint32(36+dataSize))
	buffer.WriteString("WAVEfmt ")
	_ = binary.Write(buffer, binary.LittleEndian, uint32(16))
	_ = binary.Write(buffer, binary.LittleEndian, uint16(1))
	_ = binary.Write(buffer, binary.LittleEndian, uint16(channels))
	_ = binary.Write(buffer, binary.LittleEndian, uint32(sampleRate))
	_ = binary.Write(buffer, binary.LittleEndian, uint32(sampleRate*channels*bitsPerSample/8))
	_ = binary.Write(buffer, binary.LittleEndian, uint16(channels*bitsPerSample/8))
	_ = binary.Write(buffer, binary.LittleEndian, uint16(bitsPerSample))
	buffer.WriteString("data")
	_ = binary.Write(buffer, binary.LittleEndian, uint32(dataSize))
	freq := 220.0 + float64(int(sha256.Sum256([]byte(seed))[0])%160)
	for i := 0; i < samples; i++ {
		value := int16(math.Sin(2*math.Pi*freq*float64(i)/sampleRate) * 1800)
		_ = binary.Write(buffer, binary.LittleEndian, value)
		_ = binary.Write(buffer, binary.LittleEndian, value)
	}
	return buffer.Bytes()
}

func stableID(prefix string, values ...string) string {
	h := sha256.New()
	for _, value := range values {
		_, _ = h.Write([]byte(value))
		_, _ = h.Write([]byte{0})
	}
	return prefix + "_" + hex.EncodeToString(h.Sum(nil))[:16]
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func cleanStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	cleaned := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		cleaned = append(cleaned, value)
	}
	return cleaned
}

func clamp(value, minValue, maxValue float64) float64 {
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}
