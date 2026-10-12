#!/usr/bin/env bash
# Copyright 2026 Project Capsule Authors
# SPDX-License-Identifier: Apache-2.0
set -euo pipefail
# Git hooks also work when commits are made outside an interactive Devbox shell.
if [[ "${DEVBOX_SHELL_ENABLED:-}" == 1 ]]; then
  exec "$@"
fi
exec devbox run -- "$@"
