I have created the following plan after thorough exploration and analysis of the codebase. Follow the below plan verbatim. Trust the files and references. Do not re-verify what's written in the plan. Explore only when absolutely necessary. First implement all the proposed file changes and then I'll review all the changes together at the end.

### Observations

The repository is greenfield (only `README.md` and `LICENSE` exist). The README defines clear patterns: single binary with subcommands, `GW_*` env vars, JSON logging for Datadog/APM, and configuration via viper. Datadog best practices recommend using slog's JSON handler with reserved fields (`message`, `status`, `service`) and optional trace correlation via `dd.trace_id`/`dd.span_id` placeholders. The foundation must support three subcommands (`init`, `watcher`, `injector`) that will be implemented in later phases.


### Approach

Initialize a clean Go project with standard layout (`/cmd`, `/internal`, `/pkg`). Set up a single-binary CLI using cobra with subcommands for `init`, `watcher`, and `injector`. Implement JSON structured logging with slog that's Datadog-compatible (reserved fields: `message`, `status`, `service`, trace correlation placeholders). Use viper for configuration loading from env vars and optional config files. Keep the foundation lean—just enough scaffolding for subsequent phases to build the actual commands.


### Reasoning

Listed the repository to confirm greenfield state. Read `README.md` to understand project architecture, configuration patterns, and logging requirements. Searched web for Datadog Go logging best practices with slog to ensure JSON structure aligns with Datadog's reserved fields and trace correlation patterns.


## Proposed File Changes

### go.mod(NEW)

References: 

- go.mod(NEW)

Initialize Go module with `module github.com/denniswebb/ghostwire`. Set Go version to `1.22` or later (for slog stdlib support). Add dependencies: `github.com/spf13/cobra` (latest stable, e.g., v1.8.x), `github.com/spf13/viper` (latest stable, e.g., v1.18.x). No need for external slog library since it's in stdlib as of Go 1.21. Run `go mod tidy` to generate `go.sum`.

### cmd(NEW)

Create top-level `cmd/` directory to house all command entry points following standard Go project layout.

### cmd/ghostwire(NEW)

Create `cmd/ghostwire/` subdirectory for the main binary entry point.

### cmd/ghostwire/main.go(NEW)

References: 

- internal/cmd/root.go(NEW)

Create main entry point for the ghostwire binary. Import cobra and the root command from `/internal/cmd/root.go`. In `main()` function, call `cmd.Execute()` and handle any error by logging and exiting with non-zero status. Keep this file minimal—just the entry point that delegates to cobra's root command execution.

### internal(NEW)

Create `internal/` directory for private application code that should not be importable by external projects.

### internal/cmd(NEW)

Create `internal/cmd/` directory to house cobra command definitions (root, init, watcher, injector).

### internal/cmd/root.go(NEW)

References: 

- internal/logging/logger.go(NEW)
- internal/cmd/init.go(NEW)
- internal/cmd/watcher.go(NEW)
- internal/cmd/injector.go(NEW)

Define the root cobra command for `ghostwire`. Set `Use: "ghostwire"`, `Short: "Invisible in-cluster traffic switcher for Blue/Green & Canary rollouts"`, and `Long` description from `README.md`. In `PersistentPreRunE`, initialize the global logger using `/internal/logging/logger.go` functions, passing the log level from viper config (key `log-level`, env `GW_LOG_LEVEL`, default `info`). Add persistent flags: `--log-level` (string, default `info`), `--config` (string, optional config file path). Bind flags to viper with `viper.BindPFlag()`. Set up viper to read from env vars with prefix `GW_` using `viper.SetEnvPrefix("GW")` and `viper.AutomaticEnv()`. If `--config` is provided, call `viper.SetConfigFile()` and `viper.ReadInConfig()`. Export an `Execute()` function that calls `rootCmd.Execute()`. Add placeholder subcommands (`init`, `watcher`, `injector`) in `init()` function by importing stub command files from `/internal/cmd/init.go`, `/internal/cmd/watcher.go`, `/internal/cmd/injector.go` and calling `rootCmd.AddCommand()`.

### internal/cmd/init.go(NEW)

References: 

- internal/logging/logger.go(NEW)
- internal/cmd/root.go(NEW)

Create stub cobra command for `ghostwire init`. Set `Use: "init"`, `Short: "Discover services and build DNAT rules"`. In `RunE`, log a placeholder message ("init command not yet implemented") using the global logger from `/internal/logging/logger.go` and return nil. Export the command as `InitCmd` so it can be added to root in `/internal/cmd/root.go`. This is a placeholder—actual implementation will come in the next phase.

### internal/cmd/watcher.go(NEW)

References: 

- internal/logging/logger.go(NEW)
- internal/cmd/root.go(NEW)

Create stub cobra command for `ghostwire watcher`. Set `Use: "watcher"`, `Short: "Poll pod labels and toggle iptables jump"`. In `RunE`, log a placeholder message ("watcher command not yet implemented") using the global logger from `/internal/logging/logger.go` and return nil. Export the command as `WatcherCmd` so it can be added to root in `/internal/cmd/root.go`. This is a placeholder—actual implementation will come in a later phase.

### internal/cmd/injector.go(NEW)

References: 

- internal/logging/logger.go(NEW)
- internal/cmd/root.go(NEW)

Create stub cobra command for `ghostwire injector`. Set `Use: "injector"`, `Short: "Run mutating admission webhook server"`. In `RunE`, log a placeholder message ("injector command not yet implemented") using the global logger from `/internal/logging/logger.go` and return nil. Export the command as `InjectorCmd` so it can be added to root in `/internal/cmd/root.go`. This is a placeholder—actual implementation will come in a later phase.

### internal/logging(NEW)

Create `internal/logging/` directory to house logger initialization and configuration logic.

### internal/logging/logger.go(NEW)

Implement logger initialization using Go's stdlib `log/slog`. Create a package-level variable `var Logger *slog.Logger` to hold the global logger instance. Export an `InitLogger(level string, service string)` function that: (1) parses the level string (`debug`, `info`, `warn`, `error`) into `slog.Level` (default to `slog.LevelInfo` if invalid); (2) creates a custom `slog.HandlerOptions` with the parsed level; (3) creates a `slog.JSONHandler` writing to `os.Stdout` with the options; (4) wraps the handler with a custom handler that adds Datadog-compatible reserved fields to every log record: `service` (from parameter), `status` (mapped from slog level: DEBUG→debug, INFO→info, WARN→warning, ERROR→error), and placeholder fields `dd.trace_id` and `dd.span_id` (set to empty strings for now—future phases can populate these if Datadog tracer is integrated). (5) Assign the resulting `slog.Logger` to the package-level `Logger` variable. Export a `GetLogger()` function that returns the current `Logger` instance. The custom handler wrapper should implement `slog.Handler` interface and delegate to the JSON handler while injecting the extra fields in the `Handle()` method using `record.AddAttrs()`. Note: The `message` field is automatically handled by slog's JSON handler as `msg`, which Datadog recognizes.

### internal/config(NEW)

Create `internal/config/` directory for configuration loading and validation logic (future use).

### internal/config/config.go(NEW)

Create a placeholder config package. Define a `Config` struct with fields matching the environment variables from `README.md`: `Namespace`, `Services`, `RoleLabelKey`, `RoleActive`, `RolePreview`, `SvcActivePattern`, `SvcPreviewPattern`, `DNSSuffix`, `NATChain`, `JumpHook`, `ExcludeCIDRs`, `PollInterval`, `RefreshInterval`, `IPv6`, `LogLevel`. Add struct tags for viper binding (e.g., `mapstructure:"namespace"`). Export a `Load()` function that calls `viper.Unmarshal()` to populate the struct from viper's merged config (env vars + config file). This is a foundation—actual usage will come in subsequent phases when commands need to read config. For now, just define the structure and the load function without calling it.

### pkg(NEW)

Create `pkg/` directory for public library code that could be imported by external projects (if any). Leave empty for now—future phases may add shared utilities here if needed.

### .gitignore(MODIFY)

Create `.gitignore` file with standard Go patterns: ignore `/ghostwire` (the compiled binary), `*.test`, `*.out`, `/vendor/`, `.DS_Store`, `*.swp`, `*.swo`, `/dist/`, and any local config files like `ghostwire.yaml` or `ghostwire.json`.

### .mise.toml(NEW)

Create a `.mise.toml` that pins Go to version `1.22.0` and defines tasks replacing the Makefile workflow: `build` (`go build -o ghostwire ./cmd/ghostwire`), `test` (`go test ./...`), `clean` (`rm -f ghostwire`), `fmt` (`go fmt ./...`), `vet` (`go vet ./...`), `lint` (run `golangci-lint` when available, otherwise emit a friendly message), and `help` (delegate to `mise tasks`). This keeps project automation within mise’s task runner while managing tool versions consistently.
