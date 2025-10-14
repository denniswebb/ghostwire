I have created the following plan after thorough exploration and analysis of the codebase. Follow the below plan verbatim. Trust the files and references. Do not re-verify what's written in the plan. Explore only when absolutely necessary. First implement all the proposed file changes and then I'll review all the changes together at the end.

### Observations

The project has strong patterns established: cobra commands in `/internal/cmd/`, Kubernetes client setup in discovery package (can be extracted to shared k8s package), viper configuration with GW_* env vars, structured logging with slog. The watcher needs to poll its own pod's labels (not watch all pods), detect transitions between two states (active/preview), and run continuously until shutdown. The config struct already has all necessary fields (RoleLabelKey, RoleActive, RolePreview, PollInterval). The root.go is missing viper defaults for role-related config. The watcher is a long-running sidecar, unlike the init container which runs once and exits.

### Approach

Implement the watcher command's core polling logic by creating a dedicated `/internal/k8s/` package for Kubernetes client operations (pod fetching), extending the existing watcher command stub to orchestrate a poll loop that reads pod labels at configurable intervals, detects role transitions between active and preview states, and handles graceful shutdown via context cancellation. This phase focuses purely on label polling and state detection—iptables jump management and health/metrics endpoints are explicitly deferred to the subsequent phase.

### Reasoning

Examined the existing watcher.go stub, root.go configuration setup, and config.go struct to understand the foundation. Reviewed the discovery package's client creation pattern to establish consistency for Kubernetes client setup. Read AGENTS.md and README.md to understand the watcher's role in the architecture (polls labels, detects transitions, activates routing in next phase). Confirmed that the subsequent phase handles iptables jump management and metrics, so this phase must remain focused on polling infrastructure only.

## Mermaid Diagram

sequenceDiagram
    participant Cmd as watcher command
    participant Viper as viper config
    participant K8s as k8s.NewInClusterClient()
    participant Reader as PodLabelReader
    participant Poller as Poller.Run()
    participant API as Kubernetes API
    participant Signal as OS Signal

    Cmd->>Viper: Get POD_NAME, POD_NAMESPACE from env
    Cmd->>Viper: Get role-label-key, role-active, role-preview, poll-interval
    Cmd->>K8s: NewInClusterClient()
    K8s-->>Cmd: clientset
    Cmd->>Reader: NewPodLabelReader(clientset, namespace, podName)
    Cmd->>Poller: NewPoller(config with Reader, labelKey, values, interval)
    Cmd->>Cmd: Setup signal handler (SIGTERM, SIGINT)
    Cmd->>Poller: Run(ctx) in goroutine
    
    loop Every poll-interval (default 2s)
        Poller->>Reader: GetLabel(ctx, labelKey)
        Reader->>API: Get Pod(namespace, podName)
        API-->>Reader: Pod object
        Reader->>Reader: Extract pod.Labels[labelKey]
        Reader-->>Poller: label value (e.g., "preview")
        
        alt Role changed (active -> preview)
            Poller->>Poller: Log transition at Info level
            Note over Poller: "role transition detected"<br/>previous_role=active<br/>current_role=preview
        else Role changed (preview -> active)
            Poller->>Poller: Log transition at Info level
        else No change
            Poller->>Poller: Log at Debug level (optional)
        end
        
        Poller->>Poller: Update previous role state
        Poller->>Poller: Sleep until next tick or context cancel
    end
    
    Signal->>Cmd: SIGTERM/SIGINT received
    Cmd->>Cmd: Cancel context
    Cmd->>Poller: Context cancelled
    Poller->>Poller: Exit poll loop gracefully
    Poller-->>Cmd: Run() returns
    Cmd->>Cmd: Log shutdown message

## Proposed File Changes

### internal/k8s/client.go(NEW)

References: 

- internal/discovery/client.go

Create a new package for Kubernetes client operations. Implement `NewInClusterClient()` function that wraps `rest.InClusterConfig()` and `kubernetes.NewForConfig()` to create a clientset, similar to the pattern in `/internal/discovery/client.go`. This provides a shared location for Kubernetes client creation that both discovery and watcher can use. Return `(*kubernetes.Clientset, error)`. Add descriptive error wrapping with `fmt.Errorf` for configuration and clientset creation failures. Include a comment noting that this requires the pod to run with a ServiceAccount that has appropriate RBAC permissions.

### internal/k8s/pod.go(NEW)

Implement pod fetching and label reading logic. Create a `PodLabelReader` struct with fields: `clientset` (*kubernetes.Clientset), `namespace` (string), `podName` (string). Implement `NewPodLabelReader(clientset, namespace, podName)` constructor. Add a `GetLabel(ctx context.Context, labelKey string)` method that: (1) fetches the pod using `clientset.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})`, (2) extracts the label value from `pod.Labels[labelKey]`, (3) returns the label value and error. Handle cases where the pod doesn't exist (return error), the label key doesn't exist (return empty string, no error), or the API call fails (return error with context). Add structured error messages that include the namespace, pod name, and label key for debugging. This abstraction makes the polling logic testable by allowing a mock implementation in tests.

### internal/k8s/poller.go(NEW)

References: 

- internal/logging/logger.go

Implement the core polling orchestration logic. Create a `PollerConfig` struct with fields: `LabelReader` (interface with `GetLabel(ctx, key) (string, error)` method - allows mocking in tests), `LabelKey` (string), `ActiveValue` (string), `PreviewValue` (string), `PollInterval` (time.Duration), `Logger` (*slog.Logger). Create a `Poller` struct that holds the config and current state. Implement `NewPoller(cfg PollerConfig)` constructor that validates the config (non-empty label key, non-empty active/preview values, positive poll interval) and returns `(*Poller, error)`. Add a `Run(ctx context.Context)` method that: (1) implements the main poll loop using `time.NewTicker(cfg.PollInterval)`, (2) on each tick, calls `cfg.LabelReader.GetLabel(ctx, cfg.LabelKey)` to fetch the current label value, (3) compares the fetched value against the previous value to detect transitions, (4) logs state changes with structured fields (previous_role, current_role, label_key, pod_name if available), (5) handles context cancellation for graceful shutdown (select on ctx.Done() and ticker.C), (6) continues polling until context is cancelled. Add a `GetCurrentRole()` method that returns the last observed role value (useful for health checks in next phase). Handle errors from GetLabel by logging them and continuing to poll (transient API errors shouldn't crash the watcher). Track the previous role in the Poller struct to enable transition detection. Log at Info level for transitions (active->preview, preview->active), Debug level for no-change polls, and Warn level for errors.

### internal/cmd/watcher.go(MODIFY)

References: 

- internal/logging/logger.go
- internal/k8s/client.go(NEW)
- internal/k8s/pod.go(NEW)
- internal/k8s/poller.go(NEW)

Replace the placeholder implementation with the full watcher logic. In the `RunE` function: (1) retrieve the logger using `logging.GetLogger()`, (2) read required environment variables: `POD_NAME` and `POD_NAMESPACE` using `os.Getenv()`, return error if either is missing with descriptive message, (3) load configuration from viper: `role-label-key` (GW_ROLE_LABEL_KEY, default "role"), `role-active` (GW_ROLE_ACTIVE, default "active"), `role-preview` (GW_ROLE_PREVIEW, default "preview"), `poll-interval` (GW_POLL_INTERVAL, default "2s"), (4) parse the poll interval string into `time.Duration` using `time.ParseDuration()`, return error if invalid, (5) create a Kubernetes client using `k8s.NewInClusterClient()`, handle errors, (6) create a `k8s.PodLabelReader` with the clientset, namespace, and pod name, (7) create a `k8s.PollerConfig` with the label reader, label key, active/preview values, poll interval, and logger, (8) create a `k8s.Poller` using `k8s.NewPoller()`, handle validation errors, (9) set up signal handling for graceful shutdown using `signal.Notify()` with `os.Interrupt` and `syscall.SIGTERM`, create a context with cancel, (10) start the poller in a goroutine by calling `poller.Run(ctx)`, (11) wait for shutdown signal, then cancel the context and log shutdown message, (12) return nil. Add structured logging throughout with relevant fields (pod_name, namespace, label_key, poll_interval). Import necessary packages: `context`, `os`, `os/signal`, `syscall`, `time`, and the new k8s package. Remove the placeholder log message.

### internal/cmd/root.go(MODIFY)

References: 

- README.md(MODIFY)

Add viper default values for watcher-specific configuration. In the `init()` function after the existing `SetDefault` calls (around line 59-67), add: `viper.SetDefault("role-label-key", "role")` for the pod label key to monitor, `viper.SetDefault("role-active", "active")` for the active role value, `viper.SetDefault("role-preview", "preview")` for the preview role value, `viper.SetDefault("poll-interval", "2s")` for the watcher poll cadence. These defaults align with the README documentation and provide sensible out-of-the-box behavior. Users can override via GW_ROLE_LABEL_KEY, GW_ROLE_ACTIVE, GW_ROLE_PREVIEW, and GW_POLL_INTERVAL environment variables thanks to the existing viper env binding with the GW_ prefix and underscore replacement.

### AGENTS.md(MODIFY)

References: 

- internal/k8s/poller.go(NEW)

Update the **Project Snapshot** section to document the new k8s package. After the line about `internal/iptables`, add: "`internal/k8s` (Kubernetes client setup, pod label reading, polling orchestration)". Update the **Security & Operational Notes** section to add: "The watcher sidecar requires RBAC permissions to get its own Pod in the namespace. The ServiceAccount must have a Role with `resources: [\"pods\"], verbs: [\"get\"]` bound to it. For tighter security, use `resourceNames: [\"$(POD_NAME)\"]` to restrict access to only the watcher's own pod." Update the **Coding Guidelines** section to add: "The watcher runs as a long-lived sidecar that polls pod labels at a configurable interval (default 2s). It uses context cancellation for graceful shutdown and continues polling through transient API errors. State transitions (active ↔ preview) are logged at Info level." This documents the watcher architecture and security requirements for future contributors and agents.

### README.md(MODIFY)

References: 

- internal/cmd/watcher.go(MODIFY)
- internal/k8s/poller.go(NEW)

Update the **Components** section to reflect the implemented watcher polling logic. Change the watcher bullet point to: "**`watcher`**: long-running sidecar that polls its own Pod's labels at a configurable interval (default 2s), detects role transitions between active and preview states, and logs state changes. Handles graceful shutdown via SIGTERM/SIGINT. (Note: iptables jump management and health/metrics endpoints will be added in the subsequent phase.)" Update the **Security** section to add: "Watcher sidecar needs RBAC permissions: `resources: [\"pods\"], verbs: [\"get\"]` to read its own pod labels. For enhanced security, scope the Role with `resourceNames: [\"$(POD_NAME)\"]` to restrict access to only the watcher's pod." This clarifies the current watcher implementation status and security requirements.