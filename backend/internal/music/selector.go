package music

import (
	"context"
	"hash/fnv"
	"math"
	"sort"
	"strings"

	"github.com/framemio/backend/internal/models"
	"github.com/framemio/backend/internal/store"
)

type Selector struct {
	Store                  store.Store
	RequireVerifiedLicense bool
	DefaultVolume          float64
	DuckedVolume           float64
	FadeInMS               int
	FadeOutMS              int
}

type SelectionRequest struct {
	ProjectID       string
	StoryPlanID     string
	Mood            string
	VisualStyle     models.VisualStyle
	DurationSeconds float64
	HasSpeech       bool
	Mode            string
	SelectedTrackID string
	PreviousTrackID string
}

func (s Selector) Select(ctx context.Context, request SelectionRequest) (*models.StoryAudio, error) {
	mode := strings.ToLower(strings.TrimSpace(request.Mode))
	if mode == "" || mode == "auto" {
		mode = "auto"
	}
	if mode == "none" {
		original := true
		ducking := false
		return &models.StoryAudio{Source: "original", Mode: "none", Mood: request.Mood, OriginalAudio: &original, Ducking: &ducking, OriginalVolume: 1}, nil
	}
	if s.Store == nil {
		return nil, nil
	}
	filter := models.MusicTrackFilter{RequireApproved: true, RequireVerifiedLicense: s.RequireVerifiedLicense, MinDurationSeconds: math.Min(request.DurationSeconds, 30)}
	tracks, err := s.Store.ListMusicTracks(ctx, filter)
	if err != nil {
		return nil, err
	}
	if len(tracks) == 0 {
		return nil, nil
	}
	var selected *models.MusicTrack
	if mode == "selected" && strings.TrimSpace(request.SelectedTrackID) != "" {
		for i := range tracks {
			if tracks[i].ID == strings.TrimSpace(request.SelectedTrackID) {
				selected = &tracks[i]
				break
			}
		}
	}
	if selected == nil {
		selected = bestTrack(tracks, request)
	}
	if selected == nil || selected.StorageKey == "" {
		return nil, nil
	}
	original := true
	ducking := request.HasSpeech
	volume := s.DefaultVolume
	if volume <= 0 {
		volume = 0.16
	}
	ducked := s.DuckedVolume
	if ducked <= 0 {
		ducked = 0.04
	}
	fadeIn := s.FadeInMS
	if fadeIn <= 0 {
		fadeIn = 900
	}
	fadeOut := s.FadeOutMS
	if fadeOut <= 0 {
		fadeOut = 1400
	}
	return &models.StoryAudio{
		Source:             "licensed",
		Mode:               mode,
		Mood:               request.Mood,
		TrackID:            selected.ID,
		BackgroundS3Key:    selected.StorageKey,
		OriginalAudio:      &original,
		BackgroundVolume:   volume,
		OriginalVolume:     1,
		Ducking:            &ducking,
		DuckedVolume:       ducked,
		StartOffsetSeconds: deterministicOffset(request.ProjectID+request.StoryPlanID+selected.ID, selected.DurationSeconds, request.DurationSeconds),
		FadeInMS:           fadeIn,
		FadeOutMS:          fadeOut,
	}, nil
}

func bestTrack(tracks []models.MusicTrack, request SelectionRequest) *models.MusicTrack {
	sort.SliceStable(tracks, func(i, j int) bool {
		left := musicScore(tracks[i], request)
		right := musicScore(tracks[j], request)
		if left == right {
			return tracks[i].ID < tracks[j].ID
		}
		return left > right
	})
	return &tracks[0]
}

func musicScore(track models.MusicTrack, request SelectionRequest) float64 {
	score := 0.0
	mood := strings.ToLower(strings.TrimSpace(request.Mood))
	style := strings.ToLower(strings.TrimSpace(string(request.VisualStyle)))
	for _, value := range track.Moods {
		v := strings.ToLower(strings.TrimSpace(value))
		if v == mood {
			score += 4
		}
		if v == style {
			score += 2
		}
	}
	if request.HasSpeech {
		if track.SpeechFriendly {
			score += 3
		} else {
			score -= 2
		}
	}
	if track.Instrumental {
		score += 1
	}
	idealEnergy := 0.5
	if request.HasSpeech {
		idealEnergy = 0.35
	}
	score += 1 - math.Abs(track.Energy-idealEnergy)
	if request.DurationSeconds > 0 && track.DurationSeconds >= request.DurationSeconds {
		score += 1
	}
	return score
}

func deterministicOffset(seed string, trackDuration, storyDuration float64) float64 {
	if trackDuration <= storyDuration+1 {
		return 0
	}
	maxOffset := trackDuration - storyDuration - 1
	h := fnv.New32a()
	_, _ = h.Write([]byte(seed))
	return math.Round((float64(h.Sum32()%10000)/10000)*maxOffset*100) / 100
}
