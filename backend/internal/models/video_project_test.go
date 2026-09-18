package models

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestVideoProjectSerializationRoundTripsModeConfiguration(t *testing.T) {
	t.Parallel()

	original := VideoProject{
		ID:       "project-promo",
		UserID:   "user-1",
		Type:     ProjectTypePromo,
		Title:    "Launch video",
		Context:  "Promote the new app",
		MediaIDs: []string{"media-1", "media-2"},
		Configuration: VideoProjectConfiguration{
			AIInstructions: "Lead with the product benefit.",
			Promo: &PromoProjectConfiguration{
				PromotionGoal:       "app_install",
				ProductInformation:  "FrameMio mobile app",
				CallToAction:        "Try FrameMio today",
				Hook:                "Turn moments into films",
				TargetAudience:      "new creators",
				DesiredDurationSecs: 15,
				Brand: BrandConfiguration{
					Name:         "FrameMio",
					PrimaryColor: "#FF3B30",
					LogoAssetID:  "logo-1",
				},
			},
			Composition: CompositionConfiguration{AspectRatio: "16:9", FPS: 30, Preset: "product-demo"},
			Render:      RenderConfiguration{OutputPreset: "social", Quality: "high"},
		},
		Status:    ProjectStatusDraft,
		CreatedAt: time.Date(2026, time.September, 10, 10, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, time.September, 10, 10, 1, 0, 0, time.UTC),
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	var decoded VideoProject
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Type != ProjectTypePromo || decoded.Configuration.Promo == nil {
		t.Fatalf("mode configuration was not preserved: %+v", decoded)
	}
	if decoded.Configuration.Promo.CallToAction != "Try FrameMio today" || decoded.Configuration.Composition.AspectRatio != "16:9" {
		t.Fatalf("configuration values were not preserved: %+v", decoded.Configuration)
	}
	if decoded.ID != original.ID || decoded.Title != original.Title || len(decoded.MediaIDs) != 2 {
		t.Fatalf("project fields were not preserved: %+v", decoded)
	}
}

func TestVideoProjectDeserializationDefaultsAndNormalizesType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		json string
		want ProjectType
	}{
		{name: "missing legacy type", json: `{"id":"legacy","user_id":"user-1","title":"Old story","media_ids":[]}`, want: ProjectTypeStory},
		{name: "blank legacy type", json: `{"id":"legacy","type":"  ","media_ids":[]}`, want: ProjectTypeStory},
		{name: "case and whitespace", json: `{"id":"promo","type":" PROMO ","media_ids":[]}`, want: ProjectTypePromo},
		{name: "unsupported value", json: `{"id":"unknown","type":"commercial","media_ids":[]}`, want: ProjectTypeStory},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var project VideoProject
			if err := json.Unmarshal([]byte(test.json), &project); err != nil {
				t.Fatal(err)
			}
			if project.Type != test.want {
				t.Fatalf("type = %q, want %q", project.Type, test.want)
			}
		})
	}

	data, err := json.Marshal(VideoProject{ID: "legacy"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"type":"story"`) {
		t.Fatalf("legacy project serialization omitted normalized type: %s", data)
	}
}

func TestReelProjectConfigurationDefaultsAndNormalizes(t *testing.T) {
	t.Parallel()

	project := VideoProject{
		ID:     "project-reel",
		UserID: "user-1",
		Type:   ProjectTypeReel,
		Configuration: VideoProjectConfiguration{
			Reel:        &ReelProjectConfiguration{SocialPlatform: "YouTube Shorts", TargetDurationSecs: 45, Style: "talking head", CaptionStyle: "unknown", AspectRatio: "16:9"},
			Composition: CompositionConfiguration{AspectRatio: "1:1"},
		},
	}.Normalized()

	if project.Configuration.Reel == nil {
		t.Fatal("reel configuration was not defaulted")
	}
	if project.Configuration.Reel.SocialPlatform != "youtube_shorts" {
		t.Fatalf("platform = %q, want youtube_shorts", project.Configuration.Reel.SocialPlatform)
	}
	if project.Configuration.Reel.TargetDurationSecs != 0 {
		t.Fatalf("duration = %d, want auto", project.Configuration.Reel.TargetDurationSecs)
	}
	if project.Configuration.Reel.Style != "talking-head" {
		t.Fatalf("style = %q, want talking-head", project.Configuration.Reel.Style)
	}
	if project.Configuration.Reel.CaptionStyle != "bold_social" {
		t.Fatalf("caption style = %q, want bold_social", project.Configuration.Reel.CaptionStyle)
	}
	if project.Configuration.Reel.AspectRatio != "9:16" || project.Configuration.Composition.AspectRatio != "9:16" {
		t.Fatalf("aspect ratios = reel %q composition %q, want 9:16", project.Configuration.Reel.AspectRatio, project.Configuration.Composition.AspectRatio)
	}
}
