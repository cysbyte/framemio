# Infrastructure

Deployment boundary for Docker images, AWS ECS/Fargate services, ECR, S3, SQS, PostgreSQL/RDS, secrets, IAM, and observability.

The API, video intelligence worker, and render worker must remain separate deployable workloads with independent scaling characteristics. See `../docs/DB_ECS_DEPLOYMENT_SCALING.md` for current database, ECS deployment, and scale-to-zero operations.

## SQS Queues

Production uses separate SQS queues per worker type so workers do not consume or defer each other's messages:

- `framemio-story-processing` via `SQS_STORY_QUEUE_URL` for `STORY_PLAN`, story revision, and backend orchestration jobs.
- `framemio-video-analysis` via `SQS_VIDEO_QUEUE_URL` for `MEDIA_PREPROCESS`, `TRANSCRIBE`, and `VIDEO_UNDERSTAND`.
- `framemio-render-processing` via `SQS_RENDER_QUEUE_URL` for `VIDEO_RENDER`.
- `framemio-processing` via `SQS_PROCESSING_QUEUE_URL` remains a local/dev and migration fallback only.

The backend deployment performs an IAM simulation before deployment and fails if the ECS task role cannot send, receive, delete, or extend visibility on every configured queue. Keep `infrastructure/iam/framemio-ecs-task-role-sqs-policy.json` synchronized with production queue names.

Render workers enable ECS task scale-in protection before starting long render jobs. Keep `infrastructure/iam/framemio-ecs-task-role-task-protection-policy.json` attached to `FramemioECSTaskRole`; otherwise render messages are deferred and projects can remain stuck at the film-rendered step.

Runtime credentials should be injected through ECS secret references. Set repository variables such as `INTERNAL_API_TOKEN_SECRET_ARN`, `DEEPGRAM_API_KEY_SECRET_ARN`, `STORY_PLANNER_API_KEY_SECRET_ARN`, and `STORY_REVISION_API_KEY_SECRET_ARN` after creating the matching Secrets Manager entries and granting the ECS execution role `secretsmanager:GetSecretValue`. The workflow emits a warning and preserves the existing plaintext environment behavior until each secret is migrated.

Autoscaling alarms must target the queue owned by the service: video worker alarms use `SQS_VIDEO_QUEUE_URL`, render worker alarms use `SQS_RENDER_QUEUE_URL`, and backend story one-off activation reads `SQS_STORY_QUEUE_URL`.

Migration order:

1. Create the three new queues and any matching dead-letter queues/policies.
2. Deploy the backend first so new jobs publish to type-specific queues.
3. Deploy video, render, and backend story workers with their new queue env vars.
4. Confirm `framemio-processing` has no remaining old messages, then drain or purge it.

## Video Intelligence Worker

- Build and publish `video-intelligence-worker/Dockerfile` as its own ECR image.
- Run it as a private ECS/Fargate service with no public load balancer.
- Attach a dedicated video-analysis SQS queue and dead-letter queue; do not mix render jobs into this queue.
- Grant read access to original media objects and scoped write access to `users/*/video-analysis/*` artifacts.
- Grant only the PostgreSQL, SQS, S3, logging, and secret permissions required by the worker.
- Supply video-understanding and transcription API credentials through AWS Secrets Manager or SSM, never task-definition plaintext or queue messages.
- Size ephemeral storage for the largest supported source video plus extracted evidence, with headroom for retries.
- Production workers are controlled manually with the backend scripts; keep them private and set ECS service `desiredCount` to `0` or `1` as needed.
- Set the SQS visibility timeout above the normal processing duration and keep the worker heartbeat enabled for long videos.

The worker stores deterministic preprocessing artifacts in S3 and versioned records in PostgreSQL. Lifecycle policies may expire superseded derived artifacts, but must not remove evidence still referenced by an active analysis or story project.
