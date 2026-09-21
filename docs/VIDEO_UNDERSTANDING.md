# Multimodal Video Understanding

## Purpose

The video-understanding layer converts deterministic video evidence into grounded, per-segment memory-value analysis. Its objective is not to summarize a video. It identifies moments that are meaningful, interesting, visually useful, emotionally valuable, or narratively relevant for a personal memory video.

This stage does not generate a `StoryPlan` and does not render video.

## Provider Boundary

`VideoUnderstandingProvider` is the only provider-specific interface:

```text
structured VideoUnderstandingInputV1
    -> VideoUnderstandingProvider
    -> strict VideoUnderstandingResponseV1
    -> schema and grounding validation
    -> provider-neutral immutable VideoAnalysis
```

OpenAI, Anthropic, Google Gemini, or another provider can implement this interface without changing the preprocessing pipeline, PostgreSQL storage, job worker, or future story logic.

The provider receives:

- FFprobe video metadata;
- deterministic scene boundaries;
- candidate segments with exact timestamps;
- selected timestamped frames through short-lived S3 URLs;
- normalized timestamped transcript segments;
- technical quality signals;
- the analysis objective and anti-invention rules.

The original video URL and bytes are not included when this evidence is sufficient.

## Provider Output

Every input candidate segment must produce exactly one `VideoSegmentAnalysisV1` containing:

- segment and scene identifiers;
- exact start and end timestamps copied from the evidence;
- description and reason;
- importance score;
- technical visual-quality score;
- memory-value score;
- emotional-value score and optional emotional tone;
- story-relevance score;
- recommended usage: `hero`, `supporting`, `context`, `audio_only`, or `exclude`;
- supported observations about people, activities, interactions, expressions, objects, food, locations, scenery, events, actions, speech, and meaningful audio;
- frame IDs and transcript-segment indexes grounding those observations.

Technical quality and memory value are intentionally separate. Camera shake, imperfect focus, or lower exposure may reduce visual quality without reducing the personal value of a genuine interaction.

## Grounding Rules

The provider must never invent people, actions, locations, events, speech, or timestamps. The service enforces the parts that can be checked deterministically:

- schema version must equal `video-analysis.v1`;
- unknown JSON fields and missing required fields are rejected;
- trailing or multiple JSON documents are rejected;
- every input segment appears exactly once;
- segment IDs, scene IDs, and timestamp bounds must exactly match input evidence;
- referenced frames must exist and belong to the same candidate segment;
- transcript indexes must exist and overlap the candidate time range;
- visual observations require frame references;
- speech and meaningful-audio observations require transcript references;
- all scores must be between zero and one;
- invalid recommended-usage values are rejected.

Semantic truth ultimately remains a provider responsibility, so the prompt instructs providers to exclude insufficiently supported segments instead of guessing.

## Persistent VideoAnalysis

The validated provider response is converted into a provider-neutral persistent `VideoAnalysis`. It contains:

- the original video ID, project ID, source duration, and processing status;
- scenes with exact original-video boundaries and representative frame IDs, timestamps, and S3 keys;
- the canonical timestamped transcript;
- normalized story-planning segments with importance, visual quality, memory value, emotional value, story relevance, recommendation, and reasons;
- aggregated detected people, activities, and locations;
- technical quality signals;
- immutable analysis, input, and understanding versions;
- provider and model provenance metadata.

The persistent shape intentionally omits provider prompts, provider response syntax, FFmpeg commands, perceptual hashes, frame-selection reasons, and transcription-provider details. A Story Planner consumes scenes, transcript, segments, detections, and quality signals without knowing which provider or preprocessing implementation produced them.

Every timestamp in `VideoAnalysis` refers to the original source video. Validation rejects out-of-duration scenes, frames, transcript segments, and candidate segments; duplicate IDs; unknown scene references; invalid scores or recommendation values; missing reasons; unsupported processing status; unnormalized aggregate labels; unknown JSON fields; and missing required fields.

## Immutable Versioning

`VideoAnalysis` is append-only. It has a creation timestamp and no update timestamp. The repository exposes insert and lookup operations but no update operation for analysis records, and PostgreSQL rejects updates to stored `video_analysis` rows.

The canonical completed analysis version is deterministically derived from:

```text
user ID + video ID + source ETag + input version + understanding version + provider + model
```

Changing the evidence version, understanding version, provider, model, or source object creates a separate record and preserves previous analyses. Forced reanalysis additionally includes the stable processing-job ID in the version. Retries of the same job resolve to the same immutable version, while a new forced job creates another version. Existing records are never overwritten.

PostgreSQL includes partial indexes for video ID, analysis version, project ID, processing status, and latest completed-analysis lookup.

## Queue Boundary

SQS messages continue to contain only job, project, media, and small version/configuration identifiers. Structured evidence, frame URLs, transcripts, model prompts, and `VideoAnalysis` JSON are never placed in SQS messages.

## Configuration

- `VIDEO_UNDERSTANDING_API_URL`
- `VIDEO_UNDERSTANDING_API_KEY`
- `VIDEO_UNDERSTANDING_PROVIDER`
- `VIDEO_UNDERSTANDING_MODEL`
- `VIDEO_UNDERSTANDING_VERSION`
- `VIDEO_UNDERSTANDING_DIRECT`
- `VIDEO_UNDERSTANDING_UPSTREAM_BASE_URL`
- `VIDEO_UNDERSTANDING_WIRE_API`
- `VIDEO_UNDERSTANDING_IMAGE_DETAIL`
- `VIDEO_UNDERSTANDING_MAX_OUTPUT_TOKENS`
- `VIDEO_UNDERSTANDING_RETRIES`
- `VIDEO_UNDERSTANDING_PROVIDER_TIMEOUT`
- `VIDEO_UNDERSTANDING_GEMINI_BASE_URL`
- `VIDEO_UNDERSTANDING_GEMINI_API_KEY`
- `TRANSCRIPTION_DIRECT`
- `TRANSCRIPTION_UPSTREAM_BASE_URL`
- `TRANSCRIPTION_API_KEY`
- `TRANSCRIPTION_PROVIDER`
- `TRANSCRIPTION_MODEL`
- `EVIDENCE_URL_EXPIRY`

Legacy `MULTIMODAL_*` and `PROVIDER_TIMEOUT` variables remain fallback aliases for existing environments.

Production deployments may either route through a FrameMio-compatible adapter using `VIDEO_UNDERSTANDING_API_URL` or call an OpenAI-compatible upstream directly with `VIDEO_UNDERSTANDING_DIRECT=true`, `VIDEO_UNDERSTANDING_PROVIDER=openai-direct`, `VIDEO_UNDERSTANDING_UPSTREAM_BASE_URL=https://api.cheaperinference.com/v1`, `VIDEO_UNDERSTANDING_WIRE_API=chat_completions`, and `VIDEO_UNDERSTANDING_MODEL=gemini-3.8-flash`. The 65535 gateway exposes this Gemini model through the OpenAI-compatible chat completions protocol, so it must not be configured as `gemini-agentic`; that provider uses Google's native Files and Interactions APIs. Transcription can use Deepgram prerecorded STT with `TRANSCRIPTION_PROVIDER=deepgram`, `TRANSCRIPTION_MODEL=nova-3`, `DEEPGRAM_API_KEY`, and optional `DEEPGRAM_BASE_URL`, or direct multipart audio upload with `TRANSCRIPTION_DIRECT=true`, `TRANSCRIPTION_PROVIDER=openai-direct`, `TRANSCRIPTION_UPSTREAM_BASE_URL=https://api.cheaperinference.com/v1`, and an authorized transcription model; `local` / `heuristic` remain development fallbacks.

Native Gemini Agentic video understanding is selected with `VIDEO_UNDERSTANDING_PROVIDER=gemini-agentic` (aliases `gemini` and `gemini_agentic`). It uses the deterministic FrameMio evidence for segment and grounding identifiers, presigns the original source video, uploads it to the Gemini Files API, waits for `ACTIVE`, and calls the Interactions API with `processing=agentic`. Set `VIDEO_UNDERSTANDING_MODEL=gemini-3.8-flash`, `VIDEO_UNDERSTANDING_GEMINI_API_KEY` (or `GEMINI_API_KEY` / `GOOGLE_API_KEY`), and a timeout of at least `900s` for long videos. Uploaded Gemini files are deleted after each analysis on a best-effort basis.
