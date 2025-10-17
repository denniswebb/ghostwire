#!/usr/bin/env bash
set -euo pipefail

# Exercise watcher label transitions and verify jump rule, metrics, and health behavior.

CLUSTER_NAME="${KIND_CLUSTER_NAME:-ghostwire-test}"
KUBE_CONTEXT="kind-${CLUSTER_NAME}"
NAMESPACE="ghostwire-test"
POD_NAME="${GHOSTWIRE_WATCHER_POD:-ghostwire-watcher-test}"
LABEL_KEY="role"
SLEEP_SECONDS=5

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

if ! cluster_exists; then
    echo "error: kind cluster '${CLUSTER_NAME}' not found. Run ./test/kind/setup-cluster.sh first." >&2
    exit 1
fi

log_step() {
    echo "[watcher-test] $*"
}

run_exec() {
    kubectl --context "${KUBE_CONTEXT}" exec -n "${NAMESPACE}" "${POD_NAME}" -c ghostwire-watcher -- "$@"
}

ensure_pod_ready() {
    status=$(kubectl --context "${KUBE_CONTEXT}" get pod "${POD_NAME}" -n "${NAMESPACE}" -o jsonpath='{.status.phase}' 2>/dev/null || true)
    if [[ -z "${status}" ]]; then
        echo "error: pod ${POD_NAME} not found in namespace ${NAMESPACE}" >&2
        exit 1
    fi
    if [[ "${status}" != "Running" ]]; then
        echo "error: pod ${POD_NAME} not running (phase=${status})" >&2
        exit 1
    fi
}

expect_jump_present() {
    if ! run_exec sh -c 'iptables -t nat -S OUTPUT | grep -q "CANARY_DNAT"'; then
        echo "error: expected CANARY_DNAT jump rule in OUTPUT chain" >&2
        exit 1
    fi
}

expect_jump_absent() {
    if run_exec sh -c 'iptables -t nat -S OUTPUT | grep -q "CANARY_DNAT"'; then
        echo "error: jump rule CANARY_DNAT unexpectedly present in OUTPUT chain" >&2
        exit 1
    fi
}

scrape_metrics() {
    run_exec wget -qO- http://localhost:8081/metrics
}

fetch_health() {
    run_exec wget -qO- http://localhost:8081/healthz
}

expect_log_contains() {
    local pattern="$1"
    if ! kubectl --context "${KUBE_CONTEXT}" logs -n "${NAMESPACE}" "${POD_NAME}" -c ghostwire-watcher --since=30s | grep -q "$pattern"; then
        echo "error: watcher logs missing pattern '$pattern'" >&2
        exit 1
    fi
}

ensure_pod_ready

log_step "Ensuring watcher label starts at active"
kubectl --context "${KUBE_CONTEXT}" label pod "${POD_NAME}" -n "${NAMESPACE}" "${LABEL_KEY}"=active --overwrite >/dev/null
sleep "${SLEEP_SECONDS}"

log_step "Verifying jump rule absent in initial active state"
expect_jump_absent

log_step "Switching to preview role"
kubectl --context "${KUBE_CONTEXT}" label pod "${POD_NAME}" -n "${NAMESPACE}" "${LABEL_KEY}"=preview --overwrite >/dev/null
sleep "${SLEEP_SECONDS}"

log_step "Validating jump rule installed"
expect_jump_present
expect_log_contains "activating dnat jump"

log_step "Returning to active role"
kubectl --context "${KUBE_CONTEXT}" label pod "${POD_NAME}" -n "${NAMESPACE}" "${LABEL_KEY}"=active --overwrite >/dev/null
sleep "${SLEEP_SECONDS}"

log_step "Validating jump rule removed"
expect_jump_absent
expect_log_contains "deactivating dnat jump"

log_step "Scraping metrics"
metrics_output=$(scrape_metrics)
if ! grep -q "ghostwire_jump_active 0" <<<"${metrics_output}"; then
    echo "error: expected ghostwire_jump_active to be 0 after returning to active" >&2
    exit 1
fi
if ! grep -q "# HELP ghostwire_errors_total" <<<"${metrics_output}"; then
    echo "error: ghostwire_errors_total metric not exposed" >&2
    exit 1
fi
dnat_expected=$(run_exec sh -c "grep -v '^#' /shared/dnat.map | grep -v '^\\s*$' | wc -l")
dnat_expected=${dnat_expected//[$'\r\n ']}
if ! grep -q "ghostwire_dnat_rules ${dnat_expected}" <<<"${metrics_output}"; then
    echo "error: expected ghostwire_dnat_rules ${dnat_expected}, got:\n${metrics_output}" >&2
    exit 1
fi

log_step "Checking health endpoint"
health_output=$(fetch_health)
if [[ "${health_output}" != "OK"* ]]; then
    echo "error: /healthz returned unexpected payload: ${health_output}" >&2
    exit 1
fi

log_step "All watcher transition checks passed"
