# FrameMio Media API

Go REST/JSON service for authentication, direct-to-S3 media upload authorization, story projects, asynchronous processing jobs, StoryPlan persistence, render orchestration, progress, and error reporting.

The API **never receives or renders media bytes**. Photos and videos upload directly from iPhone to S3. Analysis and rendering run asynchronously outside the HTTP request lifecycle.

## Responsibilities

- Authenticate users and enforce record ownership.
- Create idempotent media upload sessions and S3 presigned URLs.
- Store media metadata and S3 object references in PostgreSQL.
- Create story projects from ready media records.
- Persist analysis, StoryPlan, processing-job, progress, error, and render-artifact metadata.
- Publish identifier-only analysis and render messages to SQS.
- Accept authenticated worker callbacks.

The API does not run FFmpeg, Remotion, video decoding, multimodal analysis, transcription, or long-running processing.

## Request Flow

```text
iPhone ── request upload authorization ──> Go API
   │                                        │
   └──────────── direct PUT/upload parts ─> S3
                                            │
iPhone ── completion metadata/reference ──> Go API

iPhone ── create/analyze project ─────────> Go API
                                            │
                                            ├─ PostgreSQL job record
                                            └─ identifier-only SQS message

analysis worker ── analysis/StoryPlan ─────> internal callbacks
render worker ──── progress/S3 result ─────> internal callbacks
```

## Public API

All public endpoints require `Authorization: Bearer <token>`. Creation endpoints require `Idempotency-Key`, except the versioned iOS upload adapter, which receives the existing app idempotency key in its JSON body.

| Method | Path | Purpose |
| --- | --- | --- |
| `POST` | `/media/upload-session` | Create or replay a direct-S3 upload authorization. |
| `POST` | `/media` | Verify/finalize an upload and mark the media ready. |
| `GET` | `/media/:id` | Read an owned media record. |
| `POST` | `/projects` | Create a project from ready owned media. |
| `POST` | `/projects/:id/analyze` | Persist analysis jobs, publish SQS messages, and return `202`. |
| `GET` | `/projects/:id/status` | Read project state and job progress/errors. |
| `GET` | `/projects/:id/story` | Read the latest persisted StoryPlan. |
| `POST` | `/projects/:id/render` | Persist a render job, publish SQS, and return `202`. |
| `GET` | `/healthz` | Process health check. |

The existing Swift uploader is also supported without changing its models:

| Method | Path | Purpose |
| --- | --- | --- |
| `POST` | `/v1/media/upload-sessions` | Camel-case iOS upload-session contract. |
| `POST` | `/v1/media/upload-sessions/:id/complete` | Finalize an iOS multipart upload using part numbers and ETags. |
| `POST` | `/v1/media/uploads/complete` | Receive the iOS completion notification. |

## Internal Worker API

Internal routes require `X-FrameMio-Internal-Token` and derive ownership/project/media identifiers from the stored job.

| Method | Path | Purpose |
| --- | --- | --- |
| `POST` | `/internal/jobs/:id/status` | Update queued/running/succeeded/failed state, progress, errors, and render artifact reference. |
| `POST` | `/internal/jobs/:id/analysis` | Save idempotent media-analysis results for an analysis job. |
| `POST` | `/internal/jobs/:id/story-plan` | Save a versioned StoryPlan and mark it ready for rendering. |

A successful render status must include `artifact_s3_key`. Terminal jobs cannot regress to running or change from succeeded to failed.

## SQS Contract

Messages contain only identifiers and small string configuration:

```json
{
  "jobId": "job_...",
  "projectId": "project_...",
  "mediaId": "media_...",
  "jobType": "analysis",
  "configuration": {
    "analysis_version": "1"
  }
}
```

The publisher rejects missing identifiers, unsupported job types, oversized configuration, and messages over the service's 64 KiB safety limit. Media bytes, images, transcripts, AI prompts, StoryPlans, and other large payloads must never be placed in SQS.

## Idempotency and Retry Safety

- Upload sessions, projects, and jobs use durable logical keys unique per user.
- PostgreSQL resolves concurrent logical-key races by returning the winning stored record.
- Media/upload creation and media completion use transactions.
- Retried upload completion returns an already-ready media record without re-finalizing S3 multipart state.
- Unused multipart sessions created during an idempotency race are aborted.
- Multipart completion validates every authorized part exactly once and verifies the completed object size.
- Worker result writes are idempotent by project/media or project/version.
- SQS consumers must also use the persisted job ID as their idempotency boundary because standard queues can deliver messages more than once.

## Authentication

The initial authenticator accepts an HMAC-SHA256 signed bearer token:

```text
base64url({"sub":"user-id","email":"optional","exp":unix_timestamp}).base64url(signature)
```

`AUTH_HMAC_SECRET` signs/verifies public bearer tokens. `INTERNAL_API_TOKEN` is a separate worker-only secret. Neither value belongs in the iOS application. Replace the initial HMAC boundary with the production identity provider/JWKS verifier before public launch if authentication is delegated to an external provider.

## Configuration

Required environment variables:

```text
DATABASE_URL
S3_MEDIA_BUCKET
SQS_PROCESSING_QUEUE_URL
AUTH_HMAC_SECRET          # at least 32 characters
INTERNAL_API_TOKEN        # at least 32 characters
```

Optional environment variables:

```text
PORT                                  # default: 8080
UPLOAD_PART_SIZE_BYTES                # default: 8 MiB, minimum S3 part size enforced
UPLOAD_MULTIPART_THRESHOLD_BYTES      # default: 16 MiB
AWS_REGION / standard AWS credentials # consumed by AWS SDK default configuration
```

## Run Locally

PostgreSQL, AWS credentials, the S3 bucket, and the SQS queue must already exist.

```bash
cd backend
go run ./cmd/api
```

The service applies the idempotent migration at startup. The same SQL is available at `migrations/001_domain_records.sql`.

## Validation

```bash
cd backend
go test ./...
go vet ./...
go test -race ./...
```

Tests cover authentication, upload/project/job idempotency, direct-upload completion retries, identifier-only queue payloads, worker callbacks, StoryPlan persistence, terminal job protection, render artifact reporting, and the existing iOS upload JSON contract.
