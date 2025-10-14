#!/usr/bin/env bash
set -euo pipefail

CLUSTER_NAME="${KIND_CLUSTER_NAME:-ghostwire-test}"
KUBE_CONTEXT="kind-${CLUSTER_NAME}"
NAMESPACE="ghostwire-test"
POD_NAME="${GHOSTWIRE_INIT_POD:-ghostwire-init-test}"
TMP_FILE=""

cleanup() {
	if [[ -n "${TMP_FILE}" && -f "${TMP_FILE}" ]]; then
		rm -f "${TMP_FILE}"
	fi
}

trap cleanup EXIT

require_binary() {
	if ! command -v "$1" >/dev/null 2>&1; then
		echo "error: required command '$1' not found in PATH" >&2
		exit 1
	fi
}

require_binary kubectl

echo "Checking pod status..."
PHASE=$(kubectl --context "${KUBE_CONTEXT}" get pod "${POD_NAME}" -n "${NAMESPACE}" -o jsonpath='{.status.phase}' 2>/dev/null || true)
if [[ -z "${PHASE}" ]]; then
	echo "error: pod ${POD_NAME} not found in namespace ${NAMESPACE}" >&2
	exit 1
fi

if [[ "${PHASE}" != "Running" && "${PHASE}" != "Succeeded" ]]; then
	echo "error: pod ${POD_NAME} is in phase '${PHASE}'. Ensure the init workflow completed successfully." >&2
	exit 1
fi

if [[ "${PHASE}" == "Running" ]]; then
	echo "Reading /shared/dnat.map via exec..."
	if ! MAP_CONTENT=$(kubectl --context "${KUBE_CONTEXT}" exec -n "${NAMESPACE}" "${POD_NAME}" -c debug -- cat /shared/dnat.map 2>/dev/null); then
		echo "kubectl exec failed; attempting fallback via kubectl cp..."
		TMP_FILE="$(mktemp)"
		if ! kubectl --context "${KUBE_CONTEXT}" cp "${NAMESPACE}/${POD_NAME}:/shared/dnat.map" "${TMP_FILE}" >/dev/null 2>&1; then
			echo "error: unable to retrieve dnat.map via exec or cp" >&2
			exit 1
		fi
		MAP_CONTENT=$(cat "${TMP_FILE}")
	fi
else
	echo "Pod phase '${PHASE}' detected; retrieving /shared/dnat.map via kubectl cp..."
	TMP_FILE="$(mktemp)"
	if ! kubectl --context "${KUBE_CONTEXT}" cp "${NAMESPACE}/${POD_NAME}:/shared/dnat.map" "${TMP_FILE}" >/dev/null 2>&1; then
		echo "error: unable to retrieve dnat.map from completed pod" >&2
		exit 1
	fi
	MAP_CONTENT=$(cat "${TMP_FILE}")
fi
echo "dnat.map contents:"
echo "------------------"
echo "${MAP_CONTENT}"
echo "------------------"

check_contains() {
	if ! grep -q "$1" <<<"${MAP_CONTENT}"; then
		echo "error: expected entry \"$1\" not found in dnat.map" >&2
		return 1
	fi
}

check_absent() {
	if grep -q "$1" <<<"${MAP_CONTENT}"; then
		echo "error: unexpected entry \"$1\" found in dnat.map" >&2
		return 1
	fi
}

check_contains "orders:80/TCP"
check_contains "orders:443/TCP"
check_contains "payment:8080/TCP"
check_contains "api-v2:8080/TCP"
check_contains "api-v2:8443/TCP"
check_contains "api-v2:9090/TCP"
check_absent "users:"
check_absent "headless:"

echo "DNAT map validation passed."
