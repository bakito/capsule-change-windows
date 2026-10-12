#!/usr/bin/env bash
# Copyright 2026 Project Capsule Authors
# SPDX-License-Identifier: Apache-2.0
set -euo pipefail
: "${IMG:?IMG must be a tagged image reference}"
: "${KO:=ko}"
: "${IMAGE_ARCHIVE:=dist/controller.tar}"
: "${IMAGE_REFS:=dist/image-refs.txt}"
: "${PLATFORMS:=linux/amd64,linux/arm64}"
mode=${1:?Expected build or push}
# Split the tag from the last path component, retaining registry ports.
if [[ "$IMG" == *@* || "${IMG##*/}" != *:* || "$IMG" == *[[:space:]]* ]]; then
  echo 'IMG must include a tag and must not contain a digest or whitespace' >&2
  exit 1
fi
image_repo=${IMG%:*}
image_tag=${IMG##*:}
if [[ ! "$image_tag" =~ ^[a-zA-Z0-9_][a-zA-Z0-9_.-]{0,127}$ || ! "$image_repo" =~ ^[a-z0-9][a-z0-9./:_-]*$ ]]; then
  echo 'IMG contains an invalid repository or tag' >&2
  exit 1
fi
export KO_DOCKER_REPO="$image_repo"
export CGO_ENABLED=0
mkdir -p "$(dirname "$IMAGE_REFS")"
# Never leave a previous successful reference available after a failed build.
rm -f "$IMAGE_REFS"
args=(build ./cmd --bare "--tags=$image_tag" "--image-refs=$IMAGE_REFS")
case "$mode" in
  build)
    : "${IMAGE_PLATFORM:=linux/$(go env GOHOSTARCH)}"
    if [[ "$IMAGE_PLATFORM" != linux/* || "$IMAGE_PLATFORM" == *,* ]]; then
      echo 'IMAGE_PLATFORM must select one Linux architecture for the local archive' >&2
      exit 1
    fi
    mkdir -p "$(dirname "$IMAGE_ARCHIVE")"
    rm -f "$IMAGE_ARCHIVE"
    "$KO" "${args[@]}" --push=false "--platform=$IMAGE_PLATFORM" "--tarball=$IMAGE_ARCHIVE" --tag-only
    ;;
  push)
    "$KO" "${args[@]}" --push=true "--platform=$PLATFORMS"
    ;;
  *) echo 'Expected build or push' >&2; exit 1 ;;
esac
