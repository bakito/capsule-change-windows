#!/usr/bin/env bash
# Copyright 2026 Project Capsule Authors
# SPDX-License-Identifier: Apache-2.0
set -euo pipefail
: "${IMG:?IMG is required}"
: "${KUSTOMIZE:?KUSTOMIZE is required}"
render_dir=$(mktemp -d)
trap 'rm -rf "$render_dir"' EXIT
cp -R config "$render_dir/config"
cd "$render_dir/config/manager"
"$KUSTOMIZE" edit set image "controller=$IMG"
"$KUSTOMIZE" build ../default
