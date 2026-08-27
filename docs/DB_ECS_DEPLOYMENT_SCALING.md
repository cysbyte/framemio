# Database, ECS Deployment, and Scaling

Use this document as the operational source of truth for FrameMio database connectivity, GitHub Actions ECS deployment, and worker scale-to-zero behavior.

Last verified: 2026-08-23 in `us-east-1`.

## Production AWS Resources

- AWS account: `450876624497`
- Region: `us-east-1`
- ECS cluster: `my-cluster`
- RDS endpoint: `framemio.c4j042eqkxdd.us-east-1.rds.amazonaws.com:5432`
- Database name: `postgres`
- App database user: `framemio_app`
- Shared ECS network:
  - Subnets: `subnet-00d486ccc47c106c3`, `subnet-049030ce471097336`, `subnet-09957911ba06c489f`, `subnet-0207cfb1b59d111c0`, `subnet-059002c34faa60a13`, `subnet-0b38a6af4a720b169`
  - Security group: `sg-0621dd1b1173bd1c0`
  - Assign public IP: `ENABLED`

## Database Connection

Backend and video intelligence worker connect to the same RDS PostgreSQL database using IAM database authentication.

```text
DATABASE_URL=postgres://framemio_app@framemio.c4j042eqkxdd.us-east-1.rds.amazonaws.com:5432/postgres?sslmode=require
DATABASE_AUTH_MODE=iam
AWS_REGION=us-east-1
```

Do not paste a generated IAM authentication token into `.env`, ECS, or GitHub variables. The token is short-lived, and the Go code generates it at connection time when `DATABASE_AUTH_MODE=iam` is set.

### Database Access Rules

- Backend uses `DATABASE_URL` and runs its idempotent domain-record migration on startup.
- Video intelligence worker uses `DATABASE_URL` and runs its idempotent video-analysis indexes/migration on startup.
- Render worker currently does not connect to PostgreSQL; it exports the deterministic rendering pipeline and still needs a queue/callback runner before full render jobs are end-to-end.
- `DATABASE_URL` in GitHub repository variables must be the no-password DSN above.
- Local `.env` files may contain the same no-password DSN, but should not contain static IAM tokens or master-user passwords.

### Required RDS IAM Setup

The ECS task role must allow RDS IAM login for `framemio_app`:

```json
{
  "Effect": "Allow",
  "Action": "rds-db:connect",
  "Resource": "arn:aws:rds-db:us-east-1:450876624497:dbuser:<db-resource-id>/framemio_app"
}
```

PostgreSQL must also grant IAM login to the role:

```sql
GRANT rds_iam TO framemio_app;
```

Use the RDS resource ID from `aws rds describe-db-instances` or the RDS console when maintaining the IAM policy.

### Safe Connection Checks

Check task definitions without printing secrets:

```bash
aws ecs describe-task-definition \
  --region us-east-1 \
  --task-definition framemio-backend \
  --query 'taskDefinition.containerDefinitions[0].environment[?name==`DATABASE_URL` || name==`DATABASE_AUTH_MODE` || name==`AWS_REGION`]'
```

For local smoke tests, source a local env file and run package tests without echoing env values:

```bash
cd backend
set -a; source .env; set +a
GOCACHE=/tmp/framemio-go-cache/backend go test ./...
```

```bash
cd video-intelligence-worker
set -a; source .env; set +a
GOCACHE=/tmp/framemio-go-cache/video go test ./...
```

## GitHub Actions Deployment

Three workflows build Docker images, push them to ECR, register a new task definition revision, and update the ECS service.

| Component | Workflow | ECR Repository | ECS Service | Task Definition |
| --- | --- | --- | --- | --- |
| Backend API | `.github/workflows/build-backend-and-push-ecr.yml` | `framemio-backend` | `framemio-backend-service-bvglq3iy` | `framemio-backend` |
| Video intelligence worker | `.github/workflows/build-video-intelligence-worker-and-push-ecr.yml` | `framemio-video-intelligence-worker` | `framemio-video-intelligence-worker-service-xuttbpns` | `framemio-video-intelligence-worker` |
| Render worker | `.github/workflows/build-render-worker-and-push-ecr.yml` | `framemio-render-worker` | `framemio-render-worker-service-chw8cyt4` | `framemio-render-worker` |

### Deployment Flow

1. A push to `main` touches a component path or its workflow.
2. GitHub Actions assumes `arn:aws:iam::450876624497:role/FramemioGitHubActions` by OIDC.
3. The workflow builds the component Docker image.
4. The workflow pushes both `sha-<12-char-sha>` and `latest` tags to ECR.
5. `scripts/deploy-ecs-service.sh` reads the existing task definition, updates the image and selected environment variables, and registers a new revision.
6. The script updates the ECS service, or creates it if it does not exist.
7. Worker workflows keep service desired count at `0` and register scalable targets `min=0`, `max=1`.

### Trigger Rules

- Backend workflow triggers on `backend/**`, its workflow file, or `scripts/deploy-ecs-service.sh`.
- Video intelligence worker workflow triggers on `video-intelligence-worker/**`, its workflow file, or `scripts/deploy-ecs-service.sh`.
- Render worker workflow triggers on `render-worker/**`, its workflow file, or `scripts/deploy-ecs-service.sh`.
- If only backend code changes, worker workflows should not run.
- If the shared deploy script changes, all three workflows run because all deployments depend on it.

### Required GitHub Settings

Repository variables:

```text
AWS_REGION=us-east-1
BACKEND_ECR_REPOSITORY=framemio-backend
VIDEO_INTELLIGENCE_WORKER_ECR_REPOSITORY=framemio-video-intelligence-worker
RENDER_WORKER_ECR_REPOSITORY=framemio-render-worker
DATABASE_URL=postgres://framemio_app@framemio.c4j042eqkxdd.us-east-1.rds.amazonaws.com:5432/postgres?sslmode=require
```

Optional repository variables override the workflow defaults:

```text
ECS_SUBNETS=<comma-separated subnet IDs>
ECS_SECURITY_GROUPS=<comma-separated security group IDs>
ECS_ASSIGN_PUBLIC_IP=ENABLED
```

Repository secret:

```text
AWS_GITHUB_ACTIONS_ROLE_ARN=arn:aws:iam::450876624497:role/FramemioGitHubActions
```

### GitHub Actions IAM Permissions

The GitHub Actions role needs permissions to:

- Describe and push ECR images.
- Describe, register, create, update, and wait for ECS services/task definitions.
- Pass `arn:aws:iam::450876624497:role/ecsTaskExecutionRole` and `arn:aws:iam::450876624497:role/FramemioECSTaskRole` to ECS.
- Register ECS Application Auto Scaling scalable targets for worker services and keep worker services at `desiredCount=0` after deploys.

The scalable-target permission is required because worker workflows run this on every worker deploy, while also setting the worker ECS service desired count to `0`:

```bash
aws application-autoscaling register-scalable-target \
  --region us-east-1 \
  --service-namespace ecs \
  --scalable-dimension ecs:service:DesiredCount \
  --resource-id service/my-cluster/<worker-service-name> \
  --min-capacity 0 \
  --max-capacity 1
```

## Current ECS Deployment State

Last verified after workflow runs `32583031900`, `32583031868`, and `32583031887`:

| Service | Desired | Running | Task Definition |
| --- | ---: | ---: | --- |
| `framemio-backend-service-bvglq3iy` | `1` | `1` | `framemio-backend:8` |
| `framemio-video-intelligence-worker-service-xuttbpns` | `0` | `0` | `framemio-video-intelligence-worker:9` |
| `framemio-render-worker-service-chw8cyt4` | `0` | `0` | `framemio-render-worker:6` |

Verify live state:

```bash
aws ecs describe-services \
  --region us-east-1 \
  --cluster my-cluster \
  --services \
    framemio-backend-service-bvglq3iy \
    framemio-video-intelligence-worker-service-xuttbpns \
    framemio-render-worker-service-chw8cyt4 \
  --query 'services[].{serviceName:serviceName,desired:desiredCount,running:runningCount,pending:pendingCount,taskDefinition:taskDefinition}' \
  --output json
```

## Scaling Model

FrameMio uses zero-idle one-off ECS tasks for workers, matching the `product-video/worker` pattern.

### Backend API

- ECS service desired count: `1`
- Running count target: `1`
- Exposed through its service/load-balancer configuration.
- It remains online to handle iOS/API requests and to enqueue work.

### Video Intelligence Worker

- ECS service desired count: `0` when idle.
- Application Auto Scaling target: `min=0`, `max=1`.
- No queue-depth scaling policy is attached.
- Backend publishes an SQS job message, then calls `ecs:RunTask` for the video worker task definition.
- Backend passes `JOB_ID`, `JOB_TYPE`, and `WORKER_RUN_ONCE=true` as container overrides.
- The worker polls SQS for the matching `JOB_ID`, processes one message, deletes it on success, and exits.

### Render Worker

- ECS service desired count: `0` when idle.
- Application Auto Scaling target: `min=0`, `max=1`.
- Backend is configured to call `ecs:RunTask` for render jobs.
- Current limitation: the render worker image does not yet include a queue-consuming executable/callback runner, so the ECS activation path is ready but full render-job processing is not complete.

### Required Backend Runtime IAM

The backend ECS task role `arn:aws:iam::450876624497:role/FramemioECSTaskRole` must allow one-off worker task launches:

```json
{
  "Effect": "Allow",
  "Action": "ecs:RunTask",
  "Resource": [
    "arn:aws:ecs:us-east-1:450876624497:task-definition/framemio-video-intelligence-worker:*",
    "arn:aws:ecs:us-east-1:450876624497:task-definition/framemio-render-worker:*"
  ]
}
```

It must also pass the roles used by those task definitions:

```json
{
  "Effect": "Allow",
  "Action": "iam:PassRole",
  "Resource": [
    "arn:aws:iam::450876624497:role/ecsTaskExecutionRole",
    "arn:aws:iam::450876624497:role/FramemioECSTaskRole"
  ]
}
```

## Scaling and Activation Verification

Check scalable targets:

```bash
aws application-autoscaling describe-scalable-targets \
  --region us-east-1 \
  --service-namespace ecs \
  --resource-ids \
    service/my-cluster/framemio-video-intelligence-worker-service-xuttbpns \
    service/my-cluster/framemio-render-worker-service-chw8cyt4 \
  --query 'ScalableTargets[].{ResourceId:ResourceId,Min:MinCapacity,Max:MaxCapacity}' \
  --output json
```

Check recent backend-started one-off tasks:

```bash
aws ecs list-tasks \
  --region us-east-1 \
  --cluster my-cluster \
  --started-by framemio-video-worker

aws ecs list-tasks \
  --region us-east-1 \
  --cluster my-cluster \
  --started-by framemio-render-worker
```

Check SQS queue depth before and after activation:

```bash
aws sqs get-queue-attributes \
  --region us-east-1 \
  --queue-url https://sqs.us-east-1.amazonaws.com/450876624497/framemio-video-analysis \
  --attribute-names ApproximateNumberOfMessages ApproximateNumberOfMessagesNotVisible ApproximateNumberOfMessagesDelayed

aws sqs get-queue-attributes \
  --region us-east-1 \
  --queue-url https://sqs.us-east-1.amazonaws.com/450876624497/framemio-processing \
  --attribute-names ApproximateNumberOfMessages ApproximateNumberOfMessagesNotVisible ApproximateNumberOfMessagesDelayed
```

## Troubleshooting

### GitHub Actions Fails Before Build

- Check `AWS_REGION`, ECR repository variables, and `AWS_GITHUB_ACTIONS_ROLE_ARN`.
- If network variables are empty, the workflows have defaults matching current ECS services, but repository variables can override them.
- Use `gh run view <RUN_ID> --repo cysbyte/framemio --log-failed` first.

### ECS Service Deployment Hangs or Rolls Back

- Check the service events:

```bash
aws ecs describe-services \
  --region us-east-1 \
  --cluster my-cluster \
  --services <service-name> \
  --query 'services[0].events[0:10]'
```

- Check task stop reasons:

```bash
aws ecs list-tasks \
  --region us-east-1 \
  --cluster my-cluster \
  --service-name <service-name> \
  --desired-status STOPPED
```

### Worker Does Not Start From Backend

- Confirm backend task definition contains `ECS_WORKER_RUN_TASK_ENABLED=true`.
- Confirm backend task role has `ecs:RunTask` and `iam:PassRole`.
- Confirm worker task definitions are active and use container name `Main`.
- Check backend logs for `worker activation failed`.

### Database Connection Fails in ECS

- Confirm `DATABASE_AUTH_MODE=iam` is in the task definition.
- Confirm `DATABASE_URL` has no password and includes `sslmode=require`.
- Confirm `FramemioECSTaskRole` allows `rds-db:connect` for `framemio_app`.
- Confirm the PostgreSQL role `framemio_app` has `rds_iam`.
- Confirm the ECS security group can reach the RDS security group on TCP `5432`.
