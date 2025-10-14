#!/usr/bin/env bash
set -euo pipefail

# shellcheck disable=SC2155
SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" >/dev/null 2>&1 && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/../.." >/dev/null 2>&1 && pwd)"
CLUSTER_NAME="${KIND_CLUSTER_NAME:-ghostwire-test}"
IMAGE_TAG="${GHOSTWIRE_IMAGE_TAG:-ghostwire:local}"

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
require_binary docker

if ! cluster_exists; then
	echo "error: kind cluster '${CLUSTER_NAME}' not found. Run ./test/kind/setup-cluster.sh first." >&2
	exit 1
fi

echo "Building ghostwire binary..."
if command -v mise >/dev/null 2>&1; then
	(
		cd "${PROJECT_ROOT}"
		mise run build
	)
else
	echo "warning: mise not found, falling back to 'go build'" >&2
	(
		cd "${PROJECT_ROOT}"
		go build -o ghostwire ./cmd/ghostwire
	)
fi

BIN_PATH="${PROJECT_ROOT}/ghostwire"
if [[ ! -f "${BIN_PATH}" ]]; then
	echo "error: compiled binary not found at ${BIN_PATH}" >&2
	exit 1
fi

BUILD_DIR="$(mktemp -d)"
trap 'rm -rf "${BUILD_DIR}"' EXIT
cp "${BIN_PATH}" "${BUILD_DIR}/ghostwire"
chmod 0755 "${BUILD_DIR}/ghostwire"

cat > "${BUILD_DIR}/Dockerfile" <<'EOF'
FROM alpine:3.20
RUN addgroup -S ghostwire && adduser -S ghostwire -G ghostwire \
    && apk add --no-cache iptables
COPY ghostwire /usr/local/bin/ghostwire
USER ghostwire
ENTRYPOINT ["/usr/local/bin/ghostwire"]
EOF

echo "Building container image ${IMAGE_TAG}..."
docker build --tag "${IMAGE_TAG}" "${BUILD_DIR}"

echo "Loading image into kind cluster '${CLUSTER_NAME}'..."
kind load docker-image "${IMAGE_TAG}" --name "${CLUSTER_NAME}"

echo "Image '${IMAGE_TAG}' loaded. Deploy with ./test/kind/deploy-test.sh"
