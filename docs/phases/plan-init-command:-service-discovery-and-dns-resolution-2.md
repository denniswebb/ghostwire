I have created the following plan after thorough exploration and analysis of the codebase. Follow the below plan verbatim. Trust the files and references. Do not re-verify what's written in the plan. Explore only when absolutely necessary. First implement all the proposed file changes and then I'll review all the changes together at the end.

### Observations

The user wants complete auto-discovery without explicit service lists. The init container will list all Services in the namespace, identify base/preview pairs using the pattern (e.g., `orders` + `orders-preview`), and create DNAT mappings for all ports. This requires client-go to list services, pattern matching to find pairs, and handling multiple ports per service. The `GW_SERVICES` configuration is removed entirely, simplifying the user experience and making the system more dynamic.

### Approach

Implement full auto-discovery of services without requiring an explicit service list. List all Services in the namespace via the Kubernetes API, identify base/preview pairs using the configured pattern (default: `{{name}}-preview`), and create DNAT mappings for all ports on services that have preview variants. This eliminates configuration complexity and makes preview routing truly automatic and transparent.

### Reasoning

Read the user's clarification that `GW_SERVICES` should be removed and the system should auto-discover all services. Re-examined the architecture to understand that the init container should list all services in the namespace, find pairs where both base and preview exist, and create mappings for all ports automatically without any filtering or explicit configuration.

## Mermaid Diagram

sequenceDiagram
    participant Init as init command
    participant Viper as viper config
    participant K8s as Kubernetes API
    participant Discovery as discovery.Discover()
    participant Template as ApplyPattern()
    participant Logger as slog logger

    Init->>Viper: Get GW_NAMESPACE, GW_SVC_PREVIEW_PATTERN
    Init->>K8s: NewInClusterClient()
    K8s-->>Init: clientset
    Init->>Discovery: Discover(ctx, config, logger)
    Discovery->>K8s: List all Services in namespace
    K8s-->>Discovery: []Service{orders, users, orders-preview, payment, ...}
    Discovery->>Discovery: Build service name → Service map
    
    loop For each Service
        Discovery->>Template: ApplyPattern("{{name}}-preview", "orders")
        Template-->>Discovery: "orders-preview"
        
        alt Preview exists in map
            Discovery->>Discovery: Get ClusterIPs from both services
            Discovery->>Discovery: Validate IPs are non-empty and different
            
            alt Valid IPs
                loop For each port in service
                    Discovery->>Discovery: Create ServiceMapping{orders, 443, TCP, 10.96.1.50, 10.96.2.75}
                    Discovery->>Logger: Info("discovered mapping", service, port, IPs)
                end
            else Invalid IPs
                Discovery->>Logger: Warn("skipping service", reason)
            end
        else No preview
            Discovery->>Logger: Debug("no preview service found", service)
        end
    end
    
    Discovery-->>Init: []ServiceMapping (all discovered mappings)
    Init->>Logger: Info("service discovery complete", mappings count, namespace)

## Proposed File Changes

### go.mod(MODIFY)

Add Kubernetes client-go dependencies to the require block. Add `k8s.io/api v0.31.1`, `k8s.io/apimachinery v0.31.1`, and `k8s.io/client-go v0.31.1` (use the latest stable v0.31.x versions compatible with Go 1.22). These provide the Kubernetes API types, client libraries, and in-cluster configuration needed for service discovery. After modifying, run `go mod tidy` via `mise run` to update go.sum and pull in transitive dependencies.

### internal/discovery(NEW)

Create the discovery package directory to house Kubernetes service discovery logic, service pair matching, and ClusterIP extraction.

### internal/discovery/types.go(NEW)

Define the `ServiceMapping` struct with fields: `ServiceName` (string - the base service name), `Port` (int32), `Protocol` (corev1.Protocol - TCP/UDP/SCTP), `ActiveClusterIP` (string), and `PreviewClusterIP` (string). This represents a single port mapping from a base service to its preview variant. Export this type so it can be used by the iptables phase. Add a helper method `String()` that returns a human-readable representation for logging.

### internal/discovery/client.go(NEW)

Implement Kubernetes client creation. Create `NewInClusterClient()` function that uses `rest.InClusterConfig()` from client-go to get the in-cluster configuration (reads service account token and CA cert from `/var/run/secrets/kubernetes.io/serviceaccount/`), then creates a `kubernetes.Clientset` using `kubernetes.NewForConfig()`. Return the clientset and error. Add error handling with descriptive messages. Add a comment noting that this requires the pod to have a ServiceAccount with RBAC permissions to list and get Services in its namespace (the injector will configure this in a later phase).

### internal/discovery/template.go(NEW)

References: 

- README.md(MODIFY)

Implement template pattern handling for preview service name generation. Create `ApplyPattern(pattern string, serviceName string)` function that uses Go's `text/template` package to parse the pattern (e.g., "{{name}}-preview") and execute it with a data struct containing `Name: serviceName`. Return the resulting service name string and error. Handle template parsing and execution errors with descriptive messages. Cache parsed templates in a package-level map to avoid re-parsing on every call (use sync.Map for thread safety if needed, though init is single-threaded). This function generates the preview service name from the base service name using the configured pattern.

### internal/discovery/discovery.go(NEW)

References: 

- internal/discovery/types.go(NEW)
- internal/discovery/template.go(NEW)

Implement the main service discovery orchestration. Create a `Config` struct with fields: `Clientset` (*kubernetes.Clientset), `Namespace` (string), `PreviewPattern` (string - default "{{name}}-preview"). Implement `Discover(ctx context.Context, cfg Config, logger *slog.Logger)` function that: (1) lists all Services in the namespace using `clientset.CoreV1().Services(namespace).List(ctx, metav1.ListOptions{})`, (2) builds a map of service names to Service objects for fast lookup, (3) iterates through each service in the list, (4) for each service, applies the preview pattern to generate the preview service name using `ApplyPattern` from template.go, (5) checks if a service with the preview name exists in the map, (6) if preview exists, extracts ClusterIPs from both services' `Spec.ClusterIP` fields, (7) validates that both ClusterIPs are non-empty and different (if same or empty, skip with warning), (8) iterates through all ports in the base service's `Spec.Ports` slice, (9) for each port, creates a ServiceMapping with the service name, port number, protocol, and both ClusterIPs, (10) logs each discovery with structured fields (service name, port, protocol, active ClusterIP, preview ClusterIP, or skip reason), (11) returns []ServiceMapping and error. Handle edge cases: services with no ports (skip with warning), services with ClusterIP "None" (headless services, skip), preview service with different port configuration (log warning but still create mappings for matching ports). Use the provided logger with appropriate levels: Info for successful discoveries, Warn for skipped services, Debug for detailed iteration info. Include structured fields like `slog.String("service", name)`, `slog.Int("port", int(port))`, `slog.String("protocol", string(protocol))`, `slog.String("active_ip", ip)`, `slog.String("preview_ip", ip)`, `slog.String("reason", "...")` for skips.

### internal/cmd/init.go(MODIFY)

References: 

- internal/logging/logger.go
- internal/discovery/discovery.go(NEW)
- internal/discovery/client.go(NEW)

Replace the placeholder implementation with Kubernetes-based auto-discovery. In the `RunE` function: (1) create a context with timeout (e.g., 30 seconds) using `context.WithTimeout(context.Background(), 30*time.Second)`, defer the cancel function, (2) retrieve the logger using `logging.GetLogger()`, (3) load configuration from viper: `namespace` (GW_NAMESPACE, default to POD_NAMESPACE env var via `os.Getenv("POD_NAMESPACE")` or "default"), `svc-preview-pattern` (GW_SVC_PREVIEW_PATTERN, default "{{name}}-preview"), (4) create a Kubernetes client using `discovery.NewInClusterClient()` and handle errors with descriptive logging, (5) create a `discovery.Config` struct with the clientset, namespace, and preview pattern, (6) call `discovery.Discover(ctx, cfg, logger)` and handle errors, (7) log the final count of discovered mappings with `logger.Info("service discovery complete", slog.Int("mappings", len(mappings)), slog.String("namespace", namespace))`, (8) return nil. Add a comment noting that iptables rule building will be added in the subsequent phase. Import necessary packages: `context`, `os`, `time`, and the discovery package. Remove any references to GW_SERVICES since it's no longer used.

### internal/cmd/root.go(MODIFY)

References: 

- README.md(MODIFY)
- internal/cmd/init.go(MODIFY)

Add viper default values for the discovery configuration. In the `init()` function, after the existing flag bindings, add: `viper.SetDefault("namespace", "default")`, `viper.SetDefault("svc-preview-pattern", "{{name}}-preview")`. Remove any references to `svc-active-pattern` since we use the base service name as-is. Remove any references to `services` since GW_SERVICES is no longer used. The namespace will also check the `POD_NAMESPACE` environment variable at runtime in the init command. No new flags are needed—all configuration comes from environment variables with the `GW_` prefix.

### internal/config/config.go(MODIFY)

References: 

- internal/discovery/discovery.go(NEW)

Remove the `Services` field from the Config struct since GW_SERVICES is no longer used. Remove the `SvcActivePattern` field since we use the base service name as-is. Keep all other fields: `Namespace`, `RoleLabelKey`, `RoleActive`, `RolePreview`, `SvcPreviewPattern`, `DNSSuffix`, `NATChain`, `JumpHook`, `ExcludeCIDRs`, `PollInterval`, `RefreshInterval`, `IPv6`, `LogLevel`. Update any comments to reflect that service discovery is fully automatic without explicit service lists.

### AGENTS.md(MODIFY)

References: 

- internal/discovery/discovery.go(NEW)

Update the **Project Snapshot** section to document the new discovery package and Kubernetes dependency. After the line about `internal/config`, add: "`internal/discovery` (Kubernetes service auto-discovery and ClusterIP extraction)". Update the **Security & Operational Notes** section to add: "The init container requires RBAC permissions to list Services in its namespace. The ServiceAccount must have a Role with `resources: [\"services\"], verbs: [\"list\"]` bound to it. The injector will configure this in a later phase." Add a note in the **Coding Guidelines** section: "Service discovery is fully automatic—the init container lists all services in the namespace and identifies base/preview pairs using pattern matching. No explicit service lists are needed." This documents the auto-discovery architecture for future contributors and agents.

### README.md(MODIFY)

References: 

- internal/cmd/init.go(MODIFY)

Update the **Components** section to clarify the init command's auto-discovery behavior. Change the init bullet point to: "**`init`**: automatically discovers all Services in the namespace via the Kubernetes API, identifies base/preview pairs (e.g., `orders` + `orders-preview`), and prepares DNAT mappings for all ports. No explicit service list needed. (Note: iptables rule building will be added in a subsequent phase.)" Remove the **Workload Annotations** section's reference to `ghostwire.dev/services` since it's no longer used. Remove the **Environment Variables** table's `GW_SERVICES` row. Remove the `GW_SVC_ACTIVE_PATTERN` row since we use base service names as-is. Keep `GW_SVC_PREVIEW_PATTERN` with default `{{name}}-preview`. Update the **Security** section to add: "Init container needs RBAC permissions: `resources: [\"services\"], verbs: [\"list\"]` in its namespace to auto-discover services." Update the **Example: Argo Rollouts Blue/Green** section to remove the `ghostwire.dev/services` annotation from the example. Update the **Minimal Example (no injector)** section to remove the `GW_SERVICES` env var. Add a note in the **Why** section: "Fully automatic service discovery—no need to maintain service lists or update configuration when adding new services." This clarifies the auto-discovery architecture and removes all references to the deprecated GW_SERVICES configuration.