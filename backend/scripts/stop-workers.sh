#!/usr/bin/env bash
set -euo pipefail

AWS_REGION="${AWS_REGION:-us-east-1}"
ECS_CLUSTER="${ECS_CLUSTER:-my-cluster}"
VIDEO_WORKER_SERVICE="${VIDEO_WORKER_SERVICE:-framemio-video-intelligence-worker-service-xuttbpns}"
RENDER_WORKER_SERVICE="${RENDER_WORKER_SERVICE:-framemio-render-worker-service-chw8cyt4}"
WAIT_FOR_STABLE="${WAIT_FOR_STABLE:-true}"

services=("$VIDEO_WORKER_SERVICE" "$RENDER_WORKER_SERVICE")

for service in "${services[@]}"; do
  echo "Scaling $service to desiredCount=0"
  aws ecs update-service \
    --region "$AWS_REGION" \
    --cluster "$ECS_CLUSTER" \
    --service "$service" \
    --desired-count 0 >/dev/null
done

if [[ "$WAIT_FOR_STABLE" == "true" ]]; then
  echo "Waiting for worker services to stop"
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
