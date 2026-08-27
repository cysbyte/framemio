# Render Worker

Deterministic Node.js/TypeScript worker for asynchronous `StoryPlan` rendering.

The worker converts a validated story plan plus source media references into a final MP4. It intentionally does not analyze media, call AI providers, select clips, or modify story structure.

## Responsibilities

- Download only required source assets from S3/object storage.
- Preprocess requested video clips with FFmpeg using exact StoryPlan timestamps.
- Build a deterministic composition specification for Remotion, including validated caption style metadata.
- Render the composition with Remotion.
- Encode the final MP4 with FFmpeg.
- Upload the final MP4 to S3/object storage.
- Return render metadata including renderer version, StoryPlan version, resolution, duration, status, and output S3 key.

## Determinism

For the same `StoryPlan`, source asset metadata, and renderer version, the worker derives the same fingerprint, work paths, clip IDs, composition JSON, output resolution, and render metadata. Tests use fixture renderers so deterministic behavior is validated without real S3, FFmpeg, or Remotion.

## Boundaries

- No video intelligence or semantic analysis.
- No multimodal AI calls.
- No StoryPlan validation beyond defensive input checks; invalid plans and unsafe caption styles must be blocked by the backend validator before queueing render jobs.
- No large media in API requests, SQS messages, or database records.
- No PostgreSQL access; the backend persists render jobs and status before this worker runs.

## Runtime

The production entrypoint is `npm start`. It polls the shared processing SQS queue for `VIDEO_RENDER` messages, fetches render input from `GET /internal/jobs/:id/render-input`, downloads source objects from S3, uploads the MP4 output to S3, and reports completion through `POST /internal/jobs/:id/status`.

Required ECS environment variables:

- `SQS_PROCESSING_QUEUE_URL`
- `BACKEND_INTERNAL_API_URL`
- `INTERNAL_API_TOKEN`
- `S3_MEDIA_BUCKET`
- `AWS_REGION`

For one-off ECS activation, the backend overrides `JOB_ID` and `WORKER_RUN_ONCE=true` so the render task drains exactly the queued render message and exits. `RENDERER_MODE=ffmpeg` is the current integration renderer; `RENDERER_MODE=remotion` can be enabled with `REMOTION_ENTRY_POINT` once the Remotion composition entrypoint is packaged.
