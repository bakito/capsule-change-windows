#!/usr/bin/env bash
# Copyright 2026 Project Capsule Authors
# SPDX-License-Identifier: Apache-2.0
set -euo pipefail
: "${VERSION:?VERSION is required}"
: "${CHART_REGISTRY:=oci://ghcr.io/projectcapsule/charts}"
version=${VERSION#v}
if [[ ! "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]]; then
  echo 'VERSION must be SemVer without build metadata' >&2
  exit 1
fi
mkdir -p dist/charts
case "${1:?Expected package or push}" in
  package)
    helm package charts/change-windows --version "$version" --app-version "$version" --destination dist/charts
    ;;
  push)
    rm -f dist/chart-refs.txt
    helm push "dist/charts/change-windows-$version.tgz" "$CHART_REGISTRY" 2>&1 | tee dist/charts/push.log
    digest=$(sed -n 's/^Digest: \(sha256:[a-f0-9]*\)$/\1/p' dist/charts/push.log)
    if [[ ! "$digest" =~ ^sha256:[a-f0-9]{64}$ ]]; then
      echo 'Helm did not return a valid digest' >&2
      exit 1
    fi
    printf '%s/change-windows@%s\n' "${CHART_REGISTRY#oci://}" "$digest" > dist/chart-refs.txt
    ;;
  *) echo 'Expected package or push' >&2; exit 1 ;;
esac
