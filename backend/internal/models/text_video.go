package models

import "time"

type TextVideoStatus string

const (
	TextVideoStatusQueued    TextVideoStatus = "queued"
	TextVideoStatusRunning   TextVideoStatus = "running"
	TextVideoStatusCompleted TextVideoStatus = "completed"
	TextVideoStatusFailed    TextVideoStatus = "failed"
)

func IsTerminalTextVideoStatus(status TextVideoStatus) bool {
	return status == TextVideoStatusCompleted || status == TextVideoStatusFailed
}

type TextVideoGeneration struct {
	ID             string          `json:"id"`
	UserID         string          `json:"user_id"`
	ProviderTaskID string          `json:"provider_task_id,omitempty"`
	Prompt         string          `json:"prompt"`
	Seconds        int             `json:"seconds"`
	Resolution     string          `json:"resolution"`
	AspectRatio    string          `json:"aspect_ratio"`
	Model          string          `json:"model"`
	Status         TextVideoStatus `json:"status"`
	Progress       float64         `json:"progress"`
	VideoURL       string          `json:"video_url,omitempty"`
	S3Key          string          `json:"s3_key,omitempty"`
	ErrorMessage   string          `json:"error_message,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
	CompletedAt    *time.Time      `json:"completed_at,omitempty"`
	NextPollAt     time.Time       `json:"next_poll_at,omitempty"`
}
