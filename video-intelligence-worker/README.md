# FrameMio Video Intelligence Worker

Asynchronous Go worker that converts an original video into deterministic evidence and a validated semantic `VideoAnalysis`. It does not create or render the final story video.

## Responsibility

```text
SQS analysis job
    -> load job, project, and media references from PostgreSQL
    -> download source video from S3 to isolated temporary storage
    -> inspect with FFprobe
    -> detect scene boundaries with FFmpeg
    -> plan candidate segments and timestamped frames
    -> extract and deduplicate representative frames
    -> optionally extract speech audio and transcribe it
    -> send bounded evidence to a VideoUnderstandingProvider
    -> convert validated provider output into immutable VideoAnalysis
```

Source video bytes and large derived artifacts remain in S3 or the worker's temporary directory. PostgreSQL stores records and object references. SQS contains identifiers and small configuration values only.

## Deterministic Preprocessing

Before any AI request, the worker:

- uses FFmpeg scene scores rather than fixed-interval sampling alone;
- creates scenes from exact microsecond boundaries;
- splits long scenes into bounded candidate segments;
- considers entry, midpoint, exit, boundary, and motion-sensitive quarter frames;
- adds candidates for high-frame-rate and fast-moving material;
- measures brightness, contrast, sharpness, exposure, and perceptual similarity;
- removes near-identical frames while preserving scene coverage;
- caps frame counts differently for short and long videos.

Every selected frame retains its media ID, scene ID, segment ID, exact timestamp, selection reason, and S3 key. The AI provider receives short-lived signed URLs for selected evidence, not the source video.

## Cache and Idempotency

Deterministic preprocessing uses:

```text
media ID + input version + source S3 ETag
```

Canonical completed `VideoAnalysis` lookup uses:

```text
video ID + input version + understanding version + provider + model + source S3 ETag
```

Changing evidence, provider, model, or understanding version creates a distinct analysis and preserves prior results. Deterministic preprocessing evidence is reused across semantic reanalysis. Setting `configuration.force_reanalyze` to `true` adds the stable job ID to the immutable analysis version: retries of that job remain idempotent, and a new forced job appends a new record instead of replacing earlier analysis.

Completed transcripts use an independent cache key of media ID, transcription version, and source ETag. The video worker calls a provider-neutral transcription service, so changing transcription vendors does not affect the media pipeline. Only validated, completed transcripts are cached.

The SQS consumer extends visibility while work is active, applies exponential retry visibility, persists attempt counts and progress, and records terminal failures after the configured maximum attempts. A dedicated queue and dead-letter queue are required in production.

## S3 Layout

Derived artifacts use a deterministic prefix:

```text
users/{userID}/video-analysis/{mediaID}/{analysisVersion}/{etagHash}/
    evidence.json
    frames/frame-0001.jpg
    frames/frame-0002.jpg
    audio/speech.flac

users/{userID}/transcriptions/{mediaID}/{transcriptionVersion}/{etagHash}/
    provider-response.json
```

The original media remains at the key recorded by the backend. Temporary local workspaces are removed after each attempt.

## Queue Contract

`SQS_VIDEO_ANALYSIS_QUEUE_URL` must point to a queue containing only video-analysis jobs:

```json
{
  "jobId": "job-id",
  "projectId": "project-id",
  "mediaId": "media-id",
  "jobType": "analysis",
  "configuration": {
    "input_version": "video-evidence-v1",
    "understanding_version": "video-understanding-v1",
    "force_reanalyze": "false"
  }
}
```

Do not add video bytes, images, transcripts, prompts, evidence documents, or other large payloads to this message.

## Provider Contracts

The transcription adapter implements `TranscriptionProvider`. It sends an audio or video reference rather than media bytes:

```json
{
  "model": "configured-model",
  "media_id": "media-id",
  "reference": {
    "url": "short-lived-signed-url",
    "media_type": "audio",
    "duration_micros": 42000000
  }
}
```

The provider returns timestamps in seconds:

```json
{
  "language": "en",
  "segments": [
    {
      "start": 3.2,
      "end": 6.8,
      "text": "Let's go over there.",
      "confidence": 0.84
    }
  ]
}
```

The adapter converts timestamps to exact microseconds, accepts an empty segment list as valid no-speech output, and preserves low-confidence segments from noisy audio. The service supports long media through signed references and a separately configurable provider timeout. Large raw provider JSON is stored in S3; PostgreSQL stores the canonical transcript and its provider, model, language, speech, duration, segment-count, and artifact metadata.

The video-understanding adapter implements `VideoUnderstandingProvider`. It receives structured evidence and signed representative-frame URLs, not the original video:

```json
{
  "model": "configured-model",
  "response_schema": "video-analysis.v1",
  "input": {
    "schema_version": "video-understanding-input.v1",
    "input_version": "video-evidence-v1",
    "analysis_version": "video-understanding-v1",
    "objective": "Identify the moments in this everyday iPhone video that are meaningful, interesting, visually useful, or emotionally valuable for a personal memory video.",
    "media_id": "media-id",
    "project_id": "project-id",
    "metadata": {},
    "scenes": [],
    "segments": [],
    "frames": [],
    "transcript": {},
    "quality": {}
  }
}
```

The provider returns exactly one candidate analysis for every supplied segment. Each result includes exact segment timestamps, description, importance, visual quality, memory value, emotional value, story relevance, recommended usage, reason, semantic observations, and frame/transcript grounding references.

Provider-output validation rejects unknown or missing JSON fields, invented timestamps, unknown frames, non-overlapping transcript references, ungrounded visual or speech claims, invalid scores, invalid usage values, duplicate segments, and incomplete segment coverage. The validated response is converted to a renderer-independent `VideoAnalysis` containing original-video scenes and frames, transcript, normalized segments, aggregate detections, quality signals, provenance, and an immutable version. Technical visual quality remains independent from memory value, so a shaky clip of friends laughing can outrank a technically perfect empty street.

## Configuration

Required:

- `DATABASE_URL`
- `S3_MEDIA_BUCKET`
- `SQS_VIDEO_ANALYSIS_QUEUE_URL`
- `VIDEO_UNDERSTANDING_API_URL`
- `TRANSCRIPTION_API_URL` when `ENABLE_TRANSCRIPTION=true`

AWS credentials use the default AWS SDK credential chain. API keys stay in worker secrets, never in the iOS application or queue messages.

Production deployments can either point `VIDEO_UNDERSTANDING_API_URL` at a FrameMio-compatible adapter or enable direct OpenAI-compatible video understanding with `VIDEO_UNDERSTANDING_DIRECT=true`, `VIDEO_UNDERSTANDING_PROVIDER=openai-direct`, `VIDEO_UNDERSTANDING_UPSTREAM_BASE_URL=https://api2.65535.space`, `VIDEO_UNDERSTANDING_WIRE_API=chat_completions`, and `VIDEO_UNDERSTANDING_MODEL=gpt-5.4-mini`. Transcription remains behind its provider adapter with `TRANSCRIPTION_PROVIDER=openai`, `TRANSCRIPTION_MODEL=gpt-5.4-mini-transcribe`, and `ENABLE_TRANSCRIPTION=true`.

Optional tuning:

- `AWS_REGION`
- `VIDEO_WORK_ROOT`
- `VIDEO_ANALYSIS_VERSION` (deterministic input version; default `video-v2`)
- `VIDEO_UNDERSTANDING_VERSION` (default `video-understanding-v1`)
- `FFMPEG_PATH`, `FFPROBE_PATH`
- `VIDEO_UNDERSTANDING_API_KEY`, `VIDEO_UNDERSTANDING_PROVIDER`, `VIDEO_UNDERSTANDING_MODEL`
- `VIDEO_UNDERSTANDING_DIRECT`, `VIDEO_UNDERSTANDING_UPSTREAM_BASE_URL`, `VIDEO_UNDERSTANDING_WIRE_API`
- `VIDEO_UNDERSTANDING_IMAGE_DETAIL`, `VIDEO_UNDERSTANDING_MAX_OUTPUT_TOKENS`
- `VIDEO_UNDERSTANDING_RETRIES`, `VIDEO_UNDERSTANDING_PROVIDER_TIMEOUT`
- `TRANSCRIPTION_API_KEY`, `TRANSCRIPTION_PROVIDER`, `TRANSCRIPTION_MODEL`
- `TRANSCRIPTION_VERSION`, `ENABLE_TRANSCRIPTION`
- `TRANSCRIPTION_RETRIES`, `TRANSCRIPTION_PROVIDER_TIMEOUT`
- `TRANSCRIPTION_RAW_ARTIFACT_THRESHOLD_BYTES`
- `SCENE_CHANGE_THRESHOLD`, `MIN_SCENE_DURATION`, `MAX_SEGMENT_DURATION`
- `LONG_VIDEO_THRESHOLD`, `MAX_FRAMES_SHORT`, `MAX_FRAMES_LONG`
- `MAX_SCENE_BOUNDARIES`, `DUPLICATE_HASH_DISTANCE`, `ANALYSIS_FRAME_WIDTH`
- `STAGE_RETRIES`, `MAX_JOB_ATTEMPTS`, `SQS_VISIBILITY_TIMEOUT`
- `EVIDENCE_URL_EXPIRY`

Duration values use Go duration syntax such as `750ms`, `5m`, or `2h`.

The previous `SPEECH_*` and `ENABLE_SPEECH_TO_TEXT` variables remain supported as compatibility fallbacks, but new deployments should use the `TRANSCRIPTION_*` names.

The previous `MULTIMODAL_*` and `PROVIDER_TIMEOUT` variables remain supported as compatibility fallbacks, but new deployments should use the `VIDEO_UNDERSTANDING_*` names.

For local development, copy the values into `video-intelligence-worker/.env` and load them with your shell or container runtime. This worker connects to PostgreSQL for durable records and reads the same RDS database as the backend API.

## Local Validation

Unit tests use deterministic iPhone-like metadata and scene fixtures and do not require FFmpeg:

```bash
go test ./...
go test -race ./...
go vet ./...
```

For real FFmpeg coverage, install FFmpeg and generate small representative videos:

```bash
./scripts/generate-test-videos.sh
go test ./internal/ffmpeg -run TestToolchainProcessesRepresentativeFixtures -v
```

To test private videos without copying them into the repository:

```bash
FRAME_MIO_VIDEO_FIXTURE_DIR=/path/to/videos go test ./internal/ffmpeg -run TestToolchainProcessesRepresentativeFixtures -v
```

## Container

The production image includes FFmpeg and runs as an unprivileged user:

```bash
docker build -t framemio-video-intelligence-worker .
docker run --rm --env-file .env framemio-video-intelligence-worker
```

Run this image as an independently scalable ECS/Fargate service. Do not combine it with the Go API or the Remotion render worker.
