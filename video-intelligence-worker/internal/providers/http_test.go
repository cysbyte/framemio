package providers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/framemio/video-intelligence-worker/internal/domain"
	"github.com/framemio/video-intelligence-worker/internal/transcription"
	"github.com/framemio/video-intelligence-worker/internal/understanding"
)

func TestHTTPTranscriptionProviderPreservesSecondTimestamps(t *testing.T) {
	t.Parallel()
	client := &http.Client{Timeout: time.Second, Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodPost || request.Header.Get("Authorization") != "Bearer secret" {
			t.Fatalf("unexpected request: method=%s authorization=%s", request.Method, request.Header.Get("Authorization"))
		}
		var body struct {
			Model     string                       `json:"model"`
			MediaID   string                       `json:"media_id"`
			Reference transcription.MediaReference `json:"reference"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Model != "speech-model" || body.MediaID != "media-1" || body.Reference.URL == "" || body.Reference.DurationMicros != 10_000_000 {
			t.Fatalf("unexpected provider request: %+v", body)
		}
		return jsonResponse(`{"language":"en","segments":[{"start":3.2,"end":6.8,"text":"Let's go over there.","confidence":0.73}]}`), nil
	})}
	provider := HTTPTranscriptionProvider{
		URL: "https://transcription.example.test/v1/transcribe", APIKey: "secret",
		ProviderName: "test-provider", ModelName: "speech-model", Client: client,
	}
	result, err := provider.Transcribe(context.Background(), transcription.ProviderRequest{
		MediaID: "media-1", Reference: transcription.MediaReference{
			URL: "https://media.example.test/audio.flac", MediaType: "audio", DurationMicros: 10_000_000,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Transcript.Segments) != 1 || result.Transcript.Segments[0].StartMicros != 3_200_000 || result.Transcript.Segments[0].EndMicros != 6_800_000 {
		t.Fatalf("timestamps were not preserved: %+v", result.Transcript)
	}
	if len(result.RawArtifact) == 0 || provider.Name() != "test-provider" || provider.Model() != "speech-model" {
		t.Fatal("provider metadata or raw response was not preserved")
	}
}

func TestHTTPTranscriptionProviderSupportsNoSpeech(t *testing.T) {
	t.Parallel()
	provider := HTTPTranscriptionProvider{
		URL: "https://transcription.example.test/v1/transcribe",
		Client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return jsonResponse(`{"segments":[]}`), nil
		})},
	}
	result, err := provider.Transcribe(context.Background(), transcription.ProviderRequest{
		MediaID: "media-1", Reference: transcription.MediaReference{URL: "https://example.test/video.mov", MediaType: "video", DurationMicros: 2_000_000},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Transcript.Segments) != 0 {
		t.Fatalf("no-speech response contains segments: %+v", result.Transcript)
	}
}

func TestHTTPVideoUnderstandingProviderSendsStructuredEvidenceAndStrictSchema(t *testing.T) {
	t.Parallel()
	input := providerUnderstandingInput()
	responseBody, err := json.Marshal(providerUnderstandingResponse())
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		var envelope struct {
			Model          string                           `json:"model"`
			ResponseSchema string                           `json:"response_schema"`
			Input          domain.VideoUnderstandingInputV1 `json:"input"`
		}
		if err := json.NewDecoder(request.Body).Decode(&envelope); err != nil {
			t.Fatal(err)
		}
		if envelope.Model != "memory-model" || envelope.ResponseSchema != domain.VideoAnalysisSchemaV1 || envelope.Input.Objective != understanding.Objective {
			t.Fatalf("unexpected understanding envelope: %+v", envelope)
		}
		if len(envelope.Input.Frames) != 1 || envelope.Input.Frames[0].URL == "" || len(envelope.Input.Segments) != 1 || envelope.Input.Metadata.DurationMicros == 0 {
			t.Fatalf("structured evidence is incomplete: %+v", envelope.Input)
		}
		encoded, err := json.Marshal(envelope.Input)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(encoded), "source_s3_key") || strings.Contains(string(encoded), "source.mov") {
			t.Fatalf("original video was sent despite sufficient evidence: %s", encoded)
		}
		return jsonResponse(string(responseBody)), nil
	})}
	provider := HTTPVideoUnderstandingProvider{
		URL: "https://understanding.example.test/v1/analyze", APIKey: "secret",
		ProviderName: "provider-a", ModelName: "memory-model", Client: client,
	}
	response, err := provider.Understand(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if response.SchemaVersion != domain.VideoAnalysisSchemaV1 || len(response.Candidates) != 1 || provider.Name() != "provider-a" || provider.Model() != "memory-model" {
		t.Fatalf("unexpected understanding response: %+v", response)
	}
}

func TestHTTPVideoUnderstandingProviderRejectsUnknownResponseFields(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		body string
	}{
		{name: "unknown field", body: `{"schema_version":"video-analysis.v1","candidates":[],"summary":"unsupported"}`},
		{name: "missing required field", body: `{"schema_version":"video-analysis.v1","candidates":[{"segment_id":"segment-0001","scene_id":"scene-0001","start_micros":0,"end_micros":4000000,"description":"A smile","importance_score":0.8,"visual_quality_score":0.7,"emotional_value_score":0.8,"story_relevance_score":0.8,"recommended_usage":"hero","reason":"Grounded","frame_ids":["frame-0001"]}]}`},
		{name: "trailing JSON", body: `{"schema_version":"video-analysis.v1","candidates":[]} {"extra":true}`},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			provider := HTTPVideoUnderstandingProvider{
				URL: "https://understanding.example.test/v1/analyze", ProviderName: "provider-a", ModelName: "memory-model",
				Client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
					return jsonResponse(test.body), nil
				})},
			}
			if _, err := provider.Understand(context.Background(), providerUnderstandingInput()); err == nil {
				t.Fatal("expected strict JSON decoding failure")
			}
		})
	}
}

func providerUnderstandingInput() domain.VideoUnderstandingInputV1 {
	return domain.VideoUnderstandingInputV1{
		SchemaVersion: domain.VideoUnderstandingInputSchemaV1, InputVersion: "video-evidence-v1",
		AnalysisVersion: "video-understanding-v1", Objective: understanding.Objective,
		GroundingRules: []string{"Do not invent evidence."}, MediaID: "media-1", ProjectID: "project-1",
		Metadata: domain.VideoMetadata{DurationMicros: 4_000_000, Width: 1920, Height: 1080, FPS: 30, VideoCodec: "hevc"},
		Scenes:   []domain.VideoScene{{ID: "scene-0001", StartMicros: 0, EndMicros: 4_000_000}},
		Segments: []domain.CandidateSegment{{ID: "segment-0001", SceneID: "scene-0001", StartMicros: 0, EndMicros: 4_000_000}},
		Frames: []domain.VideoUnderstandingFrameV1{{
			ID: "frame-0001", SceneID: "scene-0001", SegmentID: "segment-0001",
			TimestampMicros: 2_000_000, URL: "https://evidence.example.test/frame.jpg",
		}},
		Transcript: domain.Transcript{}, Quality: domain.QualitySignals{OverallScore: 0.7},
	}
}

func providerUnderstandingResponse() domain.VideoUnderstandingResponseV1 {
	return domain.VideoUnderstandingResponseV1{
		SchemaVersion: domain.VideoAnalysisSchemaV1,
		Candidates: []domain.VideoSegmentAnalysisV1{{
			SegmentID: "segment-0001", SceneID: "scene-0001", StartMicros: 0, EndMicros: 4_000_000,
			Description: "A person smiles toward the camera", ImportanceScore: 0.8,
			VisualQualityScore: 0.7, MemoryValueScore: 0.9, EmotionalValueScore: 0.8, StoryRelevanceScore: 0.85,
			RecommendedUsage: domain.RecommendedUsageHero, Reason: "A grounded personal interaction",
			People: []string{"person"}, Expressions: []string{"smiling"}, FrameIDs: []string{"frame-0001"},
		}},
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func jsonResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
