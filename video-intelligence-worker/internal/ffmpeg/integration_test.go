package ffmpeg

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestToolchainProcessesRepresentativeFixtures(t *testing.T) {
	ffmpegPath, ffmpegErr := exec.LookPath("ffmpeg")
	ffprobePath, ffprobeErr := exec.LookPath("ffprobe")
	if ffmpegErr != nil || ffprobeErr != nil {
		t.Skip("ffmpeg and ffprobe are required for the real-video integration test")
	}
	fixtureDirectory := os.Getenv("FRAME_MIO_VIDEO_FIXTURE_DIR")
	if fixtureDirectory == "" {
		fixtureDirectory = filepath.Join("..", "..", "testdata", "videos")
	}
	entries, err := os.ReadDir(fixtureDirectory)
	if err != nil {
		if os.IsNotExist(err) {
			t.Skip("generate fixtures with scripts/generate-test-videos.sh or set FRAME_MIO_VIDEO_FIXTURE_DIR")
		}
		t.Fatal(err)
	}
	var fixtures []string
	for _, entry := range entries {
		if entry.IsDir() || !isVideoFixture(entry.Name()) {
			continue
		}
		fixtures = append(fixtures, filepath.Join(fixtureDirectory, entry.Name()))
	}
	if len(fixtures) == 0 {
		t.Skip("no representative video fixtures found")
	}
	toolchain := Toolchain{
		FFmpegPath: ffmpegPath, FFprobePath: ffprobePath, FrameWidth: 640, Runner: CommandRunner{},
	}
	for _, fixture := range fixtures {
		fixture := fixture
		t.Run(filepath.Base(fixture), func(t *testing.T) {
			metadata, err := toolchain.Probe(context.Background(), fixture)
			if err != nil {
				t.Fatal(err)
			}
			if metadata.DurationMicros <= 0 || metadata.Width <= 0 || metadata.Height <= 0 || metadata.FPS <= 0 {
				t.Fatalf("incomplete metadata: %+v", metadata)
			}
			boundaries, err := toolchain.DetectScenes(context.Background(), fixture, 0.20, 32)
			if err != nil {
				t.Fatal(err)
			}
			for index := 1; index < len(boundaries); index++ {
				if boundaries[index].TimestampMicros <= boundaries[index-1].TimestampMicros {
					t.Fatalf("scene boundaries are not chronological: %+v", boundaries)
				}
			}
			framePath := filepath.Join(t.TempDir(), "representative.jpg")
			if err := toolchain.ExtractFrame(context.Background(), fixture, metadata.DurationMicros/2, framePath); err != nil {
				t.Fatal(err)
			}
			assertNonEmptyFile(t, framePath)
			if metadata.HasAudio {
				audioPath := filepath.Join(t.TempDir(), "speech.flac")
				if err := toolchain.ExtractAudio(context.Background(), fixture, audioPath); err != nil {
					t.Fatal(err)
				}
				assertNonEmptyFile(t, audioPath)
			}
		})
	}
}

func isVideoFixture(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".mov", ".mp4", ".m4v":
		return true
	default:
		return false
	}
}

func assertNonEmptyFile(t *testing.T, path string) {
	t.Helper()
	information, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if information.Size() == 0 {
		t.Fatalf("generated file is empty: %s", path)
	}
}
