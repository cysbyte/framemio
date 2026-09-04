package ffmpeg

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/framemio/video-intelligence-worker/internal/domain"
)

type Runner interface {
	Run(ctx context.Context, executable string, arguments ...string) (stdout []byte, stderr []byte, err error)
}

type CommandRunner struct{}

func (CommandRunner) Run(ctx context.Context, executable string, arguments ...string) ([]byte, []byte, error) {
	command := exec.CommandContext(ctx, executable, arguments...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	if err != nil {
		return stdout.Bytes(), stderr.Bytes(), fmt.Errorf("%s failed: %w: %s", executable, err, truncate(stderr.String(), 4000))
	}
	return stdout.Bytes(), stderr.Bytes(), nil
}

type Toolchain struct {
	FFmpegPath  string
	FFprobePath string
	FrameWidth  int
	Runner      Runner
}

func (t Toolchain) Validate() error {
	if t.Runner == nil {
		t.Runner = CommandRunner{}
	}
	if _, err := exec.LookPath(t.FFmpegPath); err != nil {
		return fmt.Errorf("find ffmpeg: %w", err)
	}
	if _, err := exec.LookPath(t.FFprobePath); err != nil {
		return fmt.Errorf("find ffprobe: %w", err)
	}
	return nil
}

func (t Toolchain) Probe(ctx context.Context, sourcePath string) (domain.VideoMetadata, error) {
	stdout, _, err := t.runner().Run(ctx, t.FFprobePath,
		"-v", "error", "-print_format", "json", "-show_format", "-show_streams", sourcePath,
	)
	if err != nil {
		return domain.VideoMetadata{}, err
	}
	metadata, err := ParseProbe(stdout)
	if err != nil {
		return domain.VideoMetadata{}, fmt.Errorf("parse ffprobe output: %w", err)
	}
	if metadata.FileSizeBytes == 0 {
		if stat, statErr := os.Stat(sourcePath); statErr == nil {
			metadata.FileSizeBytes = stat.Size()
		}
	}
	return metadata, nil
}

func (t Toolchain) DetectScenes(ctx context.Context, sourcePath string, threshold float64, maxBoundaries int) ([]domain.SceneBoundary, error) {
	filter := fmt.Sprintf("select=gt(scene\\,%.6f),metadata=print", threshold)
	_, stderr, err := t.runner().Run(ctx, t.FFmpegPath,
		"-hide_banner", "-nostdin", "-i", sourcePath, "-map", "0:v:0", "-vf", filter, "-an", "-f", "null", "-",
	)
	if err != nil {
		return nil, err
	}
	return ParseSceneMetadata(stderr, threshold, maxBoundaries), nil
}

func (t Toolchain) ExtractFrame(ctx context.Context, sourcePath string, timestampMicros int64, destinationPath string) error {
	width := t.FrameWidth
	if width <= 0 {
		width = 1280
	}
	timestamp := strconv.FormatFloat(float64(timestampMicros)/1_000_000, 'f', 6, 64)
	filter := fmt.Sprintf("scale=min(%d\\,iw):-2", width)
	_, _, err := t.runner().Run(ctx, t.FFmpegPath,
		"-hide_banner", "-loglevel", "error", "-nostdin", "-ss", timestamp, "-i", sourcePath,
		"-map", "0:v:0", "-frames:v", "1", "-vf", filter, "-q:v", "2", "-y", destinationPath,
	)
	return err
}

func (t Toolchain) ExtractAudio(ctx context.Context, sourcePath, destinationPath string) error {
	_, _, err := t.runner().Run(ctx, t.FFmpegPath,
		"-hide_banner", "-loglevel", "error", "-nostdin", "-i", sourcePath,
		"-map", "0:a:0", "-vn", "-ac", "1", "-ar", "16000", "-c:a", "flac", "-y", destinationPath,
	)
	return err
}

func (t Toolchain) runner() Runner {
	if t.Runner != nil {
		return t.Runner
	}
	return CommandRunner{}
}

type probeDocument struct {
	Streams []probeStream `json:"streams"`
	Format  probeFormat   `json:"format"`
}

type probeStream struct {
	CodecType    string                   `json:"codec_type"`
	CodecName    string                   `json:"codec_name"`
	Width        int                      `json:"width"`
	Height       int                      `json:"height"`
	AverageFPS   string                   `json:"avg_frame_rate"`
	RealFPS      string                   `json:"r_frame_rate"`
	Duration     string                   `json:"duration"`
	BitRate      string                   `json:"bit_rate"`
	Tags         map[string]string        `json:"tags"`
	SideDataList []map[string]interface{} `json:"side_data_list"`
}

type probeFormat struct {
	Duration   string `json:"duration"`
	Size       string `json:"size"`
	BitRate    string `json:"bit_rate"`
	FormatName string `json:"format_name"`
}

func ParseProbe(data []byte) (domain.VideoMetadata, error) {
	var document probeDocument
	if err := json.Unmarshal(data, &document); err != nil {
		return domain.VideoMetadata{}, err
	}
	var metadata domain.VideoMetadata
	metadata.DurationMicros = secondsStringToMicros(document.Format.Duration)
	metadata.FileSizeBytes, _ = strconv.ParseInt(document.Format.Size, 10, 64)
	metadata.BitRate, _ = strconv.ParseInt(document.Format.BitRate, 10, 64)
	metadata.FormatName = document.Format.FormatName
	for _, stream := range document.Streams {
		switch stream.CodecType {
		case "video":
			if metadata.VideoCodec != "" {
				continue
			}
			metadata.VideoCodec = stream.CodecName
			metadata.Width = stream.Width
			metadata.Height = stream.Height
			metadata.FPS = parseRate(stream.AverageFPS)
			if metadata.FPS == 0 {
				metadata.FPS = parseRate(stream.RealFPS)
			}
			if metadata.DurationMicros == 0 {
				metadata.DurationMicros = secondsStringToMicros(stream.Duration)
			}
			if metadata.BitRate == 0 {
				metadata.BitRate, _ = strconv.ParseInt(stream.BitRate, 10, 64)
			}
			metadata.Rotation = parseRotation(stream)
		case "audio":
			metadata.HasAudio = true
			if metadata.AudioCodec == "" {
				metadata.AudioCodec = stream.CodecName
			}
		}
	}
	if metadata.VideoCodec == "" || metadata.DurationMicros <= 0 || metadata.Width <= 0 || metadata.Height <= 0 {
		return domain.VideoMetadata{}, fmt.Errorf("ffprobe output is missing a valid video stream")
	}
	return metadata, nil
}

var (
	pointsTimePattern = regexp.MustCompile(`pts_time:([0-9]+(?:\.[0-9]+)?)`)
	sceneScorePattern = regexp.MustCompile(`lavfi\.scene_score=([0-9]+(?:\.[0-9]+)?)`)
)

func ParseSceneMetadata(data []byte, fallbackScore float64, maxBoundaries int) []domain.SceneBoundary {
	lines := strings.Split(string(data), "\n")
	boundaries := make([]domain.SceneBoundary, 0)
	pendingTimestamp := int64(-1)
	for _, line := range lines {
		if match := pointsTimePattern.FindStringSubmatch(line); len(match) == 2 {
			if pendingTimestamp >= 0 {
				boundaries = append(boundaries, domain.SceneBoundary{TimestampMicros: pendingTimestamp, Score: fallbackScore})
			}
			pendingTimestamp = secondsStringToMicros(match[1])
			continue
		}
		if match := sceneScorePattern.FindStringSubmatch(line); len(match) == 2 && pendingTimestamp >= 0 {
			score, _ := strconv.ParseFloat(match[1], 64)
			boundaries = append(boundaries, domain.SceneBoundary{TimestampMicros: pendingTimestamp, Score: score})
			pendingTimestamp = -1
		}
	}
	if pendingTimestamp >= 0 {
		boundaries = append(boundaries, domain.SceneBoundary{TimestampMicros: pendingTimestamp, Score: fallbackScore})
	}
	sort.Slice(boundaries, func(left, right int) bool {
		if boundaries[left].TimestampMicros == boundaries[right].TimestampMicros {
			return boundaries[left].Score > boundaries[right].Score
		}
		return boundaries[left].TimestampMicros < boundaries[right].TimestampMicros
	})
	deduplicated := make([]domain.SceneBoundary, 0, len(boundaries))
	for _, boundary := range boundaries {
		if boundary.TimestampMicros <= 0 {
			continue
		}
		if len(deduplicated) > 0 && boundary.TimestampMicros-deduplicated[len(deduplicated)-1].TimestampMicros < 10_000 {
			if boundary.Score > deduplicated[len(deduplicated)-1].Score {
				deduplicated[len(deduplicated)-1] = boundary
			}
			continue
		}
		deduplicated = append(deduplicated, boundary)
	}
	if maxBoundaries <= 0 || len(deduplicated) <= maxBoundaries {
		return deduplicated
	}
	ranked := append([]domain.SceneBoundary(nil), deduplicated...)
	sort.SliceStable(ranked, func(left, right int) bool {
		if ranked[left].Score == ranked[right].Score {
			return ranked[left].TimestampMicros < ranked[right].TimestampMicros
		}
		return ranked[left].Score > ranked[right].Score
	})
	ranked = ranked[:maxBoundaries]
	sort.Slice(ranked, func(left, right int) bool { return ranked[left].TimestampMicros < ranked[right].TimestampMicros })
	return ranked
}

func parseRate(value string) float64 {
	numerator, denominator, ok := strings.Cut(value, "/")
	if !ok {
		parsed, _ := strconv.ParseFloat(value, 64)
		return parsed
	}
	n, _ := strconv.ParseFloat(numerator, 64)
	d, _ := strconv.ParseFloat(denominator, 64)
	if d == 0 {
		return 0
	}
	return n / d
}

func parseRotation(stream probeStream) int {
	if value := stream.Tags["rotate"]; value != "" {
		rotation, _ := strconv.Atoi(value)
		return normalizeRotation(rotation)
	}
	for _, sideData := range stream.SideDataList {
		if raw, ok := sideData["rotation"].(float64); ok {
			return normalizeRotation(int(raw))
		}
	}
	return 0
}

func normalizeRotation(rotation int) int {
	rotation %= 360
	if rotation < 0 {
		rotation += 360
	}
	return rotation
}

func secondsStringToMicros(value string) int64 {
	seconds, _ := strconv.ParseFloat(value, 64)
	return int64(seconds*1_000_000 + 0.5)
}

func truncate(value string, maximum int) string {
	if len(value) <= maximum {
		return value
	}
	return value[:maximum]
}
