# Render Worker

Deterministic Node.js/TypeScript worker for asynchronous `StoryPlan` rendering.

The worker converts a validated story plan plus source media references into a final MP4. It intentionally does not analyze media, call AI providers, select clips, or modify story structure.

## Responsibilities

- Download only required source assets from S3/object storage.
- Preprocess requested video clips with FFmpeg using exact StoryPlan timestamps.
- Build a deterministic composition specification for Remotion.
- Render the composition with Remotion.
- Encode the final MP4 with FFmpeg.
- Upload the final MP4 to S3/object storage.
- Return render metadata including renderer version, StoryPlan version, resolution, duration, status, and output S3 key.

## Determinism

For the same `StoryPlan`, source asset metadata, and renderer version, the worker derives the same fingerprint, work paths, clip IDs, composition JSON, output resolution, and render metadata. Tests use fixture renderers so deterministic behavior is validated without real S3, FFmpeg, or Remotion.

## Boundaries

- No video intelligence or semantic analysis.
- No multimodal AI calls.
- No StoryPlan validation beyond defensive input checks; invalid plans must be blocked by the backend validator before queueing render jobs.
- No large media in API requests, SQS messages, or database records.
- No PostgreSQL access; the backend persists render jobs and status before this worker runs.
