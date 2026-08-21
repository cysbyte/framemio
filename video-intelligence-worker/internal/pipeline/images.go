package pipeline

import (
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"math/bits"
	"os"
	"sort"

	"github.com/framemio/video-intelligence-worker/internal/domain"
)

type AnalyzedFrame struct {
	Candidate domain.FrameCandidate
	Frame     domain.SelectedFrame
}

func InspectFrame(path string) (domain.SelectedFrame, error) {
	file, err := os.Open(path)
	if err != nil {
		return domain.SelectedFrame{}, err
	}
	defer file.Close()
	imageValue, _, err := image.Decode(file)
	if err != nil {
		return domain.SelectedFrame{}, fmt.Errorf("decode analysis frame: %w", err)
	}
	bounds := imageValue.Bounds()
	if bounds.Dx() < 2 || bounds.Dy() < 2 {
		return domain.SelectedFrame{}, fmt.Errorf("analysis frame is too small")
	}
	brightness, contrast, sharpness := imageSignals(imageValue)
	quality := clamp(exposureScore(brightness)*0.35+clamp(contrast*2.4, 0, 1)*0.25+clamp(sharpness*4, 0, 1)*0.4, 0, 1)
	return domain.SelectedFrame{
		PerceptualHash: differenceHash(imageValue), Brightness: brightness, Contrast: contrast,
		Sharpness: sharpness, QualityScore: quality, LocalPath: path,
	}, nil
}

func SelectDistinctFrames(frames []AnalyzedFrame, maximum, duplicateDistance int) []domain.SelectedFrame {
	if maximum <= 0 || len(frames) == 0 {
		return nil
	}
	if duplicateDistance < 0 {
		duplicateDistance = 0
	}
	ranked := append([]AnalyzedFrame(nil), frames...)
	sort.SliceStable(ranked, func(left, right int) bool {
		leftScore := ranked[left].Frame.QualityScore + ranked[left].Candidate.Priority*0.12
		rightScore := ranked[right].Frame.QualityScore + ranked[right].Candidate.Priority*0.12
		if leftScore == rightScore {
			return ranked[left].Candidate.TimestampMicros < ranked[right].Candidate.TimestampMicros
		}
		return leftScore > rightScore
	})
	selected := make([]domain.SelectedFrame, 0, min(maximum, len(ranked)))
	selectedScenes := make(map[string]struct{})
	for _, analyzed := range ranked {
		duplicate := false
		for _, existing := range selected {
			temporallyNear := absolute(analyzed.Candidate.TimestampMicros-existing.TimestampMicros) <= 30_000_000
			sameScene := analyzed.Candidate.SceneID == existing.SceneID
			if (temporallyNear || sameScene) && bits.OnesCount64(analyzed.Frame.PerceptualHash^existing.PerceptualHash) <= duplicateDistance {
				duplicate = true
				break
			}
		}
		if duplicate {
			continue
		}
		frame := analyzed.Frame
		frame.ID = fmt.Sprintf("frame-%04d", len(selected)+1)
		frame.SceneID = analyzed.Candidate.SceneID
		frame.SegmentID = analyzed.Candidate.SegmentID
		frame.TimestampMicros = analyzed.Candidate.TimestampMicros
		frame.SelectionReason = analyzed.Candidate.Role
		selected = append(selected, frame)
		selectedScenes[frame.SceneID] = struct{}{}
		if len(selected) == maximum {
			break
		}
	}
	if len(selected) < maximum {
		for _, analyzed := range ranked {
			if _, exists := selectedScenes[analyzed.Candidate.SceneID]; exists {
				continue
			}
			frame := analyzed.Frame
			frame.ID = fmt.Sprintf("frame-%04d", len(selected)+1)
			frame.SceneID = analyzed.Candidate.SceneID
			frame.SegmentID = analyzed.Candidate.SegmentID
			frame.TimestampMicros = analyzed.Candidate.TimestampMicros
			frame.SelectionReason = analyzed.Candidate.Role + ":scene-preserved"
			selected = append(selected, frame)
			selectedScenes[frame.SceneID] = struct{}{}
			if len(selected) == maximum {
				break
			}
		}
	}
	sort.Slice(selected, func(left, right int) bool { return selected[left].TimestampMicros < selected[right].TimestampMicros })
	for index := range selected {
		selected[index].ID = fmt.Sprintf("frame-%04d", index+1)
	}
	return selected
}

func differenceHash(source image.Image) uint64 {
	bounds := source.Bounds()
	var hash uint64
	bit := uint(0)
	for row := 0; row < 8; row++ {
		leftY := bounds.Min.Y + row*(bounds.Dy()-1)/7
		for column := 0; column < 8; column++ {
			leftX := bounds.Min.X + column*(bounds.Dx()-1)/8
			rightX := bounds.Min.X + (column+1)*(bounds.Dx()-1)/8
			if luminance(source.At(leftX, leftY)) > luminance(source.At(rightX, leftY)) {
				hash |= 1 << bit
			}
			bit++
		}
	}
	return hash
}

func imageSignals(source image.Image) (brightness, contrast, sharpness float64) {
	bounds := source.Bounds()
	stepX := max(1, bounds.Dx()/160)
	stepY := max(1, bounds.Dy()/160)
	values := make([]float64, 0, (bounds.Dx()/stepX+1)*(bounds.Dy()/stepY+1))
	var gradientTotal float64
	var gradientCount int
	for y := bounds.Min.Y; y < bounds.Max.Y; y += stepY {
		for x := bounds.Min.X; x < bounds.Max.X; x += stepX {
			value := luminance(source.At(x, y))
			values = append(values, value)
			if x+stepX < bounds.Max.X {
				gradientTotal += math.Abs(value - luminance(source.At(x+stepX, y)))
				gradientCount++
			}
			if y+stepY < bounds.Max.Y {
				gradientTotal += math.Abs(value - luminance(source.At(x, y+stepY)))
				gradientCount++
			}
		}
	}
	for _, value := range values {
		brightness += value
	}
	brightness /= float64(len(values))
	for _, value := range values {
		difference := value - brightness
		contrast += difference * difference
	}
	contrast = math.Sqrt(contrast / float64(len(values)))
	if gradientCount > 0 {
		sharpness = gradientTotal / float64(gradientCount)
	}
	return brightness, contrast, sharpness
}

func luminance(colorValue interface{ RGBA() (r, g, b, a uint32) }) float64 {
	r, g, b, _ := colorValue.RGBA()
	return (0.2126*float64(r) + 0.7152*float64(g) + 0.0722*float64(b)) / 65535.0
}

func exposureScore(brightness float64) float64 {
	return clamp(1-math.Abs(brightness-0.5)*2, 0, 1)
}

func absolute(value int64) int64 {
	if value < 0 {
		return -value
	}
	return value
}
