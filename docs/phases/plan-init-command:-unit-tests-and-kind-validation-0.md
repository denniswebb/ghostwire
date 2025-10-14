I have created the following plan after thorough exploration and analysis of the codebase. Follow the below plan verbatim. Trust the files and references. Do not re-verify what's written in the plan. Explore only when absolutely necessary. First implement all the proposed file changes and then I'll review all the changes together at the end.

### Observations

The project already has a solid testing foundation in `iptables_test.go` with a `recordingExecutor` mock pattern that should be replicated for Kubernetes client mocking. The discovery package has complex logic (service pairing, port matching, IP validation) that needs thorough unit test coverage. The template package has caching and pattern rendering that needs validation. The AGENTS.md emphasizes table-driven tests and testability via dependency injection. The user wants incremental validation ("test after each phase") and local KIND cluster testing capability. No integration tests exist yet, making this the foundational testing phase.

### Approach

Build comprehensive unit tests for the discovery and template packages following the existing iptables test patterns (mock interfaces, table-driven tests, parallel execution). Create a KIND-based integration testing framework with cluster setup scripts, sample Service manifests, manual testing procedures, and validation scripts. This phase validates the init command implementation and establishes testing infrastructure for future phases.

### Reasoning

Listed the repository structure to identify existing test files. Read the existing `iptables_test.go` to understand the established testing patterns (recordingExecutor mock, table-driven tests with t.Parallel()). Examined the discovery and template packages to identify testable functions and edge cases. Reviewed README.md and AGENTS.md to understand testing expectations (mise run test, act integration, KIND cluster usage). Confirmed no `/test/kind/` directory exists yet, requiring creation from scratch.

## Proposed File Changes

### internal/discovery/template_test.go(NEW)

References: 

- internal/discovery/template.go
- internal/iptables/iptables_test.go(MODIFY)

Create comprehensive unit tests for the template package. Implement table-driven tests for `ApplyPattern` function covering: (1) default pattern `{{name}}-preview` with various service names (simple names like "orders", hyphenated names like "payment-api", names with numbers like "svc-v2"), (2) custom patterns like `{{name}}-canary`, `preview-{{name}}`, `{{name}}`, (3) invalid patterns (unclosed braces, invalid template syntax) that should return errors, (4) empty service names and empty patterns. Add tests for `DerivePreviewName` function covering: (1) suffix-based derivation when activeSuffix and previewSuffix are provided (e.g., "orders-active" -> "orders-preview"), (2) fallback to pattern when service doesn't have activeSuffix, (3) edge cases like empty suffixes, service name exactly matching suffix. Verify template caching by calling `ApplyPattern` multiple times with the same pattern and confirming it doesn't re-parse (this can be implicit via performance or explicit via inspecting templateCache if exported for testing). Use `t.Parallel()` for all test cases following the project's testing conventions. Add a helper function similar to `discardLogger()` from iptables tests if logging is needed. Structure tests with clear subtests using `t.Run()` for each scenario.

### internal/discovery/discovery_test.go(NEW)

References: 

- internal/discovery/discovery.go
- internal/discovery/types.go
- internal/iptables/iptables_test.go(MODIFY)

Create comprehensive unit tests for the discovery package. Implement a `fakeClientset` mock that implements the necessary Kubernetes client-go interfaces to return predefined Service lists without requiring a real cluster. The mock should allow tests to configure the Services that will be returned by `List()` calls. Create table-driven tests for the `Discover` function covering: (1) **Happy path**: namespace with matching base/preview pairs (e.g., "orders" + "orders-preview", "payment" + "payment-preview") with multiple ports, verify correct ServiceMapping structs are returned with all fields populated, (2) **No preview service**: base service exists but no matching preview, verify it's skipped and logged at Debug level, (3) **Headless services**: services with ClusterIP="None" should be skipped with warning, (4) **Empty ClusterIP**: services with empty ClusterIP should be skipped, (5) **Identical ClusterIPs**: base and preview with same ClusterIP should be skipped with warning, (6) **No ports**: service with empty Ports slice should be skipped with warning, (7) **Port mismatches**: preview service missing ports that base has, verify only matching ports create mappings and mismatches are logged, (8) **Protocol mismatches**: base port is TCP but preview port is UDP for same port number, verify skipped with warning, (9) **Multiple ports**: service with multiple ports (80, 443, 8080) all get individual mappings, (10) **IPv4 and IPv6 ClusterIPs**: services with both IP families, (11) **Suffix-based pairing**: when ActiveSuffix="-active" and PreviewSuffix="-preview", verify "orders-active" pairs with "orders-preview", (12) **Pattern-based pairing**: when using default pattern, verify "orders" pairs with "orders-preview", (13) **Preview service skipping**: when using default pattern with "-preview" suffix, verify services ending in "-preview" are skipped as base services to avoid double-processing. Test error cases: (1) nil clientset returns error, (2) empty namespace returns error, (3) empty preview pattern returns error, (4) Kubernetes API error during List() is propagated. Use a test logger that captures log output (via `slog.NewTextHandler` writing to a `bytes.Buffer`) to verify warning and debug messages are logged correctly. Use `t.Parallel()` for all test cases. Structure with clear subtests for each scenario.

### internal/discovery/types_test.go(NEW)

References: 

- internal/discovery/types.go

Create unit tests for the ServiceMapping type. Test the `String()` method (if implemented) to verify it returns a human-readable representation. If String() doesn't exist yet, add tests for basic struct field validation and equality comparisons. Test edge cases like zero values, maximum port numbers (65535), all protocol types (TCP, UDP, SCTP), IPv4 and IPv6 addresses. Keep this file simple since types.go is primarily struct definitions. Use `t.Parallel()` for test cases.

### internal/iptables/iptables_test.go(MODIFY)

References: 

- internal/iptables/iptables.go
- internal/iptables/chain.go
- internal/iptables/dnatmap.go

Expand the existing iptables tests to add coverage for: (1) `EnsureChain` function: test chain creation when it doesn't exist, test chain flushing when it already exists, test IPv6 chain handling when ipv6=true, test error handling when chain operations fail, (2) `WriteDNATMap` function: test file writing with various ServiceMapping slices (empty, single mapping, multiple mappings), verify file format matches expected output (header comments, service:port/protocol format), test file permissions are 0644, test error handling when file path is invalid or unwritable, (3) `Setup` orchestration function: test the full flow with a recordingExecutor to verify correct sequence (EnsureChain -> AddExclusions -> AddDNATRules -> WriteDNATMap), verify error propagation from each step, test with empty mappings slice, test with empty chain name (should error), test context cancellation handling. Add tests for edge cases in existing functions if not already covered: (4) `AddDNATRules` with SCTP protocol (in addition to existing TCP/UDP tests), (5) `AddExclusions` with empty CIDR list (should succeed with no commands), (6) Mixed IPv4/IPv6 CIDRs in exclusions when ipv6=false (should skip IPv6 CIDRs). Maintain the existing test structure and patterns (recordingExecutor, table-driven tests, t.Parallel()). Add helper functions as needed for common test setup (e.g., creating test ServiceMappings, temporary directories for file tests).

### test/kind(NEW)

Create the KIND test directory structure. This is a directory that will contain cluster setup scripts, manifests, and testing utilities.

### test/kind/cluster-config.yaml(NEW)

Create a KIND cluster configuration file. Define a simple single-node cluster with: (1) kind: Cluster, apiVersion: kind.x-k8s.io/v1alpha4, (2) one control-plane node, (3) networking configuration with default CNI (kindnet), (4) optional: expose port 8081 for accessing watcher metrics from host (extraPortMappings), (5) optional: configure containerdConfigPatches if needed for local image loading. Keep the configuration minimal and focused on testing ghostwire functionality. Add comments explaining each section and noting that this is for local testing only.

### test/kind/setup-cluster.sh(NEW)

Create a bash script to set up the KIND cluster for testing. The script should: (1) check if KIND is installed (command -v kind), exit with helpful error message if not, (2) check if kubectl is installed, exit with error if not, (3) define cluster name as variable (default: "ghostwire-test"), (4) check if cluster already exists (kind get clusters | grep), (5) if cluster exists, ask user if they want to delete and recreate (read -p prompt) or skip creation, (6) create cluster using `kind create cluster --name ghostwire-test --config cluster-config.yaml`, (7) wait for cluster to be ready (kubectl wait --for=condition=Ready nodes --all --timeout=120s), (8) print success message with instructions for next steps (kubectl cluster-info, how to load images, how to tear down). Add error handling for each step with descriptive messages. Make the script idempotent so it can be run multiple times safely. Add a shebang (#!/usr/bin/env bash) and set -euo pipefail for safety. Add comments explaining each section. Make the script executable (will need chmod +x in documentation).

### test/kind/teardown-cluster.sh(NEW)

References: 

- test/kind/setup-cluster.sh(NEW)

Create a bash script to tear down the KIND cluster. The script should: (1) define cluster name variable matching setup script ("ghostwire-test"), (2) check if cluster exists (kind get clusters | grep), (3) if exists, delete it with `kind delete cluster --name ghostwire-test`, (4) print confirmation message, (5) if doesn't exist, print message that cluster is already deleted. Add error handling and descriptive messages. Add shebang and set -euo pipefail. Add comments. Make executable.

### test/kind/manifests(NEW)

Create a directory to hold Kubernetes manifest files for testing.

### test/kind/manifests/namespace.yaml(NEW)

Create a Namespace manifest for testing. Define a namespace named "ghostwire-test" with labels indicating it's for testing (e.g., app: ghostwire, purpose: testing). Add comments explaining this is the namespace where test services and pods will be deployed.

### test/kind/manifests/services.yaml(NEW)

References: 

- internal/discovery/discovery.go

Create Service manifests for testing service discovery and DNAT rule generation. Define multiple service pairs to test various scenarios: (1) **orders** and **orders-preview**: ClusterIP services with ports 80 (HTTP) and 443 (HTTPS), protocol TCP, selector app=orders with role label (active/preview), (2) **payment** and **payment-preview**: ClusterIP service with single port 8080, protocol TCP, (3) **users** service WITHOUT a preview variant to test the "no preview" skip logic, (4) **headless** service with clusterIP: None to test headless service skipping, (5) **api-v2** and **api-v2-preview**: service with multiple ports (8080, 8443, 9090) to test multi-port handling. Each service should have appropriate labels and selectors. Add comments explaining what each service tests. Use the "ghostwire-test" namespace. Keep the manifests simple and focused on testing discovery logic rather than actual workload functionality.

### test/kind/manifests/rbac.yaml(NEW)

References: 

- README.md(MODIFY)
- AGENTS.md(MODIFY)

Create RBAC manifests required for the init container to list Services. Define: (1) ServiceAccount named "ghostwire-init" in the "ghostwire-test" namespace, (2) Role named "ghostwire-service-reader" with rules allowing `resources: ["services"], verbs: ["list", "get"]` in the namespace, (3) RoleBinding named "ghostwire-init-binding" that binds the ServiceAccount to the Role. Add comments explaining that these permissions are required for service discovery. Note that in production, each workload should have its own ServiceAccount with minimal permissions.

### test/kind/manifests/test-pod.yaml(NEW)

References: 

- README.md(MODIFY)
- internal/cmd/init.go

Create a test Pod manifest that runs the ghostwire-init command for manual testing. Define a Pod named "ghostwire-init-test" in the "ghostwire-test" namespace with: (1) serviceAccountName: ghostwire-init (for RBAC permissions), (2) restartPolicy: Never (so it runs once and stops), (3) a single container using the ghostwire image (image: ghostwire:local or placeholder that will be replaced after building), (4) command: ["ghostwire", "init"], (5) env vars: GW_NAMESPACE=ghostwire-test, GW_SVC_PREVIEW_PATTERN={{name}}-preview, GW_LOG_LEVEL=debug for verbose output, (6) securityContext with capabilities: add: ["NET_ADMIN"] (required for iptables), (7) a volume mount for /shared using an emptyDir volume (for dnat.map file). Add comments explaining each section and noting that the image needs to be built and loaded into KIND before deploying this pod. Add a note about how to view logs (kubectl logs -n ghostwire-test ghostwire-init-test) and how to inspect the dnat.map file (kubectl exec or describe pod).

### test/kind/load-image.sh(NEW)

References: 

- test/kind/setup-cluster.sh(NEW)

Create a bash script to build the ghostwire binary, create a Docker image, and load it into the KIND cluster. The script should: (1) check if Docker is installed, (2) define cluster name variable ("ghostwire-test"), (3) check if KIND cluster exists, exit with error if not, (4) build the ghostwire binary using `mise run build` (or direct go build if mise not available), (5) create a simple Dockerfile in a temp directory (FROM alpine or distroless, COPY ghostwire binary, set entrypoint), (6) build the Docker image with tag "ghostwire:local" using `docker build`, (7) load the image into KIND cluster using `kind load docker-image ghostwire:local --name ghostwire-test`, (8) print success message with next steps (how to deploy test pod). Add error handling for each step. Add comments explaining the process. Note that this is a simplified build process and the actual container build phase will create a proper multi-stage Dockerfile. Add shebang and set -euo pipefail. Make executable.

### test/kind/deploy-test.sh(NEW)

References: 

- test/kind/manifests/namespace.yaml(NEW)
- test/kind/manifests/services.yaml(NEW)
- test/kind/manifests/rbac.yaml(NEW)
- test/kind/manifests/test-pod.yaml(NEW)

Create a bash script to deploy all test resources to the KIND cluster. The script should: (1) check if kubectl is available, (2) check if KIND cluster exists, (3) apply manifests in order: namespace.yaml, rbac.yaml, services.yaml, (4) wait for services to be ready (kubectl wait or sleep), (5) optionally apply test-pod.yaml if user wants to run init immediately, (6) print status of deployed resources (kubectl get all -n ghostwire-test), (7) print instructions for next steps (how to run init test, how to check logs, how to validate). Add a --with-pod flag to optionally deploy the test pod. Add error handling and descriptive messages. Add shebang and set -euo pipefail. Add comments. Make executable.

### test/kind/validate-dnatmap.sh(NEW)

References: 

- internal/iptables/dnatmap.go
- test/kind/manifests/services.yaml(NEW)

Create a bash script to validate the /shared/dnat.map file generated by the init container. The script should: (1) check if kubectl is available, (2) define pod name variable ("ghostwire-init-test"), (3) check if the test pod exists in ghostwire-test namespace, (4) check if the pod completed successfully (kubectl get pod status), (5) extract the dnat.map file content using `kubectl exec` or by reading from the pod's volume (may need to use a debug container or kubectl cp if pod is completed), (6) parse the file and validate: expected number of mappings (based on services.yaml), correct format (service:port/protocol active_ip -> preview_ip), presence of expected services (orders, payment, api-v2), absence of services without preview (users, headless), (7) print validation results with pass/fail for each check, (8) exit with code 0 if all validations pass, non-zero otherwise. Add detailed comments explaining the validation logic. Add error handling. Add shebang and set -euo pipefail. Make executable. Note: Since the pod completes and exits, the script may need to use `kubectl logs` to capture the dnat.map content if it's logged, or use a different approach like keeping the pod running with a sleep command for inspection.

### test/kind/validate-iptables.sh(NEW)

References: 

- internal/iptables/iptables.go
- README.md(MODIFY)

Create a bash script to validate iptables rules created by the init container. The script should: (1) check if kubectl is available, (2) define pod name variable, (3) check if the test pod exists and is running (or use a debug pod with NET_ADMIN to inspect iptables), (4) execute `iptables -t nat -L CANARY_DNAT -n -v` inside the pod/node to list the DNAT chain rules, (5) parse the output and validate: chain exists, exclusion rules are present (RETURN targets for IMDS and DNS CIDRs), DNAT rules are present for expected services (orders, payment, api-v2), correct number of rules, correct target IPs (preview ClusterIPs), (6) print validation results with pass/fail for each check, (7) exit with code 0 if all validations pass. Add detailed comments. Add error handling. Note: This script may need to run commands on the KIND node itself (docker exec into the node container) rather than in a pod, since iptables rules are at the node level. Add instructions for this approach. Add shebang and set -euo pipefail. Make executable.

### test/kind/README.md(NEW)

References: 

- README.md(MODIFY)
- AGENTS.md(MODIFY)

Create comprehensive documentation for KIND-based testing. Structure the document with sections: (1) **Overview**: explain the purpose of this test directory (local integration testing of ghostwire init command), (2) **Prerequisites**: list required tools (KIND, kubectl, Docker, mise, Go), provide installation links, (3) **Quick Start**: step-by-step guide to run tests (setup cluster, build image, deploy resources, run init, validate results), (4) **Directory Structure**: explain each file and directory in test/kind/, (5) **Manual Testing Procedure**: detailed steps for manual testing including: create cluster, deploy services, build and load image, deploy test pod, view logs (kubectl logs -n ghostwire-test ghostwire-init-test), inspect dnat.map file, verify iptables rules (kubectl exec or docker exec into node), check service discovery logs, (6) **Validation Scripts**: explain each validation script (validate-dnatmap.sh, validate-iptables.sh) and how to use them, (7) **Expected Results**: document what successful output looks like (sample log output, sample dnat.map content, sample iptables rules), (8) **Troubleshooting**: common issues and solutions (cluster not starting, image not loading, RBAC errors, NET_ADMIN permission errors, services not found), (9) **Cleanup**: how to tear down the cluster and clean up resources, (10) **Next Steps**: mention that this is for init testing only, watcher testing will be added in a later phase. Use clear markdown formatting with code blocks for commands, tables for file descriptions, and callout boxes for important notes. Include example commands with expected output. Reference the main README.md for configuration details.

### AGENTS.md(MODIFY)

References: 

- test/kind/README.md(NEW)
- internal/iptables/iptables_test.go(MODIFY)
- internal/discovery/discovery_test.go(NEW)

Update the **Testing & Validation Strategy** section to document the new testing infrastructure. After the existing content about running `mise run test`, add: "Unit tests follow table-driven patterns with `t.Parallel()` for concurrency. The discovery package uses a fake Kubernetes clientset for testing without a real cluster. The iptables package uses a recordingExecutor mock to verify commands without executing them. Integration testing uses KIND clusters (see `/test/kind/` directory) with sample Service manifests and validation scripts. Before opening a PR, run unit tests (`mise run test`), optionally run `act pull_request` for CI simulation, and consider running KIND integration tests for init command changes. The `/test/kind/README.md` provides detailed instructions for local integration testing." This documents the complete testing strategy for future contributors and agents.

### README.md(MODIFY)

References: 

- test/kind/README.md(NEW)

Update the **Development** section to add information about integration testing. After the existing "Pre-commit Checklist" subsection, add a new subsection: **Integration Testing:** "Local integration tests use KIND clusters to validate the init command against real Kubernetes Services. The `/test/kind/` directory contains cluster setup scripts, sample Service manifests, and validation scripts. To run integration tests: (1) ensure KIND and Docker are installed, (2) run `./test/kind/setup-cluster.sh` to create a test cluster, (3) run `./test/kind/load-image.sh` to build and load the ghostwire image, (4) run `./test/kind/deploy-test.sh` to deploy test resources, (5) run validation scripts to verify behavior. See `/test/kind/README.md` for detailed instructions. Integration tests are optional for most PRs but recommended when modifying service discovery or iptables logic." This provides users with clear guidance on when and how to run integration tests.