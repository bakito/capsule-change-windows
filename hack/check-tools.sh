#!/usr/bin/env bash
# Copyright 2026 Project Capsule Authors
# SPDX-License-Identifier: Apache-2.0
set -euo pipefail

missing=false
for tool in "$@"; do
  if ! command -v "$tool" >/dev/null 2>&1; then
    printf 'Missing development tool: %s\n' "$tool" >&2
    missing=true
  fi
done
if [[ "$missing" == true ]]; then
  printf '%s\n' \
    'Use the locked Devbox toolchain: enter "devbox shell" and rerun the task,' \
    'or prefix your command with "devbox run --" (including all Task arguments).' \
    'If Devbox is not installed, see DEVELOPMENT.md for Devbox and Nix setup.' >&2
  exit 1
fi
