# FrameMio

FrameMio is a premium iPhone-first memory companion that turns everyday photos and videos into automatically generated story videos.

## Repository Layout

- `ios/` — SwiftUI iPhone application using PhotoKit, Vision, and AVFoundation.
- `backend/` — Go REST/JSON API and story orchestration boundary.
- `video-intelligence-worker/` — Go FFmpeg/FFprobe worker for asynchronous video evidence and semantic analysis.
- `render-worker/` — Node.js Remotion/FFmpeg rendering worker.
- `infrastructure/` — Docker and AWS ECS/Fargate, S3, SQS, ECR, and PostgreSQL deployment boundaries.
- `docs/` — Product and architecture specifications.

The Go API remains asynchronous and request-oriented. Long-running video understanding belongs in `video-intelligence-worker/`, while final video generation belongs only in `render-worker/`; these workloads deploy and scale independently.
