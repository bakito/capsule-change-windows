#!/usr/bin/env bash
# Copyright 2026 Project Capsule Authors
# SPDX-License-Identifier: Apache-2.0
set -euo pipefail
: "${KIND:?KIND is required}"
: "${KUBECTL:?KUBECTL is required}"
: "${KIND_CLUSTER:?KIND_CLUSTER is required}"
: "${KIND_NODE_IMAGE:?KIND_NODE_IMAGE is required}"
: "${IMG:?IMG is required}"
: "${IMAGE_ARCHIVE:?IMAGE_ARCHIVE is required}"
: "${ARTIFACTS:?ARTIFACTS is required}"
mkdir -p "$ARTIFACTS"
: > "$ARTIFACTS/e2e.log"
printf 'image=%s\nnode=%s\ncluster=%s\n' "$IMG" "$KIND_NODE_IMAGE" "$KIND_CLUSTER" > "$ARTIFACTS/run.txt"
if "$KIND" get clusters | grep -Fxq "$KIND_CLUSTER"; then
  echo "Refusing to reuse or delete existing cluster $KIND_CLUSTER" >&2
  exit 1
fi
# A private kubeconfig prevents changing the developer's current context.
export KUBECONFIG
KUBECONFIG=$(mktemp)
cluster_started=false
cleanup() {
  result=$?
  if [[ "$cluster_started" == true ]]; then
    "$KIND" export logs --name "$KIND_CLUSTER" "$ARTIFACTS/cluster" > "$ARTIFACTS/export.log" 2>&1 || true
    "$KUBECTL" get events -A > "$ARTIFACTS/events.log" 2>&1 || true
    "$KUBECTL" get changewindows -o yaml > "$ARTIFACTS/changewindows.yaml" 2>&1 || true
    "$KUBECTL" logs -n schedule-action-controller-system deployment/change-windows > "$ARTIFACTS/controller.log" 2>&1 || true
    "$KIND" delete cluster --name "$KIND_CLUSTER" || result=1
  fi
  rm -f "$KUBECONFIG"
  exit "$result"
}
trap cleanup EXIT
cluster_started=true
"$KIND" create cluster --name "$KIND_CLUSTER" --image "$KIND_NODE_IMAGE" --kubeconfig "$KUBECONFIG" --wait 120s
"$KUBECTL" apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.21.2/cert-manager.yaml
"$KUBECTL" rollout status -n cert-manager deployment/cert-manager --timeout=120s
"$KUBECTL" rollout status -n cert-manager deployment/cert-manager-webhook --timeout=120s
"$KUBECTL" rollout status -n cert-manager deployment/cert-manager-cainjector --timeout=120s
export DEV_CONTEXT="kind-$KIND_CLUSTER"
export DEV_NAMESPACE=schedule-action-controller-system
export DEV_RELEASE=change-windows
export DEV_IMAGE_MODE=kind
export DEV_KIND_CLUSTER="$KIND_CLUSTER"
export DEV_HELM_VALUES=charts/change-windows/ci/admission-values.yaml
task dev:deploy
# A second deployment must also converge without leaving an old image running.
previous_image=$("$KUBECTL" -n "$DEV_NAMESPACE" get deployment change-windows -o jsonpath='{.spec.template.spec.containers[0].image}')
task dev:deploy
current_image=$("$KUBECTL" -n "$DEV_NAMESPACE" get deployment change-windows -o jsonpath='{.spec.template.spec.containers[0].image}')
[[ "$current_image" != "$previous_image" ]] || { echo 'Repeated dev deployment did not roll the image' >&2; exit 1; }
printf 'first-image=%s\nsecond-image=%s\n' "$previous_image" "$current_image" >> "$ARTIFACTS/run.txt"
# The e2e build tag keeps cluster-dependent tests out of ordinary go test runs.
go test -tags=e2e ./e2e -v -count=1 -timeout=8m 2>&1 | tee "$ARTIFACTS/e2e.log"
