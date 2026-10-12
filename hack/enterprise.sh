#!/usr/bin/env bash
# Copyright 2026 Project Capsule Authors
# SPDX-License-Identifier: Apache-2.0
set -euo pipefail
: "${ENTERPRISE_VERSION:?Set ENTERPRISE_VERSION to a SemVer version without a v prefix}"
: "${ENTERPRISE_REGISTRY:=registry.projectcapsule.dev}"
: "${CHANNEL:=enterprise}"
: "${HELM:=helm}"
: "${KO:=ko}"
: "${DRY_RUN:=false}"
if [[ ! "$ENTERPRISE_VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$ ]]; then
  echo 'ENTERPRISE_VERSION must be SemVer without a v prefix' >&2
  exit 1
fi
# OCI image tags cannot contain SemVer build metadata.
if [[ "$ENTERPRISE_VERSION" == *+* ]]; then
  echo 'Build metadata is not supported in image tags; use a prerelease suffix' >&2
  exit 1
fi
case "$CHANNEL" in
  enterprise) chart_path=charts ;;
  prereleases) chart_path=charts/prereleases ;;
  *) echo 'Unknown release channel' >&2; exit 1 ;;
esac
image="$ENTERPRISE_REGISTRY/$CHANNEL/change-windows"
chart="oci://$ENTERPRISE_REGISTRY/$chart_path"
run() {
  if [[ "$DRY_RUN" == true ]]; then printf '%q ' "$@"; printf '\n'; else "$@"; fi
}
if [[ "${PRINT_DEPLOY_ONLY:-false}" != true ]]; then
  run mkdir -p builds
  run "$HELM" package charts/change-windows --version "$ENTERPRISE_VERSION" --app-version "$ENTERPRISE_VERSION" --destination builds
  run env "IMG=$image:$ENTERPRISE_VERSION" "KO=$KO" bash hack/image.sh push
  run "$HELM" push "builds/change-windows-$ENTERPRISE_VERSION.tgz" "$chart"
fi
printf '\nDeployment (requires cert-manager and a capsule-enterprise image pull secret):\n'
printf 'helm upgrade --install scheduled-action %q --namespace capsule-system --create-namespace --version %q --set image.registry=%q --set image.repository=%q --set image.tag=%q --set %q\n' \
  "$chart/change-windows" "$ENTERPRISE_VERSION" "$ENTERPRISE_REGISTRY" "$CHANNEL/change-windows" "$ENTERPRISE_VERSION" 'serviceAccount.imagePullSecrets[0]=capsule-enterprise'
