I have created the following plan after thorough exploration and analysis of the codebase. Follow the below plan verbatim. Trust the files and references. Do not re-verify what's written in the plan. Explore only when absolutely necessary. First implement all the proposed file changes and then I'll review all the changes together at the end.

### Observations

The codebase has strong, consistent patterns: (1) Executor interface with recordingExecutor for tests, (2) table-driven tests with t.Parallel(), (3) viper configuration with GW_ prefix and defaults in root.go, (4) structured logging with slog throughout, (5) context-aware operations with graceful cancellation. The poller already detects transitions but has no action mechanism. The iptables package has all the building blocks (Run, ChainExists) but needs jump-specific functions. The watcher needs to coordinate three concurrent operations: polling, HTTP server, and graceful shutdown. The DNAT map file provides the service count for metrics.

### Approach

Extend the iptables package with jump rule management (add/remove `-j CANARY_DNAT` to OUTPUT or PREROUTING), create a new metrics package for Prometheus instrumentation and health checks, integrate jump management into the poller via a callback interface, and add an HTTP server to the watcher command for `/healthz` and `/metrics` endpoints on port 8081. The design follows existing patterns: Executor interface for testability, dependency injection for clean separation, and structured logging throughout.

### Reasoning

Read the complete iptables package implementation to understand the Executor pattern, command construction, and IPv4/IPv6 handling. Examined the poller implementation to identify the transition detection logic. Reviewed the watcher command to understand the lifecycle and configuration loading. Checked the test patterns to ensure consistency with the recordingExecutor mock approach. Searched web for Prometheus Go client best practices to ensure proper metric naming and registration patterns.

## Mermaid Diagram

sequenceDiagram
    participant Watcher as watcher command
    participant Poller as k8s.Poller
    participant JumpMgr as jumpManager (TransitionHandler)
    participant IPTables as iptables.AddJump/RemoveJump
    participant Metrics as metrics.Metrics
    participant Health as metrics.HealthChecker
    participant HTTP as HTTP Server :8081

    Watcher->>Metrics: NewMetrics()
    Watcher->>Health: NewHealthChecker()
    Watcher->>Watcher: CountDNATMappings("/shared/dnat.map")
    Watcher->>Metrics: SetDNATRuleCount(count)
    Watcher->>IPTables: ChainExists("nat", "CANARY_DNAT")
    IPTables-->>Watcher: exists=true
    Watcher->>Health: SetChainVerified()
    
    Watcher->>JumpMgr: Create with executor, config, metrics
    Watcher->>Poller: NewPoller(config with TransitionHandler=jumpMgr)
    Watcher->>HTTP: Start server on :8081 (/healthz, /metrics)
    Watcher->>Poller: Run(ctx) in goroutine
    
    loop Every poll-interval
        Poller->>Poller: GetLabel(ctx, "role")
        Poller->>Health: SetLabelsRead() (after first success)
        
        alt Role changed: active -> preview
            Poller->>JumpMgr: OnTransition(ctx, "active", "preview")
            JumpMgr->>IPTables: AddJump(ctx, "nat", "OUTPUT", "CANARY_DNAT")
            IPTables->>IPTables: iptables -t nat -I OUTPUT 1 -j CANARY_DNAT
            JumpMgr->>Metrics: SetJumpActive(true)
            JumpMgr-->>Poller: nil (success)
        else Role changed: preview -> active
            Poller->>JumpMgr: OnTransition(ctx, "preview", "active")
            JumpMgr->>IPTables: RemoveJump(ctx, "nat", "OUTPUT", "CANARY_DNAT")
            IPTables->>IPTables: iptables -t nat -D OUTPUT -j CANARY_DNAT
            JumpMgr->>Metrics: SetJumpActive(false)
            JumpMgr-->>Poller: nil (success)
        else Error in transition
            JumpMgr->>Metrics: IncrementError("iptables")
            JumpMgr-->>Poller: error (logged, polling continues)
        end
    end
    
    Note over HTTP: Concurrent HTTP requests
    HTTP->>Health: GET /healthz
    Health->>Health: IsHealthy() (chainVerified && labelsRead)
    Health-->>HTTP: 200 OK
    
    HTTP->>Metrics: GET /metrics
    Metrics-->>HTTP: Prometheus text format
    
    Note over Watcher: SIGTERM received
    Watcher->>Watcher: Cancel context
    Watcher->>Poller: Context cancelled, exit loop
    Watcher->>HTTP: Shutdown(ctx)
    Watcher->>Watcher: Log shutdown complete

## Proposed File Changes

### go.mod(MODIFY)

Add Prometheus client library dependencies to the require block. Add `github.com/prometheus/client_golang v1.20.5` (or latest stable v1.20.x) for Prometheus metrics collection and HTTP handler. This provides the core types (Counter, Gauge, Registry) and the `promhttp` package for serving metrics. After modifying, run `go mod tidy` via `mise run` to update go.sum and pull in transitive dependencies (prometheus/client_model, prometheus/common, etc.).

### internal/iptables/jump.go(NEW)

References: 

- internal/iptables/chain.go
- internal/iptables/executor.go

Implement jump rule management functions for activating/deactivating the DNAT chain. Create `JumpExists(ctx context.Context, executor Executor, table string, hook string, chain string) (bool, error)` that checks if a jump rule exists by running `iptables -t <table> -C <hook> -j <chain>` (exit code 0 = exists, 1 = doesn't exist). Create `AddJump(ctx context.Context, executor Executor, table string, hook string, chain string, ipv6 bool, logger *slog.Logger) error` that: (1) checks if the jump already exists using JumpExists to make it idempotent, (2) if not present, runs `iptables -w 5 -t <table> -I <hook> 1 -j <chain>` to insert the jump at the top of the hook chain (position 1 ensures it runs before other rules), (3) if ipv6=true, also runs the same command with `ip6tables`, logging warnings but continuing on IPv6 failures (consistent with chain.go pattern), (4) logs each operation with structured fields (table, hook, chain, ipv6). Create `RemoveJump(ctx context.Context, executor Executor, table string, hook string, chain string, ipv6 bool, logger *slog.Logger) error` that: (1) checks if the jump exists, (2) if present, runs `iptables -w 5 -t <table> -D <hook> -j <chain>` to delete the jump rule, (3) if ipv6=true, also runs with `ip6tables`, (4) if the rule doesn't exist, logs at Debug level and returns nil (idempotent), (5) logs each operation. Both functions should use the existing `ipv4Binary`, `ipv6Binary`, and `iptablesWaitSeconds` constants from chain.go. Handle errors from executor.Run() with descriptive wrapping. The `-I <hook> 1` insertion ensures the jump is evaluated first, and `-D <hook> -j <chain>` removes by matching the target (more reliable than position-based deletion).

### internal/metrics/metrics.go(NEW)

Create a metrics package for Prometheus instrumentation. Define a `Metrics` struct with fields: `registry` (*prometheus.Registry), `jumpState` (prometheus.Gauge with label "state" - values "active" or "preview"), `errorsTotal` (*prometheus.CounterVec with label "type" - values like "label_read", "iptables", "chain_verify"), `dnatRules` (prometheus.Gauge - total count of DNAT rules from the map file). Implement `NewMetrics() *Metrics` constructor that: (1) creates a new prometheus.Registry (isolated from global registry for clean testing), (2) creates the jumpState gauge with `prometheus.NewGauge(prometheus.GaugeOpts{Namespace: "ghostwire", Name: "jump_active", Help: "Whether the DNAT jump rule is currently active (1) or inactive (0)"})`, (3) creates the errorsTotal counter with `prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: "ghostwire", Name: "errors_total", Help: "Total number of errors by type"}, []string{"type"})`, (4) creates the dnatRules gauge with `prometheus.NewGauge(prometheus.GaugeOpts{Namespace: "ghostwire", Name: "dnat_rules", Help: "Number of DNAT rules configured"})`, (5) registers all metrics with the registry using `registry.MustRegister()`, (6) returns the Metrics struct. Add methods: `SetJumpActive(active bool)` that sets jumpState to 1.0 if active, 0.0 otherwise; `IncrementError(errorType string)` that calls `errorsTotal.WithLabelValues(errorType).Inc()`; `SetDNATRuleCount(count int)` that sets dnatRules to float64(count); `Handler() http.Handler` that returns `promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})` for serving metrics. Follow Prometheus naming conventions: lowercase, snake_case, base units, _total suffix for counters. Use bounded label cardinality ("type" has a fixed set of values). The metrics are designed to be updated by the watcher's poller and jump management logic.

### internal/metrics/health.go(NEW)

Implement health check logic. Define a `HealthChecker` struct with fields: `mu` (sync.RWMutex for thread-safe access), `chainVerified` (bool - set to true once the DNAT chain existence is confirmed), `labelsRead` (bool - set to true once pod labels have been successfully read at least once). Implement `NewHealthChecker() *HealthChecker` constructor that returns a zero-initialized struct (both flags start false). Add methods: `SetChainVerified()` that acquires write lock and sets chainVerified=true; `SetLabelsRead()` that acquires write lock and sets labelsRead=true; `IsHealthy() bool` that acquires read lock and returns `chainVerified && labelsRead`; `Handler() http.HandlerFunc` that returns a handler which: (1) calls IsHealthy(), (2) if healthy, writes HTTP 200 with body "OK\n", (3) if unhealthy, writes HTTP 503 with body "Service Unavailable\n" and logs the reason (which flags are false). The health check ensures the watcher has successfully verified the DNAT chain exists (via iptables check) and has read pod labels at least once (confirming Kubernetes API access and RBAC permissions). This provides a meaningful liveness/readiness signal for Kubernetes probes.

### internal/metrics/dnatmap.go(NEW)

References: 

- internal/iptables/dnatmap.go

Implement DNAT map file parsing for metrics. Create `CountDNATMappings(path string) (int, error)` function that: (1) opens the file at the given path (typically "/shared/dnat.map"), (2) reads it line by line using bufio.Scanner, (3) counts non-empty lines that don't start with "#" (comment lines), (4) returns the count and any error. This provides the `ghostwire_dnat_rules` metric value by reading the audit file written by the init container. Handle file not found gracefully (return 0, nil) since the watcher may start before init completes in some edge cases. Handle read errors by returning the error for logging. Add validation to ensure the path doesn't contain ".." traversal (similar to dnatmap.go in iptables package). This function is called once during watcher startup to populate the initial metric value.

### internal/k8s/poller.go(MODIFY)

Extend the Poller to support transition callbacks. Add a `TransitionHandler` interface with method `OnTransition(ctx context.Context, previous string, current string) error` that will be called when the role changes between recognized values (active ↔ preview). Add a `TransitionHandler` field to `PollerConfig` (optional, can be nil). In the `pollOnce` method, after detecting a transition between recognized roles (around line 140-146), check if `p.cfg.TransitionHandler != nil`, and if so, call `p.cfg.TransitionHandler.OnTransition(ctx, previousValue, labelValue)`. If OnTransition returns an error, log it at Warn level with structured fields (previous_role, current_role, error) but continue polling (transient errors shouldn't crash the watcher). This callback mechanism allows the watcher command to inject jump management logic without coupling the poller to iptables. The interface design follows Go best practices for extensibility and keeps the k8s package focused on label polling.

### internal/cmd/watcher.go(MODIFY)

References: 

- internal/k8s/poller.go(MODIFY)
- internal/iptables/jump.go(NEW)
- internal/metrics/metrics.go(NEW)
- internal/metrics/health.go(NEW)
- internal/metrics/dnatmap.go(NEW)

Integrate jump management, metrics, and health endpoints into the watcher command. After loading the existing configuration (lines 38-46), add: (1) load iptables configuration from viper: `natChain` (nat-chain, default "CANARY_DNAT"), `jumpHook` (jump-hook, default "OUTPUT"), `ipv6` (ipv6, default false), `dnatMapPath` (iptables-dnat-map, default "/shared/dnat.map"), (2) create metrics using `metrics.NewMetrics()`, (3) create health checker using `metrics.NewHealthChecker()`, (4) count DNAT rules using `metrics.CountDNATMappings(dnatMapPath)` and set the metric with `metrics.SetDNATRuleCount(count)`, logging any error but continuing, (5) create an iptables executor using `iptables.NewExecutor()`, (6) verify the DNAT chain exists using `executor.ChainExists(ctx, "nat", natChain)`, log the result, and call `healthChecker.SetChainVerified()` if successful (this satisfies one health check requirement), (7) create a `jumpManager` struct (defined inline or as a separate type) that implements `k8s.TransitionHandler` interface with an `OnTransition` method that: checks if current == previewValue, if yes calls `iptables.AddJump(ctx, executor, "nat", jumpHook, natChain, ipv6, logger)` and `metrics.SetJumpActive(true)`, if current == activeValue calls `iptables.RemoveJump(ctx, executor, "nat", jumpHook, natChain, ipv6, logger)` and `metrics.SetJumpActive(false)`, logs any errors and increments `metrics.IncrementError("iptables")`, (8) pass the jumpManager as `TransitionHandler` in the `PollerConfig`, (9) after the poller is created, wrap the poller.Run() call to also call `healthChecker.SetLabelsRead()` after the first successful poll (this can be done by checking if poller.GetCurrentRole() returns non-empty after the first iteration), (10) start an HTTP server on `:8081` in a separate goroutine with routes: `/healthz` -> `healthChecker.Handler()`, `/metrics` -> `metrics.Handler()`, use `http.Server` with the context for graceful shutdown, (11) in the shutdown sequence (after line 98), call `server.Shutdown(ctx)` with a timeout context to gracefully stop the HTTP server. Import necessary packages: `net/http`, `github.com/denniswebb/ghostwire/internal/iptables`, `github.com/denniswebb/ghostwire/internal/metrics`. Update structured logging to include relevant fields (nat_chain, jump_hook, ipv6, dnat_map_path, http_addr). The jumpManager can be a simple struct with fields for executor, natChain, jumpHook, ipv6, previewValue, activeValue, metrics, and logger, making the OnTransition implementation clean and testable.

### AGENTS.md(MODIFY)

References: 

- internal/metrics/metrics.go(NEW)
- internal/iptables/jump.go(NEW)

Update the **Project Snapshot** section to document the new metrics package. After the line about `internal/k8s`, add: "`internal/metrics` (Prometheus metrics collection, health checks, DNAT map parsing)". Update the **Coding Guidelines** section to add: "The watcher exposes Prometheus metrics on `:8081/metrics` (jump state, error counts, DNAT rule count) and a health endpoint at `:8081/healthz` that returns 200 when the chain is verified and labels have been read. Metrics follow Prometheus naming conventions (lowercase, snake_case, _total suffix for counters, bounded label cardinality). The watcher uses a TransitionHandler callback to trigger iptables jump management when role transitions occur, keeping the poller decoupled from iptables logic." Update the **Security & Operational Notes** section to clarify: "The watcher activates routing by adding `-j CANARY_DNAT` to the configured hook (OUTPUT or PREROUTING via GW_JUMP_HOOK) when the pod's role label becomes 'preview', and removes it when the role becomes 'active'. The jump rule is inserted at position 1 to ensure it's evaluated before other rules in the hook chain." This documents the complete watcher behavior including metrics, health, and jump management.

### README.md(MODIFY)

References: 

- internal/cmd/watcher.go(MODIFY)
- internal/metrics/metrics.go(NEW)
- internal/metrics/health.go(NEW)

Update the **Components** section to reflect the completed watcher implementation. Change the watcher bullet point to: "**`watcher`**: long-running sidecar that polls its own Pod's labels at a configurable interval (default 2s), detects role transitions between active and preview states, adds `-j CANARY_DNAT` jump to OUTPUT (or PREROUTING) when role=preview, removes the jump when role=active, and exposes `/healthz` and `/metrics` endpoints on `:8081` for observability. Handles graceful shutdown via SIGTERM/SIGINT." Update the **Metrics and Observability** section to clarify the actual metric names: "- `/metrics` Prometheus endpoint on `:8081` with: `ghostwire_jump_active` (gauge, 1 when jump is active, 0 when inactive), `ghostwire_errors_total{type}` (counter by error type: label_read, iptables, chain_verify), `ghostwire_dnat_rules` (gauge, count of DNAT rules from /shared/dnat.map). - `/healthz` on `:8081` returns 200 when the watcher has verified the DNAT chain exists and successfully read pod labels at least once." This provides accurate documentation of the implemented metrics and health check behavior.