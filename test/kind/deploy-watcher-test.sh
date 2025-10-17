#!/usr/bin/env bash
set -euo pipefail

# Deploy the watcher sidecar test pod and supporting RBAC into the ghostwire KIND cluster.

# shellcheck disable=SC2155
SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" >/dev/null 2>&1 && pwd)"
MANIFEST_DIR="${SCRIPT_DIR}/manifests"
CLUSTER_NAME="${KIND_CLUSTER_NAME:-ghostwire-test}"
KUBE_CONTEXT="kind-${CLUSTER_NAME}"
POD_NAME="ghostwire-watcher-test"
NAMESPACE="ghostwire-test"

require_binary() {
    if ! command -v "$1" >/dev/null 2>&1; then
        echo "error: required command '$1' not found in PATH" >&2
        exit 1
    fi
}

cluster_exists() {
    kind get clusters 2>/dev/null | grep -qx "${CLUSTER_NAME}"
}

require_binary kind
require_binary kubectl

if [[ ! -f "${MANIFEST_DIR}/watcher-test-pod.yaml" ]]; then
    echo "error: watcher manifests not found. Run from repository root." >&2
    exit 1
fi

if ! cluster_exists; then
    echo "error: kind cluster '${CLUSTER_NAME}' not found. Run ./test/kind/setup-cluster.sh first." >&2
    exit 1
fi

echo "Applying watcher RBAC..."
kubectl --context "${KUBE_CONTEXT}" apply -f "${MANIFEST_DIR}/watcher-rbac.yaml"

echo "Applying watcher test pod..."
kubectl --context "${KUBE_CONTEXT}" apply -f "${MANIFEST_DIR}/watcher-test-pod.yaml"

echo "Waiting for watcher pod readiness..."
kubectl --context "${KUBE_CONTEXT}" wait --for=condition=Ready "pod/${POD_NAME}" -n "${NAMESPACE}" --timeout=120s

echo "Watcher pod status:"
kubectl --context "${KUBE_CONTEXT}" get pod "${POD_NAME}" -n "${NAMESPACE}"

cat <<EOF

Next steps:
  # Follow watcher logs
  kubectl --context ${KUBE_CONTEXT} logs -n ${NAMESPACE} ${POD_NAME} -c ghostwire-watcher -f

  # Check health endpoint
  kubectl --context ${KUBE_CONTEXT} exec -n ${NAMESPACE} ${POD_NAME} -c ghostwire-watcher -- wget -qO- http://localhost:8081/healthz

  # Check metrics endpoint
  kubectl --context ${KUBE_CONTEXT} exec -n ${NAMESPACE} ${POD_NAME} -c ghostwire-watcher -- wget -qO- http://localhost:8081/metrics

  # Trigger preview routing
  kubectl --context ${KUBE_CONTEXT} label pod ${POD_NAME} -n ${NAMESPACE} role=preview --overwrite

  # Inspect jump rule
  kubectl --context ${KUBE_CONTEXT} exec -n ${NAMESPACE} ${POD_NAME} -c ghostwire-watcher -- iptables -t nat -L OUTPUT -n -v | grep CANARY_DNAT

  # Revert to active routing
  kubectl --context ${KUBE_CONTEXT} label pod ${POD_NAME} -n ${NAMESPACE} role=active --overwrite
EOF

echo "Deployment complete." 
