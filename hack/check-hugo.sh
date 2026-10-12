#!/usr/bin/env bash
# Copyright 2026 Project Capsule Authors
# SPDX-License-Identifier: Apache-2.0
set -euo pipefail

# Keep this aligned with docs/hugo.toml and the Devbox pin (checked by tooling tests).
minimum=0.166.0
version=''
compatible=false
if version="$(hugo version 2>/dev/null)" &&
  [[ "$version" =~ ^hugo\ v([0-9]+)\.([0-9]+)\.([0-9]+)([^[:space:]]*) ]]; then
  major="${BASH_REMATCH[1]}"
  minor="${BASH_REMATCH[2]}"
  patch="${BASH_REMATCH[3]}"
  edition="${BASH_REMATCH[4]}"
  IFS=. read -r min_major min_minor min_patch <<< "$minimum"
  if [[ "$edition" =~ (^|\+)extended(\+|$) ]] &&
    (( major > min_major ||
       (major == min_major && minor > min_minor) ||
       (major == min_major && minor == min_minor && patch >= min_patch) )); then
    compatible=true
  fi
fi

if [[ "$compatible" != true ]]; then
  printf 'Documentation requires Hugo Extended >= %s.\n' "$minimum" >&2
  printf 'Found: %s\n' "${version:-hugo is missing or could not run}" >&2
  printf '%s\n' \
    'Run from the repository root: devbox run -- task docs:serve' \
    'Or put a compatible Hugo Extended installation on PATH.' \
    'Older Hugo versions cannot load the theme and layouts/_shortcodes templates.' >&2
  exit 1
fi
