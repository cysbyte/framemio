# FrameMio — Architecture & Engineering Reference for Codex

## 0. Purpose of this document

This document is the architectural source of truth for FrameMio.

The project has already been implemented from a series of prompts and the AWS infrastructure has already been provisioned. The current phase is **integration testing, debugging, stabilization, and correctness fixes**.

Codex must use this document as a guardrail when inspecting the repository, diagnosing failures, and making revisions.

### Primary rule

**Fix implementation problems without changing the intended architecture unless a change is demonstrably necessary and is explicitly justified.**

Do not redesign the system merely because a local implementation is inconvenient.

Before making a significant architectural change:

1. Inspect the existing repository.
2. Identify the actual failure.
3. Determine which layer owns the failure.
4. Fix the smallest responsible layer.
5. Preserve all existing valid contracts.
6. Run the relevant tests.
7. Only propose architectural changes if the current design cannot satisfy a requirement.

---

# 1. Product definition

## 1.1 Product

**FrameMio** is an iPhone-first AI memory-video application.

The product is NOT primarily:

- a general-purpose video editor
- a CapCut clone
- an AI avatar application
- an AI image generator
- a template marketplace
- a social network

The core product idea is:

> **Turn everyday iPhone photos and videos into a coherent personal memory story.**

The primary experience is:

```text
Open FrameMio
    ↓
Make My Day
    ↓
FrameMio understands the selected/relevant memories
    ↓
Finds meaningful moments
    ↓
Creates a short personal film
    ↓
User watches / lightly edits / shares
```

The product should feel like an **AI filmmaker for everyday life**, not a traditional editing timeline.

---

# 2. Core architectural philosophy

The system has strict responsibility boundaries.

```text
iPhone
  = capture / select / lightweight local intelligence / upload / playback

Go API
  = authentication / authorization / metadata / orchestration / job creation

S3
  = original media / intermediate artifacts / final videos

SQS
  = asynchronous job transport

Video Intelligence Worker
  = FFmpeg / FFprobe / scene detection / frame selection / preprocessing

Transcription
  = speech → timestamped transcript

Multimodal AI
  = semantic understanding of video evidence

VideoAnalysis
  = persistent structured representation of what was understood

Story Planner
  = decide which meaningful moments form a coherent story

Typography Planner
  = optional AI/rule-based photo text overlay decisions from StoryPlan context

StoryPlan Validator
  = deterministic validation

Render Worker
  = Remotion + FFmpeg, deterministic visual composition and text rendering

PostgreSQL
  = durable application state and structured analysis/job metadata
```

## 2.1 Critical separation

Do NOT collapse these concepts:

```text
VideoAnalysis
    ≠
StoryPlan
    ≠
RenderedVideo
```

They are separate artifacts.

### VideoAnalysis

Answers:

> What is happening in the media?

### StoryPlan

Answers:

> Which moments should be used, and in what order?

### Render

Answers:

> How should the selected story be visually composed into an MP4?

This separation is one of the most important architectural invariants.

---

# 3. Target system architecture

The intended production architecture is:

```text
                         ┌─────────────────────┐
                         │       iPhone        │
                         │      FrameMio       │
                         └──────────┬──────────┘
                                    │
                             HTTPS API calls
                                    │
                                    ▼
                         ┌─────────────────────┐
                         │        ALB          │
                         │    HTTPS / TLS      │
                         └──────────┬──────────┘
                                    │
                                    ▼
                         ┌─────────────────────┐
                         │    ECS Fargate      │
                         │       Go API        │
                         └───────┬───────┬─────┘
                                 │       │
                    ┌────────────┘       └───────────────┐
                    ▼                                    ▼
             PostgreSQL                                  SQS
                    │                         ┌───────────┴──────────┐
                    │                         ▼                      ▼
                    │               Video Intelligence         Render Worker
                    │                    Worker                  │
                    │                         │                   │
                    │                         ▼                   ▼
                    │                  FFmpeg/FFprobe          Remotion
                    │                         │                 + FFmpeg
                    │                         ▼                   │
                    │                  Transcription             │
                    │                         │                   │
                    │                         ▼                   │
                    │                 Multimodal AI               │
                    │                         │                   │
                    │                         ▼                   │
                    │                  VideoAnalysis              │
                    │                         │                   │
                    │                         ▼                   │
                    │                   Story Planner              │
                    │                         │                   │
                    │                         ▼                   │
                    │                    StoryPlan                 │
                    │                         │                   │
                    │                         ▼                   │
                    │                  StoryPlan Validator         │
                    │                         │                   │
                    └─────────────────────────┼───────────────────┘
                                              │
                                              ▼
                                             S3
                                              │
                                              ▼
                                           iPhone
```

---

# 4. Public vs private services

## Public

Only the Go API should be publicly reachable through the ALB.

Example:

```text
https://api.framemio.com
```

The iOS app talks to the Go API.

## Private

These should NOT require public HTTP endpoints:

- Video Intelligence Worker
- Render Worker
- SQS
- PostgreSQL
- private S3 objects

Workers consume jobs asynchronously.

Do not introduce an ALB for workers just to make them reachable.

---

# 5. iOS architecture

## 5.1 iPhone responsibilities

The iPhone is responsible for:

- PhotoKit media access
- user media selection
- lightweight local media intelligence
- upload orchestration
- authentication token storage
- project/status UI
- playback
- sharing
- lightweight user corrections

The iPhone is NOT responsible for:

- full video semantic understanding
- expensive scene analysis
- large-scale video frame extraction
- multimodal AI calls for video understanding
- server-side rendering
- FFmpeg-based backend processing

## 5.2 Local media intelligence

Use native frameworks where appropriate:

- PhotoKit
- Vision
- Core Image
- AVFoundation

For photos, lightweight analysis may include:

- asset ID
- timestamp
- location if available and permitted
- dimensions
- orientation
- file type
- blur/quality indicators
- brightness/exposure
- face information
- people count
- basic Vision classifications
- duplicate/near-duplicate information

For videos, collect lightweight metadata:

- asset ID
- timestamp
- location if available and permitted
- duration
- dimensions
- orientation
- FPS
- file size
- audio availability
- basic quality indicators

Do not turn the iPhone into a full video-analysis engine.

---

# 6. Media upload architecture

Large media must NOT pass through the Go API.

Correct:

```text
iPhone
   ↓
POST /media/upload-session
   ↓
Go API
   ↓
presigned S3 URL(s)
   ↓
iPhone
   ↓
direct upload
   ↓
S3
   ↓
notify Go API
```

For large videos:

- use multipart upload
- support retries
- support resumability where practical
- do not restart successfully uploaded parts
- do not store video bytes in PostgreSQL
- do not put video bytes into SQS

The backend stores references such as:

```text
mediaId
s3Key
mediaType
duration
size
checksum where available
```


## 6.1 Current iOS upload contract

The current iOS flow uses authenticated direct-S3 upload sessions:

```text
iOS
  ↓
POST /v1/media/upload-sessions
  ↓
Go API validates authenticated user and idempotency key
  ↓
Go API creates media + upload session records
  ↓
Go API returns presigned single-part or multipart URLs
  ↓
iOS uploads bytes directly to private S3
  ↓
iOS calls /v1/media/uploads/complete or /v1/media/upload-sessions/:id/complete
  ↓
Go API marks media ready
```

Important implementation details:

- upload-session creation requires `Authorization: Bearer <accessToken>`
- upload idempotency is scoped to the authenticated user
- S3 object keys are scoped under the authenticated user and media/project identifiers
- completed upload sessions must not have their presigned URLs refreshed
- stale or expired pending upload sessions may be refreshed without changing the media identity
- iOS may use byte-scoped authorization idempotency keys so a retried upload of the same local asset and byte size can converge on the existing media record

## 6.2 Source format correctness

Do not trust filename extensions or client content type as the only source of truth.

Real iPhones can produce combinations such as:

```text
filename: IMG_0111.HEIC
content type: image/heic
actual bytes: JPEG
```

The backend should preserve source metadata for traceability, but workers that decode media must be robust to mismatches. The render worker currently detects JPEG magic bytes for `.HEIC`/`.HEIF` paths and bypasses HEIC conversion when the source is already JPEG.

---

# 7. Authentication and authorization

## 7.1 Product requirement

FrameMio should not force account creation before the user can use the core experience.

The intended UX is:

```text
Install
  ↓
Open
  ↓
Make My Day
```

No mandatory sign-up wall.

## 7.2 Backend security

No hardcoded secret API key should be used as the primary security mechanism.

The app should have an authenticated application identity, initially supporting an anonymous identity.

Conceptually:

```text
First launch
   ↓
anonymous identity
   ↓
backend authentication
   ↓
access token
   ↓
API requests
```

The access token is stored securely on iOS.


## 7.2.1 Current anonymous authentication implementation

FrameMio currently implements anonymous application authentication with no visible sign-in/sign-out requirement:

```text
First launch
  ↓
iOS creates/stores anonymous installation ID in Keychain
  ↓
POST /v1/auth/anonymous { installation_id }
  ↓
Go API creates or retrieves user ios-anon-<installation_id>
  ↓
Go API returns short-lived access token + refresh token
  ↓
iOS stores tokens in Keychain
```

Current token behavior:

- access tokens are HMAC-SHA256 signed bearer tokens
- refresh tokens are HMAC-SHA256 signed bearer tokens with a distinct token type
- token validation verifies signature, expiration, subject, and token type
- authenticated user identity is attached to request context before protected route handling
- access tokens are accepted by protected public API routes
- refresh tokens are accepted only by `/v1/auth/refresh`
- refresh tokens must not authorize protected routes
- `AUTH_HMAC_SECRET` must be server-only and at least 32 characters
- `INTERNAL_API_TOKEN` is separate and is used only for worker-to-API internal callbacks

The initial HMAC implementation is a production boundary for the current self-hosted API. If FrameMio later delegates identity to an external provider, replace this boundary with a verifier such as JWKS without changing handler ownership checks.

## 7.2.2 Current iOS token behavior

The iOS API client obtains and attaches tokens automatically:

```text
API request
  ↓
KeychainAuthTokenProvider.accessToken()
  ↓
Authorization: Bearer <accessToken>
  ↓
if API returns 401, invalidate access token and retry once with a freshly issued/refreshed token
```

The app must not show Sign In or Sign Out as a prerequisite for first use. Sign in with Apple remains an identity-upgrade/account-linking feature.

## 7.3 Authorization

Every resource must be owned by an authenticated identity.

Never trust a client-supplied `userId`.

Bad:

```text
POST /projects
{
  "userId": "123"
}
```

and then trusting that value.

Correct:

```text
Authorization: Bearer <token>
             ↓
authenticated user ID
             ↓
database ownership check
```

For every project/media/job request, enforce ownership.


This applies to all user-owned domain records:

- users
- upload sessions
- media
- projects
- jobs
- PhotoAnalysis
- VideoAnalysis
- StoryPlans
- render outputs

Handlers must derive `userID` from the authenticated request context. They must never trust a request body, query string, or route parameter as proof of ownership.

Example:

```sql
SELECT *
FROM projects
WHERE id = $1
AND user_id = $authenticatedUserID;
```

## 7.4 Sign in with Apple

Sign in with Apple can later upgrade/link the anonymous identity.

Do not create duplicate identities unnecessarily.

Conceptually:

```text
anonymous user
      ↓
Sign in with Apple
      ↓
link/upgrade identity
      ↓
persistent account
```

---

# 8. API architecture

The Go API is an orchestration layer.

It should NOT perform expensive video work synchronously.

Good:

```text
POST /projects/:id/analyze
        ↓
create job
        ↓
publish SQS message
        ↓
return job ID
```

Bad:

```text
POST /projects/:id/analyze
        ↓
download video
        ↓
FFmpeg
        ↓
AI
        ↓
wait 10 minutes
        ↓
return response
```

## 8.1 Example API groups

Authentication:

```text
POST /v1/auth/anonymous
POST /v1/auth/apple
POST /v1/auth/link-apple
POST /v1/auth/logout
```

Media:

```text
POST /v1/media/upload-session
POST /v1/media
GET  /v1/media/:id
```

Projects:

```text
POST /v1/projects
GET  /v1/projects
GET  /v1/projects/:id
```

Processing:

```text
POST /v1/projects/:id/analyze
GET  /v1/projects/:id/status
POST /v1/projects/:id/render
GET  /v1/projects/:id/video
```

Exact routes may differ if the repository already established a coherent convention. Do not rename routes solely to match this document.


## 8.2 Current implemented public routes

Current public routes include both the newer iOS compatibility routes and older project/media routes. Preserve existing clients when changing them.

Authentication:

```text
POST /v1/auth/anonymous
POST /v1/auth/refresh
```

Current iOS upload/story compatibility:

```text
POST /v1/media/upload-sessions
POST /v1/media/upload-sessions/:id/complete
POST /v1/media/uploads/complete
GET  /v1/stories
POST /v1/stories
GET  /v1/stories/:id
POST /v1/stories/:id/versions
```

`POST /v1/stories/:id/versions` accepts edit preferences such as selected/removed media, style changes, `subtitlesEnabled`, and `subtitleStyle`. Subtitle preferences are stored as story-plan revision configuration (`subtitles_enabled` and `caption_style_preset`) and affect only generated `StoryPlan` captions plus renderer-safe `caption_style`; iOS never sends raw renderer commands, font paths, STT provider information, or transcript text.

## 8.3 Edit Story contract

Edit Story is an intent-based story revision flow. It must preserve this separation:

```text
Project Media ≠ Story Moments ≠ StoryPlan
```

The iOS client may request high-level changes such as add media, remove moments, replace moments, style, target duration, subtitle settings, and natural-language revision instructions. The client must not choose final story ordering, raw video trim ranges, renderer commands, internal job stages, provider IDs, or raw `StoryPlan` JSON.

Add media supports both existing Project Media and brand-new Photos selections:

```text
Photos Picker
  -> existing upload session flow
  -> Project Media record
  -> attach media to existing story project after ownership/status checks
  -> save local photo analysis when provided by iOS
  -> queue only missing media analysis for new videos
  -> publish deferred StoryPlan revision when selected media are analysis-ready
  -> StoryPlan Validator
  -> Render Worker
```

Removing or replacing a moment changes the story revision intent only; it must not delete the underlying Project Media. Existing `VideoAnalysis`, transcript, and photo analysis records are reused. Only newly added media without valid analysis should enter the media-analysis pipeline.

If a selected new video is not analysis-ready, the backend creates a queued but unpublished `STORY_PLAN` revision job marked `deferred_until_analysis=true`, queues the missing media stages through the existing SQS pipeline, and publishes the deferred revision once the selected media are ready. This avoids reanalyzing existing story media and avoids rendering from incomplete evidence.

Background music is intentionally out of scope for V1. Edit Story must not expose music controls, and story revisions must not call music providers, download music files, or attach music to media/moments. Future music should be added as a StoryPlan-level composition component, not as a property of Project Media or Story Moments.

Existing project/media routes:

```text
POST /media/upload-session
POST /media
GET  /media/:id
POST /projects
POST /projects/:id/analyze
GET  /projects/:id/status
GET  /projects/:id/story
POST /projects/:id/render
```

Internal worker routes:

```text
GET  /internal/jobs/:id/render-input
POST /internal/jobs/:id/status
POST /internal/jobs/:id/analysis
POST /internal/jobs/:id/story-plan
```

Internal routes are protected by `X-FrameMio-Internal-Token`; public routes are protected by `Authorization: Bearer <accessToken>` after anonymous authentication.

---

# 9. SQS rules

SQS carries job references, not large data.

Good:

```json
{
  "jobId": "...",
  "projectId": "...",
  "mediaId": "..."
}
```

Bad:

```json
{
  "videoBytes": "...",
  "largeTranscript": "...",
  "thousandsOfFrames": "..."
}
```

Large data belongs in:

- S3
- PostgreSQL, when it is structured and reasonably sized

SQS should contain enough information for a worker to locate the required data.


## 9.1 Current job message contract

Current job messages are identifier-only and may include bounded string configuration:

```json
{
  "jobId": "job-...",
  "projectId": "project-...",
  "mediaId": "media-...",
  "jobType": "VIDEO_RENDER",
  "configuration": {
    "pipeline_version": "async-pipeline-v1",
    "story_plan_version": "1",
    "output_preset": "premium-memory"
  }
}
```

Do not add raw media, transcripts, StoryPlan JSON blobs, frame arrays, or presigned URL lists to SQS messages. Workers must load durable state from the Go API/PostgreSQL and media/artifacts from S3.

---

# 10. Job model

Supported conceptual stages:

```text
MEDIA_PREPROCESS
TRANSCRIBE
VIDEO_UNDERSTAND
STORY_PLAN
STORY_VALIDATE
VIDEO_RENDER
```

A job should have:

```text
id
projectId
mediaId where applicable
type
status
progress
attemptCount
createdAt
startedAt
completedAt
errorCode
errorMessage
```

States:

```text
QUEUED
RUNNING
COMPLETED
FAILED
CANCELLED
```


Project-level status currently maps to the iOS user-facing flow:

```text
draft       → queued / preparing
analyzing   → Finding meaningful moments
story_ready → internal StoryPlan complete state; should normally advance to rendering
rendering   → Creating your film
ready       → playable final video is available
failed      → retry is required
```

A render job is only successful when it completes with a bounded S3 `artifact_s3_key` for the final MP4. A ready project without a completed render artifact is invalid for playback and should be logged.

## 10.1 Idempotency

Every stage must be safely retryable.

Example:

If transcription succeeds but video understanding fails:

```text
DO:
retry VIDEO_UNDERSTAND

DO NOT:
rerun transcription unnecessarily
```

If VideoAnalysis exists:

```text
reuse it when the source and analysis version are identical
```

If rendering fails:

```text
retry rendering
do not rerun AI analysis
```


Current orchestration supports this by creating a new render job for the same StoryPlan/version/preset when a prior render reached a terminal failed state and the caller supplies a new idempotency key. Rendering retries must reuse the existing StoryPlan and must not re-run VideoAnalysis, transcription, or story planning unless explicitly requested.

---

# 11. Video Intelligence Worker

The Video Intelligence Worker is responsible for turning raw video into structured evidence.

Pipeline:

```text
S3 source video
      ↓
FFprobe
      ↓
metadata
      ↓
scene detection
      ↓
candidate segments
      ↓
representative frame selection
      ↓
deduplication
      ↓
audio extraction
      ↓
transcription
      ↓
multimodal AI
      ↓
VideoAnalysis
```

## 11.1 FFmpeg / FFprobe

Use FFprobe for reliable media metadata.

Use FFmpeg for media preprocessing.

Do not duplicate media-processing logic unnecessarily between iOS and backend.

Backend video preprocessing should be the canonical processing path.


Current implementation notes:

- frame extraction seeks before opening the input (`-ss` before `-i`) for faster representative-frame extraction
- preprocessing artifacts are cached by user, media, input/analysis version, and source ETag
- VideoAnalysis is cached by user, media, input version, understanding version, provider/model, and source ETag
- production deployments use either a FrameMio-compatible provider adapter or direct OpenAI-compatible video understanding with `VIDEO_UNDERSTANDING_DIRECT=true`, `VIDEO_UNDERSTANDING_PROVIDER=openai-direct`, `VIDEO_UNDERSTANDING_UPSTREAM_BASE_URL=https://api2.65535.space`, and `VIDEO_UNDERSTANDING_MODEL=gpt-5.4-mini`; `heuristic-v1` is a development fallback only
- transcription uses the replaceable backend provider abstraction; production can use Deepgram prerecorded STT with `TRANSCRIPTION_PROVIDER=deepgram`, `TRANSCRIPTION_MODEL=nova-3`, `DEEPGRAM_API_KEY=...`, optional `DEEPGRAM_BASE_URL`, and `ENABLE_TRANSCRIPTION=true`, or an upstream/OpenAI-compatible adapter with an authorized transcription model, while still falling back to visual-only evidence when the provider is unavailable
- force reanalysis should reuse deterministic preprocessing where possible and append a new semantic analysis version rather than overwriting old records

## 11.2 Frame selection

Do not rely exclusively on fixed-interval sampling.

Candidate frame selection should consider:

- scene boundaries
- action changes
- people entering/leaving
- environmental changes
- visually distinct frames
- useful timestamps
- redundancy

The objective is:

```text
high information
+
low redundancy
+
reasonable AI cost
```

---

# 12. Transcription

Transcription is a replaceable service.

Create a provider abstraction such as:

```text
TranscriptionProvider
```

Output should contain timestamped segments:

```json
{
  "segments": [
    {
      "start": 3.2,
      "end": 6.8,
      "text": "Let's go over there."
    }
  ]
}
```

The current backend provider choices are the FrameMio-compatible HTTP adapter, OpenAI-compatible multipart audio upload, and Deepgram prerecorded STT. Deepgram maps `results.utterances[].start/end/transcript` into FrameMio timestamped transcript segments and uses detected language from channel metadata when present. The rest of the application must not depend on one specific transcription provider.

No transcript should be placed in SQS.


## 12.1 Transcription failure behavior

Transcription is useful evidence, but it must not be a single point of failure for visual memories.

Current behavior:

```text
if video has audio and transcription succeeds:
    include timestamped transcript in VideoEvidence
else if transcription fails or times out:
    log a warning
    continue with visual-only evidence
```

This preserves the pipeline boundary: the Video Intelligence Worker may still create `VideoAnalysis` from frames, scenes, metadata, and quality signals when speech evidence is unavailable.

---

# 13. Multimodal video understanding

The multimodal AI layer answers:

> What is happening in this video, and which moments are meaningful for a personal memory story?

It should analyze evidence such as:

- timestamped frames
- scene boundaries
- transcript
- metadata
- quality signals

Potential providers can be changed later.

Do not tightly couple the rest of the application to a specific provider or model.

## 13.1 Important distinction

The model is NOT merely asked:

> Summarize this video.

It should identify candidate moments.

A candidate segment should contain:

```text
start
end
description
importance
visualQuality
emotionalValue
storyRelevance
recommended
reason
```

## 13.2 Memory value vs technical quality

This is a core product rule.

A technically shaky clip can be more valuable than a perfect shot.

Example:

```text
friends laughing with camera shake
```

may be more important than:

```text
perfectly stable empty street
```

The AI should preserve human/memory significance.

## 13.3 Hallucination control

The model must not invent:

- people
- actions
- locations
- events
- timestamps

Only return information supported by the supplied evidence.

Validate AI output against a strict schema.

---

# 14. VideoAnalysis

VideoAnalysis is a persistent, versioned artifact.

It should contain concepts such as:

```text
videoId
analysisVersion
provider
model
duration
scenes
transcript
segments
detectedPeople
detectedActivities
detectedLocations
qualitySignals
```

A segment:

```text
segmentId
start
end
description
importance
visualQuality
emotionalValue
storyRelevance
recommended
reasons
```

## 14.1 Immutability

Do not silently overwrite old analysis.

If analysis is rerun with a new model/version:

```text
VideoAnalysisV1
VideoAnalysisV2
...
```

The StoryPlan must explicitly reference the analysis version it used.

---

# 15. Story Planner

The Story Planner is an editorial layer.

It receives structured analysis.

It does NOT analyze raw video.

Inputs can include:

- PhotoAnalysis
- VideoAnalysis
- media clusters
- timestamps
- locations
- user-selected media
- requested duration
- story type
- visual style
- optional user instruction

Its job is:

> Decide which meaningful moments form the best coherent memory story.

Possible story types:

```text
Today
Weekend
Trip
Birthday
Family
Friends
Pet
Event
Custom
```

## 15.1 Story selection principles

Prefer:

- meaningful people
- interactions
- emotional moments
- memorable activities
- visually strong scenes
- variety
- coherent pacing

Avoid:

- repeated clips
- repeated photos
- technically perfect but meaningless footage
- excessive transitions
- unnecessary text

## 15.2 Chronology

Chronological ordering is the default for everyday memories, but it is not an absolute rule.

A stronger narrative may justify a different order.

## 15.3 Photo typography planning

Photo typography is part of Story planning, not a separate rendering timeline. The implemented flow is:

```text
PhotoAnalysis + VideoAnalysis + Story context
    ↓
Story Planner selects ordered moments
    ↓
Typography Planner receives the StoryPlan-shaped moment context
    ↓
TextOverlay is attached to StoryPlan.moments[].typography
    ↓
StoryPlan Validator validates the overlay deterministically
    ↓
Render Worker executes the validated StoryPlan
```

The Typography Planner may be local/rule-based or an external OpenAI-compatible provider selected by backend configuration. It must return structured JSON only. Backend validation owns enum support, text length limits, timing clamping, safe margins, max width, source metadata, and confidence bounds. AI output must never become renderer code, CSS, font paths, ImageMagick commands, FFmpeg arguments, or a second timeline.

Typography is optional. The planner should not add text to every photo and should prefer short cinematic/editorial language only when it improves the memory film. It must avoid invented locations, dates, names, events, hashtags, memes, and literal duplicate captions. If typography planning fails or returns invalid output, Story generation continues without typography. The local rule-based fallback can also derive conservative titles from reliable photo metadata such as cluster label, location, or bounded analysis labels, so photos without captions/descriptions can still receive typography when their metadata is meaningful.

Current backend configuration:

```text
TYPOGRAPHY_ENABLED=true|false              default true
TYPOGRAPHY_PROVIDER=local|disabled|openai  default local
TYPOGRAPHY_BASE_URL=...                    defaults to OPENAI_BASE_URL or https://api.openai.com
TYPOGRAPHY_API_KEY=...                     defaults to OPENAI_API_KEY for external providers
TYPOGRAPHY_MODEL=...                       required for external providers
TYPOGRAPHY_WIRE_API=responses|chat_completions
TYPOGRAPHY_MAX_OUTPUT_TOKENS=1400
TYPOGRAPHY_TIMEOUT=12s
```

These are server/worker-side settings only. Provider keys and model names must not be exposed to iOS.

---

# 16. StoryPlan

StoryPlan is a structured, renderer-independent artifact.

It should specify:

- selected assets
- exact video time ranges
- order
- clip duration
- transitions
- future composition extensions
- text/captions
- aspect ratio
- target duration
- pacing/style

Every selected video segment must reference:

```text
mediaId
VideoAnalysis version
valid start/end timestamps
```

Never invent an asset or timestamp.

StoryPlan must not contain renderer-specific implementation details unless they are part of a deliberately stable rendering contract.

Scene transitions are renderer-stable intent. `cut` remains an immediate switch, same-source `soft_cut` normalizes to a cut in the renderer, and cross-media `fade` / `crossfade` / `dissolve` produce short fade-out/fade-in transitions without changing selected clip timestamps.


## 16.1 Current StoryPlan schema contract

Current schema version is `story_plan_v1`.

Top-level fields include:

```text
id
project_id
version
requested_duration_ms
story_type
visual_style
title_text
ending_text
user_instructions
moments
captions
caption_style
audio
output
total_duration_ms
```

Each `StoryMoment` is the renderer-stable selection unit:

```text
moment_id
asset_id
media_id
media_type
video_analysis_version
start_micros
end_micros
narrative
caption
duration_ms
crop_intent
transition
reason
score
typography
```

Photo moments must have positive `duration_ms` and no source timestamps. Video moments must have `start_micros`, `end_micros`, a matching `duration_ms`, and a valid `video_analysis_version`.

`moments[].typography` is an optional renderer-stable `TextOverlay` for cinematic photo typography. It is separate from speech captions/transcript subtitles and should normally be attached only to photo/live-photo moments. Current fields are:

```text
enabled
type              moment_title | moment_caption | location | date | chapter_title | story_intro | story_outro
title
subtitle
style             cinematic | minimal | editorial | memory | location
placement         top_left | top_center | top_right | center_left | center | center_right | bottom_left | bottom_center | bottom_right
start_ms
duration_ms
max_width
safe_area         normalized top/right/bottom/left margins
animation         fade | fade_up | fade_down | subtle_scale | none
source            ai | user | system
reason
confidence
```

The backend clamps V1 typography text and layout using centralized domain limits: title max 40 characters, subtitle max 100 characters, reason max 240 characters, default max width 0.72, default safe margin 0.08, and default start time 350 ms. Unsupported enum values or unusable timings are rejected before rendering.

`caption_style` is optional renderer-safe styling metadata for captions/subtitles. It may include whitelisted presets such as `bold_social`, `cinematic_subtitle`, `clean_minimal`, and `playful_pop`, plus validated fields for font family, font size, text/stroke/background colors, background opacity, position, bottom offset, max lines, and max characters per line. It must not contain raw FFmpeg/Remotion commands, arbitrary font file paths, provider information, or transcript source details.

## 16.2 V1 audio and future background music

FrameMio V1 does not provide background music. Story creation, Edit Story, Story Planner, workers, Remotion, FFmpeg, S3, SQS, and public APIs must not require or call ElevenLabs, a music provider, a music catalog, music generation jobs, music previews, background music downloads, audio ducking, or a music mixer.

Original source-video audio remains supported. `StoryPlan.audio` is optional and, in V1, is limited to source-audio intent such as:

```text
source            original | none
original_audio    whether source video audio is preserved
original_volume   source audio volume, default 1.0
```

If `audio` is omitted, the renderer should preserve original video audio by default. `source=none` or `original_audio=false` may be used only when the story intentionally disables all source audio.

Future background music should be introduced as an optional StoryPlan-level composition feature:

```text
StoryPlan
  + future music section
  -> Render Worker
  -> Audio Mixer
  -> Remotion / FFmpeg
```

Future music must belong to the overall Story composition, not to Media, Moment, Photo, or VideoSegment records. Adding future music must not require redesigning Story, StoryVersion, Moment, Media, Add Media, Edit Story, VideoAnalysis, or Transcript relationships.

---

# 17. StoryPlan Validator

Before rendering, validate deterministically.

Check:

1. Asset exists.
2. Asset belongs to the authenticated user/project.
3. VideoAnalysis version exists.
4. Timestamp is within source duration.
5. `start < end`.
6. S3 object exists or is resolvable.
7. Total duration is within allowed range.
8. Aspect ratio is supported.
9. Text lengths are valid.
10. Audio configuration is valid.
11. Schema is valid.
12. Optional typography overlays use supported enums, safe timing, safe margins, and bounded text.

The validator must NOT use AI.

Invalid StoryPlans must never reach rendering.


Current validator behavior includes:

- project exists and belongs to authenticated user
- referenced media belongs to the project
- duplicate moment references are rejected for identical photo or identical video range references
- video moment timestamps must be inside the source duration
- video `duration_ms` must match `end_micros - start_micros`
- video moments must reference an existing VideoAnalysis version
- photo moments must not define video timestamps
- optional moment typography is validated with the same deterministic `TextOverlay` rules used for AI output
- referenced S3 objects must be resolvable before rendering

---

# 18. Render Worker

The Render Worker is deterministic.

Technology:

- Node.js
- TypeScript
- Remotion
- FFmpeg
- S3

It receives:

```text
Validated StoryPlan
+
source media
```

and produces:

```text
final MP4
```

The renderer does NOT:

- analyze video
- call multimodal AI
- choose story moments
- change the StoryPlan
- decide which media is important

## 18.1 Supported output

Initial aspect ratios:

```text
9:16
1:1
16:9
```

Primary FrameMio output:

```text
9:16
```

## 18.2 Rendering features

Photos:

- pan
- zoom
- Ken Burns-style motion

Videos:

- precise trimming
- crop
- scale
- optional speed adjustment
- audio handling

Transitions:

- cut
- fade
- dissolve

Audio:

- preserves original source video audio by default
- does not download or mix background music in V1
- does not apply music ducking in V1

Text:

- title
- captions
- optional photo typography from `moments[].typography`
- optional chapter text
- ending

Photo typography is rendered by Remotion when `RENDERER_MODE=remotion` using the checked-in Remotion entrypoint. The renderer applies controlled typography presets, safe-area placement, restrained fade/fade-up/fade-down/subtle-scale animation, text wrapping, shadows, and subtle gradient readability treatments. The renderer must not call AI and must not decide text content.

The FFmpeg fallback renderer keeps a deterministic `drawtext` path for environments not yet using Remotion mode, but `TextOverlay` remains the single source of truth in StoryPlan.


## 18.3 Current render-worker contract

The render worker currently:

1. receives a `VIDEO_RENDER` SQS message
2. calls `GET /internal/jobs/:id/render-input`
3. loads the validated StoryPlan and source media references
4. downloads source assets from private S3
5. trims video moments to exact StoryPlan source ranges
6. applies deterministic Ken Burns-style motion to photo moments
7. builds a deterministic composition
8. renders/encodes an MP4 with FFmpeg/Remotion-compatible composition logic, including optional typography overlays
9. uploads the final MP4 to the configured S3 key
10. calls `POST /internal/jobs/:id/status` with `COMPLETED` and `artifact_s3_key`

The renderer must not call AI, must not call ElevenLabs/music providers, and must not choose or reorder story moments. It executes the StoryPlan.

## 18.4 Render input resilience

The renderer must be resilient to common iPhone media realities:

- vertical videos and photos
- HEIC/HEIF photo filenames
- photo files whose extension/content type does not match the actual bytes
- multiple selected ranges from the same source video
- photo-only, video-only, and mixed photo/video StoryPlans

When a `.HEIC` or `.HEIF` source contains JPEG bytes, the renderer should use the source image directly rather than failing HEIC conversion.

## 18.5 Photo derivatives

For the first public publish, server-side ImageMagick photo preprocessing is intentionally disabled. The Go backend does not queue photo `MEDIA_PREPROCESS` jobs, story planning does not wait for photo derivative artifacts, and render input uses the original uploaded photo S3 object.

Original source photos remain immutable in S3. Any future normalized derivative or static preview feature must write separate artifacts only and must not burn AI typography into source photos. Final animated typography belongs to Remotion/renderer execution of `StoryPlan.moments[].typography`.

The video-intelligence worker image no longer installs ImageMagick and no runtime `MAGICK_PATH` configuration is required. Reintroducing photo derivatives should be feature-flagged and must not block StoryPlan creation or final rendering if derivative generation fails.

---

# 19. Rendering performance

Rendering can be expensive.

Do not solve slow rendering by moving rendering into the synchronous Go API.

Correct:

```text
API
 ↓
SQS
 ↓
Render Worker
 ↓
Remotion/FFmpeg
```

The user sees asynchronous progress.

Rendering workers can later scale horizontally based on queue depth.

If rendering is slow, first investigate:

- unnecessary re-encoding
- source resolution
- frame rate
- composition complexity
- unnecessary asset preprocessing
- FFmpeg parameters
- worker CPU/memory
- downloading the same asset repeatedly
- redundant intermediate files

Do not replace Remotion with another architecture merely because one render is slow unless benchmarking demonstrates the need.

---

# 20. AWS deployment

Intended deployment:

```text
Route 53
   ↓
ALB
   ↓
ECS Fargate Go API
```

Workers:

```text
ECS Fargate Video Worker
ECS Fargate Render Worker
```

Messaging:

```text
SQS
```

Storage:

```text
S3
```

Database:

```text
PostgreSQL
```

Secrets:

```text
AWS Secrets Manager
```

Logs:

```text
CloudWatch
```

## 20.1 Public endpoint

Only the API needs a public HTTPS endpoint.

Example:

```text
https://api.framemio.com
```

Workers should remain private.


## 20.2 Worker activation and manual scaling

Current AWS operation supports both queued workers and one-off ECS task activation:

- backend orchestration publishes identifier-only SQS messages
- when configured, the Go API can start one-off ECS Fargate worker tasks for video, story planning, and rendering stages
- long-running worker services can also be manually scaled with scripts, but normal production worker deploys keep `desiredCount=0` and backend `RunTask` activation starts one-off tasks when jobs arrive

Current helper scripts:

```text
backend/scripts/run-workers.sh   # scale video + render worker services up
backend/scripts/stop-workers.sh  # scale video + render worker services to 0
```

`desiredCount` is the ECS service's target number of running tasks. Setting `DESIRED_COUNT=2 backend/scripts/run-workers.sh` should request two running tasks for each configured worker service. Scaling services to 0 is acceptable for cost control, but queued jobs will not make progress unless either one-off ECS activation is enabled or a worker service/task is running.

---

# 21. Security requirements

Never put these in the iOS app:

- AWS secret access keys
- database passwords
- server-side AI provider secrets
- Stripe/other server secrets
- privileged internal API keys

The iPhone receives only what it needs.

S3 buckets containing user media must remain private.

Use presigned URLs for controlled upload/download.

Every API resource must enforce ownership.

Every expensive operation must be rate limited.


Expensive public operations include:

- creating iOS stories
- creating upload sessions
- starting analysis
- starting render/re-render

Current per-user rate limiting is enforced for story creation, analysis, and render/re-render. Upload-session quotas should also remain protected because direct S3 upload authorization creates storage and bandwidth cost.

Rate limits and quotas must be applied per authenticated user, not per client-provided user identifier.

Especially protect:

```text
upload-session
analyze
render
```

because they create infrastructure/AI cost.

---

# 22. Database principles

PostgreSQL stores structured state.

Store:

- users
- projects
- media metadata
- jobs
- analysis metadata
- VideoAnalysis structured data
- StoryPlan
- render records
- ownership relationships

Do not store large raw videos in PostgreSQL.

Do not use PostgreSQL as a replacement for S3.

Use S3 for:

- source videos
- source photos when uploaded
- frame artifacts
- audio artifacts
- large analysis artifacts
- rendered videos

---

# 23. End-to-end project lifecycle

A typical project should follow:

```text
1. User opens FrameMio

2. iOS silently obtains anonymous application authentication

3. User chooses "Make My Day"

4. iOS identifies selected/candidate photos and videos

5. iOS performs lightweight local analysis

6. iOS requests authenticated upload sessions

7. iOS uploads media directly to private S3

8. Go API creates project and stage jobs

9. SQS distributes identifier-only jobs

10. Video Worker preprocesses videos; photo evidence comes from uploaded media plus lightweight local analysis

11. Transcription runs when useful and falls back to visual-only evidence when unavailable

12. Multimodal AI creates VideoAnalysis for videos

13. Story Planner combines PhotoAnalysis, VideoAnalysis, metadata, and clusters into StoryPlan

14. StoryPlan Validator validates deterministically

15. Render Worker renders mixed photo/video MP4 from the validated StoryPlan

16. Final video is uploaded to S3

17. iPhone receives completion status and presigned playback URL

18. User watches video

19. User can:
    - regenerate
    - remove moment
    - replace moment
    - make shorter
    - make longer
    - share
```

---

# 24. Regeneration rules

This is important for cost and reliability.

## If user says "regenerate story"

Do NOT necessarily rerun video understanding.

Use existing:

```text
VideoAnalysis
```

and create a new:

```text
StoryPlan
```

## If user says "render again"

Do not rerun:

- video analysis
- transcription
- story planning

Use the existing validated StoryPlan.


If a render failed after a valid StoryPlan was stored, retry rendering only. The retry should create/reuse a render job for the same story plan version and output preset, then execute the render worker. Do not restart upload, analysis, transcription, or planning for this failure class.

## If the user explicitly asks to reanalyze

Then create a new analysis version.

---

# 25. Debugging rules for Codex

When a test fails, follow this order.

## Step 1 — Identify the layer

Classify the failure as one of:

```text
iOS
API
Authentication
Authorization
S3
SQS
PostgreSQL
Video preprocessing
Transcription
AI provider
VideoAnalysis
Story Planner
StoryPlan validation
Rendering
Deployment/networking
```

## Step 2 — Reproduce

Do not guess.

Find:

- exact error
- request
- response
- relevant logs
- job state
- AWS resource state
- database state

## Step 3 — Trace the contract

Ask:

```text
What does the upstream layer produce?
What does the downstream layer expect?
```

Many integration errors are contract mismatches.

Check:

- JSON shape
- IDs
- timestamps
- enum values
- authentication
- S3 keys
- queue message format
- database schema
- API response shape

## Step 4 — Fix the owner

Fix the layer responsible for violating the contract.

Do not add random compatibility code everywhere.

## Step 5 — Retest

Run the smallest relevant test first.

Then run integration tests.

Then run the complete pipeline if necessary.

---

# 26. Common wrong directions to avoid

## Do not make Go API process videos synchronously

Wrong:

```text
HTTP request
 ↓
FFmpeg
 ↓
AI
 ↓
Remotion
 ↓
HTTP response
```

Correct:

```text
HTTP
 ↓
job
 ↓
SQS
 ↓
worker
```

## Do not upload videos through Go

Wrong:

```text
iPhone
 ↓
Go API
 ↓
S3
```

Correct:

```text
iPhone
 ↓
presigned URL
 ↓
S3
```

## Do not put large objects into SQS

Wrong:

```text
SQS
 ↓
video bytes
```

Correct:

```text
SQS
 ↓
mediaId / jobId / projectId
 ↓
S3
```

## Do not perform full video understanding on iPhone

Wrong:

```text
iPhone
 ↓
large frame extraction
 ↓
multimodal AI
```

Correct:

```text
iPhone
 ↓
lightweight metadata
```

then:

```text
S3
 ↓
Video Worker
 ↓
AI
```

## Do not let AI directly control Remotion

Wrong:

```text
AI
 ↓
arbitrary Remotion code
```

Correct:

```text
AI
 ↓
structured StoryPlan
 ↓
validator
 ↓
Remotion
```

## Do not mix VideoAnalysis and StoryPlan

VideoAnalysis describes evidence.

StoryPlan makes editorial decisions.

## Do not make every worker publicly accessible

Only the Go API needs the public API endpoint.

---

# 27. AI provider abstraction

AI providers must be replaceable.

Use interfaces similar to:

```text
VideoUnderstandingProvider
TranscriptionProvider
StoryPlanningProvider
```

Provider-specific request/response logic stays inside adapters. Video understanding, transcription, and story planning use separate replaceable provider interfaces; StoryPlan and render worker contracts reference FrameMio domain IDs and storage keys, not provider response types. Background music is a future StoryPlan-level composition extension and is not active in V1.

The core domain model must not depend on:

- one vendor
- one model name
- one SDK response shape

Store provider/model/version metadata with AI-generated artifacts.

---

# 28. Versioning

Version all important contracts.

Examples:

```text
VideoAnalysisV1
StoryPlanV1
RenderSchemaV1
```

Also track:

```text
analysisVersion
promptVersion
model
rendererVersion
```

This is essential because AI output changes over time.

A render should be reproducible from:

```text
StoryPlan version
+
source asset versions
+
renderer version
```

---

# 29. Observability

Every asynchronous operation should have a traceable ID.

At minimum:

```text
requestId
projectId
jobId
mediaId
```

Logs should make it possible to trace:

```text
iPhone request
 ↓
API request
 ↓
job creation
 ↓
SQS message
 ↓
worker execution
 ↓
VideoAnalysis
 ↓
StoryPlan
 ↓
render
 ↓
final S3 object
```

Avoid logging:

- authentication tokens
- AWS secrets
- AI API keys
- unnecessary private user content

---

# 30. Error handling

Errors should be categorized.

Example:

```text
AUTHENTICATION_ERROR
AUTHORIZATION_ERROR
UPLOAD_ERROR
S3_ERROR
QUEUE_ERROR
MEDIA_DECODE_ERROR
TRANSCRIPTION_ERROR
AI_PROVIDER_ERROR
AI_SCHEMA_ERROR
STORY_PLAN_ERROR
VALIDATION_ERROR
RENDER_ERROR
```

The iOS app should receive user-friendly states.

Do not expose raw infrastructure errors to the user.

For example, instead of:

```text
NoSuchKey: s3://bucket/...
```

show:

```text
We couldn't access one of your videos. Please try again.
```

But keep the detailed error in backend logs.

---

# 31. Testing strategy

Test at four levels.

## Unit tests

Test:

- domain logic
- validators
- parsers
- StoryPlan validation
- ownership checks
- timestamp validation

## Integration tests

Test:

```text
Go API
 ↔ PostgreSQL
 ↔ S3
 ↔ SQS
```

and worker components.

## Media pipeline tests

Use a small fixed set of real representative videos:

- short video
- long video
- silent video
- speech video
- multiple scenes
- static scene
- fast motion
- low-quality footage
- portrait iPhone video

## End-to-end test

Test:

```text
upload
 ↓
preprocess
 ↓
transcribe
 ↓
understand
 ↓
story plan
 ↓
validate
 ↓
render
 ↓
download/play
```

---

# 32. Definition of "fixed"

A bug is not fixed merely because one exception disappeared.

A fix is complete when:

1. Root cause is understood.
2. The responsible layer is corrected.
3. The relevant contract remains valid.
4. Regression test exists where appropriate.
5. Retry/idempotency behavior remains correct.
6. No unrelated architecture was changed.
7. The end-to-end pipeline still works.

---

# 33. Codex operating procedure

When asked to modify FrameMio, Codex should first:

```text
1. Inspect repository structure.

2. Inspect existing architecture and implementation.

3. Inspect relevant tests.

4. Inspect environment/configuration names.

5. Inspect AWS integration code.

6. Identify the failing layer.

7. Compare implementation against this document.

8. Fix the smallest responsible component.

9. Preserve existing API/database/message contracts unless they are demonstrably wrong.

10. Add/update tests.

11. Run targeted tests.

12. Run broader tests.

13. Report:
    - root cause
    - files changed
    - why the fix is correct
    - tests executed
    - remaining issues
```

Do not blindly rewrite working code.

---

# 34. Architecture change policy

A proposed architecture change must answer:

### What is currently broken?

### Why can't the existing architecture solve it?

### What component should own the new responsibility?

### What contracts change?

### What migration is required?

### What are the operational/cost implications?

### What tests prove the new design works?

If those questions cannot be answered, prefer a local implementation fix.

---

# 35. Current project priority

The current priority is NOT adding large new features.

Priority order:

```text
1. Make the existing architecture correct.

2. Make the end-to-end pipeline reliable.

3. Fix integration contracts.

4. Make uploads reliable.

5. Make async jobs reliable.

6. Make AI outputs schema-safe.

7. Make StoryPlan deterministic and valid.

8. Make rendering reliable.

9. Improve performance.

10. Improve UX.

11. Add additional product features.
```

Do not expand the feature surface while the core pipeline is unstable.

---

# 36. Product-level success criteria

The first meaningful milestone is:

```text
Real iPhone photos + videos
       ↓
FrameMio anonymous auth
       ↓
authenticated presigned S3 upload
       ↓
Video Intelligence for videos + PhotoAnalysis for photos
       ↓
VideoAnalysis
       ↓
StoryPlan with selected photo moments and exact video ranges
       ↓
deterministic Validator
       ↓
Render Worker / Remotion / FFmpeg
       ↓
final MP4 in private S3
       ↓
presigned iPhone playback
```

The system should successfully do this repeatedly.

After that, optimize:

- processing speed
- AI cost
- render time
- battery use
- upload reliability
- memory usage
- user experience

---

# 37. Final guardrails for Codex

Before making a change, ask:

1. **Which architectural layer owns this responsibility?**
2. **Am I fixing the root cause or masking a symptom?**
3. **Am I accidentally moving server work onto iOS?**
4. **Am I accidentally making the Go API synchronous?**
5. **Am I putting large data into SQS/PostgreSQL/API requests?**
6. **Am I bypassing S3 for large media?**
7. **Am I mixing VideoAnalysis with StoryPlan?**
8. **Am I letting AI directly control rendering?**
9. **Am I breaking idempotency?**
10. **Am I creating a public endpoint unnecessarily?**
11. **Am I introducing a provider-specific dependency into the core domain?**
12. **Can this be fixed with a smaller change?**
13. **What regression test proves the fix?**

If the answer to any of these indicates architectural drift, stop and reconsider the implementation.

---

# 38. One-sentence architecture summary

**FrameMio is an iPhone-first AI memory filmmaker where the iPhone performs lightweight media intelligence, S3 stores media, Go orchestrates authenticated asynchronous jobs, workers perform video preprocessing and AI understanding, VideoAnalysis represents evidence, StoryPlan represents editorial decisions, and Remotion/FFmpeg deterministically renders the final film.**
