# FrameMio Architecture

## Goal

FrameMio turns a user’s iPhone photos and videos into a short, automatically edited memory story. The repository separates mobile capture and presentation, API orchestration, asynchronous video intelligence, asynchronous rendering, and cloud infrastructure so each responsibility can evolve independently.

## Repository Boundaries

### `/ios`

Native iPhone application built with Swift and SwiftUI.

- Uses **PhotoKit** to request permission and read user-selected photos and videos.
- Uses **Vision** for on-device visual signals such as people, faces, objects, and scene observations where appropriate.
- Uses **AVFoundation** for media inspection, playback, thumbnails, and local composition support.
- Presents the Create Story, Today, Weekend, Trip, selection, preview, and save/share flows.
- Uploads only the media or derived metadata required by an explicit story request.
- Polls or receives story status updates and presents the generated video.

The iOS client owns user experience and local media access. It does not own long-running cloud rendering.

### On-Device Media Intelligence

The iOS layer performs only inexpensive, resumable signals before a story request:

- PhotoKit metadata such as identifiers, dates, dimensions, locations when available, Live Photo state, and resource UTI.
- Small thumbnail analysis through Vision and Core Image for faces, people count, basic classifications, brightness, exposure, blur/sharpness, and perceptual hashes.
- AVFoundation metadata inspection for video duration, transformed dimensions, frame rate, audio presence, and local file size when available.
- Near-duplicate clustering from perceptual hashes, date proximity, and optional location proximity.

The device never performs full semantic video understanding, speech transcription, large-scale video frame extraction, or multimodal AI calls. Each completed result is persisted locally so a cancelled or backgrounded pass can resume without reprocessing completed assets. The upload boundary is represented by `MediaSelection`: explicitly selected assets are always eligible, while automatically discovered assets are eligible only when the automatic-memory feature is enabled. The entire photo library is never uploaded by this layer.

### `/backend`

Go REST/JSON API responsible for product orchestration and durable application state.

- Authenticates users and authorizes access to their stories and media.
- Creates idempotent direct-S3 upload sessions and validates selected media references.
- Persists media metadata, projects, processing jobs, analysis records, versioned StoryPlans, progress, errors, and output references in PostgreSQL.
- Runs the renderer-independent Story Planner over `PhotoAnalysis`, `VideoAnalysis`, and media clusters to assemble strict `StoryPlan` JSON for later rendering.
- Publishes identifier-only analysis and render jobs to AWS SQS.
- Accepts authenticated worker callbacks for analysis, StoryPlan, progress, errors, and completed render artifacts.
- Exposes project status, StoryPlans, and completed-video S3 references to the iOS client.
- Accepts story edit preferences for subtitle enablement and whitelisted subtitle style presets, then translates them into StoryPlan revision configuration.

The Go API must remain request-oriented. It must not perform long-running video rendering, invoke Remotion directly in the request path, or block HTTP requests while waiting for FFmpeg.

### `/video-intelligence-worker`

Dedicated Go worker for asynchronous semantic video understanding.

- Polls a dedicated AWS SQS video-analysis queue containing identifiers and small configuration values only.
- Loads authorized job, project, and media records from PostgreSQL and source media from S3.
- Uses FFprobe for technical metadata and FFmpeg for scene detection, representative frames, and optional speech audio extraction.
- Calls a provider-independent transcription service that accepts S3-backed audio or video references, preserves timestamped segments, retries transient failures, and caches completed transcripts by media, version, and source ETag.
- Selects deterministic, timestamp-preserving evidence using scene boundaries, candidate segments, quality signals, motion-aware candidates, and perceptual duplicate removal.
- Sends only bounded evidence and short-lived artifact URLs to configured transcription and `VideoUnderstandingProvider` implementations; it does not send the original video when evidence is sufficient.
- Validates strict, grounded provider output, converts it into provider-neutral immutable `VideoAnalysis`, and appends versioned PostgreSQL records without destroying earlier analyses.
- Stores frames, audio, and evidence documents in S3 and reuses them for the same media, source ETag, and analysis version.
- Stores canonical transcript metadata in PostgreSQL and large raw transcription artifacts in S3; transcripts never appear in SQS messages.

This worker never renders the final story video. It scales independently from both the request-oriented API and the Node.js render worker.

### `/render-worker`

Dedicated Node.js worker for asynchronous video generation.

- Polls AWS SQS for render jobs.
- Loads the referenced `StoryPlan` and source assets.
- Builds the visual composition with Remotion.
- Uses FFmpeg for media normalization, audio handling, encoding, and final output.
- Uploads the completed video and render metadata to S3.
- Reports success or failure to the Go API through an internal callback or authenticated job-status endpoint.
- Retries safely and records enough state to avoid duplicate finalization.

The worker is the only component responsible for long-running video rendering. It scales independently from the API service.

### `/infrastructure`

Deployment and local-environment definitions.

- Dockerfiles and local orchestration for the API, video intelligence worker, and render worker.
- AWS ECS/Fargate service definitions for independently scaling API and worker workloads.
- ECR image configuration.
- S3 buckets for source media, intermediate artifacts, and finished videos.
- SQS processing queue and dead-letter queue for analysis and render jobs.
- PostgreSQL/RDS networking, credentials, backups, and migrations.
- IAM policies, secrets, logging, and environment configuration.

Infrastructure should make the API and worker separate deployable services even when they share a repository.

## Core Data Flow

```text
iPhone media selection
    │
    ├─ PhotoKit / Vision / AVFoundation
    │
    ▼
Go API ── creates upload session + signed URLs ──┐
    │                                             │
    ├─ stores metadata + S3 references ─ PostgreSQL│
    │                                             ▼
    │                                      Direct S3 upload
    │                                             │
    ├─ receives completion notification ◄─────────┘
    │
    ├─ creates video ANALYSIS jobs ── SQS
    │                                      │
    │                                      ▼
    │                         Go video intelligence worker
    │                         FFprobe + FFmpeg + providers
    │                                      │
    │                    S3 evidence + PostgreSQL VideoAnalysis
    │                                      │
    ├─ creates and persists versioned StoryPlan ◄──────────────┘
    │
    └─ creates RENDER job ─── SQS
                               │
                               ▼
                     Node.js render worker
                        Remotion + FFmpeg
                               │
                               ▼
                          S3 output video
                               │
                               ▼
                     Go API status + iOS preview
```

## Direct Media Upload Contract

Media bytes never pass through the Go API, SQS, or PostgreSQL. The iOS client
uploads only explicitly selected assets, or assets permitted by an enabled
automatic-memory feature:

1. iOS requests a short-lived upload session from `POST /v1/media/upload-sessions`.
2. The Go API returns an idempotent upload identifier, S3 key, and one or more
   presigned `PUT` URLs. Multipart sessions also return the S3 multipart upload
   identifier and part ranges.
3. iOS streams the original PhotoKit resource to an application-support staging
   file, then uploads that file directly to S3. Large media uploads use bounded
   multipart part files, persisted part ETags, retries, and background URL
   sessions where iOS permits.
4. iOS calls the multipart-completion endpoint with part numbers and ETags only.
5. iOS notifies `POST /v1/media/uploads/complete` with metadata and S3 references.

The Prompt-8 orchestration routes `POST /media/upload-session` and `POST /media`
provide the same direct-upload boundary. The versioned routes remain as a thin
camel-case adapter for the existing Swift upload models.

The API stores only media metadata, upload state, object keys, and checksums or
ETags. It never accepts large media bodies, embeds media in JSON, publishes
media in SQS messages, or stores media blobs in database records. Upload
requests use deterministic client idempotency keys so retries and app restarts
do not create duplicate logical uploads.

## StoryPlan Boundary

`StoryPlan` is the contract between story intelligence and video rendering. It should describe narrative intent rather than expose editor controls.

Initial conceptual fields:

- Story identity and version.
- Ordered moments and source asset identifiers.
- Narrative beats, optional captions, and validated caption style metadata.
- Per-moment duration, crop intent, and transition intent.
- Audio direction and voiceover/caption guidance, if enabled.
- Output constraints such as aspect ratio, duration range, and quality preset.

The render worker consumes a validated `StoryPlan`; it should not need to infer the user’s intent again.

## Asynchronous Rendering Contract

1. The API persists the project and creates `ANALYSIS` jobs with durable idempotency keys.
2. Analysis workers load referenced records, perform external AI orchestration outside the request lifecycle, and return analysis/StoryPlan results through internal callbacks.
3. After a StoryPlan exists, the API persists a `RENDER` job and publishes its identifiers to SQS.
4. The Node.js worker claims the SQS message and marks the render attempt active.
5. The worker renders outside the API request lifecycle, uploads the output to S3, and reports only the artifact reference and metadata.
6. The API transitions the project to `READY` or `FAILED` and exposes that state to iOS.

SQS visibility timeouts, idempotency keys, retries, and a dead-letter queue are required before production use.

## Implemented API Shape

The Go service implements user-intent routes rather than editing primitives:

- `POST /media/upload-session` — request an idempotent direct-to-S3 upload session.
- `POST /media` — verify/finalize an upload and persist its ready state.
- `GET /media/:id` — read owned media metadata and S3 reference.
- `POST /projects` — create a project from ready selected media.
- `POST /projects/:id/analyze` — persist asynchronous analysis jobs and immediately return job IDs.
- `GET /projects/:id/status` — read project/job progress and errors.
- `GET /projects/:id/story` — read the latest StoryPlan.
- `POST /projects/:id/render` — persist an asynchronous render job and immediately return its ID.
- `POST /internal/jobs/:id/status` — report worker progress, failure, or render artifact references.
- `POST /internal/jobs/:id/analysis` — save idempotent media analysis.
- `POST /internal/jobs/:id/story-plan` — save an idempotent versioned StoryPlan.

The existing `/v1/media/...` Swift upload routes are also supported as an adapter.
All creation paths are retry-safe, and terminal job states cannot regress.

## Privacy and Security Direction

- Request the minimum PhotoKit access needed for the selected flow.
- Keep signed S3 URLs short-lived and scoped to a user/story.
- Encrypt media in transit and at rest.
- Keep internal worker callbacks authenticated and non-public where possible.
- Avoid sending unnecessary raw media or personal metadata to external AI providers.
- Make deletion of source media, derived metadata, and rendered outputs an explicit lifecycle concern.
