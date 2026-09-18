package models

import (
	"strings"
	"time"
)

type User struct {
	ID        string    `json:"id"`
	Email     string    `json:"email,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type MediaType string

const (
	MediaTypePhoto     MediaType = "photo"
	MediaTypeVideo     MediaType = "video"
	MediaTypeLivePhoto MediaType = "live_photo"
)

type MediaStatus string

const (
	MediaStatusUploading MediaStatus = "uploading"
	MediaStatusReady     MediaStatus = "ready"
	MediaStatusFailed    MediaStatus = "failed"
)

type MediaAsset struct {
	ID              string      `json:"id"`
	UserID          string      `json:"user_id"`
	Type            MediaType   `json:"media_type"`
	Status          MediaStatus `json:"status"`
	FileName        string      `json:"file_name"`
	ContentType     string      `json:"content_type"`
	FileSizeBytes   int64       `json:"file_size_bytes"`
	DurationSeconds *float64    `json:"duration,omitempty"`
	CreationDate    *time.Time  `json:"creation_date,omitempty"`
	PixelWidth      int         `json:"pixel_width"`
	PixelHeight     int         `json:"pixel_height"`
	Orientation     string      `json:"orientation,omitempty"`
	S3Key           string      `json:"s3_key"`
	UploadID        string      `json:"upload_id"`
	SourceAssetID   string      `json:"source_asset_id,omitempty"`
	ETag            string      `json:"etag,omitempty"`
	CreatedAt       time.Time   `json:"created_at"`
	UpdatedAt       time.Time   `json:"updated_at"`
}

type UploadStrategy string

const (
	UploadStrategySingle    UploadStrategy = "single"
	UploadStrategyMultipart UploadStrategy = "multipart"
)

type UploadPart struct {
	PartNumber  int       `json:"part_number"`
	OffsetBytes int64     `json:"offset_bytes"`
	LengthBytes int64     `json:"length_bytes"`
	UploadURL   string    `json:"upload_url"`
	ExpiresAt   time.Time `json:"expires_at"`
}

type CompletedUploadPart struct {
	PartNumber int    `json:"part_number"`
	ETag       string `json:"etag"`
}

type UploadSession struct {
	ID                string         `json:"upload_id"`
	UserID            string         `json:"user_id"`
	MediaID           string         `json:"media_id"`
	S3Key             string         `json:"s3_key"`
	Strategy          UploadStrategy `json:"strategy"`
	MultipartUploadID string         `json:"multipart_upload_id,omitempty"`
	PartSizeBytes     int64          `json:"part_size_bytes"`
	Parts             []UploadPart   `json:"parts"`
	ExpiresAt         time.Time      `json:"expires_at"`
	IdempotencyKey    string         `json:"-"`
	CreatedAt         time.Time      `json:"created_at"`
	CompletedAt       *time.Time     `json:"completed_at,omitempty"`
}

type ProjectStatus string

const (
	ProjectStatusDraft      ProjectStatus = "draft"
	ProjectStatusAnalyzing  ProjectStatus = "analyzing"
	ProjectStatusStoryReady ProjectStatus = "story_ready"
	ProjectStatusRendering  ProjectStatus = "rendering"
	ProjectStatusReady      ProjectStatus = "ready"
	ProjectStatusFailed     ProjectStatus = "failed"
)

type JobType string

const (
	JobTypeMediaPreprocess JobType = "MEDIA_PREPROCESS"
	JobTypeTranscribe      JobType = "TRANSCRIBE"
	JobTypeVideoUnderstand JobType = "VIDEO_UNDERSTAND"
	JobTypeStoryPlan       JobType = "STORY_PLAN"
	JobTypeStoryValidate   JobType = "STORY_VALIDATE"
	JobTypeVideoRender     JobType = "VIDEO_RENDER"

	JobTypeAnalysis JobType = JobTypeMediaPreprocess
	JobTypeRender   JobType = JobTypeVideoRender
)

type JobStatus string

const (
	JobStatusQueued    JobStatus = "QUEUED"
	JobStatusRunning   JobStatus = "RUNNING"
	JobStatusCompleted JobStatus = "COMPLETED"
	JobStatusSucceeded JobStatus = JobStatusCompleted
	JobStatusFailed    JobStatus = "FAILED"
	JobStatusCancelled JobStatus = "CANCELLED"
)

func NormalizeJobType(value JobType) (JobType, bool) {
	switch JobType(strings.ToUpper(strings.TrimSpace(string(value)))) {
	case JobTypeMediaPreprocess:
		return JobTypeMediaPreprocess, true
	case JobTypeTranscribe:
		return JobTypeTranscribe, true
	case JobTypeVideoUnderstand:
		return JobTypeVideoUnderstand, true
	case JobTypeStoryPlan:
		return JobTypeStoryPlan, true
	case JobTypeStoryValidate:
		return JobTypeStoryValidate, true
	case JobTypeVideoRender:
		return JobTypeVideoRender, true
	}
	switch strings.ToLower(strings.TrimSpace(string(value))) {
	case "analysis":
		return JobTypeMediaPreprocess, true
	case "render":
		return JobTypeVideoRender, true
	default:
		return "", false
	}
}

func NormalizeJobStatus(value JobStatus) (JobStatus, bool) {
	switch JobStatus(strings.ToUpper(strings.TrimSpace(string(value)))) {
	case JobStatusQueued:
		return JobStatusQueued, true
	case JobStatusRunning:
		return JobStatusRunning, true
	case JobStatusCompleted:
		return JobStatusCompleted, true
	case JobStatusFailed:
		return JobStatusFailed, true
	case JobStatusCancelled:
		return JobStatusCancelled, true
	}
	switch strings.ToLower(strings.TrimSpace(string(value))) {
	case "succeeded", "success", "complete", "completed":
		return JobStatusCompleted, true
	case "queued":
		return JobStatusQueued, true
	case "running":
		return JobStatusRunning, true
	case "failed":
		return JobStatusFailed, true
	case "cancelled", "canceled":
		return JobStatusCancelled, true
	default:
		return "", false
	}
}

func IsTerminalJobStatus(status JobStatus) bool {
	return status == JobStatusCompleted || status == JobStatusFailed || status == JobStatusCancelled
}

type ProcessingJob struct {
	ID              string            `json:"id"`
	UserID          string            `json:"user_id"`
	ProjectID       string            `json:"project_id"`
	ProjectType     ProjectType       `json:"project_type,omitempty"`
	MediaID         string            `json:"media_id,omitempty"`
	Type            JobType           `json:"type"`
	Status          JobStatus         `json:"status"`
	Progress        float64           `json:"progress"`
	Attempt         int               `json:"attempt"`
	MaxAttempts     int               `json:"max_attempts,omitempty"`
	Configuration   map[string]string `json:"configuration,omitempty"`
	ErrorCode       string            `json:"error_code,omitempty"`
	ErrorMessage    string            `json:"error_message,omitempty"`
	ArtifactS3Key   string            `json:"artifact_s3_key,omitempty"`
	ArtifactType    string            `json:"artifact_content_type,omitempty"`
	CreatedAt       time.Time         `json:"created_at"`
	StartedAt       *time.Time        `json:"started_at,omitempty"`
	UpdatedAt       time.Time         `json:"updated_at"`
	CompletedAt     *time.Time        `json:"completed_at,omitempty"`
	NextRunAt       *time.Time        `json:"next_run_at,omitempty"`
	VisibilityUntil *time.Time        `json:"visibility_until,omitempty"`
	IdempotencyKey  string            `json:"-"`
}

type MediaAnalysis struct {
	ID           string                 `json:"id"`
	MediaID      string                 `json:"media_id"`
	ProjectID    string                 `json:"project_id"`
	Labels       []string               `json:"labels,omitempty"`
	PeopleCount  int                    `json:"people_count"`
	QualityScore float64                `json:"quality_score,omitempty"`
	Signals      map[string]interface{} `json:"signals,omitempty"`
	CreatedAt    time.Time              `json:"created_at"`
	UpdatedAt    time.Time              `json:"updated_at"`
}

type StoryPlan struct {
	SchemaVersion       string         `json:"schema_version,omitempty"`
	ID                  string         `json:"id"`
	ProjectID           string         `json:"project_id"`
	ProjectType         ProjectType    `json:"project_type,omitempty"`
	Version             int            `json:"version"`
	RequestedDurationMS int            `json:"requested_duration_ms,omitempty"`
	StoryType           StoryType      `json:"story_type,omitempty"`
	VisualStyle         VisualStyle    `json:"visual_style,omitempty"`
	TitleText           string         `json:"title_text,omitempty"`
	EndingText          string         `json:"ending_text,omitempty"`
	CallToAction        string         `json:"call_to_action,omitempty"`
	UserInstructions    string         `json:"user_instructions,omitempty"`
	Moments             []StoryMoment  `json:"moments"`
	BRoll               []StoryBRoll   `json:"broll,omitempty"`
	Captions            []StoryCaption `json:"captions,omitempty"`
	CaptionStyle        *CaptionStyle  `json:"caption_style,omitempty"`
	Audio               *StoryAudio    `json:"audio,omitempty"`
	Output              StoryOutput    `json:"output"`
	TotalDurationMS     int            `json:"total_duration_ms,omitempty"`
	CreatedAt           time.Time      `json:"created_at"`
}

type TextOverlayType string

const (
	TextOverlayMomentTitle   TextOverlayType = "moment_title"
	TextOverlayMomentCaption TextOverlayType = "moment_caption"
	TextOverlayLocation      TextOverlayType = "location"
	TextOverlayDate          TextOverlayType = "date"
	TextOverlayChapterTitle  TextOverlayType = "chapter_title"
	TextOverlayStoryIntro    TextOverlayType = "story_intro"
	TextOverlayStoryOutro    TextOverlayType = "story_outro"
	TextOverlayPromoHeadline TextOverlayType = "promo_headline"
	TextOverlayPromoSupport  TextOverlayType = "promo_support"
	TextOverlayPromoCTA      TextOverlayType = "promo_cta"
)

type TypographyStyle string

const (
	TypographyStyleCinematic TypographyStyle = "cinematic"
	TypographyStyleMinimal   TypographyStyle = "minimal"
	TypographyStyleEditorial TypographyStyle = "editorial"
	TypographyStyleMemory    TypographyStyle = "memory"
	TypographyStyleLocation  TypographyStyle = "location"
)

type TextPlacement string

const (
	TextPlacementTopLeft      TextPlacement = "top_left"
	TextPlacementTopCenter    TextPlacement = "top_center"
	TextPlacementTopRight     TextPlacement = "top_right"
	TextPlacementCenterLeft   TextPlacement = "center_left"
	TextPlacementCenter       TextPlacement = "center"
	TextPlacementCenterRight  TextPlacement = "center_right"
	TextPlacementBottomLeft   TextPlacement = "bottom_left"
	TextPlacementBottomCenter TextPlacement = "bottom_center"
	TextPlacementBottomRight  TextPlacement = "bottom_right"
)

type TextAnimation string

const (
	TextAnimationFade        TextAnimation = "fade"
	TextAnimationFadeUp      TextAnimation = "fade_up"
	TextAnimationFadeDown    TextAnimation = "fade_down"
	TextAnimationSubtleScale TextAnimation = "subtle_scale"
	TextAnimationNone        TextAnimation = "none"
)

type TextSource string

const (
	TextSourceAI     TextSource = "ai"
	TextSourceUser   TextSource = "user"
	TextSourceSystem TextSource = "system"
)

type SafeArea struct {
	Top    float64 `json:"top,omitempty"`
	Right  float64 `json:"right,omitempty"`
	Bottom float64 `json:"bottom,omitempty"`
	Left   float64 `json:"left,omitempty"`
}

type TextOverlay struct {
	Enabled    bool            `json:"enabled"`
	Type       TextOverlayType `json:"type,omitempty"`
	Title      string          `json:"title,omitempty"`
	Subtitle   string          `json:"subtitle,omitempty"`
	Style      TypographyStyle `json:"style,omitempty"`
	Placement  TextPlacement   `json:"placement,omitempty"`
	StartMS    int             `json:"start_ms,omitempty"`
	DurationMS int             `json:"duration_ms,omitempty"`
	MaxWidth   float64         `json:"max_width,omitempty"`
	SafeArea   SafeArea        `json:"safe_area,omitempty"`
	Animation  TextAnimation   `json:"animation,omitempty"`
	Source     TextSource      `json:"source,omitempty"`
	Reason     string          `json:"reason,omitempty"`
	Confidence float64         `json:"confidence,omitempty"`
}

type PromoScenePurpose string

const (
	PromoScenePurposeHook        PromoScenePurpose = "hook"
	PromoScenePurposeProblem     PromoScenePurpose = "problem"
	PromoScenePurposeProduct     PromoScenePurpose = "product"
	PromoScenePurposeFeature     PromoScenePurpose = "feature"
	PromoScenePurposeBenefit     PromoScenePurpose = "benefit"
	PromoScenePurposeProof       PromoScenePurpose = "proof"
	PromoScenePurposeTestimonial PromoScenePurpose = "testimonial"
	PromoScenePurposeCTA         PromoScenePurpose = "cta"
	PromoScenePurposeOutro       PromoScenePurpose = "outro"
)

type PromoSceneText struct {
	Headline   string `json:"headline,omitempty"`
	Supporting string `json:"supporting,omitempty"`
	CTA        string `json:"cta,omitempty"`
}

type SceneVoiceover struct {
	Text       string `json:"text,omitempty"`
	StartMS    int    `json:"start_ms,omitempty"`
	DurationMS int    `json:"duration_ms,omitempty"`
}

type StoryMoment struct {
	MomentID             string            `json:"moment_id,omitempty"`
	AssetID              string            `json:"asset_id"`
	MediaID              string            `json:"media_id,omitempty"`
	MediaType            MediaType         `json:"media_type,omitempty"`
	VideoAnalysisVersion string            `json:"video_analysis_version,omitempty"`
	StartMicros          int64             `json:"start_micros,omitempty"`
	EndMicros            int64             `json:"end_micros,omitempty"`
	Narrative            string            `json:"narrative,omitempty"`
	Caption              string            `json:"caption,omitempty"`
	DurationMS           int               `json:"duration_ms"`
	CropIntent           string            `json:"crop_intent,omitempty"`
	Transition           string            `json:"transition,omitempty"`
	Reason               string            `json:"reason,omitempty"`
	Score                float64           `json:"score,omitempty"`
	Typography           *TextOverlay      `json:"typography,omitempty"`
	Order                int               `json:"order,omitempty"`
	ScenePurpose         PromoScenePurpose `json:"scene_purpose,omitempty"`
	SceneText            *PromoSceneText   `json:"scene_text,omitempty"`
	Voiceover            *SceneVoiceover   `json:"voiceover,omitempty"`
}

type StoryBRoll struct {
	BrollID              string            `json:"broll_id,omitempty"`
	AssetID              string            `json:"asset_id"`
	MediaID              string            `json:"media_id,omitempty"`
	MediaType            MediaType         `json:"media_type,omitempty"`
	VideoAnalysisVersion string            `json:"video_analysis_version,omitempty"`
	SourceStartMicros    int64             `json:"source_start_micros,omitempty"`
	SourceEndMicros      int64             `json:"source_end_micros,omitempty"`
	StartMS              int               `json:"start_ms"`
	DurationMS           int               `json:"duration_ms"`
	Anchor               *StoryBRollAnchor `json:"anchor,omitempty"`
	CropIntent           string            `json:"crop_intent,omitempty"`
	Reason               string            `json:"reason,omitempty"`
	Confidence           float64           `json:"confidence,omitempty"`
	Score                float64           `json:"score,omitempty"`
}

type StoryBRollAnchor struct {
	Type     string `json:"type"`
	MomentID string `json:"moment_id,omitempty"`
	StartMS  int    `json:"start_ms,omitempty"`
	EndMS    int    `json:"end_ms,omitempty"`
}

type StoryAudio struct {
	Source         string  `json:"source,omitempty"`
	OriginalAudio  *bool   `json:"original_audio,omitempty"`
	OriginalVolume float64 `json:"original_volume,omitempty"`
}

type StoryOutput struct {
	AspectRatio  string `json:"aspect_ratio"`
	MaxDurationS int    `json:"max_duration_seconds"`
	Preset       string `json:"preset"`
}

type RenderJob struct {
	ProcessingJobID   string `json:"job_id"`
	ProjectID         string `json:"project_id"`
	StoryPlanID       string `json:"story_plan_id,omitempty"`
	StoryPlanVersion  int    `json:"story_plan_version"`
	OutputPreset      string `json:"output_preset"`
	OutputS3Key       string `json:"output_s3_key,omitempty"`
	OutputContentType string `json:"output_content_type,omitempty"`
}
