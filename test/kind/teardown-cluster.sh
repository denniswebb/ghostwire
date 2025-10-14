#!/usr/bin/env bash
set -euo pipefail

CLUSTER_NAME="${KIND_CLUSTER_NAME:-ghostwire-test}"

require_binary() {
	if ! command -v "$1" >/dev/null 2>&1; then
		echo "error: required command '$1' not found in PATH" >&2
		exit 1
	}
}

cluster_exists() {
	kind get clusters 2>/dev/null | grep -qx "${CLUSTER_NAME}"
}

require_binary kind

if ! cluster_exists; then
	echo "kind cluster '${CLUSTER_NAME}' is already deleted."
	exit 0
fi

echo "Deleting kind cluster '${CLUSTER_NAME}'..."
kind delete cluster --name "${CLUSTER_NAME}"
echo "Cluster '${CLUSTER_NAME}' deleted."
