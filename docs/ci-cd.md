# CI/CD Reference

## Overview
- GitHub Actions provides the primary automation surface. Every workflow pins to the toolchain defined in `.mise.toml` via `jdx/mise-action@v2` so the CLI builds and tests consistently.
- Fast feedback is the priority: lint, static analysis, and race-enabled tests run on every push and pull request so regressions never reach `main`.
- Container publishing and releases target both linux/amd64 and linux/arm64, giving first-class support to Intel/AMD servers, AWS Graviton, and Apple Silicon-based fleets.
- Concurrency groups, artifact uploads, and caching keep runs deterministic while reducing cost.

## Workflows
- **`.github/workflows/ci.yml`** – Executes formatting checks, `mise run vet`, `mise run test -- -race -shuffle=on -coverprofile=coverage.out`, and `golangci-lint`. Uploads coverage artifacts for inspection and cancels superseded runs via a concurrency group.
- **`.github/workflows/build.yml`** – Validates `mise run build` across `ubuntu-latest`, `macos-latest`, and `windows-latest` runners for Go `1.22` and `1.23`. Artifacts expose the compiled binaries for ad-hoc testing.
- **`.github/workflows/container.yml`** – Provisions QEMU + Buildx, then builds a multi-architecture image (`linux/amd64`, `linux/arm64`). Pushes to GHCR on `main` merges using cache-backed layers. (Requires a Dockerfile under `build/` in future phases.)
- **`.github/workflows/release.yml`** – Runs tests, cross-compiles binaries into `dist/`, and publishes GitHub releases on `v*` tags. Currently a staging point for eventual GoReleaser integration and container tagging.
- **`.github/dependabot.yml`** – Automates dependency updates for Go modules and GitHub Actions on a weekly cadence with grouped pull requests.

## Multi-Architecture Container Builds
- Docker Buildx with QEMU enables building images locally and in CI for both amd64 and arm64. Workflows set `platforms: linux/amd64,linux/arm64` so GHCR receives a single manifest referencing both variants.
- Multi-arch support ensures parity between developer laptops (including Apple Silicon) and production clusters mixing x86_64 and ARM nodes. This avoids skew in kernel or libc expectations.
- Build cache is persisted with `type=gha` to minimize rebuild time while still producing reproducible layers.

## Local Testing with `act`
- Install [`act`](https://github.com/nektos/act) (e.g., `brew install act`) to execute workflows in Docker. The repo ships an `.actrc` that pins `ubuntu-latest` to `catthehacker/ubuntu:act-22.04`, forces `linux/amd64` containers, enables artifact storage at `.artifacts/`, and turns on offline mode for cached actions.
- Common commands:
  - `act pull_request` – run the same matrix GitHub will execute for PRs.
  - `act -j test` / `act -j lint` – focus on a single job from `ci.yml`.
  - `act -l` – list available jobs and associated workflows.
- Docker Buildx is required locally for accurate container builds. Install it via Docker Desktop or `docker buildx install`. QEMU emulation works on Apple Silicon but may be slower; expect multi-arch builds to take longer than native runs.
- The artifact server path is relative; delete `.artifacts/` when you need a clean slate or add it to `.gitignore` (already configured).

## Adding New Workflows
- Always bootstrap tools with `jdx/mise-action@v2` so jobs use the repository’s pinned toolchain, and enable caching to re-use downloads between runs.
- Follow the principle of least privilege: default to `permissions: contents: read` unless a workflow must write releases, packages, or other artifacts.
- Prefer matrix strategies to cover multiple platforms instead of duplicating jobs, and upload artifacts so reviewers can validate binaries.
- Test new or modified workflows with `act` before committing to avoid iterative pushes. Update `AGENTS.md` if you introduce new expectations.

## Secrets and Variables
- `GITHUB_TOKEN` (provided automatically) authenticates GitHub API calls, uploads artifacts, and logs into GHCR for container pushes. No manual secret is required unless you target an external registry.
- Add additional secrets via the repository settings when third-party services are integrated. Reference them through `${{ secrets.NAME }}` and never echo values into logs.
- When adding container registries, scope service accounts to minimum required permissions and document expectations here.

## Troubleshooting
- **Tool downloads fail** – Ensure `GITHUB_TOKEN` is available; `act` users may need to run `act -s GITHUB_TOKEN=$(gh auth token)` if private releases are referenced.
- **Cache misses** – GitHub Actions caches are keyless in these workflows; long gaps between runs may rebuild tools. This is expected but can be tuned by introducing explicit cache keys.
- **`act` image too large** – The `catthehacker` images are sizable; use `act -P ubuntu-latest=node:18-bullseye` for a lighter alternative at the expense of fewer preinstalled tools.
- **Docker context errors** – Verify Docker Desktop or the Docker daemon is running. On Linux, ensure the runner user is in the `docker` group.
- **Slow multi-arch builds** – QEMU adds overhead. Consider native arm64 runners when ARM builds become a bottleneck or rely on registry cache hits.
