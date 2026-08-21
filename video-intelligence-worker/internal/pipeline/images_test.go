package pipeline

import (
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"

	"github.com/framemio/video-intelligence-worker/internal/domain"
)

func TestSelectDistinctFramesRemovesNearDuplicates(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	paths := []string{
		writeSolidJPEG(t, directory, "first.jpg", color.RGBA{R: 180, G: 120, B: 90, A: 255}),
		writeSolidJPEG(t, directory, "duplicate.jpg", color.RGBA{R: 180, G: 120, B: 90, A: 255}),
		writeSplitJPEG(t, directory, "different.jpg"),
	}
	analyzed := make([]AnalyzedFrame, 0, len(paths))
	for index, path := range paths {
		frame, err := InspectFrame(path)
		if err != nil {
			t.Fatal(err)
		}
		analyzed = append(analyzed, AnalyzedFrame{
			Candidate: domain.FrameCandidate{
				ID: "candidate", SceneID: "scene-0001", SegmentID: "segment-0001",
				TimestampMicros: int64(index+1) * 1_000_000, Role: "segment-midpoint", Priority: 1,
			},
			Frame: frame,
		})
	}
	selected := SelectDistinctFrames(analyzed, 10, 0)
	if len(selected) != 2 {
		t.Fatalf("selected frame count = %d, want 2", len(selected))
	}
	if selected[0].TimestampMicros >= selected[1].TimestampMicros {
		t.Fatalf("selected frames lost chronological ordering: %+v", selected)
	}
}

func writeSplitJPEG(t *testing.T, directory, name string) string {
	t.Helper()
	value := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			fill := color.RGBA{R: 24, G: 42, B: 58, A: 255}
			if x < 32 {
				fill = color.RGBA{R: 230, G: 212, B: 175, A: 255}
			}
			value.Set(x, y, fill)
		}
	}
	return writeJPEG(t, directory, name, value)
}

func writeSolidJPEG(t *testing.T, directory, name string, fill color.Color) string {
	t.Helper()
	value := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			value.Set(x, y, fill)
		}
	}
	return writeJPEG(t, directory, name, value)
}

func writeJPEG(t *testing.T, directory, name string, value image.Image) string {
	t.Helper()
	path := filepath.Join(directory, name)
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := jpeg.Encode(file, value, &jpeg.Options{Quality: 90}); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}
