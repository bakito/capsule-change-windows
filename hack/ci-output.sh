#!/usr/bin/env bash
# Copyright 2026 Project Capsule Authors
# SPDX-License-Identifier: Apache-2.0
set -euo pipefail
case "${1:?Expected cache, version, or image}" in
  cache)
    : "${GITHUB_OUTPUT:?GITHUB_OUTPUT is required}"
    printf 'build=%s\nmodules=%s\n' "$(go env GOCACHE)" "$(go env GOMODCACHE)" >> "$GITHUB_OUTPUT"
    ;;
  version)
    : "${GITHUB_REF_NAME:?GITHUB_REF_NAME is required}"
    version=${GITHUB_REF_NAME#v}
    if [[ ! "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]]; then
      echo 'Release tag must be vMAJOR.MINOR.PATCH with an optional prerelease' >&2
      exit 1
    fi
    printf 'version=%s\n' "$version" >> "${GITHUB_OUTPUT:?GITHUB_OUTPUT is required}"
    ;;
  image)
    : "${IMAGE_REFS:=dist/image-refs.txt}"
    ref=$(cat "$IMAGE_REFS")
    if [[ ! "$ref" =~ ^[^[:space:]@]+@sha256:[a-f0-9]{64}$ ]]; then
      echo 'Expected one immutable published image reference' >&2
      exit 1
    fi
    image=${ref%@*}
    # ko includes the build tag before the digest; provenance needs the repository.
    if [[ "${image##*/}" == *:* ]]; then image=${image%:*}; fi
    printf 'image=%s\ndigest=%s\n' "$image" "${ref##*@}" >> "${GITHUB_OUTPUT:?GITHUB_OUTPUT is required}"
    ;;
  *) echo 'Unknown CI output' >&2; exit 1 ;;
esac
