I have created the following plan after thorough exploration and analysis of the codebase. Follow the below plan verbatim. Trust the files and references. Do not re-verify what's written in the plan. Explore only when absolutely necessary. First implement all the proposed file changes and then I'll review all the changes together at the end.

### Observations

The discovery phase provides clean ServiceMapping structs with all data needed for DNAT rules. The init command has a TODO comment marking exactly where iptables logic should integrate. Configuration infrastructure (viper, config struct) is ready but needs default values added for iptables-specific settings. The README clearly documents expected behavior: create chain, add exclusions, add DNAT rules, write audit file, but do NOT add jump (watcher's job). The subsequent testing phase is already planned separately, so this phase focuses purely on implementation without test scaffolding.

### Approach

Create an `/internal/iptables/` package that wraps iptables command execution with a testable interface. Build DNAT rules from the ServiceMappings discovered in the previous phase, add CIDR exclusions at the top of the chain, support dual-stack IPv4/IPv6, and write an audit file to `/shared/dnat.map`. The init command will orchestrate this after service discovery completes. The watcher (in a later phase) will add the jump rule to activate routing.

### Reasoning

Read the existing discovery package to understand ServiceMapping structure (ServiceName, Port, Protocol, ActiveClusterIP, PreviewClusterIP). Examined the init command to see where iptables integration will hook in (after discovery returns mappings). Reviewed README.md for configuration requirements (GW_NAT_CHAIN, GW_EXCLUDE_CIDRS, GW_IPV6, /shared/dnat.map). Checked config.go to confirm all necessary fields exist. Searched web for Kubernetes default DNS and IMDS IPs to determine sensible exclusion defaults (169.254.169.254/32 for cloud metadata, 10.96.0.10/32 for typical DNS service).

## Mermaid Diagram

sequenceDiagram
    participant Init as init command
    participant Discovery as discovery.Discover()
    participant IPTables as iptables.Setup()
    participant Executor as RealExecutor
    participant System as iptables/ip6tables

    Init->>Discovery: Discover services in namespace
    Discovery-->>Init: []ServiceMapping

    Init->>IPTables: Setup(config, mappings, logger)
    
    IPTables->>Executor: ChainExists("nat", "CANARY_DNAT")
    Executor->>System: iptables -t nat -L CANARY_DNAT
    System-->>Executor: exit code
    Executor-->>IPTables: exists=true/false
    
    alt Chain exists
        IPTables->>Executor: Run("iptables", "-t", "nat", "-F", "CANARY_DNAT")
        Executor->>System: Flush chain
    else Chain doesn't exist
        IPTables->>Executor: Run("iptables", "-t", "nat", "-N", "CANARY_DNAT")
        Executor->>System: Create chain
    end
    
    loop For each exclude CIDR
        IPTables->>Executor: Run("iptables", "-t", "nat", "-A", "CANARY_DNAT", "-d", "169.254.169.254/32", "-j", "RETURN")
        Executor->>System: Add exclusion rule
    end
    
    loop For each ServiceMapping
        IPTables->>Executor: Run("iptables", "-t", "nat", "-A", "CANARY_DNAT", "-d", "10.96.1.50", "-p", "tcp", "--dport", "443", "-j", "DNAT", "--to-destination", "10.96.2.75:443")
        Executor->>System: Add DNAT rule
    end
    
    alt IPv6 enabled
        Note over IPTables,System: Repeat all commands with ip6tables
    end
    
    IPTables->>IPTables: WriteDNATMap("/shared/dnat.map", mappings)
    Note over IPTables: Write audit file:<br/>orders:443/TCP 10.96.1.50 -> 10.96.2.75<br/>payment:8080/TCP 10.96.3.20 -> 10.96.4.30
    
    IPTables-->>Init: Success (rules created, NOT activated)
    Init->>Init: Log: "DNAT chain ready, watcher will activate"

## Proposed File Changes

### internal/iptables(NEW)

Create the iptables package directory to house all iptables command execution, chain management, rule building, and audit file writing logic.

### internal/iptables/types.go(NEW)

Define core types for the iptables package. Create a `Config` struct with fields: `ChainName` (string, e.g., "CANARY_DNAT"), `ExcludeCIDRs` ([]string, e.g., ["169.254.169.254/32", "10.96.0.10/32"]), `IPv6` (bool, whether to also run ip6tables commands), `DnatMapPath` (string, path to write audit file, typically "/shared/dnat.map"). Add a `Rule` struct representing a single iptables rule with fields: `Table` (string, e.g., "nat"), `Chain` (string), `RuleSpec` ([]string, the arguments after chain name), `Comment` (string, for logging). This provides a structured representation of rules before execution.

### internal/iptables/executor.go(NEW)

Define the `Executor` interface with methods: `Run(ctx context.Context, command string, args ...string) error` for executing iptables/ip6tables commands, and `ChainExists(ctx context.Context, table string, chain string) (bool, error)` for checking if a chain exists. Implement `RealExecutor` struct that uses `os/exec.CommandContext` to run actual iptables commands. In the `Run` method, execute the command, capture stdout/stderr, and return an error with the full command and output if it fails (for debugging). In `ChainExists`, run `iptables -t <table> -L <chain>` and check the exit code (0 = exists, 1 = doesn't exist, other = error). Add a `NewExecutor()` constructor that returns a RealExecutor. This abstraction allows the next phase to inject a mock executor for unit tests without requiring root privileges or actual iptables.

### internal/iptables/chain.go(NEW)

References: 

- internal/iptables/executor.go(NEW)

Implement chain management functions. Create `EnsureChain(ctx context.Context, executor Executor, table string, chain string, ipv6 bool, logger *slog.Logger)` that: (1) checks if the chain exists using `executor.ChainExists()`, (2) if it exists, flushes all rules with `iptables -t <table> -F <chain>` (and `ip6tables` if ipv6=true) to start clean, (3) if it doesn't exist, creates it with `iptables -t <table> -N <chain>` (and `ip6tables` if ipv6=true), (4) logs each operation with structured fields (table, chain, action, ipv6). This makes the init command idempotent—running it multiple times will reset the chain to a clean state. Handle errors from executor and return descriptive error messages. The function should run commands for both iptables and ip6tables when ipv6=true, but continue if ip6tables fails (log warning) since not all systems have IPv6 support.

### internal/iptables/exclusions.go(NEW)

References: 

- internal/iptables/executor.go(NEW)

Implement CIDR exclusion rule building. Create `AddExclusions(ctx context.Context, executor Executor, table string, chain string, cidrs []string, ipv6 bool, logger *slog.Logger)` that: (1) iterates through each CIDR in the cidrs slice, (2) for each CIDR, determines if it's IPv4 or IPv6 by checking for ':' character (IPv6 addresses contain colons), (3) builds an iptables rule: `iptables -t <table> -A <chain> -d <cidr> -j RETURN` for IPv4 CIDRs, (4) if ipv6=true and the CIDR is IPv6, also runs `ip6tables -t <table> -A <chain> -d <cidr> -j RETURN`, (5) logs each exclusion rule added with structured fields (cidr, table, chain, ipv6), (6) returns error if any command fails. The RETURN target causes packets matching the CIDR to skip the rest of the chain, preventing DNAT for excluded destinations like IMDS (169.254.169.254/32) and DNS (10.96.0.10/32). These rules MUST be added before DNAT rules to ensure exclusions take precedence.

### internal/iptables/rules.go(NEW)

References: 

- internal/iptables/executor.go(NEW)
- internal/discovery/types.go

Implement DNAT rule building from ServiceMappings. Create `AddDNATRules(ctx context.Context, executor Executor, table string, chain string, mappings []discovery.ServiceMapping, ipv6 bool, logger *slog.Logger)` that: (1) iterates through each ServiceMapping from the discovery phase, (2) determines if the ClusterIPs are IPv4 or IPv6 by checking for ':' character, (3) converts the Protocol field (corev1.Protocol type) to lowercase string ("TCP" -> "tcp", "UDP" -> "udp", "SCTP" -> "sctp") for iptables command, (4) builds the DNAT rule: `iptables -t <table> -A <chain> -d <ActiveClusterIP> -p <protocol> --dport <Port> -j DNAT --to-destination <PreviewClusterIP>:<Port>`, (5) if ipv6=true and the IPs are IPv6, also runs the same rule with `ip6tables`, (6) logs each DNAT rule added with structured fields (service, port, protocol, active_ip, preview_ip, ipv6), (7) returns error if any command fails. Each rule redirects traffic destined for the active service ClusterIP:port to the preview service ClusterIP:port. The protocol and port matching ensures only the specific service port is redirected, not all traffic to that IP.

### internal/iptables/dnatmap.go(NEW)

References: 

- internal/discovery/types.go

Implement audit file writing. Create `WriteDNATMap(path string, mappings []discovery.ServiceMapping, logger *slog.Logger)` that: (1) creates or truncates the file at the given path (typically "/shared/dnat.map"), (2) writes a header comment explaining the file format (e.g., "# DNAT mappings generated by ghostwire-init", "# Format: service:port/protocol active_ip -> preview_ip"), (3) iterates through each ServiceMapping and writes a line in human-readable format: `<ServiceName>:<Port>/<Protocol> <ActiveClusterIP> -> <PreviewClusterIP>` (e.g., "orders:443/TCP 10.96.1.50 -> 10.96.2.75"), (4) ensures the file is readable by the watcher sidecar (permissions 0644), (5) logs the file path and number of mappings written, (6) returns error if file operations fail. This file provides a simple audit trail for debugging and can be read by the watcher for metrics (in a later phase). The shared volume mount at /shared makes this file accessible to both init and watcher containers.

### internal/iptables/iptables.go(NEW)

References: 

- internal/iptables/types.go(NEW)
- internal/iptables/executor.go(NEW)
- internal/iptables/chain.go(NEW)
- internal/iptables/exclusions.go(NEW)
- internal/iptables/rules.go(NEW)
- internal/iptables/dnatmap.go(NEW)
- internal/discovery/types.go

Implement the main orchestration function that ties everything together. Create `Setup(ctx context.Context, cfg Config, mappings []discovery.ServiceMapping, logger *slog.Logger) error` that: (1) creates a RealExecutor using `NewExecutor()`, (2) calls `EnsureChain()` to create/flush the DNAT chain in the nat table, (3) calls `AddExclusions()` to add CIDR exclusion rules at the top of the chain, (4) calls `AddDNATRules()` to add DNAT rules for each ServiceMapping, (5) calls `WriteDNATMap()` to write the audit file, (6) logs a summary with structured fields (chain_name, exclusions_count, dnat_rules_count, ipv6_enabled, dnat_map_path), (7) returns error if any step fails. Add a prominent log message at the end: "DNAT chain configured but NOT activated - watcher will add jump rule when role=preview". This function provides a clean API for the init command to call without needing to understand iptables internals. The function should be context-aware and check for cancellation between major steps.

### internal/cmd/init.go(MODIFY)

References: 

- internal/iptables/iptables.go(NEW)
- internal/iptables/types.go(NEW)
- internal/discovery/types.go

Integrate iptables setup into the init command. After the service discovery completes (line 66-76), replace the TODO comment (line 78) with iptables integration: (1) retrieve iptables configuration from viper: `nat-chain` (default "CANARY_DNAT"), `exclude-cidrs` (default "169.254.169.254/32,10.96.0.10/32", parse as CSV by splitting on comma and trimming whitespace), `ipv6` (default false), (2) create an `iptables.Config` struct with ChainName, ExcludeCIDRs (parsed slice), IPv6 flag, and DnatMapPath set to "/shared/dnat.map", (3) call `iptables.Setup(ctx, iptablesCfg, mappings, logger)` and handle errors with descriptive logging, (4) log success message with the chain name and number of rules created. Import the iptables package. Keep the existing context, logger, and discovery logic unchanged. The init command now has a complete flow: discover services -> build iptables rules -> exit (watcher will activate later).

### internal/cmd/root.go(MODIFY)

References: 

- README.md(MODIFY)

Add viper default values for iptables configuration. In the `init()` function after the existing `SetDefault` calls (around line 54-57), add: `viper.SetDefault("nat-chain", "CANARY_DNAT")` for the iptables chain name, `viper.SetDefault("exclude-cidrs", "169.254.169.254/32,10.96.0.10/32")` for default CIDR exclusions (IMDS and typical Kubernetes DNS), `viper.SetDefault("ipv6", false)` to disable IPv6 by default, `viper.SetDefault("jump-hook", "OUTPUT")` for documentation (not used in this phase but will be used by watcher). These defaults align with the README documentation and provide sensible out-of-the-box behavior. Users can override via GW_NAT_CHAIN, GW_EXCLUDE_CIDRS, GW_IPV6, and GW_JUMP_HOOK environment variables thanks to the existing viper env binding.

### AGENTS.md(MODIFY)

References: 

- internal/iptables/iptables.go(NEW)

Update the **Project Snapshot** section to document the new iptables package. After the line about `internal/discovery`, add: "`internal/iptables` (iptables/ip6tables command wrapper, DNAT chain and rule management)". Update the **Coding Guidelines** section to add: "The iptables package uses an Executor interface for testability—production code uses RealExecutor (runs actual commands), tests inject a mock that records commands without executing them. All iptables operations are idempotent (init can be run multiple times safely)." Update the **Security & Operational Notes** section to clarify: "The init container creates the DNAT chain and rules but does NOT add the jump rule—the watcher sidecar activates routing by adding `-j CANARY_DNAT` to OUTPUT (or PREROUTING) when the pod's role label becomes 'preview'. This separation ensures routing is controlled by pod labels, not just the presence of rules." This documents the iptables architecture and the critical separation of concerns between init (prepare rules) and watcher (activate rules).

### README.md(MODIFY)

References: 

- internal/cmd/init.go(MODIFY)
- internal/iptables/iptables.go(NEW)

Update the **Components** section to reflect the completed init implementation. Change the init bullet point to: "**`init`**: automatically discovers all Services in the namespace via the Kubernetes API, identifies base/preview pairs (e.g., `orders` + `orders-preview`), creates a custom DNAT chain (default: `CANARY_DNAT`), adds exclusion rules for IMDS and DNS, builds DNAT rules mapping active ClusterIP:port → preview ClusterIP:port for all discovered services, and writes `/shared/dnat.map` for audit. Does NOT activate routing—that's the watcher's job." Update the **Injector Behavior** section's initContainer description to: "Discovers services, builds DNAT rules with exclusions, writes `/shared/dnat.map` for audit/debug, and exits without enabling the chain (watcher activates it)." This clarifies the complete init behavior now that iptables integration is implemented.