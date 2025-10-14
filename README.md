# ghostwire

Invisible in-cluster traffic switcher for Blue/Green & Canary rollouts.

`ghostwire` makes pods labeled as “preview” route to matching preview services (like `*-preview`) instead of the active ones. It does this at L4 with DNAT rules. No app code changes, no mesh dependency, no DNS roulette. You choose the labels, patterns, and behavior.

## Why
- Preview pods automatically talk to preview backends.
- Fully automatic service discovery—no service lists to maintain when teams add new workloads.
- If a preview isn’t present, traffic falls back to active.
- No restarts during promotions; Argo Rollouts flips labels, `ghostwire` flips routing.
- All the knobs you’ll ask for later are here now.

## Components
- **`init`**: automatically discovers all Services in the namespace via the Kubernetes API, identifies base/preview pairs (e.g., `orders` + `orders-preview`), creates a custom DNAT chain (default: `CANARY_DNAT`), adds exclusion rules for IMDS and DNS, builds DNAT rules mapping active ClusterIP:port → preview ClusterIP:port for all discovered services, and writes `/shared/dnat.map` for audit. Does **not** activate routing—that's the watcher’s job.
- **`watcher`**: long-running sidecar that polls its own Pod's labels at a configurable interval (default 2s), detects role transitions between active and preview states, and logs state changes. Handles graceful shutdown via SIGTERM/SIGINT. (Note: iptables jump management and health/metrics endpoints will be added in the subsequent phase.)
- **`injector`**: mutating admission webhook that injects the init and watcher based on annotations. Optional, but saves your wrists.

Language: **Go**. Single static binaries. Tiny images. Fewer surprises.

---

## Development

Install [mise](https://mise.jdx.dev) to manage the toolchain defined in `.mise.toml`, then run `mise install` to pull Go 1.24.9 into your environment. Common workflows run through `mise run`: `build` produces the `ghostwire` binary, `test` runs `go test ./...`, `fmt` formats sources, `vet` performs static analysis, `lint` executes `golangci-lint` when it is on your PATH, and `clean` removes local build artifacts. `mise run help` lists every available task.

**Continuous Integration:** All code changes are validated by GitHub Actions (see `.github/workflows/`). The CI pipeline runs formatting checks, static analysis, race-enabled tests with coverage, linting via `golangci-lint`, and cross-platform builds to ensure the CLI stays portable.

**Local Testing:** Use [`act`](https://github.com/nektos/act) to exercise workflows before pushing. Install with `brew install act` on macOS (see upstream docs for other platforms), then run `act pull_request` for the full pipeline or target specific jobs such as `act -j test`. Defaults live in `.actrc`, including artifact storage under `.artifacts/`.

**Pre-commit Checklist:** Before opening a PR, run `mise run fmt`, `mise run vet`, `mise run test`, and optionally `act pull_request` to catch CI failures early.

**Integration Testing:** Local integration tests use [KIND](https://kind.sigs.k8s.io/) to validate the init command against real Kubernetes Services. The `/test/kind/` directory contains cluster setup scripts, sample manifests, and validation helpers. Typical flow: `./test/kind/setup-cluster.sh`, `./test/kind/load-image.sh`, `./test/kind/deploy-test.sh`, followed by the validation scripts under `/test/kind/`. See `/test/kind/README.md` for detailed instructions. Integration runs are optional for most PRs but recommended when touching service discovery or iptables logic.

**Multi-Architecture Support:** Container images are built for `linux/amd64` and `linux/arm64`, providing coverage for Intel/AMD servers, AWS Graviton nodes, and Apple Silicon-based Kubernetes clusters.

---

## Install (high level)
1. Deploy the injector (mutating webhook) and its certs/CA bundle.
2. Label/annotate workloads so the injector knows to mutate them.
3. Profit when preview routing flips without a restart.

Helm chart and raw manifests live in `/deploy/` in this repo. The injector is namespaced-scoped by default; cluster-scoped if you insist on danger.

---

## Workload Annotations (injector controls)

Add these to a `Deployment`, `StatefulSet`, or `Rollout` to enable:

```yaml
metadata:
  annotations:
    ghostwire.dev/enabled: "true"                    # enable injection
    ghostwire.dev/roleLabelKey: "role"               # label key to read on Pod
    ghostwire.dev/roleActive: "active"               # value that disables jump
    ghostwire.dev/rolePreview: "preview"             # value that enables jump
    ghostwire.dev/svcPreviewPattern: "{{name}}-preview"
    ghostwire.dev/namespace: "prod"                  # default: Pod namespace
    ghostwire.dev/dnsSuffix: ".svc.cluster.local"    # override if you like pain
    ghostwire.dev/jumpHook: "OUTPUT"                 # OUTPUT or PREROUTING
    ghostwire.dev/excludeCidrs: "169.254.169.254/32,10.96.0.10/32"  # don’t touch
    ghostwire.dev/pollInterval: "2s"                 # watcher poll interval
    ghostwire.dev/refreshInterval: "15m"             # optional full DNAT rebuild
    ghostwire.dev/ipv6: "false"                      # true enables ip6tables too
```

> You can set repo-wide defaults via a ConfigMap; annotations always win.

---

## Environment Variables (for when not using the injector)

`ghostwire-init` and `ghostwire-watcher` accept the same knobs via env:

| Var | Default | What it does |
|---|---|---|
| `GW_NAMESPACE` | Pod namespace | Namespace for service discovery (falls back to `POD_NAMESPACE` or `default`) |
| `GW_ROLE_LABEL_KEY` | `role` | Pod label key to read |
| `GW_ROLE_ACTIVE` | `active` | “Active” value |
| `GW_ROLE_PREVIEW` | `preview` | “Preview” value |
| `GW_SVC_PREVIEW_PATTERN` | `{{name}}-preview` | Go-template preview service name |
| `GW_ACTIVE_SUFFIX` | `-active` | Suffix used to detect active services when pairing |
| `GW_PREVIEW_SUFFIX` | `-preview` | Preview suffix paired with `GW_ACTIVE_SUFFIX` matches |
| `GW_DNS_SUFFIX` | `.svc.cluster.local` | Cluster DNS suffix |
| `GW_NAT_CHAIN` | `CANARY_DNAT` | iptables chain name |
| `GW_IPTABLES_DNAT_MAP` | `/shared/dnat.map` | Path where `ghostwire init` writes the DNAT map artefact |
| `GW_JUMP_HOOK` | `OUTPUT` | `OUTPUT` or `PREROUTING` |
| `GW_EXCLUDE_CIDRS` | IMDS, DNS | CSV of CIDRs to skip |
| `GW_POLL_INTERVAL` | `2s` | Watcher poll cadence |
| `GW_REFRESH_INTERVAL` | empty | If set, periodic rebuild of DNAT |
| `GW_IPV6` | `false` | Add ip6tables rules |
| `GW_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |

---

### NAT Chain Configuration Examples

- **Custom chain name**: keep the watcher jump stable while testing alternate rule sets.
  ```sh
  export GW_NAT_CHAIN="CANARY_DNAT_V2"
  ghostwire init
  ```
- **Additional exclusions**: skip corporate CIDRs alongside the built-in IMDS/DNS ranges.
  ```sh
  export GW_EXCLUDE_CIDRS="169.254.169.254/32,10.3.0.0/16"
  ghostwire init
  ```
- **Dual-stack clusters**: enable ip6tables rules when preview/endpoints use IPv6 addresses.
  ```sh
  export GW_IPV6="true"
  ghostwire init
  ```

> `ghostwire init` issues iptables commands with `-w 5`, telling the kernel to wait up to five seconds for xtables locks. Concurrent init pods will serialize on the kernel lock instead of racing each other; if contention persists past the timeout, the command fails and surfaces in logs.

> Mount the `/shared` volume (where `GW_IPTABLES_DNAT_MAP` lives) with permissions that prevent peer containers from writing to the file. The default `emptyDir` mode is `0777`; tighten it to match your security posture if multiple containers share the volume.

---

## Example: Argo Rollouts Blue/Green

Rollouts flips the labels. `ghostwire` reads the label and toggles one jump.

```yaml
strategy:
  blueGreen:
    activeService: orders-active
    previewService: orders-preview
    activeMetadata:  { labels: { role: "active" } }
    previewMetadata: { labels: { role: "preview" } }
```

Workload annotated for injection:

```yaml
metadata:
  annotations:
    ghostwire.dev/enabled: "true"
```

That’s it. No app changes. No service mesh hand-holding.

---

## Security
- Pods need `NET_ADMIN` to program iptables. Yes, that’s spicy. Scope the ServiceAccount per workload and bind only `get` on its own Pod:
  - Role: `resources: ["pods"], verbs: ["get"]`
  - Optionally template `resourceNames: ["$(POD_NAME)"]`
- Watcher sidecar needs RBAC permissions: `resources: ["pods"], verbs: ["get"]` to read its own pod labels. For enhanced security, scope the Role with `resourceNames: ["$(POD_NAME)"]` to restrict access to only the watcher's pod.
- Init container needs RBAC permissions to list Services in its namespace (`resources: ["services"], verbs: ["list"]`).
- Injector runs with minimal RBAC, mutating only annotated workloads.
- Exclude CIDRs for IMDS, DNS, or anything else you shouldn’t mangle.

---

## Failure Modes (so you’re not surprised)
- **No preview service**: DNAT rule isn’t created. Calls go to active. Boring by design.
- **Service recreated**: ClusterIP changes. Either roll the pods or set `GW_REFRESH_INTERVAL` to rebuild periodically.
- **TLS/SNI**: L4 DNAT doesn’t rewrite SNI. Since preview/active are same app, SNI usually matches. If you need real SNI routing, swap DNAT for an Envoy `tcp_proxy` later; the control flow stays the same.
- **Dual stack**: Set `GW_IPV6=true`. You’ll get iptables and ip6tables rules.

---

## Injector Behavior (what actually gets added)

- An **initContainer** that:
  - Discovers services, builds DNAT rules with exclusions, writes `/shared/dnat.map` for audit/debug, and exits without enabling the chain (watcher activates it).
- A **watcher sidecar** that:
  - Polls the Pod’s `role` label.
  - Adds or removes a single `-j CANARY_DNAT` jump in `OUTPUT` (or `PREROUTING`) accordingly.
  - Exposes `/healthz` and `/metrics` on `:8081` because ops likes graphs.

Both containers are injected with the env derived from annotations, and both get `NET_ADMIN`. If that offends your sensibilities, don’t use iptables to route traffic.

---

## Minimal Example (no injector)

```yaml
volumes:
- name: shared
  emptyDir: {}

initContainers:
- name: ghostwire-init
  image: ghcr.io/yourorg/ghostwire-init:latest
  securityContext:
    capabilities: { add: ["NET_ADMIN"] }
  env:
  - { name: GW_SVC_PREVIEW_PATTERN, value: "{{name}}-preview" }
  volumeMounts:
  - { name: shared, mountPath: /shared }

containers:
- name: ghostwire-watcher
  image: ghcr.io/yourorg/ghostwire-watcher:latest
  securityContext:
    capabilities: { add: ["NET_ADMIN"] }
  env:
  - name: GW_ROLE_LABEL_KEY
    value: "role"
  - name: GW_ROLE_ACTIVE
    value: "active"
  - name: GW_ROLE_PREVIEW
    value: "preview"
  - name: POD_NAME
    valueFrom: { fieldRef: { fieldPath: metadata.name } }
  - name: POD_NAMESPACE
    valueFrom: { fieldRef: { fieldPath: metadata.namespace } }
  volumeMounts:
  - { name: shared, mountPath: /shared }
```

---

## Metrics and Observability
- `/metrics` Prometheus endpoint with counters:
  - `ghostwire_dnat_rules{service}` current rules
  - `ghostwire_jump_state{state}` 0/1
  - `ghostwire_errors_total{type}`
- `/healthz` returns 200 when the watcher has read labels and verified chain presence.

---

## Resource Footprint
- `init`: runs once, exits. < 15 MB image, < 10 ms per service resolve in normal clusters.
- `watcher`: ~8–12 MB RSS. Poll default 2s, configurable.

---

## License
MIT. The iptables you typed along the way are your own responsibility.
