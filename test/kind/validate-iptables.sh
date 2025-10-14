#!/usr/bin/env bash
set -euo pipefail

CLUSTER_NAME="${KIND_CLUSTER_NAME:-ghostwire-test}"
CHAIN_NAME="${GHOSTWIRE_CHAIN:-CANARY_DNAT}"
KUBE_CONTEXT="kind-${CLUSTER_NAME}"
NAMESPACE="ghostwire-test"

require_binary() {
	if ! command -v "$1" >/dev/null 2>&1; then
		echo "error: required command '$1' not found in PATH" >&2
		exit 1
	}
}

require_binary kind
require_binary docker
require_binary kubectl

NODE_NAME=$(kind get nodes --name "${CLUSTER_NAME}" 2>/dev/null | head -n1 || true)
if [[ -z "${NODE_NAME}" ]]; then
	echo "error: unable to determine kind node for cluster '${CLUSTER_NAME}'" >&2
	exit 1
fi

echo "Inspecting iptables rules on node ${NODE_NAME} (chain ${CHAIN_NAME})..."
if ! docker exec "${NODE_NAME}" iptables -t nat -L "${CHAIN_NAME}" -n >/dev/null 2>&1; then
	echo "error: chain ${CHAIN_NAME} not found. Ensure the init container completed successfully." >&2
	exit 1
fi

RULES=$(docker exec "${NODE_NAME}" iptables -t nat -S "${CHAIN_NAME}")
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
