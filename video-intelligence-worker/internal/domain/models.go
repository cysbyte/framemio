package domain

import "time"

type JobMessage struct {
	JobID         string            `json:"jobId"`
	ProjectID     string            `json:"projectId"`
	MediaID       string            `json:"mediaId"`
	JobType       string            `json:"jobType"`
	Configuration map[string]string `json:"configuration,omitempty"`
}

type ProcessingJob struct {
	ID             string            `json:"id"`
	UserID         string            `json:"user_id"`
	ProjectID      string            `json:"project_id"`
	MediaID        string            `json:"media_id,omitempty"`
	Type           string            `json:"type"`
	Status         string            `json:"status"`
	Progress       float64           `json:"progress"`
	Attempt        int               `json:"attempt"`
	Configuration  map[string]string `json:"configuration,omitempty"`
	ErrorCode      string            `json:"error_code,omitempty"`
	ErrorMessage   string            `json:"error_message,omitempty"`
	ArtifactS3Key  string            `json:"artifact_s3_key,omitempty"`
	ArtifactType   string            `json:"artifact_content_type,omitempty"`
	CreatedAt      time.Time         `json:"created_at"`
	UpdatedAt      time.Time         `json:"updated_at"`
	CompletedAt    *time.Time        `json:"completed_at,omitempty"`
	IdempotencyKey string            `json:"-"`
}

type MediaAsset struct {
	ID              string     `json:"id"`
	UserID          string     `json:"user_id"`
	Type            string     `json:"media_type"`
	Status          string     `json:"status"`
	FileName        string     `json:"file_name"`
	ContentType     string     `json:"content_type"`
	FileSizeBytes   int64      `json:"file_size_bytes"`
	DurationSeconds *float64   `json:"duration,omitempty"`
	CreationDate    *time.Time `json:"creation_date,omitempty"`
	PixelWidth      int        `json:"pixel_width"`
	PixelHeight     int        `json:"pixel_height"`
	Orientation     string     `json:"orientation,omitempty"`
	S3Key           string     `json:"s3_key"`
	UploadID        string     `json:"upload_id"`
	ETag            string     `json:"etag,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

type StoryProject struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	Title     string    `json:"title"`
	Context   string    `json:"context"`
	MediaIDs  []string  `json:"media_ids"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type JobContext struct {
	Job     ProcessingJob
	Media   MediaAsset
	Project StoryProject
}

type VideoMetadata struct {
	DurationMicros int64   `json:"duration_micros"`
	Width          int     `json:"width"`
	Height         int     `json:"height"`
	Rotation       int     `json:"rotation"`
	FPS            float64 `json:"fps"`
	VideoCodec     string  `json:"video_codec"`
	AudioCodec     string  `json:"audio_codec,omitempty"`
	BitRate        int64   `json:"bit_rate,omitempty"`
	FileSizeBytes  int64   `json:"file_size_bytes"`
	HasAudio       bool    `json:"has_audio"`
	FormatName     string  `json:"format_name,omitempty"`
}

type SceneBoundary struct {
	TimestampMicros int64   `json:"timestamp_micros"`
	Score           float64 `json:"score"`
}

type VideoScene struct {
	ID          string  `json:"id"`
	StartMicros int64   `json:"start_micros"`
	EndMicros   int64   `json:"end_micros"`
	ChangeScore float64 `json:"change_score"`
}

type CandidateSegment struct {
	ID          string `json:"id"`
	SceneID     string `json:"scene_id"`
	StartMicros int64  `json:"start_micros"`
	EndMicros   int64  `json:"end_micros"`
}

type FrameCandidate struct {
	ID              string  `json:"id"`
	SceneID         string  `json:"scene_id"`
	SegmentID       string  `json:"segment_id"`
	TimestampMicros int64   `json:"timestamp_micros"`
	Role            string  `json:"role"`
	Priority        float64 `json:"priority"`
}

type SelectedFrame struct {
	ID              string  `json:"id"`
	MediaID         string  `json:"media_id"`
	SceneID         string  `json:"scene_id"`
	SegmentID       string  `json:"segment_id"`
	TimestampMicros int64   `json:"timestamp_micros"`
	S3Key           string  `json:"s3_key"`
	PerceptualHash  uint64  `json:"perceptual_hash"`
	Brightness      float64 `json:"brightness"`
	Contrast        float64 `json:"contrast"`
	Sharpness       float64 `json:"sharpness"`
	QualityScore    float64 `json:"quality_score"`
	SelectionReason string  `json:"selection_reason"`
	LocalPath       string  `json:"-"`
}

type TranscriptSegment struct {
	StartMicros int64   `json:"start_micros"`
	EndMicros   int64   `json:"end_micros"`
	Text        string  `json:"text"`
	Confidence  float64 `json:"confidence,omitempty"`
}

type Transcript struct {
	Language  string              `json:"language,omitempty"`
	Text      string              `json:"text,omitempty"`
	HasSpeech bool                `json:"has_speech"`
	Segments  []TranscriptSegment `json:"segments,omitempty"`
}

type TranscriptionRecord struct {
	ID               string     `json:"id"`
	UserID           string     `json:"user_id"`
	ProjectID        string     `json:"project_id"`
	MediaID          string     `json:"media_id"`
	SourceETag       string     `json:"source_etag,omitempty"`
	Version          string     `json:"version"`
	Provider         string     `json:"provider"`
	ProviderModel    string     `json:"provider_model"`
	Language         string     `json:"language,omitempty"`
	HasSpeech        bool       `json:"has_speech"`
	SegmentCount     int        `json:"segment_count"`
	DurationMicros   int64      `json:"duration_micros"`
	Transcript       Transcript `json:"transcript"`
	RawArtifactS3Key string     `json:"raw_artifact_s3_key,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

type QualitySignals struct {
	OverallScore      float64 `json:"overall_score"`
	LowResolution     bool    `json:"low_resolution"`
	LowBitRate        bool    `json:"low_bit_rate"`
	HighFrameRate     bool    `json:"high_frame_rate"`
	LikelyStatic      bool    `json:"likely_static"`
	FastMoving        bool    `json:"fast_moving"`
	SceneChangesPM    float64 `json:"scene_changes_per_minute"`
	AverageSharpness  float64 `json:"average_sharpness,omitempty"`
	AverageBrightness float64 `json:"average_brightness,omitempty"`
}

type VideoEvidence struct {
	SchemaVersion   int                `json:"schema_version"`
	AnalysisVersion string             `json:"analysis_version"`
	MediaID         string             `json:"media_id"`
	ProjectID       string             `json:"project_id"`
	SourceS3Key     string             `json:"source_s3_key"`
	SourceETag      string             `json:"source_etag,omitempty"`
	Metadata        VideoMetadata      `json:"metadata"`
	Scenes          []VideoScene       `json:"scenes"`
	Segments        []CandidateSegment `json:"segments"`
	Frames          []SelectedFrame    `json:"frames"`
	Transcript      Transcript         `json:"transcript"`
	Quality         QualitySignals     `json:"quality"`
	AudioS3Key      string             `json:"audio_s3_key,omitempty"`
	CreatedAt       time.Time          `json:"created_at"`
}

type PreprocessingArtifact struct {
	ID              string         `json:"id"`
	UserID          string         `json:"user_id"`
	ProjectID       string         `json:"project_id"`
	MediaID         string         `json:"media_id"`
	AnalysisVersion string         `json:"analysis_version"`
	SourceETag      string         `json:"source_etag,omitempty"`
	EvidenceS3Key   string         `json:"evidence_s3_key"`
	FrameCount      int            `json:"frame_count"`
	Metadata        VideoMetadata  `json:"metadata"`
	Quality         QualitySignals `json:"quality"`
	CreatedAt       time.Time      `json:"created_at"`
}

const (
	VideoUnderstandingInputSchemaV1 = "video-understanding-input.v1"
	VideoAnalysisSchemaV1           = "video-analysis.v1"
	VideoAnalysisStatusCompleted    = "completed"
	RecommendedUsageHero            = "hero"
	RecommendedUsageSupporting      = "supporting"
	RecommendedUsageContext         = "context"
	RecommendedUsageAudioOnly       = "audio_only"
	RecommendedUsageExclude         = "exclude"
)

type VideoUnderstandingFrameV1 struct {
	ID              string  `json:"id"`
	SceneID         string  `json:"scene_id"`
	SegmentID       string  `json:"segment_id"`
	TimestampMicros int64   `json:"timestamp_micros"`
	URL             string  `json:"url"`
	Brightness      float64 `json:"brightness"`
	Contrast        float64 `json:"contrast"`
	Sharpness       float64 `json:"sharpness"`
	QualityScore    float64 `json:"quality_score"`
	SelectionReason string  `json:"selection_reason"`
}

type VideoUnderstandingInputV1 struct {
	SchemaVersion   string                      `json:"schema_version"`
	InputVersion    string                      `json:"input_version"`
	AnalysisVersion string                      `json:"analysis_version"`
	Objective       string                      `json:"objective"`
	GroundingRules  []string                    `json:"grounding_rules"`
	MediaID         string                      `json:"media_id"`
	ProjectID       string                      `json:"project_id"`
	Metadata        VideoMetadata               `json:"metadata"`
	Scenes          []VideoScene                `json:"scenes"`
	Segments        []CandidateSegment          `json:"segments"`
	Frames          []VideoUnderstandingFrameV1 `json:"frames"`
	Transcript      Transcript                  `json:"transcript"`
	Quality         QualitySignals              `json:"quality"`
}

type VideoSegmentAnalysisV1 struct {
	SegmentID                string   `json:"segment_id"`
	SceneID                  string   `json:"scene_id"`
	StartMicros              int64    `json:"start_micros"`
	EndMicros                int64    `json:"end_micros"`
	Description              string   `json:"description"`
	ImportanceScore          float64  `json:"importance_score"`
	VisualQualityScore       float64  `json:"visual_quality_score"`
	MemoryValueScore         float64  `json:"memory_value_score"`
	EmotionalValueScore      float64  `json:"emotional_value_score"`
	StoryRelevanceScore      float64  `json:"story_relevance_score"`
	EmotionalTone            string   `json:"emotional_tone,omitempty"`
	RecommendedUsage         string   `json:"recommended_usage"`
	Reason                   string   `json:"reason"`
	People                   []string `json:"people,omitempty"`
	Activities               []string `json:"activities,omitempty"`
	Interactions             []string `json:"interactions,omitempty"`
	Expressions              []string `json:"expressions,omitempty"`
	Objects                  []string `json:"objects,omitempty"`
	Food                     []string `json:"food,omitempty"`
	Locations                []string `json:"locations,omitempty"`
	Scenery                  []string `json:"scenery,omitempty"`
	Events                   []string `json:"events,omitempty"`
	InterestingActions       []string `json:"interesting_actions,omitempty"`
	Speech                   []string `json:"speech,omitempty"`
	MeaningfulAudio          []string `json:"meaningful_audio,omitempty"`
	FrameIDs                 []string `json:"frame_ids,omitempty"`
	TranscriptSegmentIndexes []int    `json:"transcript_segment_indexes,omitempty"`
}

type VideoUnderstandingResponseV1 struct {
	SchemaVersion string                   `json:"schema_version"`
	Candidates    []VideoSegmentAnalysisV1 `json:"candidates"`
}

type Frame struct {
	FrameID         string `json:"frame_id"`
	TimestampMicros int64  `json:"timestamp_micros"`
	S3Key           string `json:"s3_key"`
}

type Scene struct {
	SceneID              string  `json:"scene_id"`
	StartMicros          int64   `json:"start_micros"`
	EndMicros            int64   `json:"end_micros"`
	RepresentativeFrames []Frame `json:"representative_frames"`
}

type Segment struct {
	SegmentID        string   `json:"segment_id"`
	SceneID          string   `json:"scene_id"`
	StartMicros      int64    `json:"start_micros"`
	EndMicros        int64    `json:"end_micros"`
	Description      string   `json:"description"`
	Importance       float64  `json:"importance"`
	VisualQuality    float64  `json:"visual_quality"`
	MemoryValue      float64  `json:"memory_value"`
	EmotionalValue   float64  `json:"emotional_value"`
	StoryRelevance   float64  `json:"story_relevance"`
	Recommended      bool     `json:"recommended"`
	RecommendedUsage string   `json:"recommended_usage"`
	Reasons          []string `json:"reasons"`
}

type VideoAnalysis struct {
	ID                   string         `json:"id"`
	SchemaVersion        string         `json:"schema_version"`
	UserID               string         `json:"user_id"`
	ProjectID            string         `json:"project_id"`
	VideoID              string         `json:"video_id"`
	AnalysisVersion      string         `json:"analysis_version"`
	InputVersion         string         `json:"input_version"`
	UnderstandingVersion string         `json:"understanding_version"`
	Provider             string         `json:"provider"`
	Model                string         `json:"model"`
	SourceETag           string         `json:"source_etag,omitempty"`
	EvidenceS3Key        string         `json:"evidence_s3_key"`
	ProcessingStatus     string         `json:"processing_status"`
	DurationMicros       int64          `json:"duration_micros"`
	Scenes               []Scene        `json:"scenes"`
	Transcript           Transcript     `json:"transcript"`
	Segments             []Segment      `json:"segments"`
	DetectedPeople       []string       `json:"detected_people,omitempty"`
	DetectedActivities   []string       `json:"detected_activities,omitempty"`
	DetectedLocations    []string       `json:"detected_locations,omitempty"`
	QualitySignals       QualitySignals `json:"quality_signals"`
	CreatedAt            time.Time      `json:"created_at"`
}
