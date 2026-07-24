# convoy

**convoy** is a Kubernetes-native continuous delivery system. You describe *what*
to deploy, *from where*, and *to which cluster* as Kubernetes objects; convoy
fetches the source, renders it, applies it with Server-Side Apply, and
continuously reports whether the live state still matches (drift) and whether the
workloads are healthy.

It is built as an **aggregated API server** plus controllers — so its resources
are real Kubernetes API objects you manage with `kubectl`, RBAC, and admission,
not CRDs or an external control plane.

- [API design docs](docs/design/api/00-overview.md)
- [Architecture](docs/architecture/00-overview.md)
- [Roadmap](ROADMAP.md)

---

## What convoy does

A **`Release`** is the unit of delivery: one application, rendered from one
**source**, applied to one **cluster**. convoy:

1. **fetches** content from a Git repo, Helm registry, or OCI registry;
2. **renders** it (raw manifests, or a Helm chart rendered in-process — no
   `helm install`, no shelling out) with a layered values pipeline;
3. optionally **mutates** the rendered objects (post-render patches / templating);
4. **applies** it to the target cluster via Server-Side Apply, pruning what the
   render no longer contains;
5. **observes** the result — detecting drift and evaluating health — and reports
   it back on the `Release`'s status.

Delivery is push-based today: the control plane connects out to the target
(itself, or a remote cluster via kubeconfig) and applies directly.

---

## API

convoy serves three aggregated API groups. All objects are namespaced unless
noted.

### `core.convoy.kumodo.io/v1alpha1`

- **`Cluster`** — a deploy target (`inCluster` self-management, or `agentless`
  push to a remote cluster). Records reachability + discovered Kubernetes version
  in status. Subresource: `/status`.
- **`Release`** — the delivery unit: `source` + `content` + `clusterRef` +
  policies. Rich subresource surface:

  | Subresource  | Verb    | Purpose                                                               |
  |--------------|---------|-----------------------------------------------------------------------|
  | `/status`    | —       | conditions + render/health status                                     |
  | `/sync`      | POST    | request an immediate reconcile (and apply, in manual mode)            |
  | `/pin`       | PUT     | freeze the current render (hold newer renders until unpinned)         |
  | `/mutations` | GET/PUT | read/replace the post-render mutation pipeline (separately RBAC-able) |
  | `/preview`   | GET     | the rendered manifests that *would* be applied                        |
  | `/diff`      | GET     | structured per-object drift of live vs. desired                       |
  | `/health`    | GET     | per-object health of the managed resources                            |

### `source.convoy.kumodo.io/v1alpha1`

Reusable, credentialed endpoints (the ref/version to fetch lives on the `Release`):

- **`GitRepository`** — branch/tag/commit refs; auth: `none` / `basic` / `token` /
  `sshKey` (with mandatory known-hosts host-key verification).
- **`HelmRegistry`** — classic HTTP Helm repos; auth: `none` / `basic` / `token`.
- **`OCIRegistry`** — Helm charts pulled from OCI registries; auth: `none` /
  `basic` / `token`.

Each has a `/status` subresource reporting reachability.

### `extensions.convoy.kumodo.io/v1alpha1`

- **`GlobalHealthRuleSet`** (cluster-scoped) / **`HealthRuleSet`** (namespaced) —
  CEL-based health rules. A `Release` opts in by selecting rule sets; each rule
  grades a resource kind as healthy/progressing/degraded.

---

## Features (implemented)

**Sources & content**
- Git / Helm-registry / OCI-registry sources, with `none` / `basic` / `token` /
  `sshKey` (git) authentication.
- Content types: **raw manifests** and **Helm** (in-process SDK render, vendored
  dependencies). Layered Helm values from inline / ConfigMap / Secret / Git.

**Delivery**
- `inCluster` and `agentless` (push) connection modes; cluster auth via kubeconfig.
- Server-Side Apply with a stable field manager → continuous drift correction on
  owned fields.
- **Sync policy**: automatic or **manual** mode, re-sync interval, failure
  backoff, and inventory-based **prune** of resources dropped from the render
  (CRDs/Namespaces protected by default).
- **Deletion policy** with finalizers (keep-workloads, block-on-unreachable;
  cascade vs. orphan Releases on Cluster delete).

**Post-render mutations**
- Four types — `jsonPatch`, `strategicMergePatch`, `goTemplate`, `celTransform` —
  each targeting objects by GVK/name.
- **Universal templating** with a `{cluster, self, object}` context; mutation
  content can be inline or sourced from a ConfigMap / Secret / GitRepository, with
  the source name/path itself templatable.
- Runs in the shared render path, so apply / preview / diff / health all see the
  same mutated result.

**Observation & status**
- A dedicated **observer** runs two loops: **drift detection** (→ `InSync`
  condition, with automatic re-sync on drift in automatic mode) and **health
  assessment** (→ `Healthy` condition + health summary).
- Three single-writer status conditions with per-writer field ownership:
  **`Applied`** (controller-manager), **`InSync`** (observer), **`Healthy`**
  (observer) — no ambiguous composite condition.
- On-demand `/preview`, `/diff` (structured), and `/health` served by the observer
  and proxied through the API server.

**Security**
- Same-namespace reference guard: a `Release` may only reference `Cluster`s and
  `Source`s (and mutation sources) in its own namespace, preventing
  credential-borrowing across namespaces.

**Tooling**
- `kubectl convoy` plugin: `release pin | unpin | sync | preview | diff | health |
  mutations (get/set/add)`.
- Components ship as separate binaries: `convoy-apiserver`,
  `convoy-controller-manager`, `convoy-observer`, and the `kubectl-convoy` plugin.
- kind + Tilt dev harness.

---

## Planned

Designed and/or scoped, not yet built:

- **Sync waves & `/lifecycle` hooks** — ordered, health-gated resource rollout
  within a Release; pre/post lifecycle hooks.
- **`Fleet` + `ReleaseTemplate`** — fan-out that generates one `Release` per
  (template × cluster), with cluster-metadata templating, fleet-wide common
  mutations (with conditional `if`), and cross-cluster rollout strategies.
- **More source auth** — native cloud identity / OIDC, and an out-of-process
  credential-provider registry for `execPlugin`-style cluster auth (avoids baking
  third-party auth binaries into convoy and the exec RCE surface).
- **More content types** — `kustomize`, `jsonnet`, Helm dependency `resolve` mode.
- **Durable storage** (unlocks: rollback to a previous render, `/history`,
  historical diff, orphaned-object drift).
- **Agent (pull) mode** — an in-cluster agent for NAT/firewalled/air-gapped
  targets, authenticating with its own ServiceAccount.
- **Cross-namespace references** via an explicit grant model; least-privilege
  apply/observe identities.
- **Operability** — metrics, leader election / HA; webhook-driven source
  triggering; notifications; a read-only web dashboard.

See the [Roadmap](ROADMAP.md) for detail and sequencing.

## Non-goals

Secret management (use External Secrets / Vault), in-cluster progressive delivery
(canary/blue-green — delegated to external tooling), and non-Kubernetes deploy
targets.
