I have created the following plan after thorough exploration and analysis of the codebase. Follow the below plan verbatim. Trust the files and references. Do not re-verify what's written in the plan. Explore only when absolutely necessary. First implement all the proposed file changes and then I'll review all the changes together at the end.

### Observations

The project already uses `mise` for task management (`.mise.toml` defines build, test, fmt, vet, lint, clean tasks) and has documented this in `AGENTS.md`. The user wants to add GitHub Actions for CI/CD with local testing via `act`, and specifically requires multi-architecture container images (amd64 and arm64). The web search revealed best practices: use `jdx/mise-action@v2` for tool/task integration, `actions/setup-go@v5` with caching, `golangci/golangci-lint-action@v8` for linting, and Docker Buildx for multi-platform container builds. The project is greenfield with minimal code, making this the ideal time to establish CI/CD patterns before subsequent phases add complexity.

### Approach

Create a GitHub Actions CI/CD pipeline that leverages the existing `mise` tasks, follows Go best practices (test with race detection, lint with golangci-lint, security scanning), and supports local testing via `act`. Add workflows for: (1) CI (test/lint/vet on PR and push), (2) multi-architecture container builds (amd64 and arm64 for future phases), and (3) release automation (placeholder). Configure `act` with sensible defaults for local iteration. Update `AGENTS.md` to document the CI/CD workflow and local testing procedures.

### Reasoning

Listed the repository structure to confirm existing files. Read `AGENTS.md`, `.mise.toml`, and `README.md` to understand the project's tooling choices and task definitions. Searched the web for GitHub Actions best practices with Go (2024/2025), `act` local testing patterns, and `mise-action` integration to ensure the plan follows current standards and avoids deprecated patterns (e.g., artifact actions v3). The user clarified the need for multi-architecture container images (amd64 and arm64).

## Mermaid Diagram

sequenceDiagram
    participant Dev as Developer
    participant Local as Local Machine
    participant Act as act (Docker)
    participant GH as GitHub Actions
    participant Buildx as Docker Buildx
    participant GHCR as GitHub Container Registry

    Dev->>Local: git commit & push
    Local->>Act: act pull_request (optional local test)
    Act->>Act: Run ci.yml in Docker
    Act-->>Dev: Test/lint results

    Local->>GH: git push origin feature-branch
    GH->>GH: Trigger ci.yml (test, lint)
    GH->>GH: Trigger build.yml (cross-platform)
    GH-->>Dev: CI status on PR

    Dev->>GH: Merge PR to main
    GH->>GH: Run ci.yml on main
    GH->>GH: Run container.yml
    GH->>Buildx: Build for linux/amd64
    GH->>Buildx: Build for linux/arm64
    Buildx->>GHCR: Push multi-arch manifest
    GHCR-->>GH: Image published

    Dev->>GH: Push tag v1.0.0
    GH->>GH: Trigger release.yml
    GH->>GH: Build binaries, create release
    GH->>GH: Trigger container.yml (tagged)
    GH->>GHCR: Push v1.0.0 multi-arch image
    GH-->>Dev: Release published

## Proposed File Changes

### .github(NEW)

Create `.github/` directory to house GitHub-specific configuration (workflows, dependabot, issue templates).

### .github/workflows(NEW)

Create `.github/workflows/` directory for GitHub Actions workflow definitions.

### .github/workflows/ci.yml(NEW)

References: 

- .mise.toml

Create the main CI workflow triggered on `push` to `main` and `pull_request` events. Set concurrency group to `ci-${{ github.ref }}` with `cancel-in-progress: true` to auto-cancel superseded runs. Define a `test` job running on `ubuntu-latest` with steps: (1) `actions/checkout@v4`, (2) `jdx/mise-action@v2` with `install: true`, `cache: true`, and `version: 2024.10.0` (or latest stable), (3) `mise run fmt` to verify formatting (fail if changes detected via `git diff --exit-code`), (4) `mise run vet` for static analysis, (5) `mise run test` with additional flags `-race -shuffle=on -coverprofile=coverage.out` to enable race detection, randomize test order, and generate coverage, (6) `actions/upload-artifact@v4` to upload `coverage.out` with `retention-days: 7`. Add a `lint` job (can run in parallel with `test`) using `golangci/golangci-lint-action@v8` with `version: latest` (or pin a specific version like `v1.61`), `args: --timeout=5m`, and `only-new-issues: false` for full repo linting. Set default permissions to `contents: read` for security hardening. Add env var `GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}` to the mise-action step to avoid rate limits when fetching tools from GitHub releases.

### .github/workflows/build.yml(NEW)

References: 

- .mise.toml

Create a build workflow triggered on `push` to `main`, `pull_request`, and `workflow_dispatch` (for manual runs). Define a `build` job with a matrix strategy covering `os: [ubuntu-latest, macos-latest, windows-latest]` and `go-version: ['1.22', '1.23']` (or just the versions you want to support). Steps: (1) `actions/checkout@v4`, (2) `jdx/mise-action@v2` with `install: true` and `cache: true`, (3) `mise run build` to compile the binary, (4) verify the binary exists and is executable (use conditional checks for Windows vs Unix), (5) `actions/upload-artifact@v4` to upload the compiled binary with a name like `ghostwire-${{ matrix.os }}-${{ matrix.go-version }}` and `retention-days: 7`. This validates cross-platform builds and provides downloadable binaries for testing. Set `permissions: contents: read`.

### .github/workflows/container.yml(NEW)

Create a multi-architecture container build workflow (placeholder for future phases when Dockerfile is added). Trigger on `push` to `main` and `pull_request`, plus `workflow_dispatch`. Define a `build-container` job that: (1) checks out code with `actions/checkout@v4`, (2) sets up QEMU with `docker/setup-qemu-action@v3` to enable cross-platform emulation, (3) sets up Docker Buildx with `docker/setup-buildx-action@v3`, (4) logs in to GHCR with `docker/login-action@v3` using `${{ secrets.GITHUB_TOKEN }}` (only on push to main, not PRs, controlled by `if: github.event_name != 'pull_request'`), (5) extracts metadata with `docker/metadata-action@v5` to generate tags (e.g., `latest`, `sha-<commit>`, semver tags on release) and labels, (6) builds and pushes with `docker/build-push-action@v6` targeting `./build/Dockerfile` (which doesn't exist yet—document this as a placeholder), setting `platforms: linux/amd64,linux/arm64` for multi-architecture builds, `push: ${{ github.event_name != 'pull_request' }}` to only push on main, `tags` and `labels` from metadata action, `cache-from: type=gha` and `cache-to: type=gha,mode=max` for GitHub Actions cache to speed up builds. Add a comment at the top: `# Placeholder: requires Dockerfile in /build/ directory (added in later phase)`. Set `permissions: contents: read, packages: write` to allow GHCR pushes. Document that the multi-arch build will produce images for both amd64 (x86_64) and arm64 (aarch64) architectures, suitable for Intel/AMD servers and ARM-based systems (AWS Graviton, Apple Silicon, etc.).

### .github/workflows/release.yml(NEW)

References: 

- .mise.toml

Create a release workflow triggered on `push` with `tags: ['v*']` pattern. Define a `release` job that: (1) checks out code with `fetch-depth: 0` for full history, (2) sets up mise and Go, (3) runs `mise run test` to validate the tagged commit, (4) builds binaries for multiple platforms using a matrix or a tool like GoReleaser (document GoReleaser as optional/future enhancement), (5) creates a GitHub release with `softprops/action-gh-release@v2` or similar, attaching the binaries and generating release notes from commits. Add a comment: `# Placeholder: expand with GoReleaser or multi-platform build matrix in later phase`. Set `permissions: contents: write` to allow release creation. Note that this workflow should also trigger the container build workflow to produce multi-arch container images tagged with the release version.

### .github/dependabot.yml(NEW)

Create Dependabot configuration to keep GitHub Actions and Go dependencies up to date. Define two update configurations: (1) `package-ecosystem: github-actions` with `directory: /` and `schedule: interval: weekly` to auto-update action versions, (2) `package-ecosystem: gomod` with `directory: /` and `schedule: interval: weekly` to update Go module dependencies. Set `open-pull-requests-limit: 5` for each to avoid PR spam. Add `groups` to batch minor/patch updates together (e.g., group all GitHub Actions updates into a single PR). This ensures the project stays current with security patches and new features.

### .actrc(NEW)

Create `act` configuration file to set sensible defaults for local GitHub Actions testing. Define: (1) `-P ubuntu-latest=catthehacker/ubuntu:act-22.04` to use a medium-sized runner image with common tools preinstalled (balances size and compatibility), (2) `--container-architecture linux/amd64` for Apple Silicon Macs to avoid architecture mismatches (note: act runs workflows in a single architecture locally; multi-arch builds require Docker Buildx which act supports), (3) `--artifact-server-path .artifacts` to enable local artifact upload/download with actions v4, (4) `--action-offline-mode` to cache action code and avoid re-downloading on every run (speeds up iteration). Add comments explaining each flag and noting that users can override with command-line flags. Document that the `.artifacts/` directory should be added to `.gitignore`. Note that multi-architecture container builds will work in act if Docker Buildx is available, but QEMU emulation may be slow for cross-platform builds.

### .gitignore(MODIFY)

Add entries for `act` artifacts and GitHub Actions local testing: `.artifacts/` (local artifact server directory), `.act/` (act cache directory if used), and any other act-related temporary files. Ensure the existing entries for the compiled binary (`ghostwire`), Go build artifacts (`*.test`, `*.out`), and editor files (`.DS_Store`, `*.swp`) are preserved.

### .golangci.yml(NEW)

References: 

- .mise.toml
- .github/workflows/ci.yml(NEW)

Create golangci-lint configuration file to define linting rules and behavior. Enable a curated set of linters appropriate for a new Go project: `gofmt`, `goimports`, `govet`, `errcheck`, `staticcheck`, `unused`, `gosimple`, `ineffassign`, `typecheck`, `misspell`, `revive` (or `golint` if preferred), `gosec` (security), `gocritic` (style/performance). Configure `linters-settings` to tune specific linters (e.g., `revive` rules, `gosec` exclusions for known-safe patterns). Set `issues.exclude-use-default: false` to see all issues. Add `run.timeout: 5m` and `run.tests: true` to lint test files. Document that this config is used by both the GitHub Actions lint job and local `mise run lint` (which calls `golangci-lint run`).

### AGENTS.md(MODIFY)

References: 

- .actrc(NEW)
- .github/workflows/ci.yml(NEW)
- .github/workflows/build.yml(NEW)
- .github/workflows/container.yml(NEW)

Update the `Workflow Expectations` section to add a new subsection: **4. Continuous Integration & Local Testing.** Document that: (1) All CI/CD runs through GitHub Actions (workflows in `.github/workflows/`), (2) The `ci.yml` workflow runs on every PR and push to main, executing `mise run fmt`, `mise run vet`, `mise run test` (with race detection and coverage), and `golangci-lint`, (3) Developers should test GitHub Actions locally using `act` before pushing (install via `brew install act` or the official installer), (4) Run `act pull_request` to simulate a PR build locally, or `act -j test` to run a specific job, (5) Configuration defaults are in `.actrc` (uses medium Ubuntu image, enables artifacts, caches actions), (6) The `build.yml` workflow validates cross-platform builds, and `container.yml` builds multi-architecture container images (amd64 and arm64) for deployment, (7) `release.yml` is a placeholder for future tagged releases. Add a new section under `Useful Commands` titled **Local CI Testing (via act)** with a table: `act pull_request` (simulate PR CI locally), `act -j test` (run just the test job), `act -j lint` (run just the lint job), `act -l` (list all jobs), `act --artifact-server-path .artifacts` (enable artifact upload/download). Update the `Testing & Validation Strategy` section to mention: Before opening a PR, run `mise run test` and optionally `act pull_request` to catch CI failures early. Update the `Collaboration Tips` section to note: When modifying GitHub Actions workflows, test locally with `act` to avoid push-fix-push cycles. Add a note about multi-architecture container builds: The `container.yml` workflow produces images for both linux/amd64 and linux/arm64 platforms, ensuring compatibility with Intel/AMD servers and ARM-based systems (AWS Graviton, Apple Silicon Kubernetes nodes, etc.).

### README.md(MODIFY)

References: 

- .actrc(NEW)
- .github/workflows/ci.yml(NEW)
- .github/workflows/container.yml(NEW)
- AGENTS.md(MODIFY)

Update the `Development` section to add a subsection on CI/CD and local testing. After the existing `mise` task documentation, add: **Continuous Integration:** All code changes are validated by GitHub Actions (see `.github/workflows/`). The CI pipeline runs tests with race detection, linting with `golangci-lint`, and cross-platform builds. Container images are built for both amd64 and arm64 architectures. **Local Testing:** Use `act` to run GitHub Actions workflows locally before pushing. Install with `brew install act` (macOS) or see [nektos/act](https://github.com/nektos/act) for other platforms. Run `act pull_request` to simulate a full PR build, or `act -j test` to run just the test job. Configuration defaults are in `.actrc`. **Pre-commit Checklist:** Before opening a PR, run `mise run fmt`, `mise run vet`, `mise run test`, and optionally `act pull_request` to catch issues early. **Multi-Architecture Support:** Container images are built for linux/amd64 and linux/arm64, supporting Intel/AMD servers, AWS Graviton instances, and Apple Silicon-based Kubernetes nodes. This ensures developers understand the CI/CD workflow, multi-arch support, and how to test locally.

### docs/ci-cd.md(NEW)

References: 

- .github/workflows/ci.yml(NEW)
- .github/workflows/build.yml(NEW)
- .github/workflows/container.yml(NEW)
- .github/workflows/release.yml(NEW)
- .actrc(NEW)
- AGENTS.md(MODIFY)

Create a dedicated CI/CD documentation file explaining the GitHub Actions setup in detail. Structure: **Overview** (describe the CI/CD philosophy: fast feedback, test before merge, automate releases, multi-architecture support), **Workflows** (list each workflow file with its purpose: `ci.yml` for test/lint, `build.yml` for cross-platform validation, `container.yml` for multi-arch Docker images targeting linux/amd64 and linux/arm64, `release.yml` for tagged releases), **Multi-Architecture Container Builds** (explain the use of Docker Buildx and QEMU for cross-platform builds, the platforms supported, and why this matters for deployment to heterogeneous Kubernetes clusters with both x86 and ARM nodes), **Local Testing with act** (installation instructions, common commands, troubleshooting tips for Docker/Podman, Apple Silicon notes, artifact server setup, note that multi-arch builds work locally if Docker Buildx is configured but may be slow due to QEMU emulation), **Adding New Workflows** (guidelines: use `mise-action` for tool setup, follow security best practices from `AGENTS.md`, test locally with `act` before committing), **Secrets and Variables** (document any required secrets like `GITHUB_TOKEN` for GHCR pushes, explain how to add repo secrets), **Troubleshooting** (common issues: rate limits, cache misses, act image size, Docker context problems, QEMU emulation slowness for cross-platform builds, Buildx setup). Reference the GitHub Actions best practices from the web search (concurrency, caching, security hardening). This provides a single source of truth for CI/CD that both humans and agents can reference.