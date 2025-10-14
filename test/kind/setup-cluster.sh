#!/usr/bin/env bash
set -euo pipefail

# shellcheck disable=SC2155
SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" >/dev/null 2>&1 && pwd)"
CLUSTER_NAME="${KIND_CLUSTER_NAME:-ghostwire-test}"
CONFIG_FILE="${SCRIPT_DIR}/cluster-config.yaml"

require_binary() {
	if ! command -v "$1" >/dev/null 2>&1; then
		echo "error: required command '$1' not found in PATH" >&2
		exit 1
	}
}

cluster_exists() {
	kind get clusters 2>/dev/null | grep -qx "${CLUSTER_NAME}"
}

prompt_recreate() {
	read -r -p "kind cluster '${CLUSTER_NAME}' already exists. Recreate it? [y/N] " response
	case "${response}" in
		[yY][eE][sS]|[yY]) return 0 ;;
		*) return 1 ;;
	esac
}

require_binary kind
require_binary kubectl

if [[ ! -f "${CONFIG_FILE}" ]]; then
	echo "error: cluster config not found at ${CONFIG_FILE}" >&2
	exit 1
fi

if cluster_exists; then
	if prompt_recreate; then
		echo "Deleting existing cluster '${CLUSTER_NAME}'..."
		kind delete cluster --name "${CLUSTER_NAME}"
	else
		echo "Cluster '${CLUSTER_NAME}' already exists. Skipping creation."
		exit 0
	fi
fi

echo "Creating kind cluster '${CLUSTER_NAME}'..."
kind create cluster --name "${CLUSTER_NAME}" --config "${CONFIG_FILE}"

KUBE_CONTEXT="kind-${CLUSTER_NAME}"
echo "Waiting for nodes to become Ready..."
kubectl wait --context "${KUBE_CONTEXT}" --for=condition=Ready nodes --all --timeout=180s

echo "Cluster '${CLUSTER_NAME}' is ready."
echo
echo "Next steps:"
echo "  - kubeconfig context: ${KUBE_CONTEXT}"
echo "  - kubectl cluster-info --context ${KUBE_CONTEXT}"
echo "  - ./test/kind/load-image.sh to build and load the ghostwire image"
echo "  - ./test/kind/deploy-test.sh to apply manifests"
echo "  - ./test/kind/teardown-cluster.sh to remove the cluster"
