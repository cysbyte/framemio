# Infrastructure

Deployment boundary for Docker images, AWS ECS/Fargate services, ECR, S3, SQS, PostgreSQL/RDS, secrets, IAM, and observability.

The API, video intelligence worker, and render worker must remain separate deployable workloads with independent scaling characteristics. See `../docs/DB_ECS_DEPLOYMENT_SCALING.md` for current database, ECS deployment, and scale-to-zero operations.

## Video Intelligence Worker

- Build and publish `video-intelligence-worker/Dockerfile` as its own ECR image.
- Run it as a private ECS/Fargate service with no public load balancer.
- Attach a dedicated video-analysis SQS queue and dead-letter queue; do not mix render jobs into this queue.
- Grant read access to original media objects and scoped write access to `users/*/video-analysis/*` artifacts.
- Grant only the PostgreSQL, SQS, S3, logging, and secret permissions required by the worker.
- Supply video-understanding and transcription API credentials through AWS Secrets Manager or SSM, never task-definition plaintext or queue messages.
- Size ephemeral storage for the largest supported source video plus extracted evidence, with headroom for retries.
- Production currently uses zero-idle one-off ECS `RunTask` activation from the backend instead of queue-depth scaling policies; keep service scalable targets at `min=0`, `max=1`.
- Set the SQS visibility timeout above the normal processing duration and keep the worker heartbeat enabled for long videos.

The worker stores deterministic preprocessing artifacts in S3 and versioned records in PostgreSQL. Lifecycle policies may expire superseded derived artifacts, but must not remove evidence still referenced by an active analysis or story project.
