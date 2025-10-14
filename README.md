# ghostwire

Invisible in-cluster traffic switcher for Blue/Green & Canary rollouts.

`ghostwire` makes pods labeled as “preview” route to matching preview services (like `*-preview`) instead of the active ones. It does this at L4 with DNAT rules. No app code changes, no mesh dependency, no DNS roulette. You choose the labels, patterns, and behavior.

## Why
- Preview pods automatically talk to preview backends.
- If a preview isn’t present, traffic falls back to active.
- No restarts during promotions; Argo Rollouts flips labels, `ghostwire` flips routing.
- All the knobs you’ll ask for later are here now.

## Components
- **`init`**: discovers Service IPs and builds an iptables NAT chain mapping “active IP:port → preview IP:port” for services where a real preview exists.
- **`watcher`**: polls its own Pod labels and toggles a single jump into that chain when the pod’s role becomes preview.
- **`injector`**: mutating admission webhook that injects the init and watcher based on annotations. Optional, but saves your wrists.

Language: **Go**. Single static binaries. Tiny images. Fewer surprises.

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
    ghostwire.dev/services: "orders:443,users:8080"  # name:port[,name:port...]
    ghostwire.dev/roleLabelKey: "role"               # label key to read on Pod
    ghostwire.dev/roleActive: "active"               # value that disables jump
    ghostwire.dev/rolePreview: "preview"             # value that enables jump
    ghostwire.dev/svcActivePattern: "{{name}}-active"
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
| `GW_NAMESPACE` | Pod namespace | Namespace for service discovery |
| `GW_SERVICES` | required | Comma list `name:port` |
| `GW_ROLE_LABEL_KEY` | `role` | Pod label key to read |
| `GW_ROLE_ACTIVE` | `active` | “Active” value |
| `GW_ROLE_PREVIEW` | `preview` | “Preview” value |
| `GW_SVC_ACTIVE_PATTERN` | `{{name}}-active` | Go-template service name |
| `GW_SVC_PREVIEW_PATTERN` | `{{name}}-preview` | Go-template service name |
| `GW_DNS_SUFFIX` | `.svc.cluster.local` | Cluster DNS suffix |
| `GW_NAT_CHAIN` | `CANARY_DNAT` | iptables chain name |
| `GW_JUMP_HOOK` | `OUTPUT` | `OUTPUT` or `PREROUTING` |
| `GW_EXCLUDE_CIDRS` | IMDS, DNS | CSV of CIDRs to skip |
| `GW_POLL_INTERVAL` | `2s` | Watcher poll cadence |
| `GW_REFRESH_INTERVAL` | empty | If set, periodic rebuild of DNAT |
| `GW_IPV6` | `false` | Add ip6tables rules |
| `GW_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |

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
    ghostwire.dev/services: "orders:443,ledger:443"
```

That’s it. No app changes. No service mesh hand-holding.

---

## Security
- Pods need `NET_ADMIN` to program iptables. Yes, that’s spicy. Scope the ServiceAccount per workload and bind only `get` on its own Pod:
  - Role: `resources: ["pods"], verbs: ["get"]`
  - Optionally template `resourceNames: ["$(POD_NAME)"]`
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
  - Resolves `<svcActive>` and `<svcPreview>` FQDNs and builds DNAT rules only when the preview ClusterIP is distinct.
  - Writes a small `/shared/dnat.map` for audit/debug.
  - Exits without enabling the chain.
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
  - { name: GW_SERVICES, value: "orders:443,users:8080" }
  - { name: GW_SVC_ACTIVE_PATTERN, value: "{{name}}-active" }
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
