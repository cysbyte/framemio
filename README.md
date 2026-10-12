# FrameMio

FrameMio is a premium iPhone-first memory companion that turns everyday photos and videos into automatically generated story videos.

## Repository Layout

- `ios/` — SwiftUI iPhone application using PhotoKit, Vision, and AVFoundation.
- `backend/` — Go REST/JSON API and story orchestration boundary.
- `video-intelligence-worker/` — Go FFmpeg/FFprobe worker for asynchronous video evidence and semantic analysis.
- `render-worker/` — Node.js Remotion/FFmpeg rendering worker.
- `infrastructure/` — Docker and AWS ECS/Fargate, S3, SQS, ECR, and PostgreSQL deployment boundaries.
- `docs/` — Product, architecture, and operational handoff specifications.

The Go API remains asynchronous and request-oriented. Long-running video understanding belongs in `video-intelligence-worker/`, while final video generation belongs only in `render-worker/`; these workloads deploy and scale independently.


Hello App Review Team,

Thank you for reviewing FrameMio. The app is ready for review, and we have added the requested details below.

1. Screen recording

Screen recording captured on a physical iPhone running the latest iOS version, it's attached in app review information.

The recording starts from launching the app and demonstrates the typical flow: opening FrameMio, creating a Story/Promo/Reel, viewing project progress, previewing the generated video, and opening the Profile/credits purchase area.

FrameMio does not require manual account registration or login. The app creates an anonymous device-scoped account automatically so reviewers can access the app immediately.

2. App purpose and target audience

FrameMio is an AI-assisted video creation app for iPhone users who want to turn their own photos and videos into short personal videos, memory stories, promos, and social reels without using a complex timeline editor.

The target audience is everyday iPhone users, creators, families, travelers, and small business owners who have media in their photo library and want a fast way to generate polished videos. The value is that FrameMio reduces manual editing work by selecting meaningful media moments, arranging them into a video, and rendering a shareable result.

3. Setup and access instructions

No login credentials are required.

4. External services, tools, and platforms

FrameMio uses the following services to deliver core functionality:
  - Apple Photos / PhotoKit for selecting user-owned photos and videos.
  - Apple AVFoundation for local media handling and local reel rendering.
  - Apple StoreKit for subscriptions, credit packs, purchase restore, and purchase status.
  - FrameMio backend API for authentication, project creation, credits, job status, and generated video metadata.
  - Amazon Web Services, including S3 for private media/object storage, SQS/ECS for asynchronous processing, and CloudFront/CDN delivery where applicable.
  - Neon PostgreSQL for backend database storage.
  - AI/video processing providers for media understanding, story planning, transcription, and generated video creation.
  - Deepgram may be used for speech transcription / voice prompt transcription where enabled.

  The app does not use third-party social login and does not contain a public user feed.

5. Regional differences

FrameMio functions consistently across all supported regions. There are no region-specific content differences or region-specific feature restrictions in the submitted build, except for normal App Store availability of In-App Purchase products.

6. Regulated industry / protected third-party material

FrameMio does not operate in a highly regulated industry. The app is a consumer creative tool for generating videos from media supplied by the user. It does not provide medical, legal, financial, gambling, or regulated professional services.

FrameMio does not provide protected third-party media for users to edit. Users create videos from their own photos, videos, prompts, and selected media. Any generated output is based on user-provided input and app-provided templates.
