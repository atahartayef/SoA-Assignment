#!/usr/bin/env bash
# Watches a service after a rolling update and reverses it if the error rate
# rises, so a bad release costs minutes rather than a Friday evening.
set -euo pipefail
SERVICE=$1; shift
WINDOW=10m; MAX=1.0
while [ $# -gt 0 ]; do case $1 in --window) WINDOW=$2; shift 2;; --max-error-rate) MAX=$2; shift 2;; *) shift;; esac; done
echo "watching $SERVICE for $WINDOW (roll back above ${MAX}% 5xx)"
rate=$(curl -fsS --get "$SIGNOZ_URL/api/v1/query_range" \
  --data-urlencode "query=100*sum(rate(http_server_requests_total{service=\"$SERVICE\",status=~\"5..\"}[$WINDOW]))/sum(rate(http_server_requests_total{service=\"$SERVICE\"}[$WINDOW]))" \
  | jq -r '.data.result[0].values[-1][1] // "0"')
echo "observed error rate: ${rate}%"
if awk "BEGIN{exit !($rate > $MAX)}"; then
  echo "above threshold: rolling back to the previous task definition"
  previous=$(aws ecs list-task-definitions --family-prefix "$SERVICE" --sort DESC \
             --max-items 2 --query 'taskDefinitionArns[1]' --output text)
  aws ecs update-service --cluster maltashop-production --service "$SERVICE" \
    --task-definition "$previous" >/dev/null
  aws ecs wait services-stable --cluster maltashop-production --services "$SERVICE"
  echo "rolled back to $previous"
  exit 1
fi
echo "release held"
