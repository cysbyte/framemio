#!/usr/bin/env bash
set -euo pipefail

require_var() {
  local name="$1"
  if [[ -z "${!name:-}" ]]; then
    echo "Missing required environment variable $name" >&2
    exit 1
  fi
}

require_var AWS_REGION
require_var ECS_CLUSTER
require_var ECS_SERVICE
require_var ECS_TASK_DEFINITION
require_var ECS_CONTAINER_NAME
require_var IMAGE

: "${ECS_SERVICE_DESIRED_COUNT:=1}"
: "${ECS_LAUNCH_TYPE:=FARGATE}"

if [[ "${ECS_SERVICE_DESIRED_COUNT}" =~ ^[0-9]+$ ]]; then
  :
else
  echo "ECS_SERVICE_DESIRED_COUNT must be a non-negative integer" >&2
  exit 1
fi

echo "Fetching task definition $ECS_TASK_DEFINITION"
aws ecs describe-task-definition \
  --region "$AWS_REGION" \
  --task-definition "$ECS_TASK_DEFINITION" \
  --query 'taskDefinition' \
  --output json > task-definition.json

IMAGE="$IMAGE" ECS_CONTAINER_NAME="$ECS_CONTAINER_NAME" ECS_TASK_CPU="${ECS_TASK_CPU:-}" ECS_TASK_MEMORY="${ECS_TASK_MEMORY:-}" ECS_TASK_ROLE_ARN="${ECS_TASK_ROLE_ARN:-}" ECS_EXECUTION_ROLE_ARN="${ECS_EXECUTION_ROLE_ARN:-}" ECS_SET_ENV_VARS="${ECS_SET_ENV_VARS:-}" node --input-type=module -e '
  import { readFileSync, writeFileSync } from "node:fs";

  const taskDefinition = JSON.parse(readFileSync("task-definition.json", "utf8"));
  const container = taskDefinition.containerDefinitions.find(({ name }) => name === process.env.ECS_CONTAINER_NAME);

  if (!container) {
    throw new Error(`Container ${process.env.ECS_CONTAINER_NAME} was not found in the task definition.`);
  }

  container.image = process.env.IMAGE;

  if (process.env.ECS_TASK_CPU) {
    taskDefinition.cpu = process.env.ECS_TASK_CPU;
  }

  if (process.env.ECS_TASK_MEMORY) {
    taskDefinition.memory = process.env.ECS_TASK_MEMORY;
  }

  if (process.env.ECS_TASK_ROLE_ARN) {
    taskDefinition.taskRoleArn = process.env.ECS_TASK_ROLE_ARN;
  }

  if (process.env.ECS_EXECUTION_ROLE_ARN) {
    taskDefinition.executionRoleArn = process.env.ECS_EXECUTION_ROLE_ARN;
  }

  const environment = Array.isArray(container.environment) ? container.environment : [];
  const setEnvironmentVariable = (name, value) => {
    const existing = environment.find((entry) => entry.name === name);
    if (existing) {
      existing.value = value;
    } else {
      environment.push({ name, value });
    }
  };

  for (const name of (process.env.ECS_SET_ENV_VARS || "").split(/[\n,]/).map((item) => item.trim()).filter(Boolean)) {
    const value = process.env[name];
    if (value === undefined || value === "") {
      throw new Error(`ECS_SET_ENV_VARS includes ${name}, but ${name} is not set.`);
    }
    setEnvironmentVariable(name, value);
  }
  container.environment = environment;

  for (const field of [
    "taskDefinitionArn",
    "revision",
    "status",
    "requiresAttributes",
    "compatibilities",
    "registeredAt",
    "registeredBy",
    "deregisteredAt",
    "inferenceAccelerators",
    "ephemeralStorage",
  ]) {
    delete taskDefinition[field];
  }

  writeFileSync("rendered-task-definition.json", JSON.stringify(taskDefinition));
'

echo "Registering a new ECS task definition revision"
TASK_DEFINITION_ARN="$(aws ecs register-task-definition \
  --region "$AWS_REGION" \
  --cli-input-json file://rendered-task-definition.json \
  --query 'taskDefinition.taskDefinitionArn' \
  --output text)"

echo "Registered $TASK_DEFINITION_ARN"

SERVICE_STATUS="$(aws ecs describe-services \
  --region "$AWS_REGION" \
  --cluster "$ECS_CLUSTER" \
  --services "$ECS_SERVICE" \
  --query 'services[0].status' \
  --output text 2>/dev/null || true)"

if [[ "$SERVICE_STATUS" == "ACTIVE" ]]; then
  echo "Updating ECS service $ECS_SERVICE"
  aws ecs update-service \
    --region "$AWS_REGION" \
    --cluster "$ECS_CLUSTER" \
    --service "$ECS_SERVICE" \
    --task-definition "$TASK_DEFINITION_ARN" \
    --desired-count "$ECS_SERVICE_DESIRED_COUNT" \
    --force-new-deployment >/dev/null
else
  echo "Creating ECS service $ECS_SERVICE"
  require_var ECS_SUBNETS
  require_var ECS_SECURITY_GROUPS

  NETWORK_CONFIGURATION="$(node --input-type=module -e '
    const subnets = (process.env.ECS_SUBNETS || "").split(",").map((item) => item.trim()).filter(Boolean);
    const securityGroups = (process.env.ECS_SECURITY_GROUPS || "").split(",").map((item) => item.trim()).filter(Boolean);

    if (subnets.length === 0) {
      throw new Error("ECS_SUBNETS must contain at least one subnet when creating a service.");
    }

    if (securityGroups.length === 0) {
      throw new Error("ECS_SECURITY_GROUPS must contain at least one security group when creating a service.");
    }

    process.stdout.write(JSON.stringify({
      awsvpcConfiguration: {
        subnets,
        securityGroups,
        assignPublicIp: process.env.ECS_ASSIGN_PUBLIC_IP || "ENABLED",
      },
    }));
  ')"

  aws ecs create-service \
    --region "$AWS_REGION" \
    --cluster "$ECS_CLUSTER" \
    --service-name "$ECS_SERVICE" \
    --task-definition "$TASK_DEFINITION_ARN" \
    --desired-count "$ECS_SERVICE_DESIRED_COUNT" \
    --launch-type "$ECS_LAUNCH_TYPE" \
    --network-configuration "$NETWORK_CONFIGURATION" >/dev/null
fi

aws ecs wait services-stable \
  --region "$AWS_REGION" \
  --cluster "$ECS_CLUSTER" \
  --services "$ECS_SERVICE"

aws ecs describe-services \
  --region "$AWS_REGION" \
  --cluster "$ECS_CLUSTER" \
  --services "$ECS_SERVICE" \
  --query 'services[0].{serviceName:serviceName,status:status,desiredCount:desiredCount,runningCount:runningCount,pendingCount:pendingCount,taskDefinition:taskDefinition}' \
  --output json
