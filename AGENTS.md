# AGENTS Guidance for `ghostwire`

> This document follows the open format described at [https://agents.md](https://agents.md) so that coding agents have a predictable starting point when working in this repository.

## Project Snapshot
- **Language & Tooling:** Go 1.24 managed via `mise` (`.mise.toml` is canonical).
- **Binary:** Single CLI at `cmd/ghostwire/main.go` with cobra-driven subcommands.
- **Core packages:** `internal/cmd` (CLI), `internal/logging` (slog handler), `internal/config` (viper bindings), `internal/discovery` (Kubernetes service auto-discovery and ClusterIP extraction), `internal/iptables` (iptables/ip6tables command wrapper, DNAT chain and rule management), `internal/k8s` (Kubernetes client setup, pod label reading, polling orchestration), `internal/metrics` (Prometheus metrics collection, health checks, DNAT map parsing); `pkg/` reserved for public utilities.
- **Logging:** Go `log/slog` JSON handler decorated for Datadog (`service`, `status`, `dd.trace_id`, `dd.span_id` placeholders).
- **Configuration:** `spf13/viper` sourcing env vars (`GW_*`), flags, and optional config file.

## Workflow Expectations
1. **Bootstrap with `mise`.**
   - Run `mise install` to provision Go.
   - Execute tasks through `mise run <name>` so we stay on the pinned toolchain.
2. **Honor the project layout.**
   - Place Go entrypoints under `cmd/`, app internals under `internal/`, reusable APIs under `pkg/`.
3. **Use the shared logger and Viper wiring.**
   - Retrieve the logger via `logging.GetLogger()`.
   - Add new configuration through Viper (set defaults, env bindings, and flags together).
4. **Continuous Integration & Local Testing.**
   - All CI/CD automation runs via GitHub Actions; workflows live in `.github/workflows/`.
   - `ci.yml` executes `mise run fmt`, `mise run vet`, `mise run test` (with `-race`, `-shuffle=on`, and coverage output), and `golangci-lint`.
   - Install [`act`](https://github.com/nektos/act) (e.g., `brew install act`) to rehearse workflows locally before pushing.
   - Run `act pull_request` for end-to-end simulation or `act -j test` / `act -j lint` for targeted iterations; defaults are defined in `.actrc` with artifacts stored under `.artifacts/`.
   - `build.yml` verifies cross-platform builds, `container.yml` produces linux/amd64 and linux/arm64 container images for Intel/AMD servers, AWS Graviton, and Apple Silicon nodes, and `release.yml` is staged for future tagged releases.
5. **Keep changes scoped and explicit.**
   - Avoid introducing background processes or network dependencies unless requested.
   - Talk through disruptive changes (new services, alternative CLIs) before implementing.

## Useful Commands (via `mise run`)
| Task | Purpose |
| --- | --- |
| `mise run help` | Show all registered tasks. |
| `mise run build` | Compile the CLI to `./ghostwire`. |
| `mise run test` | Execute `go test ./...`. |
| `mise run fmt` | Format Go sources (`go fmt ./...`). |
| `mise run vet` | Run static analysis (`go vet ./...`). |
| `mise run lint` | Run `golangci-lint` when available (prints a skip notice otherwise). |
| `mise run clean` | Remove the compiled binary. |

*Note:* Go commands may touch the global build cache under `~/Library/Caches/go-build`; in restricted sandboxes you may need elevated permissions (request via the CLI harness rather than modifying commands).

## Local CI Testing (via act)
| Command | Purpose |
| --- | --- |
| `act pull_request` | Simulate the full CI pipeline as if a PR were opened. |
| `act -j test` | Run only the `test` job from `ci.yml`. |
| `act -j lint` | Run only the `lint` job from `ci.yml`. |
| `act -l` | List all jobs discovered in the current workflows. |
| `act --artifact-server-path .artifacts` | Override artifact storage location when needed. |

## Coding Guidelines
- Prefer idiomatic, composable Go. Keep functions focused and avoid global state beyond the shared logger.
- Use dependency injection (passing interfaces where helpful) to keep packages testable.
- Favor table-driven tests for behavior-heavy code and keep fixtures small and explicit.
- Propagate `context.Context` through future long-running operations to support cancellation.
- Stick to structured logging; include relevant key/value pairs (`logger.Info("...", slog.String("component", "..."))`).
- The iptables package uses an Executor interface for testability—production code uses RealExecutor (runs actual commands), tests inject a mock that records commands without executing them. All iptables operations are idempotent (init can be run multiple times safely).
- Service discovery is fully automatic—the init container lists namespace services and matches base/preview pairs via pattern templates. Do not reintroduce explicit service lists.
- Maintain ASCII-only source unless extending existing Unicode text.
- The watcher runs as a long-lived sidecar that polls pod labels at a configurable interval (default 2s), reacts to role transitions (active ↔ preview) with Info logs, exposes `/metrics` and `/healthz` on `:8081`, and keeps running through transient API errors until its context is cancelled. Metrics follow Prometheus naming conventions (lowercase snake_case, `_total` suffix for counters, bounded label cardinality) and the watcher relies on a `TransitionHandler` callback to trigger iptables jump management without coupling the poller to iptables internals. The exported set today is `ghostwire_jump_active` (gauge; 1 when preview routing is active, 0 otherwise), `ghostwire_errors_total{type=...}` for error categories, and `ghostwire_dnat_rules` (gauge; total rule count). We intentionally avoid `jump_state{state="preview"|"active"}` or per-service DNAT labels to keep scrape cardinality predictable.

## Testing & Validation Strategy
- Add unit tests when evolving command behavior, configuration parsing, or logging utilities.
- Always run `mise run test` yourself before handing work back—never defer this command to the user—and run `act pull_request` when you need to mirror CI locally.
- When new dependencies are added, finish by running `go mod tidy` (through the mise-managed toolchain) so `go.sum` stays accurate.

Unit tests follow table-driven patterns with `t.Parallel()` for concurrency. The discovery package exercises service pairing logic against a fake Kubernetes clientset, and the iptables package relies on the `recordingExecutor` mock to validate command sequencing without touching the host firewall. Integration testing uses KIND clusters (see `/test/kind/`) with sample Service manifests and validation scripts. Before opening a PR, run unit tests (`mise run test`), optionally run `act pull_request` for CI rehearsal, and consider the KIND flow when changes affect service discovery or iptables logic. `/test/kind/README.md` documents the full integration workflow.

## Observability & Error Handling
- Use the provided slog-based logger; do not introduce alternative logging frameworks.
- Map errors to actionable log fields; lean on `fmt.Errorf("...: %w", err)` for wrapping.
- Keep Datadog field naming consistent (`status`, `service`, `dd.trace_id`, `dd.span_id`); future tracer integration will rely on these keys.

## Security & Operational Notes
- The runtime components eventually require `NET_ADMIN` capabilities; ensure docs and code continue to call that out.
- The init container creates the DNAT chain and rules but does **not** add the jump rule—the watcher sidecar activates routing by adding `-j CANARY_DNAT` to the configured hook (OUTPUT or PREROUTING via `GW_JUMP_HOOK`) when the pod's role label becomes `preview`, removes it when the role returns to `active`, and inserts the jump at position 1 so it evaluates before other rules in the hook chain.
- The init container must run with a ServiceAccount permitted to list Services in its namespace (`resources: ["services"], verbs: ["list"]`).
- The watcher sidecar requires RBAC permissions to get its own Pod in the namespace. The ServiceAccount must have a Role with `resources: ["pods"], verbs: ["get"]` bound to it. For tighter security, use `resourceNames: ["$(POD_NAME)"]` to restrict access to only the watcher's own pod.
- Respect user environment variables and configuration precedence: flags > config file > env vars > defaults.
- Avoid storing secrets or credentials in the repo; use environment variables or dedicated secret managers.

## Collaboration Tips
- Coordinate large refactors (CLI structure, logging strategy, configuration contracts) before executing.
- Update `README.md`, `.mise.toml`, and this file when workflows or tooling expectations shift.
- Leave TODO comments sparingly and with clear follow-up context (e.g., `TODO(ghostwire/123)`).
- When modifying GitHub Actions workflows, validate them with `act` to avoid push-fix cycles.

## Agent Workflow Principles
- Scan entire files when context matters so you understand existing code and architecture before editing.
- Structure work into logical milestones and signal completion before moving on to the next stage.
- Verify external library usage against current documentation—check Context7 MCP docs first and fall back to web search only if needed.
- If required dependencies seem broken, recheck the docs, debug methodically, and ask for guidance before proposing alternatives.
- Draft and share a plan for non-trivial tasks before writing significant code, and confirm the approach when scope or architecture questions remain.
- Approach tasks with the confidence of a seasoned polyglot across architecture, systems, UX, and copywriting.
- For UI/UX work, pursue designs that are aesthetically pleasing, easy to use, and attentive to interaction details.
- Always run linting after major changes to catch syntax and style issues early.
- Organize code into appropriate files and follow best practices for naming, modularity, complexity, and commenting.
- Optimize every change for readability; code is read more than it is written.
- Deliver working implementations—avoid “dummy” scaffolds unless the user explicitly requests them.
- Ask clarifying questions whenever requirements are unclear instead of making risky assumptions.
- Avoid large refactors unless the user explicitly requests them.
- When repeated issues arise, focus on identifying the root cause instead of trying random fixes or abandoning required tools.
- Break large or vague tasks into smaller subtasks, or collaborate with the user to refine the scope before proceeding.

Following these guidelines keeps development predictable for both humans and agents. When unsure, consult the `README.md`, `.mise.toml`, or start a discussion before diverging from the practices above.
