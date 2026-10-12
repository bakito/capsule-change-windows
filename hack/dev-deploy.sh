#!/usr/bin/env bash
# Copyright 2026 Project Capsule Authors
# SPDX-License-Identifier: Apache-2.0
set -euo pipefail
umask 077

: "${DEV_CONTEXT:?DEV_CONTEXT must name the development kubeconfig context}"
: "${DEV_NAMESPACE:?DEV_NAMESPACE is required}"
: "${DEV_RELEASE:?DEV_RELEASE is required}"
: "${IMG:?IMG must be a tagged image reference}"
: "${DEV_IMAGE_MODE:=kind}"
: "${DEV_TIMEOUT:=180}"
case "$DEV_IMAGE_MODE" in
  kind) : "${DEV_KIND_CLUSTER:?DEV_KIND_CLUSTER must name an existing Kind cluster}" ;;
  registry) ;;
  *) echo 'DEV_IMAGE_MODE must be kind or registry' >&2; exit 1 ;;
esac
[[ "$DEV_TIMEOUT" =~ ^[1-9][0-9]*$ ]] || { echo 'DEV_TIMEOUT must be a positive number of seconds' >&2; exit 1; }
values=(--reset-values)
if [[ -n "${DEV_HELM_VALUES:-}" ]]; then
  [[ -f "$DEV_HELM_VALUES" ]] || { echo "Helm values file not found: $DEV_HELM_VALUES" >&2; exit 1; }
  values+=(-f "$DEV_HELM_VALUES")
fi
kube=(kubectl --context "$DEV_CONTEXT")
"${kube[@]}" cluster-info >/dev/null
"${kube[@]}" get crd certificates.cert-manager.io issuers.cert-manager.io >/dev/null || {
  echo 'Install cert-manager before running dev:deploy.' >&2
  exit 1
}

temp=$(mktemp -d)
cleanup() {
  result=$?
  rm -rf "$temp"
  exit "$result"
}
trap cleanup EXIT
pull_policy=IfNotPresent
if [[ "$DEV_IMAGE_MODE" == kind ]]; then
  # Verify the selected context actually connects to the cluster receiving the image.
  kind get kubeconfig --name "$DEV_KIND_CLUSTER" > "$temp/kubeconfig"
  cluster_path='{.clusters[0].cluster.server}{"\n"}{.clusters[0].cluster.certificate-authority-data}'
  expected=$(kubectl --kubeconfig "$temp/kubeconfig" config view --minify --flatten --raw -o "jsonpath=$cluster_path")
  actual=$("${kube[@]}" config view --minify --flatten --raw -o "jsonpath=$cluster_path")
  [[ -n "$expected" && "$actual" == "$expected" ]] || { echo 'DEV_CONTEXT does not point to DEV_KIND_CLUSTER' >&2; exit 1; }
  arch=$("${kube[@]}" get nodes -o 'jsonpath={range .items[*]}{.status.nodeInfo.architecture}{"\n"}{end}' | sort -u)
  case "$arch" in amd64|arm64) ;; *) echo 'Kind deployment requires a single amd64 or arm64 architecture' >&2; exit 1 ;; esac
  if [[ -n "${IMAGE_PLATFORM:-}" && "$IMAGE_PLATFORM" != "linux/$arch" ]]; then
    echo "IMAGE_PLATFORM must match the Kind nodes: linux/$arch" >&2
    exit 1
  fi
  export IMAGE_PLATFORM="linux/$arch"
  # Repeated deployments of an uncommitted working tree must replace running Pods.
  IMG="$IMG-$(date -u +%Y%m%d%H%M%S)-$(openssl rand -hex 4)"
  export IMG
  task image:build
  kind load image-archive "$IMAGE_ARCHIVE" --name "$DEV_KIND_CLUSTER"
  image="$IMG"
  pull_policy=Never
  # Kind runs inside containers which may not support nested user namespaces.
  values+=(--set manager.hostUsers=true)
else
  task image:push
  image=$(cat "$IMAGE_REFS")
  if [[ ! "$image" =~ ^[^[:space:]@]+@sha256:[a-f0-9]{64}$ || "${image%@*}" != "${IMG%:*}" ]]; then
    echo 'ko did not produce the expected immutable image reference' >&2
    exit 1
  fi
fi

# Inline CRDs avoid a separate hook image and support upgrades from dev:setup.
# Explicitly replace host webhook settings when switching to in-cluster execution.
helm --kube-context "$DEV_CONTEXT" upgrade --install "$DEV_RELEASE" charts/change-windows \
  --namespace "$DEV_NAMESPACE" --create-namespace --skip-crds \
  --wait --timeout "${DEV_TIMEOUT}s" "${values[@]}" \
  --set crds.install=true --set crds.inline=true --set crds.keep=true --set crds.exclusive=false \
  --set webhooks.enabled=true --set webhooks.exclusive=false \
  --set-string webhooks.service.url= --set-string webhooks.service.caBundle= \
  --set webhooks.certificate.generate.enabled=true \
  --set-string "manager.image.reference=$image" \
  --set-string "manager.image.pullPolicy=$pull_policy"
selector="app.kubernetes.io/instance=$DEV_RELEASE"
"${kube[@]}" -n "$DEV_NAMESPACE" rollout status deployment --selector "$selector" --timeout="${DEV_TIMEOUT}s"
"${kube[@]}" -n "$DEV_NAMESPACE" wait certificate --selector "$selector" --for=condition=Ready --timeout="${DEV_TIMEOUT}s"
for index in 0 1; do
  "${kube[@]}" wait validatingwebhookconfiguration --selector "$selector" \
    --for="jsonpath={.webhooks[$index].clientConfig.caBundle}" --timeout="${DEV_TIMEOUT}s"
done
# Probe through the API server to check TLS and admission registration, with no persisted object.
cat > "$temp/probe.yaml" <<'YAML'
apiVersion: addons.projectcapsule.dev/v1alpha1
kind: ChangeWindow
metadata:
  generateName: dev-readiness-
spec:
  namespaceSelectors:
    - matchLabels:
        change-windows.dev/readiness: probe
  schedules:
    - name: probe
      cron: '0 0 * * *'
      duration: 1m
  targets:
    - name: configs
      apiVersion: v1
      kind: ConfigMap
  admission:
    - name: probe
      targets: [configs]
      operations: [UPDATE]
      when: Active
      action: Deny
      message: Readiness probe
YAML
deadline=$((SECONDS + DEV_TIMEOUT))
until "${kube[@]}" create --dry-run=server -f "$temp/probe.yaml" > "$temp/probe.log" 2>&1; do
  if (( SECONDS >= deadline )); then cat "$temp/probe.log" >&2; exit 1; fi
  sleep 1
done
printf 'Deployed %s to %s (%s/%s); controller and admission are ready.\n' "$image" "$DEV_CONTEXT" "$DEV_NAMESPACE" "$DEV_RELEASE"
