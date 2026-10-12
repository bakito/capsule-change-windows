#!/usr/bin/env bash
# Copyright 2026 Project Capsule Authors
# SPDX-License-Identifier: Apache-2.0
set -euo pipefail
: "${COMMIT_HEAD:=HEAD}"
if [[ -z "${COMMIT_BASE:-}" || "$COMMIT_BASE" =~ ^0+$ ]]; then
  commits=$(git rev-list -1 "$COMMIT_HEAD" --)
else
  commits=$(git rev-list "$COMMIT_BASE..$COMMIT_HEAD" --)
fi
while IFS= read -r commit; do
  [[ -n "$commit" ]] || continue
  git show -s --format=%B "$commit" -- | cog verify --ignore-merge-commits --file -
done <<< "$commits"
