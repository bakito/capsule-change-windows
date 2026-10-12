#!/usr/bin/env bash
# Copyright 2026 Project Capsule Authors
# SPDX-License-Identifier: Apache-2.0
set -euo pipefail
: "${IMAGE_REFS:=dist/image-refs.txt}"
ref=$(cat "$IMAGE_REFS")
if [[ ! "$ref" =~ ^[^[:space:]@]+@sha256:[a-f0-9]{64}$ ]]; then
  echo 'Expected one immutable image reference; run task image:push first' >&2
  exit 1
fi
cosign sign --yes "$ref"
