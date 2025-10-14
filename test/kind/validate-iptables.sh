#!/usr/bin/env bash
set -euo pipefail

CLUSTER_NAME="${KIND_CLUSTER_NAME:-ghostwire-test}"
CHAIN_NAME="${GHOSTWIRE_CHAIN:-CANARY_DNAT}"
KUBE_CONTEXT="kind-${CLUSTER_NAME}"
NAMESPACE="ghostwire-test"
POD_NAME="${GHOSTWIRE_INIT_POD:-ghostwire-init-test}"

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

if [[ "${PHASE}" != "Running" ]]; then
	echo "error: pod ${POD_NAME} is in phase '${PHASE}'. Ensure the debug container is running." >&2
	exit 1
fi

echo "Inspecting iptables rules in pod ${POD_NAME} (chain ${CHAIN_NAME})..."
if ! kubectl --context "${KUBE_CONTEXT}" exec -n "${NAMESPACE}" "${POD_NAME}" -c debug -- iptables -t nat -L "${CHAIN_NAME}" -n >/dev/null 2>&1; then
	echo "error: chain ${CHAIN_NAME} not found in pod namespace." >&2
	exit 1
fi

if ! RULES=$(kubectl --context "${KUBE_CONTEXT}" exec -n "${NAMESPACE}" "${POD_NAME}" -c debug -- iptables -t nat -S "${CHAIN_NAME}" 2>/dev/null); then
	echo "error: failed to read iptables rules" >&2
	exit 1
fi

echo "iptables -t nat -S ${CHAIN_NAME} output:"
echo "-----------------------------------------"
echo "${RULES}"
echo "-----------------------------------------"

expect_rule() {
	if ! grep -q "$1" <<<"${RULES}"; then
		echo "error: expected rule \"$1\" not present" >&2
		return 1
	fi
}

get_cluster_ip() {
	local svc="$1"
	kubectl --context "${KUBE_CONTEXT}" -n "${NAMESPACE}" get service "${svc}" -o jsonpath='{.spec.clusterIP}'
}

expect_rule "-A ${CHAIN_NAME} -d 169.254.169.254/32 -j RETURN"

services=(
	"orders orders-preview 80"
	"orders orders-preview 443"
	"payment payment-preview 8080"
	"api-v2 api-v2-preview 8080"
	"api-v2 api-v2-preview 8443"
	"api-v2 api-v2-preview 9090"
)

for entry in "${services[@]}"; do
	read -r active preview port <<<"${entry}"
	active_ip=$(get_cluster_ip "${active}")
	preview_ip=$(get_cluster_ip "${preview}")
	if [[ -z "${active_ip}" || -z "${preview_ip}" || "${active_ip}" == "None" || "${preview_ip}" == "None" ]]; then
		echo "error: missing cluster IPs for ${active}/${preview}" >&2
		exit 1
	fi
	rule="-A ${CHAIN_NAME} -d ${active_ip} -p tcp --dport ${port} -j DNAT --to-destination ${preview_ip}:${port}"
	expect_rule "${rule}"
done

echo "iptables validation succeeded."
