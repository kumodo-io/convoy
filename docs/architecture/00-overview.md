# convoy Architecture

## Motivation

Kubernetes CD tools share a few recurring pain points that convoy is designed around:

- **Helm release state in Secrets** — Helm stores each release's state in an in-cluster Secret, subject to Kubernetes' ~1MB object-size limit; large releases hit the ceiling. Tools that deploy through the Helm SDK (e.g. Flux's helm-controller) inherit this. (Tools that instead render `helm template` and apply the output — e.g. Argo CD — avoid Helm release Secrets, but then manage rendered/live state another way.)
- **Post-render patching is awkward** — applying arbitrary patches (JSON/strategic-merge) or overlays on top of a *rendered* Helm chart is limited or bolted-on (e.g. Flux's `postRenderers`).
- **Lifecycle hooks are chart-bound** — Helm hooks live inside charts as annotations, coupling operational steps (migrations, smoke tests) to chart content; deploying teams can't add hooks to a vendored chart without forking it.
- **Imperative operations don't fit the spec model** — actions like sync, preview, diff, and pin are awkward to express as declarative spec fields or as separate CRDs.

convoy's design responses (see [Implementation Status](#implementation-status) for what's built vs. planned):

- A **composable rendering pipeline** — source, render (`manifests`/`helm` today; kustomize/jsonnet/cue planned), and an ordered **mutation** stage (planned) kept independent.
- **Server-Side Apply** as the deploy mechanism (implemented); `ignoreFields` mapping to SSA field ownership (planned).
- **External (S3) artifact/state storage** (planned) — no Helm release Secrets, with built-in history/versioning.
- **First-class API subresources** for operations — `/pin` and `/sync` implemented; `/preview`, `/diff`, `/lifecycle` planned.
- A move toward **decoupled components** (apiserver / controller-manager / observer / agent-manager / agent); today all controllers run in the controller-manager with render and apply combined in the Release controller.

> **Note:** this document describes the **target architecture**. Much of it is not built yet. See [Implementation Status](#implementation-status) for what exists today; sections describing unbuilt parts are marked _Status: planned_.

---

## Implementation Status

The core delivery path works end to end: Git, Helm, and OCI sources are fetched, rendered, and applied to target clusters across two connection modes, with pruning, deletion policy, and manual/automatic sync.

**Implemented**

- Kubernetes **aggregated API server** serving all seven kinds with the `/status` subresource, plus the `Release` `/pin` and `/sync` subresources.
- `convoy-controller-manager` running five controllers:
  - **Cluster** — connects to a target (`inCluster` self-management, or `agentless` + `kubeconfig`) and records reachability and discovered version in status.
  - **GitRepository**, **HelmRegistry**, **OCIRegistry** — fetch source content (auth `none` only today), resolve the tracked ref/version, and publish to an in-process artifact store.
  - **Release** — renders `manifests` and `helm` (vendored dependencies) content from the source artifact and applies it to the target cluster via Server-Side Apply.
- In-memory artifact store shared between the source and release controllers (lost on restart; repopulated on the next source reconcile). Content-addressed by `(repo URL, revision)` and deduped across Releases.
- **Release sync policy**: `interval` (self-requeue), `prune` (orphan GC with CRD/Namespace protection), `retryBackoff`, and `automatic` (manual mode — apply is withheld until a `/sync` is requested).
- **Deletion policy + finalizers**: `Cluster.deletionPolicy.pruneReleases` and `Release.deletionPolicy.{keepWorkloads,blockOnClusterUnreachable}`, enforced via finalizers.
- `Release` status records the last attempted and last successful render (`status.lastAttempted`/`lastSuccessful`), and reflects the pin in `status.pin`.
- `/pin` (freeze-current-render) and `/sync` (annotation-based reconcile request) subresources; the `kubectl-convoy` plugin exposes `release pin`/`unpin`/`sync`.
- Release field selector (`spec.clusterRef.{name,namespace}`) and a `releaseByCluster` informer index.

**Planned (described below, not built yet)**

- External (S3) state storage, history, and versioning — the artifact store is in-memory only today. This gates render **history**, **rollback**, and historical **diff**; it does **not** gate current-render diff/drift or freeze-pin (see [External State Storage](#external-state-storage)).
- The mutation pipeline, sync waves, and lifecycle hooks.
- The **observer** component (drift-detector + health-assessor loops): current-render drift detection, health-rule evaluation, and the `InSync`/`Healthy` conditions (the rule *types* are served, but nothing evaluates them). It also serves `/preview` and `/diff`.
- The remaining subresources (`/webhooks`, `/mutations`, `/preview`, `/diff`, `/history`, `/lifecycle`).
- The `agent` (pull) connection mode — delivery is control-plane **push** only today.
- Source-fetch auth methods beyond `none` (token/basic/SSH/etc. are defined in the API but return `Unsupported`), and the `helm` `resolve` dependency mode, `kustomize`/`jsonnet`/`cue` content types.
- Rollback (freeze-pin is built), the `Fleet`/`ReleaseTemplate` fleet-orchestration layer, and notifications.

---

## Core Concepts

### Rendering Pipeline

Rendering is treated as a pipeline:

```
Source → Render → Apply mutations → Artifact (stored in S3)
```

Each stage is independent and composable. Multiple source types and mutation mechanisms are supported without coupling them to the deploy mechanism.

Mutation types: `helmValues`, `goTemplate`, `jsonPatch`, `strategicMergePatch`, `celTransform`.

_Status: partially implemented. The Release controller renders `manifests` content to an in-memory artifact; the mutation stage and the other content/source types are planned._

### Server-Side Apply

All resources are applied via Kubernetes Server-Side Apply (SSA). This means:

- Field ownership is tracked by the API server — only managed fields are owned by convoy.
- `ignoreFields` maps directly to SSA field manager semantics.

_Status: implemented (the Release controller applies via SSA with field manager `convoy`). The `ignoreFields` mapping is planned._

### External State Storage

_Status: planned. Today controllers share a single in-memory artifact store — no S3, history, versioning, or filesystem cache. It is lost on restart and repopulated on the next reconcile._

> **What storage does and does not gate.** External storage gates only the features that need a **durable record of past renders**: render **history**, **rollback**, and **diff against a previous/historical render**. It does **not** gate current-render operations — current-render diff/drift (render-on-demand + live read + SSA `dryRun`) and **freeze-pin** (holds the render already produced) are all buildable without it. This is why freeze-pin and the observer's current-render path ship ahead of the storage backend.

Rendered artifacts and deployed state are stored in S3-compatible storage, not in etcd. This provides:

- No Helm release Secrets — no object size limit.
- Built-in history and versioning (S3 object versioning).
- Natural rollback and historical-diff support (freeze-pin and current-diff do not depend on it).
- Multi-cluster state in a single store (namespaced by cluster).

Controllers cache state on local filesystem after writing to S3. On restart, state is hydrated from S3.

**Fetch flow and the artifact store.** Source content is **content-addressed**: the store is keyed by `(normalized repo URL, resolved revision)`, so identical content is held once and shared across Releases — regardless of which `GitRepository` referenced it. Access is gated at consume time by the referencing `GitRepository` being `Ready` (its credentials verified against that URL), so shared content is only reachable through a source that has proven access. Fetching is demand-driven: a `Release` asks a **fetch coordinator** to ensure `(source, ref)` is resolved and present, then reads the artifact by revision; the coordinator resolves the ref, fetches once (deduping concurrent requests), and writes to the store. The coordinator is an interface — in-process today, swappable for an out-of-process transport (gRPC/bus) later without touching the controllers.

### Delivery & Connection Modes

`Cluster.spec.connectionMode` selects how convoy reaches a target:

- **`inCluster`** — convoy manages the cluster it runs in, using its own in-cluster ServiceAccount identity. No endpoint, CA, or credentials are configured.
- **`agentless`** — the control plane connects **out** to a remote cluster's API server using per-cluster credentials (e.g. a `kubeconfig`) and applies workloads directly via Server-Side Apply (push model).
- **`agent`** *(planned)* — an **agent** runs inside the target cluster and **pulls** work from the control plane over outbound HTTPS, applying locally. This suits NAT/firewalled/air-gapped clusters the control plane cannot reach; the control plane (via the agent-manager) serves the rendered content so the agent needs no direct S3 access. See [Target evolution](#target-evolution-planned).

_Status: `inCluster` and `agentless` are implemented as control-plane **push** — the Cluster controller probes the target and the Release controller applies to it. The `agent` (pull) mode — the in-cluster agent, the agent-manager serving surface, and content proxying — is planned. Today all controllers run in one `convoy-controller-manager` binary._

---

## Controllers

Today `convoy-controller-manager` runs **five independent controllers — one per reconciled kind**: `Cluster`, `GitRepository`, `HelmRegistry`, `OCIRegistry`, and `Release`. They are *not* organized as the Source/Render/Executor/Sync pipeline stages an earlier draft described; each is a standalone reconciler on a shared harness. That functional decomposition survives only as the [target evolution](#target-evolution-planned) at the end of this section.

### Controller harness (`base.Controller`)

Every controller embeds a shared `base.Controller`, which owns the generic machinery: a typed work queue with N workers, informer wiring (`WatchInformer`), rate-limited retry, and `EnqueueAfter` for timed self-requeue. A concrete controller supplies only its reconcile function and its dependencies.

The harness's enqueue filter (`shouldEnqueueOnUpdate`) decides which informer updates warrant a reconcile: it enqueues on **resync** (informer re-delivers with an identical resourceVersion), on a **spec change** (`metadata.generation` bumped — status writes don't bump it, thanks to the `/status` subresource), and on a change to a **configured reconcile-request annotation** (opt-in per controller via `WithReconcileRequestAnnotation`). Other status/metadata-only self-updates are skipped, so a controller's own status writes don't re-trigger it. The Release controller opts into the `convoy.kumodo.io/sync-requested-at` annotation so `/sync` wakes it (see [Sync Signaling](#sync-signaling)).

### Cluster controller

Reconciles `Cluster`. Connects to each target per its `connectionMode`/auth and records reachability, the discovered Kubernetes version, and served API versions in status; the other controllers gate on `status.Reachable` before acting on a cluster. Enforces the cluster deletion policy (`deletionPolicy.pruneReleases`) via a finalizer that blocks deletion until referencing Releases are handled.

_Status: implemented for the `inCluster` and `agentless` + `kubeconfig` cases._

### Source controllers — one per source kind

There are **three separate source controllers** — `GitRepository`, `HelmRegistry`, and `OCIRegistry` — not a single "source controller." Each reconciles its own kind: it verifies the source is **reachable and authenticated** and records that in the source's `status`. Crucially, **they do not fetch or store content**. The ref/version to fetch lives on the `Release` (not on the source), so content retrieval is demand-driven by the Release controller's fetch coordinator, not pushed by these controllers on change.

_Status: all three implemented; change detection via periodic resync; auth `none` only (other methods return `Unsupported`). Webhook-driven change detection is planned._

### Fetch coordinator (a library, not a controller)

Content retrieval is factored into a `fetch.Coordinator` that the **Release controller** calls — it is not a controller and does not watch anything. `Ensure(source, namespace)` dispatches on the source type, resolves the ref/version, fetches once (deduping concurrent requests for the same content), and writes to a **content-addressed** in-memory artifact store keyed by `(repo URL, resolved revision)`, so identical content is held once and shared across Releases. The `Coordinator` is an interface — in-process today, swappable for an out-of-process transport (gRPC/bus) later without touching the controllers.

### Release controller

The delivery path. Embedding the harness, it holds the fetch coordinator, a secret resolver (for Helm value sources), the applied-resource inventory, and the cluster deployer. Each reconcile:

1. **Fetch** source content via the coordinator (gating on the referenced source being `Ready`).
2. **Render** — `manifests` or `helm` (vendored dependencies) — and compute a content-hash `renderRef`.
3. **Honor a freeze pin** — if `.pin` targets a render that differs from the current one, hold: set `Ready` reason `Pinned` and apply nothing.
4. **Manual-sync gate** — in `syncPolicy.automatic: false` mode, apply only when a `/sync` is pending; otherwise observe (render, project `status.pin`, update status) without applying.
5. **Apply** via Server-Side Apply (field manager `convoy`) through the cluster connection.
6. **Prune** orphans — resources dropped since the last successful apply, tracked in the in-memory inventory; CRDs and Namespaces are protected unless opted in. Prune failure requeues and leaves the inventory unchanged (fails safe: under-prune, never over-delete).
7. **Record status** — `status.lastAttempted`/`lastSuccessful`, the `Ready` condition and reason, `status.pin`, and `lastHandledSyncAt`.

Deletion is handled via a finalizer honoring `Release.deletionPolicy.{keepWorkloads,blockOnClusterUnreachable}`. Transient failures retry with a per-Release backoff; `syncPolicy.interval` drives the next drift re-check via `EnqueueAfter`.

_Status: implemented for `manifests` and `helm` (vendored deps) content. The mutation pipeline, S3 artifact/state storage, sync-wave ordering, lifecycle hooks, and health evaluation are planned._

### Target evolution *(planned)*

Today all five per-kind controllers run in `convoy-controller-manager`, with render and apply combined in the Release controller. The **target model** separates responsibilities across five binaries. The organizing principle: **rendering is always central; only *apply* and *observe* move by the target cluster's `connectionMode`.**

- **apiserver** — serves the API only (including subresource handlers that proxy to other components).
- **controller-manager** — reconciliation: fetch → render → **store** the rendered artifact (in-memory + persistent). For **push** clusters (`inCluster`/`agentless`) it also **applies + prunes**; for **pull** clusters (`agent`) it stops after storing — the agent applies. It renders for *every* Release regardless of mode.
- **observer** — one binary running two parallel loops that share a live-state client/cache and the target-cluster connection:
  - **drift-detector loop** — compares live managed-resource state against the desired render; on drift, fires `/sync` (the reconcile-request annotation) so the controller-manager re-applies.
  - **health-assessor loop** — evaluates health rules (built-in + `KindHealthRule` + `ResourceHealthRule`) against live resources and aggregates per-resource states into `status.health`.

  Drift and health are **distinct responsibilities** (different logic, inputs, and cadence) that happen to share a live-state reader — colocated for efficiency, not merged. The observer also serves the on-demand `/preview` (render only) and `/diff` (render + compare) that the apiserver proxies to (the `pods/log` → kubelet pattern). It handles **push-mode** clusters centrally.
- **agent-manager** — the control-plane serving surface for pull mode: serves the stored rendered state to agents and ingests their apply/drift/health reports, writing status for **agent-mode** Releases. A serving surface, kept separate from the reconcile surface (mirroring the apiserver/controller-manager split).
- **agent** *(pull mode)* — runs inside a target cluster the control plane cannot reach. Colocates the deployer plus the **same** drift-detector and health-assessor loops as the observer: it pulls the stored rendered state, inventories it in-cluster, applies, observes, and reports back through the agent-manager.

`internal/render` is extracted as a library shared by the controller-manager (render + apply) and the observer/agent (compare); the drift + health loops are shared code between the observer and the agent. The apiserver imports neither.

**Status ownership.** A Release's status is written by several producers on **disjoint field groups**, reconciled with Server-Side Apply and per-writer field managers so no writer clobbers another. `connectionMode` selects which component plays *applier* and *observer*:

| Status field group | push writer | pull writer |
|---|---|---|
| **render** — `renderRef`, resolved source, `lastAttempted`, `pin` | controller-manager | controller-manager |
| **Applied** — `lastSuccessful`, synced | controller-manager | agent-manager |
| **InSync** (drift) | observer (drift loop) | agent-manager |
| **Healthy** (health) | observer (health loop) | agent-manager |

The rule underneath: **`Applied` is owned by whoever applies, `InSync`/`Healthy` by whoever observes, and render is always the controller-manager** — which is exactly the `Applied`/`InSync`/`Healthy` split in [Release State Model](#release-state-model). Adopting SSA-of-own-fields for status (replacing the Release controller's current full-object `UpdateStatus`, which would clobber another writer's fields) is the enabling primitive to build first. A change to a cluster's `connectionMode` hands its Releases' apply/observe ownership between controller-manager and agent-manager, and is treated as a drain/hand-off rather than live migration.

Two further responsibilities are planned within this model:

- **Mutation pipeline + artifact storage** — apply mutations (`helmValues`, `goTemplate`, `jsonPatch`, `strategicMergePatch`, `celTransform`) in order during render, and store the rendered artifact and deployed state to S3 (content-addressed), decoupling render failures from deploy failures and enabling render caching and history.
- **Notification controller** — subscribes to events emitted by the other components and dispatches to destinations (Slack, webhook, PagerDuty, …) per `NotificationConfig`. Components are expected to emit structured events so this can be added without changing them.

_Status: planned. Today there is no separate observer, agent-manager, or agent, no mutation pipeline, no S3 storage, and no notification controller; render and apply are combined in the controller-manager's Release controller, and a Release is (re-)applied on every reconcile (resync heartbeat + spec change), correcting drift opportunistically rather than on observed change._

---

## Sync Signaling

_Status: the `Release` `/sync` subresource and manual sync mode are implemented. Source `/sync` and webhook-driven signaling are planned._

Sync is signaled by a **reconcile-request annotation**, not a separate state machine or internal request objects. The `/sync` subresource (a `rest.Connecter`, POST) stamps `convoy.kumodo.io/sync-requested-at` with the current time. The controller's informer filter wakes on a change to that annotation (in addition to resync and spec/generation changes), so a `/sync` reliably triggers a reconcile even across process boundaries — the apiserver cannot enqueue into the controller's work queue directly, so the request rides on an observed object change.

> _An earlier design used a `Synced`/`NotSynced`/`Syncing`/`Failed` state machine carried on `/sync`, plus internal `SourceSyncRequest`/`ReleaseSyncRequest` objects. That was dropped in favor of the annotation mechanism above._

### Automatic vs manual sync

`Release.spec.syncPolicy.automatic` selects when the controller **applies**:

- **`automatic: true`** (default) — every reconcile renders and applies, correcting drift.
- **`automatic: false`** (manual) — the controller still reconciles and observes (rendering, projecting `status.pin`, updating status) on every trigger, but **withholds the apply** until a `/sync` is pending. A request is *pending* when `sync-requested-at` is newer than `status.lastHandledSyncAt`; on a successful apply the controller records the handled request. With no pending request, an in-sync Release reports `Ready`/`Applied` and an out-of-sync one reports `Ready=False`/`WaitingForSync`.

Manual mode gates only the **apply**, not reconciliation — so status stays fresh without mutating the cluster.

A source change supersedes a pending release drift correction — re-fetching produces a new render that is applied (in automatic mode) or awaits a `/sync` (in manual mode), correcting drift in the process.

---

## Release State Model

A deployed `Release` has three orthogonal state signals:

| Condition | Question answered                                                            |
|-----------|------------------------------------------------------------------------------|
| `Applied` | Did the controller successfully apply the rendered manifests to the cluster? |
| `InSync`  | Does the cluster currently match the rendered manifests?                     |
| `Healthy` | Are the deployed workloads functioning correctly per health rules?           |

These aggregate into a composite `Ready` condition. `Applied` reflects the controller's past action; `InSync` reflects present cluster state; `Healthy` reflects runtime workload state. They are independent — a Release can be `Applied: True`, `InSync: True`, and `Healthy: False` simultaneously.

Each condition has a single owner in the [target model](#target-evolution-planned): `Applied` is written by whoever applied (controller-manager for push, agent-manager for pull), `InSync` by the observer's drift loop (or the agent), and `Healthy` by the observer's health loop (or the agent) — on disjoint status fields via SSA field managers.

_Status: a single `Ready` condition is implemented, carrying reason detail for the current outcome — `Applied`, `Pinned` (a freeze pin is withholding a newer render), `WaitingForSync` (manual mode, apply pending a `/sync`), and the failure reasons. The separate `InSync` (needs drift detection) and `Healthy` (needs health evaluation) conditions are planned._

See [`docs/design/api/core/release.md`](docs/design/api/core/release.md) for the Release design.

---

## Health Checking

_Status: planned. `KindHealthRule` and `ResourceHealthRule` are served by the API server, but nothing evaluates them yet and there is no health loop._

Health assessment runs as the **health-assessor loop** of the [observer](#target-evolution-planned) binary (push mode) or the in-cluster agent (pull mode) — a distinct responsibility from drift detection, colocated with it and sharing a live-state reader but not merged. Two layers, unified under CEL evaluation:

1. **Built-in rules** — predefined CEL expressions for standard Kubernetes resources (Deployment, StatefulSet, DaemonSet, Pod, PVC, Job, etc.). Applied automatically, no configuration required.
2. **`KindHealthRule`** — cluster-scoped, user-defined CEL rules for all resources of a given kind. Overrides built-in rules.
3. **`ResourceHealthRule`** — namespaced, user-defined CEL rules for a specific resource instance identified by `(apiGroup, kind, namespace, name)`. Overrides `KindHealthRule`. For cluster-scoped target resources, `namespace` is empty.

Precedence: `ResourceHealthRule` → `KindHealthRule` → built-in rules → kstatus fallback → `Unknown`.

Health states per resource: `Healthy`, `Progressing`, `Degraded`, `Suspended`, `Unknown`. Release-level health aggregates per-resource states into `Release.status.health`.

See [`docs/architecture/01-health-assessment.md`](docs/architecture/01-health-assessment.md) for the full health assessment design.

---

## Sync Waves

_Status: planned. Wave ordering is not implemented — rendered objects are applied without ordering._

Resources within a Release can be ordered using wave annotations:

```yaml
metadata:
  annotations:
    convoy.kumodo.io/sync-wave: "-10"   # apply early (e.g., CRDs)
    # convoy.kumodo.io/sync-wave: "10"    # apply late (e.g., CRs of the above CRD)
```

Resources within the same wave apply in parallel. Between waves, the Release controller waits based on the configured transition strategy in `Release.spec.syncPolicy.waveStrategy`:

| Strategy                    | Transition condition                               |
|-----------------------------|----------------------------------------------------|
| `waitForApplied`            | All resources accepted by the API server (default) |
| `waitForHealthy`            | All resources reach `Healthy` per health rules     |
| `waitForHealthyWithTimeout` | `waitForHealthy` with a timeout fallback           |

ArgoCD's `argocd.argoproj.io/sync-wave` annotation is recognized for ecosystem compatibility. Helm `helm.sh/hook` annotations are translated into wave behavior automatically.

---

## Lifecycle Hooks

_Status: planned._

Hooks extend the sync pipeline with side-effect operations (migrations, smoke tests, alerts, backups) configured via the `/lifecycle` subresource on each `Release`. They are decoupled from chart content — deploying teams own hooks independently of vendored charts.

Phases:

| Phase        | When                                            |
|--------------|-------------------------------------------------|
| `PreRender`  | Before mutations are applied                    |
| `PostRender` | After rendering, before applying                |
| `PreSync`    | Before any wave applies                         |
| `PostSync`   | After all waves complete                        |
| `SyncFail`   | When sync fails at any point                    |
| `PreDelete`  | Before workloads are pruned on Release deletion |
| `PostDelete` | After workloads are pruned                      |

Hook types: `Job`, `Pod`. Hooks support CEL `when` conditions and per-phase bindings (`.Release`, `.Cluster`, `.Source`, `.Sync`). Helm `helm.sh/hook` annotations are translated into lifecycle entries automatically.

---

## Multi-Cluster

Clusters are registered as `Cluster` resources. `connectionMode` selects how convoy reaches the target — `inCluster` (self), `agentless` (remote, push), or `agent` (remote, pull; planned). In agentless mode the auth method is one of `kubeconfig`, `token`, `clientCert`, `execPlugin`, `oidc`, or `cloudIdentity` (cloud-provider workload identity).

`Cluster.spec.metadata` holds free-form per-cluster metadata (region, tier, cloud, etc.) available in mutation templates and lifecycle hook conditions as `.Cluster.metadata.<key>`.

_Status: `inCluster` and `agentless` + `kubeconfig` auth are implemented. The remaining agentless auth methods, the `agent` mode, and per-cluster S3-namespaced state are planned._

---

## API Design

convoy is implemented as a Kubernetes Aggregation API server. This gives full control over the API surface, enables subresources as first-class operations, and avoids the semantic mismatch of using CRs for imperative actions.

### User-Facing Resources

All resources are backed by etcd via the generic registry, preserving standard watch/informer semantics for controllers.

- `GitRepository` — a reusable, credentialed handle to a Git repo (URL + auth). The ref to fetch (`branch`/`tag`/`commit`/`semver`) is set per `Release` in `spec.source.gitRepository.ref`, so one repo serves many Releases without duplicating credentials.
- `HelmRegistry` — defines a Helm chart registry source. Reusable across releases (the chart + version are selected on the `Release`).
- `OCIRegistry` — defines an OCI registry source. Reusable across releases (the artifact version is selected on the `Release`).
- `KindHealthRule` — cluster-scoped, defines CEL-based health evaluation rules for all resources of a given kind.
- `ResourceHealthRule` — namespaced, defines CEL-based health evaluation rules for a specific resource instance identified by `(apiGroup, kind, namespace, name)`.
- `Release` — the deployment unit. Binds one source + one `Cluster` + a content interpretation (`helm`, `kustomize`, `manifests`, `jsonnet`, `cue`) with an ordered mutation pipeline. The `mutations` field is a top-level field on `Release` (not in `spec`), separately addressable via the `/mutations` subresource for field-level RBAC and audit.
- `Cluster` — registers a target cluster with connection info, auth, and free-form metadata.
- `NotificationConfig` *(future)* — defines notification destinations and event filters.
- `ReleaseTemplate` + `Fleet` *(planned)* — the fleet-orchestration layer. `ReleaseTemplate` is a reusable single-app definition (source + content with `{{ .Cluster.* }}` placeholders, no `clusterRef`); `Fleet` composes a `clusterSelector` × `templateSelector` matrix and generates one owned `Release` per pair via **generation-time templating**, plus fleet-wide policy defaults, common mutations, and a rollout strategy. This supersedes the earlier `ReleaseSet` single-resource idea (rejected: inlining many app specs is unmaintainable). See [`docs/design/api/core/fleet.md`](../design/api/core/fleet.md).

_Status: every resource except `NotificationConfig`, `ReleaseTemplate`, and `Fleet` is served by the API server. `ReleaseTemplate`/`Fleet` are designed but not built. The `mutations` top-level field/subresource is planned; of the content types, `manifests` and `helm` (vendored deps) render today._

### Subresources

_Status: the `Release` `/status`, `/pin`, and `/sync` subresources are served. The rest are planned; `/preview` and `/diff` will proxy to the observer._

| Resource                                       | Subresource  | Verbs                   | Purpose                                                       | Status  |
|------------------------------------------------|--------------|-------------------------|---------------------------------------------------------------|---------|
| `GitRepository`, `HelmRegistry`, `OCIRegistry` | `/sync`      | POST                    | Trigger source fetch + render                                 | planned |
| `GitRepository`, `HelmRegistry`, `OCIRegistry` | `/webhooks`  | GET, PUT, PATCH, DELETE | Manage webhook registrations                                  | planned |
| `Release`                                      | `/mutations` | GET, PUT, PATCH         | Manage the ordered mutation pipeline                          | planned |
| `Release`                                      | `/sync`      | POST                    | Request a reconcile (drift correction; apply, in manual mode) | **built** |
| `Release`                                      | `/pin`       | GET, PUT                | Freeze the current render (PUT empty to unpin)                | **built** |
| `Release`                                      | `/preview`   | GET, POST               | Return rendered manifest set (what would deploy)              | planned |
| `Release`                                      | `/diff`      | GET, POST               | Return diff between rendered and live/pinned/historical state | planned |
| `Release`                                      | `/history`   | GET                     | Paginated list of past render records                         | planned |
| `Release`                                      | `/lifecycle` | GET, PUT, PATCH, DELETE | Manage lifecycle hook configuration                           | planned |

See [Sync Signaling](#sync-signaling) for how `/sync` triggers a reconcile, and [Pin and Rollback](#pin-and-rollback) for the `/pin` semantics.

### Internal Resources

_Status: none. An earlier design proposed `SourceSyncRequest`/`ReleaseSyncRequest` as internal etcd-backed resources for apiserver→controller communication. These were dropped: `/sync` uses the reconcile-request annotation (see [Sync Signaling](#sync-signaling)), and `/preview`/`/diff` will proxy from the apiserver to the observer rather than persisting request objects._

---

## Storage Layout (S3)

_Status: planned. No external storage today; the artifact store is in-memory._

```
{bucket}/
  {cluster-id}/
    rendered/
      {release-name}/
        {content-hash}.yaml       # rendered+mutated artifact
    state/
      {release-name}/
        current.yaml              # last successfully deployed state
        {timestamp}.yaml          # historical versions (via S3 versioning)
```

---

## Pin and Rollback

A `Release` can be pinned via the `/pin` subresource. Two intents:

- `freeze` — pin to the **current** render, so newer renders (from spec/source/values changes) are withheld until unpinned. Used during incidents. **Does not require external storage** — the pin holds the render already produced.
- `rollback` — re-apply a **previous** render. Used to recover from a bad deploy. **Requires external state/history** to reproduce a past render.

`/pin` is GET + PUT: PUT with a `renderRef` pins; PUT with an empty pin unpins. Server-side validation enforces **freeze-current-only** — the `renderRef` must equal `status.lastSuccessful.renderRef`, so pinning a different render (rollback) is rejected until history exists. While a freeze pin holds a render that differs from the current one, the controller withholds the apply and sets the `Ready` condition reason to `Pinned`; the observed pin (intent + render inputs) is projected into `status.pin`. Unpinning resumes normal reconciliation.

_Status: **freeze is implemented** (current-render pin, `/pin` GET/PUT, `status.pin` projection, `Pinned` condition). **Rollback is planned** — it depends on external state/history. This reflects the general rule that storage gates history/rollback, not current-render operations._

---

## RBAC

Access control is delegated to native Kubernetes RBAC. Users define `Role`/`ClusterRole` bindings on convoy resources as they would for any other Kubernetes resource. The `/mutations` subresource is separately addressable, enabling teams to grant mutation-edit rights without full Release edit rights. No custom RBAC layer is introduced.

_Status: native RBAC delegation is implemented — the aggregated API server delegates authz to host-cluster RBAC (`SubjectAccessReview`). The separately-addressable `/mutations` subresource is planned._

---

## Non-Goals

- **Secret management** — sensitive values should come from a secrets manager (External Secrets Operator, Vault, etc.), not stored in Git or S3 unencrypted.
- **Progressive delivery** — canary/blue-green rollouts are out of scope. convoy handles deployment; progressive delivery strategies are delegated to external tooling.
- **Non-Kubernetes workloads** — convoy targets Kubernetes exclusively.
