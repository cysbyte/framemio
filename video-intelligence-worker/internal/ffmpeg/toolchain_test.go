package ffmpeg

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseRepresentativeIPhoneProbeProfiles(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		file         string
		width        int
		height       int
		rotation     int
		minimumFPS   float64
		hasAudio     bool
		durationSecs int64
	}{
		{name: "short portrait HEVC", file: "short-portrait-hevc.ffprobe.json", width: 1920, height: 1080, rotation: 90, minimumFPS: 29, hasAudio: true, durationSecs: 12},
		{name: "high FPS action", file: "high-fps-action.ffprobe.json", width: 1920, height: 1080, minimumFPS: 120, hasAudio: true, durationSecs: 18},
		{name: "long 4K trip", file: "long-trip-4k.ffprobe.json", width: 3840, height: 2160, minimumFPS: 29, hasAudio: true, durationSecs: 600},
		{name: "low quality static", file: "low-quality-static.ffprobe.json", width: 640, height: 480, minimumFPS: 30, hasAudio: false, durationSecs: 45},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "profiles", test.file))
			if err != nil {
				t.Fatal(err)
			}
			metadata, err := ParseProbe(data)
			if err != nil {
				t.Fatal(err)
			}
			if metadata.Width != test.width || metadata.Height != test.height || metadata.Rotation != test.rotation {
				t.Fatalf("unexpected dimensions/rotation: %+v", metadata)
			}
			if metadata.FPS < test.minimumFPS || metadata.HasAudio != test.hasAudio || metadata.DurationMicros/1_000_000 != test.durationSecs {
				t.Fatalf("unexpected temporal/audio metadata: %+v", metadata)
			}
		})
	}
}

func TestParseSceneMetadataRanksAndCapsBoundaries(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "profiles", "high-fps-action.scenes.txt"))
	if err != nil {
		t.Fatal(err)
	}
	boundaries := ParseSceneMetadata(data, 0.32, 4)
	if len(boundaries) != 4 {
		t.Fatalf("boundary count = %d, want 4", len(boundaries))
	}
	for index := 1; index < len(boundaries); index++ {
		if boundaries[index].TimestampMicros <= boundaries[index-1].TimestampMicros {
			t.Fatalf("boundaries are not chronological: %+v", boundaries)
		}
	}
}
