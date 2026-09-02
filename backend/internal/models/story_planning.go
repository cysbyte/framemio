package models

import "time"

const StoryPlanSchemaVersionV1 = "story_plan_v1"

type StoryType string

const (
	StoryTypeToday    StoryType = "today"
	StoryTypeWeekend  StoryType = "weekend"
	StoryTypeTrip     StoryType = "trip"
	StoryTypeBirthday StoryType = "birthday"
	StoryTypeFamily   StoryType = "family"
	StoryTypeFriends  StoryType = "friends"
	StoryTypePet      StoryType = "pet"
	StoryTypeEvent    StoryType = "event"
	StoryTypeCustom   StoryType = "custom"
)

type VisualStyle string

const (
	VisualStyleNatural   VisualStyle = "natural"
	VisualStyleCinematic VisualStyle = "cinematic"
	VisualStyleCalm      VisualStyle = "calm"
	VisualStyleWarm      VisualStyle = "warm"
	VisualStylePlayful   VisualStyle = "playful"
	VisualStyleMinimal   VisualStyle = "minimal"
	VisualStyleElegant   VisualStyle = "elegant"
)

type PhotoAnalysis struct {
	ID              string     `json:"id"`
	PhotoID         string     `json:"photo_id"`
	ProjectID       string     `json:"project_id"`
	AnalysisVersion string     `json:"analysis_version"`
	TimestampMicros int64      `json:"timestamp_micros,omitempty"`
	TakenAt         *time.Time `json:"taken_at,omitempty"`
	Location        string     `json:"location,omitempty"`
	People          []string   `json:"people,omitempty"`
	Labels          []string   `json:"labels,omitempty"`
	Caption         string     `json:"caption,omitempty"`
	Description     string     `json:"description,omitempty"`
	MemoryValue     float64    `json:"memory_value,omitempty"`
	EmotionalValue  float64    `json:"emotional_value,omitempty"`
	VisualQuality   float64    `json:"visual_quality,omitempty"`
	StoryRelevance  float64    `json:"story_relevance,omitempty"`
	ClusterID       string     `json:"cluster_id,omitempty"`
	Selected        bool       `json:"selected,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
}

type Frame struct {
	FrameID         string `json:"frame_id"`
	TimestampMicros int64  `json:"timestamp_micros"`
	S3Key           string `json:"s3_key"`
}

type VideoScene struct {
	SceneID              string  `json:"scene_id"`
	StartMicros          int64   `json:"start_micros"`
	EndMicros            int64   `json:"end_micros"`
	RepresentativeFrames []Frame `json:"representative_frames,omitempty"`
}

type VideoTranscriptSegment struct {
	StartMicros int64   `json:"start_micros"`
	EndMicros   int64   `json:"end_micros"`
	Text        string  `json:"text"`
	Confidence  float64 `json:"confidence,omitempty"`
}

type VideoTranscript struct {
	Language  string                   `json:"language,omitempty"`
	Text      string                   `json:"text,omitempty"`
	HasSpeech bool                     `json:"has_speech"`
	Segments  []VideoTranscriptSegment `json:"segments,omitempty"`
}

type VideoQualitySignals struct {
	OverallScore      float64 `json:"overall_score"`
	LowResolution     bool    `json:"low_resolution,omitempty"`
	LowBitRate        bool    `json:"low_bit_rate,omitempty"`
	HighFrameRate     bool    `json:"high_frame_rate,omitempty"`
	LikelyStatic      bool    `json:"likely_static,omitempty"`
	FastMoving        bool    `json:"fast_moving,omitempty"`
	SceneChangesPM    float64 `json:"scene_changes_per_minute,omitempty"`
	AverageSharpness  float64 `json:"average_sharpness,omitempty"`
	AverageBrightness float64 `json:"average_brightness,omitempty"`
}

type VideoAnalysis struct {
	ID                   string              `json:"id"`
	VideoID              string              `json:"video_id"`
	ProjectID            string              `json:"project_id"`
	AnalysisVersion      string              `json:"analysis_version"`
	InputVersion         string              `json:"input_version,omitempty"`
	UnderstandingVersion string              `json:"understanding_version,omitempty"`
	Provider             string              `json:"provider,omitempty"`
	Model                string              `json:"model,omitempty"`
	SourceETag           string              `json:"source_etag,omitempty"`
	DurationMicros       int64               `json:"duration_micros"`
	Scenes               []VideoScene        `json:"scenes,omitempty"`
	Transcript           VideoTranscript     `json:"transcript"`
	Segments             []VideoSegment      `json:"segments,omitempty"`
	DetectedPeople       []string            `json:"detected_people,omitempty"`
	DetectedActivities   []string            `json:"detected_activities,omitempty"`
	DetectedLocations    []string            `json:"detected_locations,omitempty"`
	QualitySignals       VideoQualitySignals `json:"quality_signals"`
	CreatedAt            time.Time           `json:"created_at"`
}

type VideoSegment struct {
	SegmentID          string   `json:"segment_id"`
	SceneID            string   `json:"scene_id"`
	StartMicros        int64    `json:"start_micros"`
	EndMicros          int64    `json:"end_micros"`
	Description        string   `json:"description"`
	Importance         float64  `json:"importance"`
	VisualQuality      float64  `json:"visual_quality"`
	MemoryValue        float64  `json:"memory_value"`
	EmotionalValue     float64  `json:"emotional_value"`
	StoryRelevance     float64  `json:"story_relevance"`
	Recommended        bool     `json:"recommended"`
	Reasons            []string `json:"reasons,omitempty"`
	RecommendedUsage   string   `json:"recommended_usage,omitempty"`
	People             []string `json:"people,omitempty"`
	Activities         []string `json:"activities,omitempty"`
	Interactions       []string `json:"interactions,omitempty"`
	Expressions        []string `json:"expressions,omitempty"`
	Objects            []string `json:"objects,omitempty"`
	Food               []string `json:"food,omitempty"`
	Locations          []string `json:"locations,omitempty"`
	Scenery            []string `json:"scenery,omitempty"`
	Events             []string `json:"events,omitempty"`
	InterestingActions []string `json:"interesting_actions,omitempty"`
	Speech             []string `json:"speech,omitempty"`
	MeaningfulAudio    []string `json:"meaningful_audio,omitempty"`
	Labels             []string `json:"labels,omitempty"`
}

type MediaCluster struct {
	ID             string     `json:"id"`
	ProjectID      string     `json:"project_id"`
	Label          string     `json:"label,omitempty"`
	MediaIDs       []string   `json:"media_ids"`
	PrimaryMediaID string     `json:"primary_media_id,omitempty"`
	StartAt        *time.Time `json:"start_at,omitempty"`
	EndAt          *time.Time `json:"end_at,omitempty"`
	Location       string     `json:"location,omitempty"`
	Importance     float64    `json:"importance,omitempty"`
	RepetitionRisk float64    `json:"repetition_risk,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
}

type StoryCaption struct {
	MomentID string `json:"moment_id"`
	Text     string `json:"text"`
	StartMS  int    `json:"start_ms,omitempty"`
	EndMS    int    `json:"end_ms,omitempty"`
}

type CaptionStyle struct {
	Preset               string  `json:"preset,omitempty"`
	FontFamily           string  `json:"font_family,omitempty"`
	FontSize             int     `json:"font_size,omitempty"`
	TextColor            string  `json:"text_color,omitempty"`
	StrokeColor          string  `json:"stroke_color,omitempty"`
	StrokeWidth          int     `json:"stroke_width,omitempty"`
	BackgroundEnabled    bool    `json:"background_enabled,omitempty"`
	BackgroundColor      string  `json:"background_color,omitempty"`
	BackgroundOpacity    float64 `json:"background_opacity,omitempty"`
	Position             string  `json:"position,omitempty"`
	BottomOffset         int     `json:"bottom_offset,omitempty"`
	MaxLines             int     `json:"max_lines,omitempty"`
	MaxCharactersPerLine int     `json:"max_characters_per_line,omitempty"`
}
