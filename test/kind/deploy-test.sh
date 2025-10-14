#!/usr/bin/env bash
set -euo pipefail

# shellcheck disable=SC2155
SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" >/dev/null 2>&1 && pwd)"
MANIFEST_DIR="${SCRIPT_DIR}/manifests"
CLUSTER_NAME="${KIND_CLUSTER_NAME:-ghostwire-test}"
KUBE_CONTEXT="kind-${CLUSTER_NAME}"
WITH_POD="false"

for arg in "$@"; do
	case "${arg}" in
		--with-pod)
			WITH_POD="true"
			shift
			;;
		*)
			echo "usage: $0 [--with-pod]" >&2
			exit 1
			;;
	esac
done

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
require_binary kubectl

if ! cluster_exists; then
	echo "error: kind cluster '${CLUSTER_NAME}' not found. Run ./test/kind/setup-cluster.sh first." >&2
	exit 1
fi

echo "Applying namespace and RBAC manifests..."
kubectl --context "${KUBE_CONTEXT}" apply -f "${MANIFEST_DIR}/namespace.yaml"
kubectl --context "${KUBE_CONTEXT}" apply -f "${MANIFEST_DIR}/rbac.yaml"

echo "Applying service manifests..."
kubectl --context "${KUBE_CONTEXT}" apply -f "${MANIFEST_DIR}/services.yaml"

echo "Current resources in ghostwire-test namespace:"
kubectl --context "${KUBE_CONTEXT}" get all -n ghostwire-test

if [[ "${WITH_POD}" == "true" ]]; then
	echo "Deploying ghostwire init test pod..."
	kubectl --context "${KUBE_CONTEXT}" apply -f "${MANIFEST_DIR}/test-pod.yaml"
	echo "Waiting for pod completion..."
	kubectl --context "${KUBE_CONTEXT}" wait --for=condition=Completed pod/ghostwire-init-test -n ghostwire-test --timeout=180s
	kubectl --context "${KUBE_CONTEXT}" logs -n ghostwire-test ghostwire-init-test
fi

echo "Deployment complete. Validate outputs with:"
echo "  ./test/kind/validate-dnatmap.sh"
echo "  ./test/kind/validate-iptables.sh"
