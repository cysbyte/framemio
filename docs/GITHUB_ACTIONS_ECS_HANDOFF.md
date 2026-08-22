# GitHub Actions and ECS Handoff

Use this document when a future agent needs to inspect deployment logs, verify image pushes, or inspect ECS once task definitions and services exist.

## Repository Context

- GitHub repository: `cysbyte/framemio`
- Default branch: `main`
- Workflow files:
  - `.github/workflows/build-backend-and-push-ecr.yml`
  - `.github/workflows/build-video-intelligence-worker-and-push-ecr.yml`
  - `.github/workflows/build-render-worker-and-push-ecr.yml`
- Workflow names:
  - `Build and push backend image to ECR`
  - `Build and push video intelligence worker image to ECR`
  - `Build and push render worker image to ECR`
- AWS account ID: `450876624497`
- AWS region: `us-east-1`
- ECS cluster: `my-cluster`
- ECR repositories:
  - `framemio-backend`
  - `framemio-video-intelligence-worker`
  - `framemio-render-worker`

## GitHub Actions Access

Confirm GitHub CLI access first:

```bash
gh auth status
```

If the token is missing or invalid, re-authenticate locally:

```bash
gh auth login
```

### List Recent Runs

```bash
gh run list --repo cysbyte/framemio --workflow build-backend-and-push-ecr.yml --limit 10
gh run list --repo cysbyte/framemio --workflow build-video-intelligence-worker-and-push-ecr.yml --limit 10
gh run list --repo cysbyte/framemio --workflow build-render-worker-and-push-ecr.yml --limit 10
```

### View a Run Summary

```bash
gh run view <RUN_ID> --repo cysbyte/framemio
```

### View Failed Logs

```bash
gh run view <RUN_ID> --repo cysbyte/framemio --log-failed
```

### Watch an Active Run

```bash
gh run watch <RUN_ID> --repo cysbyte/framemio
```

### Rerun a Failed Workflow

```bash
gh run rerun <RUN_ID> --repo cysbyte/framemio --failed
```

### Common Failure Checks

- Missing repository variable `AWS_REGION`
- Missing repository variables for the ECR repository names
- Missing secret `AWS_GITHUB_ACTIONS_ROLE_ARN`
- OIDC assume-role failures in `aws-actions/configure-aws-credentials`
- ECR repository name mismatches

### Required GitHub Settings

Repository variables:

- `AWS_REGION`
- `BACKEND_ECR_REPOSITORY`
- `VIDEO_INTELLIGENCE_WORKER_ECR_REPOSITORY`
- `RENDER_WORKER_ECR_REPOSITORY`
- `ECS_SUBNETS` when GitHub Actions needs to create a new ECS service
- `ECS_SECURITY_GROUPS` when GitHub Actions needs to create a new ECS service
- `ECS_ASSIGN_PUBLIC_IP` if the ECS service should launch with a public IP

Repository secret:

- `AWS_GITHUB_ACTIONS_ROLE_ARN`

## AWS Access

The current GitHub Actions role is:

```text
arn:aws:iam::450876624497:role/FramemioGitHubActions
```

The role trusts GitHub OIDC for both subject formats used by this account:

- `repo:cysbyte/framemio:ref:refs/heads/main`
- `repo:cysbyte@92586970/framemio@1342215963:ref:refs/heads/main`

If GitHub Actions reports `Not authorized to perform sts:AssumeRoleWithWebIdentity`, check CloudTrail for the actual `AssumeRoleWithWebIdentity` `Username` and add that subject to the role trust policy.

Inspect ECR repositories:

```bash
aws ecr describe-repositories --region us-east-1
```

Inspect images in one repository:

```bash
aws ecr describe-images \
  --region us-east-1 \
  --repository-name framemio-backend
```

## ECS Access

This repository now uses GitHub Actions to build, push, and deploy each service automatically.

- Backend and video worker workflows update their ECS services after pushing a new image.
- Render worker workflow also updates its ECS service, but the service should stay at `desiredCount=0` and be started with `RunTask` when work arrives.
- If a service does not exist yet, the workflow creates it in `my-cluster` using the network variables above.

When ECS exists, capture the following values in the deployment notes:

- cluster name
- service name
- task definition family or ARN
- container name
- CloudWatch log group

### Discover Clusters

```bash
aws ecs list-clusters --region us-east-1
```

### Discover Services

```bash
aws ecs list-services --region us-east-1 --cluster <cluster-name>
```

### Describe a Service

```bash
aws ecs describe-services \
  --region us-east-1 \
  --cluster <cluster-name> \
  --services <service-name>
```

### Describe a Task Definition

```bash
aws ecs describe-task-definition \
  --region us-east-1 \
  --task-definition <task-definition-family-or-arn>
```

### List Running Tasks

```bash
aws ecs list-tasks \
  --region us-east-1 \
  --cluster <cluster-name> \
  --service-name <service-name> \
  --desired-status RUNNING
```

### Describe Running Tasks

```bash
TASKS=$(aws ecs list-tasks \
  --region us-east-1 \
  --cluster <cluster-name> \
  --service-name <service-name> \
  --desired-status RUNNING \
  --query 'taskArns[]' \
  --output text)

aws ecs describe-tasks \
  --region us-east-1 \
  --cluster <cluster-name> \
  --tasks $TASKS
```

### CloudWatch Logs

If the task definition writes to CloudWatch Logs, inspect the group with:

```bash
aws logs describe-log-groups --region us-east-1
```

Then tail the log stream or use:

```bash
aws logs tail <log-group-name> --region us-east-1 --follow
```

## Handoff Tips

- Save the workflow run ID, commit SHA, and conclusion before changing deployment settings.
- Prefer `gh run view --log-failed` first when a workflow fails quickly.
- Prefer AWS CLI `--query` filters when inspecting ECS so secrets stay out of chat logs.
- Record the final ECS cluster/service/task-definition values next to this document after deployment.
- If you change the ECS network or IAM settings, update the repository variables and this doc together.

## Database Authentication

The RDS cluster `my-db-cluster` requires IAM database authentication because Internet Access Gateway is enabled. Backend and video worker tasks should use:

- `DATABASE_URL=postgres://framemio_app@framemio.c4j042eqkxdd.us-east-1.rds.amazonaws.com:5432/postgres?sslmode=require`
- `DATABASE_AUTH_MODE=iam`

The RDS console Go snippet shows the same instance endpoint and master user `postgres`. For this app, keep that endpoint but use the least-privilege app role `framemio_app`. The app code generates the IAM token at connection time, so do not paste a static token into ECS or GitHub.

The ECS task role is `arn:aws:iam::450876624497:role/FramemioECSTaskRole` and must allow `rds-db:connect` for the `framemio_app` database user. The PostgreSQL role `framemio_app` must be a member of `rds_iam`. The GitHub repository variable `DATABASE_URL` must contain the no-password DSN above so workflows can upsert it into backend and video worker task definitions.
