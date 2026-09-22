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
: "${ECS_REGISTER_SCALABLE_TARGET:=false}"
: "${ECS_PRESERVE_DESIRED_COUNT:=false}"
: "${ECS_MIN_CAPACITY:=$ECS_SERVICE_DESIRED_COUNT}"
: "${ECS_MAX_CAPACITY:=$ECS_SERVICE_DESIRED_COUNT}"
: "${ECS_ENABLE_SQS_AUTOSCALING:=false}"
: "${ECS_SQS_QUEUE_URL:=}"
: "${ECS_SQS_SCALE_OUT_CAPACITY:=1}"
: "${ECS_SQS_SCALE_OUT_COOLDOWN:=30}"
: "${ECS_SQS_SCALE_IN_COOLDOWN:=60}"
: "${ECS_SQS_SCALE_OUT_EVALUATION_PERIODS:=1}"
: "${ECS_SQS_SCALE_IN_EVALUATION_PERIODS:=3}"
: "${ECS_ENABLE_REQUEST_AUTOSCALING:=false}"
: "${ECS_REQUEST_SCALE_OUT_CAPACITY:=1}"
: "${ECS_REQUEST_SCALE_OUT_COOLDOWN:=60}"
: "${ECS_REQUEST_SCALE_IN_COOLDOWN:=300}"
: "${ECS_REQUEST_SCALE_OUT_EVALUATION_PERIODS:=1}"
: "${ECS_REQUEST_SCALE_IN_EVALUATION_PERIODS:=5}"
: "${ECS_CONTAINER_STOP_TIMEOUT:=}"
: "${RDS_AUTO_START_ENABLED:=false}"
: "${RDS_CLUSTER_IDENTIFIER:=}"
: "${DATABASE_PROVIDER:=rds}"

for numeric_var in ECS_SERVICE_DESIRED_COUNT ECS_MIN_CAPACITY ECS_MAX_CAPACITY ECS_SQS_SCALE_OUT_CAPACITY ECS_SQS_SCALE_OUT_COOLDOWN ECS_SQS_SCALE_IN_COOLDOWN ECS_SQS_SCALE_OUT_EVALUATION_PERIODS ECS_SQS_SCALE_IN_EVALUATION_PERIODS ECS_REQUEST_SCALE_OUT_CAPACITY ECS_REQUEST_SCALE_OUT_COOLDOWN ECS_REQUEST_SCALE_IN_COOLDOWN ECS_REQUEST_SCALE_OUT_EVALUATION_PERIODS ECS_REQUEST_SCALE_IN_EVALUATION_PERIODS ECS_CONTAINER_STOP_TIMEOUT; do
  value="${!numeric_var}"
  if [[ -z "$value" || "$value" =~ ^[0-9]+$ ]]; then
    :
  else
    echo "$numeric_var must be a non-negative integer" >&2
    exit 1
  fi
done

start_rds_cluster_if_needed() {
  local database_provider
  database_provider="$(printf '%s' "$DATABASE_PROVIDER" | tr '[:upper:]' '[:lower:]')"

  if [[ "$database_provider" != "rds" && "$database_provider" != "aurora" && "$database_provider" != "aws-rds" ]]; then
    echo "Skipping RDS cluster preflight for DATABASE_PROVIDER=$DATABASE_PROVIDER"
    return 0
  fi

  if [[ "$RDS_AUTO_START_ENABLED" != "true" ]]; then
    return 0
  fi

  require_var RDS_CLUSTER_IDENTIFIER

  echo "Checking RDS cluster $RDS_CLUSTER_IDENTIFIER"
  local status
  status="$(aws rds describe-db-clusters \
    --region "$AWS_REGION" \
    --db-cluster-identifier "$RDS_CLUSTER_IDENTIFIER" \
    --query 'DBClusters[0].Status' \
    --output text)"

  case "$status" in
    available)
      echo "RDS cluster $RDS_CLUSTER_IDENTIFIER is available"
      ;;
    stopped)
      echo "Starting RDS cluster $RDS_CLUSTER_IDENTIFIER"
      aws rds start-db-cluster \
        --region "$AWS_REGION" \
        --db-cluster-identifier "$RDS_CLUSTER_IDENTIFIER" >/dev/null
      echo "Waiting for RDS cluster $RDS_CLUSTER_IDENTIFIER to become available"
      aws rds wait db-cluster-available \
        --region "$AWS_REGION" \
        --db-cluster-identifier "$RDS_CLUSTER_IDENTIFIER"
      ;;
    starting | backing-up | modifying | rebooting | configuring-iam-database-auth | upgrading)
      echo "RDS cluster $RDS_CLUSTER_IDENTIFIER is $status; waiting for availability"
      aws rds wait db-cluster-available \
        --region "$AWS_REGION" \
        --db-cluster-identifier "$RDS_CLUSTER_IDENTIFIER"
      ;;
    stopping)
      echo "RDS cluster $RDS_CLUSTER_IDENTIFIER is stopping; wait for it to stop before restarting" >&2
      exit 1
      ;;
    *)
      echo "RDS cluster $RDS_CLUSTER_IDENTIFIER is in unsupported status: $status" >&2
      exit 1
      ;;
  esac
}

start_rds_cluster_if_needed

queue_name_from_url() {
  QUEUE_URL="$1" node --input-type=module -e '
    const queueURL = process.env.QUEUE_URL || "";
    if (!queueURL) {
      throw new Error("queue URL is empty");
    }
    const parsed = new URL(queueURL);
    const parts = parsed.pathname.split("/").filter(Boolean);
    if (parts.length === 0) {
      throw new Error(`could not determine queue name from ${queueURL}`);
    }
    process.stdout.write(decodeURIComponent(parts[parts.length - 1]));
  '
}

put_sqs_metric_math_alarm() {
  local alarm_name="$1"
  local alarm_description="$2"
  local queue_name="$3"
  local comparison_operator="$4"
  local evaluation_periods="$5"
  local alarm_action="$6"
  local metrics_file
  metrics_file="$(mktemp)"
  QUEUE_NAME="$queue_name" node --input-type=module >"$metrics_file" -e '
    const queueName = process.env.QUEUE_NAME;
    const metric = (id, metricName) => ({
      Id: id,
      MetricStat: {
        Metric: {
          Namespace: "AWS/SQS",
          MetricName: metricName,
          Dimensions: [{ Name: "QueueName", Value: queueName }],
        },
        Period: 60,
        Stat: "Maximum",
      },
      ReturnData: false,
    });
    process.stdout.write(JSON.stringify([
      metric("visible", "ApproximateNumberOfMessagesVisible"),
      metric("inflight", "ApproximateNumberOfMessagesNotVisible"),
      metric("delayed", "ApproximateNumberOfMessagesDelayed"),
      {
        Id: "total",
        Expression: "visible + inflight + delayed",
        Label: "Total queue messages",
        ReturnData: true,
      },
    ]));
  '
  aws cloudwatch put-metric-alarm \
    --region "$AWS_REGION" \
    --alarm-name "$alarm_name" \
    --alarm-description "$alarm_description" \
    --comparison-operator "$comparison_operator" \
    --threshold 0 \
    --evaluation-periods "$evaluation_periods" \
    --datapoints-to-alarm "$evaluation_periods" \
    --metrics "file://$metrics_file" \
    --treat-missing-data notBreaching \
    --alarm-actions "$alarm_action" >/dev/null
  rm -f "$metrics_file"
}

configure_sqs_autoscaling() {
  if [[ "$ECS_ENABLE_SQS_AUTOSCALING" != "true" ]]; then
    return 0
  fi

  require_var ECS_SQS_QUEUE_URL

  local queue_name resource_id scale_out_policy_arn scale_in_policy_arn
  queue_name="$(queue_name_from_url "$ECS_SQS_QUEUE_URL")"
  resource_id="service/$ECS_CLUSTER/$ECS_SERVICE"

  echo "Configuring SQS autoscaling for $ECS_SERVICE using queue $queue_name"

  scale_out_policy_arn="$(aws application-autoscaling put-scaling-policy \
    --region "$AWS_REGION" \
    --service-namespace ecs \
    --scalable-dimension ecs:service:DesiredCount \
    --resource-id "$resource_id" \
    --policy-name "$ECS_SERVICE-sqs-scale-out" \
    --policy-type StepScaling \
    --step-scaling-policy-configuration "{\"AdjustmentType\":\"ExactCapacity\",\"Cooldown\":$ECS_SQS_SCALE_OUT_COOLDOWN,\"MetricAggregationType\":\"Maximum\",\"StepAdjustments\":[{\"MetricIntervalLowerBound\":0,\"ScalingAdjustment\":$ECS_SQS_SCALE_OUT_CAPACITY}]}" \
    --query PolicyARN \
    --output text)"

  scale_in_policy_arn="$(aws application-autoscaling put-scaling-policy \
    --region "$AWS_REGION" \
    --service-namespace ecs \
    --scalable-dimension ecs:service:DesiredCount \
    --resource-id "$resource_id" \
    --policy-name "$ECS_SERVICE-sqs-scale-in" \
    --policy-type StepScaling \
    --step-scaling-policy-configuration "{\"AdjustmentType\":\"ExactCapacity\",\"Cooldown\":$ECS_SQS_SCALE_IN_COOLDOWN,\"MetricAggregationType\":\"Maximum\",\"StepAdjustments\":[{\"MetricIntervalUpperBound\":0,\"ScalingAdjustment\":0}]}" \
    --query PolicyARN \
    --output text)"

  put_sqs_metric_math_alarm \
    "$ECS_SERVICE-sqs-has-work" \
    "Scale $ECS_SERVICE to $ECS_SQS_SCALE_OUT_CAPACITY when SQS has visible, in-flight, or delayed messages." \
    "$queue_name" \
    GreaterThanThreshold \
    "$ECS_SQS_SCALE_OUT_EVALUATION_PERIODS" \
    "$scale_out_policy_arn"

  put_sqs_metric_math_alarm \
    "$ECS_SERVICE-sqs-idle" \
    "Scale $ECS_SERVICE to 0 after SQS is idle." \
    "$queue_name" \
    LessThanOrEqualToThreshold \
    "$ECS_SQS_SCALE_IN_EVALUATION_PERIODS" \
    "$scale_in_policy_arn"
}

configure_request_autoscaling() {
  if [[ "$ECS_ENABLE_REQUEST_AUTOSCALING" != "true" ]]; then
    return 0
  fi

  local resource_id target_group_arn load_balancer_arn target_group_dimension load_balancer_dimension
  local scale_out_policy_arn scale_in_policy_arn
  resource_id="service/$ECS_CLUSTER/$ECS_SERVICE"

  target_group_arn="$(aws ecs describe-services \
    --region "$AWS_REGION" \
    --cluster "$ECS_CLUSTER" \
    --services "$ECS_SERVICE" \
    --query 'services[0].loadBalancers[0].targetGroupArn' \
    --output text)"
  if [[ -z "$target_group_arn" || "$target_group_arn" == "None" ]]; then
    echo "Cannot configure request autoscaling for $ECS_SERVICE: no target group is attached" >&2
    exit 1
  fi

  load_balancer_arn="$(aws elbv2 describe-target-groups \
    --region "$AWS_REGION" \
    --target-group-arns "$target_group_arn" \
    --query 'TargetGroups[0].LoadBalancerArns[0]' \
    --output text)"
  if [[ -z "$load_balancer_arn" || "$load_balancer_arn" == "None" ]]; then
    echo "Cannot configure request autoscaling for $ECS_SERVICE: target group has no load balancer" >&2
    exit 1
  fi

  target_group_dimension="${target_group_arn##*:}"
  load_balancer_dimension="${load_balancer_arn##*loadbalancer/}"

  echo "Configuring request autoscaling for $ECS_SERVICE using load balancer $load_balancer_dimension"

  scale_out_policy_arn="$(aws application-autoscaling put-scaling-policy \
    --region "$AWS_REGION" \
    --service-namespace ecs \
    --scalable-dimension ecs:service:DesiredCount \
    --resource-id "$resource_id" \
    --policy-name "$ECS_SERVICE-request-scale-out" \
    --policy-type StepScaling \
    --step-scaling-policy-configuration "{\"AdjustmentType\":\"ExactCapacity\",\"Cooldown\":$ECS_REQUEST_SCALE_OUT_COOLDOWN,\"MetricAggregationType\":\"Maximum\",\"StepAdjustments\":[{\"MetricIntervalLowerBound\":0,\"ScalingAdjustment\":$ECS_REQUEST_SCALE_OUT_CAPACITY}]}" \
    --query PolicyARN \
    --output text)"

  scale_in_policy_arn="$(aws application-autoscaling put-scaling-policy \
    --region "$AWS_REGION" \
    --service-namespace ecs \
    --scalable-dimension ecs:service:DesiredCount \
    --resource-id "$resource_id" \
    --policy-name "$ECS_SERVICE-request-scale-in" \
    --policy-type StepScaling \
    --step-scaling-policy-configuration "{\"AdjustmentType\":\"ExactCapacity\",\"Cooldown\":$ECS_REQUEST_SCALE_IN_COOLDOWN,\"MetricAggregationType\":\"Maximum\",\"StepAdjustments\":[{\"MetricIntervalUpperBound\":0,\"ScalingAdjustment\":0}]}" \
    --query PolicyARN \
    --output text)"

  aws cloudwatch put-metric-alarm \
    --region "$AWS_REGION" \
    --alarm-name "$ECS_SERVICE-request-scale-out" \
    --alarm-description "Scale $ECS_SERVICE to $ECS_REQUEST_SCALE_OUT_CAPACITY when the backend load balancer returns an error with no healthy target." \
    --namespace AWS/ApplicationELB \
    --metric-name HTTPCode_ELB_5XX_Count \
    --dimensions "Name=LoadBalancer,Value=$load_balancer_dimension" \
    --statistic Sum \
    --period 60 \
    --evaluation-periods "$ECS_REQUEST_SCALE_OUT_EVALUATION_PERIODS" \
    --datapoints-to-alarm "$ECS_REQUEST_SCALE_OUT_EVALUATION_PERIODS" \
    --comparison-operator GreaterThanThreshold \
    --threshold 0 \
    --treat-missing-data notBreaching \
    --alarm-actions "$scale_out_policy_arn" >/dev/null

  local scale_in_metrics
  scale_in_metrics="$(
    LOAD_BALANCER_DIMENSION="$load_balancer_dimension" \
    TARGET_GROUP_DIMENSION="$target_group_dimension" \
    node --input-type=module -e '
      const loadBalancer = process.env.LOAD_BALANCER_DIMENSION;
      const targetGroup = process.env.TARGET_GROUP_DIMENSION;
      const metrics = [
        {
          Id: "requests",
          MetricStat: {
            Metric: {
              Namespace: "AWS/ApplicationELB",
              MetricName: "RequestCount",
              Dimensions: [{ Name: "LoadBalancer", Value: loadBalancer }]
            },
            Period: 60,
            Stat: "Sum"
          },
          ReturnData: false
        },
        {
          Id: "healthy",
          MetricStat: {
            Metric: {
              Namespace: "AWS/ApplicationELB",
              MetricName: "HealthyHostCount",
              Dimensions: [
                { Name: "LoadBalancer", Value: loadBalancer },
                { Name: "TargetGroup", Value: targetGroup }
              ]
            },
            Period: 60,
            Stat: "Average"
          },
          ReturnData: false
        },
        {
          Id: "idle",
          Expression: "IF(FILL(healthy,0)>0,FILL(requests,0),1)",
          Label: "Idle requests while healthy",
          ReturnData: true
        }
      ];
      process.stdout.write(JSON.stringify(metrics));
    '
  )"

  aws cloudwatch put-metric-alarm \
    --region "$AWS_REGION" \
    --alarm-name "$ECS_SERVICE-request-scale-in" \
    --alarm-description "Scale $ECS_SERVICE to 0 after five idle minutes while at least one backend target is healthy." \
    --metrics "$scale_in_metrics" \
    --evaluation-periods "$ECS_REQUEST_SCALE_IN_EVALUATION_PERIODS" \
    --datapoints-to-alarm "$ECS_REQUEST_SCALE_IN_EVALUATION_PERIODS" \
    --comparison-operator LessThanOrEqualToThreshold \
    --threshold 0 \
    --treat-missing-data breaching \
    --alarm-actions "$scale_in_policy_arn" >/dev/null
}

echo "Fetching task definition $ECS_TASK_DEFINITION"
aws ecs describe-task-definition \
  --region "$AWS_REGION" \
  --task-definition "$ECS_TASK_DEFINITION" \
  --query 'taskDefinition' \
  --output json > task-definition.json

IMAGE="$IMAGE" ECS_CONTAINER_NAME="$ECS_CONTAINER_NAME" ECS_TASK_CPU="${ECS_TASK_CPU:-}" ECS_TASK_MEMORY="${ECS_TASK_MEMORY:-}" ECS_TASK_ROLE_ARN="${ECS_TASK_ROLE_ARN:-}" ECS_EXECUTION_ROLE_ARN="${ECS_EXECUTION_ROLE_ARN:-}" ECS_CONTAINER_STOP_TIMEOUT="${ECS_CONTAINER_STOP_TIMEOUT:-}" ECS_SET_ENV_VARS="${ECS_SET_ENV_VARS:-}" ECS_SET_OPTIONAL_ENV_VARS="${ECS_SET_OPTIONAL_ENV_VARS:-}" ECS_SET_SECRET_ENV_VARS="${ECS_SET_SECRET_ENV_VARS:-}" node --input-type=module -e '
  import { readFileSync, writeFileSync } from "node:fs";

  const taskDefinition = JSON.parse(readFileSync("task-definition.json", "utf8"));
  const container = taskDefinition.containerDefinitions.find(({ name }) => name === process.env.ECS_CONTAINER_NAME);

  if (!container) {
    throw new Error(`Container ${process.env.ECS_CONTAINER_NAME} was not found in the task definition.`);
  }

  container.image = process.env.IMAGE;

  if (process.env.ECS_CONTAINER_STOP_TIMEOUT) {
    container.stopTimeout = Number(process.env.ECS_CONTAINER_STOP_TIMEOUT);
  }

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
  const secrets = Array.isArray(container.secrets) ? container.secrets : [];
  const list = (value) => (value || "").split(/[\n,]/).map((item) => item.trim()).filter(Boolean);
  const secretNames = new Set(list(process.env.ECS_SET_SECRET_ENV_VARS));
  const secretValueFrom = (name) => process.env[`${name}_SECRET_ARN`] || process.env[`${name}_SECRET_VALUE_FROM`] || "";
  const removeByName = (entries, name) => {
    const index = entries.findIndex((entry) => entry.name === name);
    if (index >= 0) {
      entries.splice(index, 1);
    }
  };
  const setEnvironmentVariable = (name, value) => {
    removeByName(secrets, name);
    const existing = environment.find((entry) => entry.name === name);
    if (existing) {
      existing.value = value;
    } else {
      environment.push({ name, value });
    }
  };
  const setSecretEnvironmentVariable = (name, valueFrom) => {
    removeByName(environment, name);
    const existing = secrets.find((entry) => entry.name === name);
    if (existing) {
      existing.valueFrom = valueFrom;
    } else {
      secrets.push({ name, valueFrom });
    }
  };

  for (const name of secretNames) {
    const valueFrom = secretValueFrom(name);
    if (!valueFrom) {
      throw new Error(`ECS_SET_SECRET_ENV_VARS includes ${name}, but ${name}_SECRET_ARN or ${name}_SECRET_VALUE_FROM is not set.`);
    }
    setSecretEnvironmentVariable(name, valueFrom);
  }

  for (const name of list(process.env.ECS_SET_ENV_VARS)) {
    if (secretNames.has(name)) {
      continue;
    }
    const value = process.env[name];
    if (value === undefined || value === "") {
      throw new Error(`ECS_SET_ENV_VARS includes ${name}, but ${name} is not set.`);
    }
    setEnvironmentVariable(name, value);
  }

  for (const name of list(process.env.ECS_SET_OPTIONAL_ENV_VARS)) {
    if (secretNames.has(name)) {
      continue;
    }
    const value = process.env[name];
    if (value !== undefined && value !== "") {
      setEnvironmentVariable(name, value);
    }
  }
  container.environment = environment;
  container.secrets = secrets;

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
  UPDATE_SERVICE_ARGS=(
    --region "$AWS_REGION"
    --cluster "$ECS_CLUSTER"
    --service "$ECS_SERVICE"
    --task-definition "$TASK_DEFINITION_ARN"
    --force-new-deployment
  )
  if [[ "$ECS_PRESERVE_DESIRED_COUNT" == "true" ]]; then
    echo "Preserving current desired count for $ECS_SERVICE"
  else
    UPDATE_SERVICE_ARGS+=(--desired-count "$ECS_SERVICE_DESIRED_COUNT")
  fi
  aws ecs update-service "${UPDATE_SERVICE_ARGS[@]}" >/dev/null
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

if [[ "$ECS_REGISTER_SCALABLE_TARGET" == "true" ]]; then
  echo "Registering ECS scalable target for $ECS_SERVICE min=$ECS_MIN_CAPACITY max=$ECS_MAX_CAPACITY"
  aws application-autoscaling register-scalable-target \
    --region "$AWS_REGION" \
    --service-namespace ecs \
    --scalable-dimension ecs:service:DesiredCount \
    --resource-id "service/$ECS_CLUSTER/$ECS_SERVICE" \
    --min-capacity "$ECS_MIN_CAPACITY" \
    --max-capacity "$ECS_MAX_CAPACITY" >/dev/null
fi

configure_sqs_autoscaling
configure_request_autoscaling

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
