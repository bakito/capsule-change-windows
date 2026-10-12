#!/usr/bin/env bash
# Copyright 2026 Project Capsule Authors
# SPDX-License-Identifier: Apache-2.0
set -euo pipefail
umask 077

: "${DEV_CONTEXT:?DEV_CONTEXT must name the development kubeconfig context}"
: "${DEV_NAMESPACE:?DEV_NAMESPACE is required}"
: "${DEV_RELEASE:?DEV_RELEASE is required}"
: "${DEV_CERT_DIR:?DEV_CERT_DIR is required}"
mode=${1:?Expected setup or run}
case "$mode" in setup|run) ;; *) echo "Unknown operation: $mode" >&2; exit 1 ;; esac

# Validate before generating credentials or changing cluster resources.
kubectl --context "$DEV_CONTEXT" cluster-info >/dev/null
mkdir -p "$DEV_CERT_DIR"
chmod 700 "$DEV_CERT_DIR"
DEV_CERT_DIR=$(cd "$DEV_CERT_DIR" && pwd)
cert="$DEV_CERT_DIR/tls.crt"
key="$DEV_CERT_DIR/tls.key"

if [[ "$mode" == run ]]; then
  [[ -s "$cert" && -s "$key" ]] || { echo 'Run task dev:setup first to create the serving certificates.' >&2; exit 1; }
  # Snapshot only the explicitly selected context, without mutating the user's config.
  run_dir=$(mktemp -d "$DEV_CERT_DIR/run.XXXXXX")
  controller_pid=''
  # Called indirectly by the EXIT trap in this branch.
  # shellcheck disable=SC2329
  cleanup_run() {
    if [[ -n "$controller_pid" ]]; then
      kill -TERM "$controller_pid" 2>/dev/null || true
      wait "$controller_pid" || true
    fi
    rm -rf "$run_dir"
  }
  trap 'cleanup_run' EXIT
  trap 'exit 130' INT
  trap 'exit 143' TERM
  kubectl --context "$DEV_CONTEXT" config view --minify --flatten --raw > "$run_dir/kubeconfig"
  # go run does not reliably forward termination to its compiled child.
  go build -o "$run_dir/manager" ./cmd
  KUBECONFIG="$run_dir/kubeconfig" "$run_dir/manager" \
    --system-namespace="$DEV_NAMESPACE" \
    --webhook-cert-path="$DEV_CERT_DIR" \
    --metrics-bind-address=0 \
    --health-probe-bind-address=127.0.0.1:8081 &
  controller_pid=$!
  wait "$controller_pid"
  exit
fi

: "${DEV_HOST:?Set DEV_HOST or LAPTOP_HOST_IP to a host reachable from the API server}"
if [[ "$DEV_HOST" == *:* && "$DEV_HOST" =~ ^[0-9a-fA-F:]+$ ]]; then
  san="IP:$DEV_HOST"
  check=(-checkip "$DEV_HOST")
  url="https://[$DEV_HOST]:9443"
elif [[ "$DEV_HOST" =~ ^[0-9.]+$ ]]; then
  san="IP:$DEV_HOST"
  check=(-checkip "$DEV_HOST")
  url="https://$DEV_HOST:9443"
elif [[ "$DEV_HOST" =~ ^[a-zA-Z0-9][a-zA-Z0-9.-]*[a-zA-Z0-9]$ ]]; then
  san="DNS:$DEV_HOST"
  check=(-checkhost "$DEV_HOST")
  url="https://$DEV_HOST:9443"
else
  echo 'DEV_HOST must be an IP address or DNS name, without a scheme, port, or path.' >&2
  exit 1
fi
case "$DEV_HOST" in
  localhost|127.*|::1|0.0.0.0|::) echo 'DEV_HOST must be reachable from the cluster, not a loopback or unspecified address.' >&2; exit 1 ;;
esac

# Keep the trust bundle stable across repeat setup; renew expiring or mismatched certs.
if [[ ! -s "$cert" || ! -s "$key" ]] || \
  ! openssl x509 -in "$cert" -checkend 86400 -noout >/dev/null 2>&1 || \
  ! openssl x509 -in "$cert" "${check[@]}" -noout >/dev/null 2>&1; then
  temp=$(mktemp -d "$DEV_CERT_DIR/new.XXXXXX")
  trap 'rm -rf "$temp"' EXIT
  openssl req -x509 -newkey rsa:3072 -noenc -days 30 \
    -subj '/O=Project Capsule/CN=Change Windows development' \
    -addext "subjectAltName=$san" \
    -addext 'basicConstraints=critical,CA:TRUE' \
    -addext 'keyUsage=critical,digitalSignature,keyEncipherment,keyCertSign' \
    -addext 'extendedKeyUsage=serverAuth' \
    -keyout "$temp/tls.key" -out "$temp/tls.crt" 2>"$temp/openssl.log" || {
      cat "$temp/openssl.log" >&2
      exit 1
    }
  mv "$temp/tls.key" "$key"
  mv "$temp/tls.crt" "$cert"
  rm -rf "$temp"
  trap - EXIT
fi
chmod 600 "$key"
ca_bundle=$(openssl base64 -A -in "$cert")

# Inline CRDs avoid a hook job and make Helm the sole installer for development.
# Keep CRDs on uninstall, while removing the fail-closed webhook configuration.
helm --kube-context "$DEV_CONTEXT" upgrade --install "$DEV_RELEASE" charts/change-windows \
  --namespace "$DEV_NAMESPACE" --create-namespace --skip-crds --wait --timeout 2m \
  --set crds.install=true --set crds.inline=true --set crds.keep=true --set crds.exclusive=true \
  --set webhooks.enabled=true --set webhooks.exclusive=true \
  --set webhooks.certificate.generate.enabled=false \
  --set-string "webhooks.service.url=$url" \
  --set-string "webhooks.service.caBundle=$ca_bundle"
kubectl --context "$DEV_CONTEXT" wait --for=condition=Established \
  crd/changewindows.addons.projectcapsule.dev --timeout=60s
printf 'Admission points to %s. Start the host controller with task dev:run using the same DEV_CONTEXT and DEV_NAMESPACE.\n' "$url"
