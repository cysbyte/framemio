#!/usr/bin/env bash
set -euo pipefail

AWS_REGION="${AWS_REGION:-us-east-1}"
ECS_CLUSTER="${ECS_CLUSTER:-my-cluster}"
VIDEO_WORKER_SERVICE="${VIDEO_WORKER_SERVICE:-framemio-video-intelligence-worker-service-xuttbpns}"
RENDER_WORKER_SERVICE="${RENDER_WORKER_SERVICE:-framemio-render-worker-service-chw8cyt4}"
DESIRED_COUNT="${DESIRED_COUNT:-1}"
WAIT_FOR_STABLE="${WAIT_FOR_STABLE:-true}"
AWS_USE_PROXY="${AWS_USE_PROXY:-false}"

if [[ "$AWS_USE_PROXY" != "true" ]]; then
  unset HTTP_PROXY HTTPS_PROXY ALL_PROXY http_proxy https_proxy all_proxy
fi

if [[ ! "$DESIRED_COUNT" =~ ^[0-9]+$ ]] || [[ "$DESIRED_COUNT" -lt 1 ]]; then
  echo "DESIRED_COUNT must be a positive integer" >&2
  exit 1
fi

services=("$VIDEO_WORKER_SERVICE" "$RENDER_WORKER_SERVICE")

for service in "${services[@]}"; do
  echo "Scaling $service to desiredCount=$DESIRED_COUNT"
  aws ecs update-service \
    --region "$AWS_REGION" \
    --cluster "$ECS_CLUSTER" \
    --service "$service" \
    --desired-count "$DESIRED_COUNT" >/dev/null
done

if [[ "$WAIT_FOR_STABLE" == "true" ]]; then
  echo "Waiting for worker services to stabilize"
  aws ecs wait services-stable \
    --region "$AWS_REGION" \
    --cluster "$ECS_CLUSTER" \
    --services "${services[@]}"
fi

aws ecs describe-services \
  --region "$AWS_REGION" \
  --cluster "$ECS_CLUSTER" \
  --services "${services[@]}" \
  --query 'services[].{serviceName:serviceName,status:status,desiredCount:desiredCount,runningCount:runningCount,pendingCount:pendingCount,taskDefinition:taskDefinition}' \
  --output table
