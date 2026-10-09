#!/usr/bin/env bash
# Runs the pipeline's verify, build and integration jobs on this machine, with
# the same commands .gitlab-ci.yml runs on a hosted runner. Used to produce the
# evidence screenshot when a hosted run is not to hand.
set -uo pipefail
cd "$(dirname "$0")/.."
GO_IMAGE=golang:1.23.3-alpine3.20
ok=0; failed=0
stage() { printf '\n\033[1m== %s ==\033[0m\n' "$1"; }
step() {
  local label=$1; shift
  local start; start=$(date +%s)
  if output=$("$@" 2>&1); then
    printf '  PASS  %-46s %4ss\n' "$label" "$(( $(date +%s) - start ))"; ok=$((ok+1))
  else
    printf '  FAIL  %-46s\n%s\n' "$label" "$output"; failed=$((failed+1))
  fi
}
go_in_docker() { docker run --rm -v "$PWD":/src -w /src "$GO_IMAGE" sh -c "$1"; }

stage "verify"
step "gofmt: every file formatted"      bash -c 'test -z "$(docker run --rm -v "$PWD":/src -w /src '"$GO_IMAGE"' gofmt -l .)"'
step "go vet: suspicious constructs"    bash -c 'docker run --rm -v "$PWD":/src -w /src '"$GO_IMAGE"' go vet ./...'
step "unit + contract tests, race on"   bash -c 'docker run --rm -v "$PWD":/src -w /src '"$GO_IMAGE"' go test ./... -count=1'

stage "build"
for s in order inventory payment notification; do
  step "$s image (static binary, distroless)" bash -c "docker build -q --build-arg SERVICE=$s -t maltashop/$s:local ."
done

stage "integration"
step "stack healthy" bash -c "docker compose ps --format '{{.Service}} {{.Status}}' | grep -q healthy"
step "choreographed saga, both failure paths" ./scripts/smoke_test.sh

printf '\n'
if [ "$failed" -eq 0 ]; then printf '\033[32mpipeline passed: %s checks, 0 failures\033[0m\n' "$ok"
else printf '\033[31mpipeline failed: %s failures\033[0m\n' "$failed"; exit 1; fi
