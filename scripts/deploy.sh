#!/usr/bin/env bash
# Rolling update of one service on ECS Fargate (phase 1) or Kubernetes (phase 3).
# Nothing is built here: the image digest was produced by the build job and is
# the only thing promoted, so production runs the bytes the tests passed against.
set -euo pipefail
SERVICE=$1
DIGEST=$2
echo "deploying $SERVICE at $DIGEST"
aws ecs register-task-definition --cli-input-json \
  "$(jq --arg image "$DIGEST" '.containerDefinitions[0].image = $image' "deploy/task-$SERVICE.json")" >/dev/null
aws ecs update-service --cluster maltashop-production --service "$SERVICE" \
  --task-definition "$SERVICE" --force-new-deployment >/dev/null
# minimumHealthyPercent 100 / maximumPercent 200: new tasks must pass their
# readiness check before any old task is drained, so capacity never dips.
aws ecs wait services-stable --cluster maltashop-production --services "$SERVICE"
echo "$SERVICE is stable"
