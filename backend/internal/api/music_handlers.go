package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/framemio/backend/internal/models"
	"github.com/framemio/backend/internal/music"
)

type publicMusicTrack struct {
	ID              string   `json:"id"`
	Title           string   `json:"title"`
	DurationSeconds float64  `json:"durationSeconds"`
	BPM             int      `json:"bpm,omitempty"`
	Moods           []string `json:"moods,omitempty"`
	Energy          float64  `json:"energy,omitempty"`
	Genre           string   `json:"genre,omitempty"`
	Instrumental    bool     `json:"instrumental"`
	SpeechFriendly  bool     `json:"speechFriendly"`
	PreviewURL      string   `json:"previewUrl,omitempty"`
}

type internalMusicGenerateRequest struct {
	Category        string              `json:"category,omitempty"`
	Title           string              `json:"title,omitempty"`
	Prompt          string              `json:"prompt,omitempty"`
	DurationSeconds int                 `json:"durationSeconds,omitempty"`
	Metadata        music.TrackMetadata `json:"metadata,omitempty"`
}

type internalMusicStatusRequest struct {
	Status        string `json:"status,omitempty"`
	LicenseStatus string `json:"licenseStatus,omitempty"`
	TermsVersion  string `json:"termsVersion,omitempty"`
}

func (s *Server) dispatchMusic(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		s.writeError(response, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	parts := strings.Split(strings.Trim(request.URL.Path, "/"), "/")
	if len(parts) == 2 {
		s.listMusic(response, request)
		return
	}
	if len(parts) == 3 {
		s.getMusic(response, request, parts[2])
		return
	}
	s.writeError(response, http.StatusNotFound, "not_found", "route not found")
}

func (s *Server) listMusic(response http.ResponseWriter, request *http.Request) {
	filter := models.MusicTrackFilter{
		RequireApproved:        true,
		RequireVerifiedLicense: s.RequireVerifiedMusicLicense,
		Mood:                   request.URL.Query().Get("mood"),
		Genre:                  request.URL.Query().Get("genre"),
		SpeechFriendlyOnly:     truthyQuery(request.URL.Query().Get("speechFriendly")),
	}
	tracks, err := s.Store.ListMusicTracks(request.Context(), filter)
	if err != nil {
		s.mapStoreError(response, err)
		return
	}
	output := make([]publicMusicTrack, 0, len(tracks))
	for _, track := range tracks {
		output = append(output, s.publicMusicTrack(request, track))
	}
	s.writeJSON(response, http.StatusOK, map[string]interface{}{"tracks": output})
}

func (s *Server) getMusic(response http.ResponseWriter, request *http.Request, trackID string) {
	track, err := s.Store.GetMusicTrack(request.Context(), strings.TrimSpace(trackID))
	if err != nil {
		s.mapStoreError(response, err)
		return
	}
	if !trackPubliclyAvailable(track, s.RequireVerifiedMusicLicense) {
		s.writeError(response, http.StatusNotFound, "not_found", "music track not found")
		return
	}
	s.writeJSON(response, http.StatusOK, s.publicMusicTrack(request, track))
}

func (s *Server) dispatchInternalMusic(response http.ResponseWriter, request *http.Request) {
	if !s.authenticateInternal(response, request) {
		return
	}
	parts := strings.Split(strings.Trim(request.URL.Path, "/"), "/")
	if len(parts) == 3 && parts[0] == "internal" && parts[1] == "music" && parts[2] == "generate" && request.Method == http.MethodPost {
		s.generateInternalMusic(response, request)
		return
	}
	if len(parts) == 4 && parts[0] == "internal" && parts[1] == "music" && request.Method == http.MethodPost {
		s.updateInternalMusicStatus(response, request, parts[2], parts[3])
		return
	}
	s.writeError(response, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
}

func (s *Server) generateInternalMusic(response http.ResponseWriter, request *http.Request) {
	if s.MusicGenerator == nil {
		s.writeError(response, http.StatusServiceUnavailable, "music_generator_unavailable", "music generation is not configured")
		return
	}
	var input internalMusicGenerateRequest
	if !s.decodeJSON(response, request, &input) {
		return
	}
	track, err := s.MusicGenerator.GenerateTrack(request.Context(), music.GenerateTrackRequest{Category: input.Category, Title: input.Title, Prompt: input.Prompt, DurationSeconds: input.DurationSeconds, Metadata: input.Metadata})
	if err != nil {
		s.Logger.Error("internal music generation failed", "category", input.Category, "title", input.Title, "error", err)
		s.writeError(response, http.StatusBadGateway, "music_generation_failed", "music generation failed")
		return
	}
	s.Logger.Info("internal music track generated", "track_id", track.ID, "provider", track.Provider, "status", string(track.Status), "license_status", string(track.LicenseMetadata.LicenseStatus))
	s.writeJSON(response, http.StatusAccepted, track)
}

func (s *Server) updateInternalMusicStatus(response http.ResponseWriter, request *http.Request, trackID, action string) {
	track, err := s.Store.GetMusicTrack(request.Context(), strings.TrimSpace(trackID))
	if err != nil {
		s.mapStoreError(response, err)
		return
	}
	var input internalMusicStatusRequest
	if request.Body != nil && request.ContentLength != 0 {
		_ = json.NewDecoder(request.Body).Decode(&input)
	}
	nowFunc := s.Now
	if nowFunc == nil {
		nowFunc = time.Now
	}
	now := nowFunc().UTC()
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "approve":
		track.Status = models.MusicTrackStatusApproved
		if status := normalizeMusicLicenseStatus(input.LicenseStatus); status != "" {
			track.LicenseMetadata.LicenseStatus = status
		}
		if strings.TrimSpace(input.TermsVersion) != "" {
			track.LicenseMetadata.TermsVersion = strings.TrimSpace(input.TermsVersion)
		}
		if track.LicenseMetadata.LicenseStatus == models.MusicLicenseStatusVerified {
			track.LicenseMetadata.VerifiedAt = &now
		}
	case "disable":
		track.Status = models.MusicTrackStatusDisabled
	default:
		s.writeError(response, http.StatusNotFound, "not_found", "route not found")
		return
	}
	track.UpdatedAt = now
	if err := s.Store.SaveMusicTrack(request.Context(), track); err != nil {
		s.mapStoreError(response, err)
		return
	}
	s.writeJSON(response, http.StatusOK, track)
}

func (s *Server) publicMusicTrack(request *http.Request, track models.MusicTrack) publicMusicTrack {
	previewURL := ""
	if strings.TrimSpace(track.PreviewStorageKey) != "" && s.ObjectStorage != nil {
		if url, err := s.ObjectStorage.PresignedDownloadURL(request.Context(), track.PreviewStorageKey, 15*time.Minute); err == nil {
			previewURL = url
		}
	}
	return publicMusicTrack{
		ID:              track.ID,
		Title:           track.Title,
		DurationSeconds: track.DurationSeconds,
		BPM:             track.BPM,
		Moods:           append([]string(nil), track.Moods...),
		Energy:          track.Energy,
		Genre:           track.Genre,
		Instrumental:    track.Instrumental,
		SpeechFriendly:  track.SpeechFriendly,
		PreviewURL:      previewURL,
	}
}

func trackPubliclyAvailable(track models.MusicTrack, requireVerified bool) bool {
	if track.Status != models.MusicTrackStatusApproved {
		return false
	}
	if requireVerified && track.LicenseMetadata.LicenseStatus != models.MusicLicenseStatusVerified {
		return false
	}
	return true
}

func normalizeMusicLicenseStatus(value string) models.MusicLicenseStatus {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "verified":
		return models.MusicLicenseStatusVerified
	case "restricted":
		return models.MusicLicenseStatusRestricted
	case "unknown":
		return models.MusicLicenseStatusUnknown
	case "pending":
		return models.MusicLicenseStatusPending
	default:
		return ""
	}
}

func truthyQuery(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
