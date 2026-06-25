# convoy Architecture

## Motivation

Existing CD solutions (Flux, ArgoCD) share fundamental limitations:

- **Helm state stored in Secrets** — Kubernetes has a ~1MB object size limit. Large charts with many manifests hit this ceiling.
- **Patching is awkward** — applying JSON patches or Kustomize overlays on top of a Helm chart is either unsupported (ArgoCD) or buried (Flux `postRenderers`).
- **Inflexible rendering pipeline** — sources, renderers, and mutation mechanisms are tightly coupled.
- **Lifecycle hooks are chart-bound** — hooks (migrations, smoke tests, alerts) live inside charts as annotations. Deploying teams cannot add hooks to vendored charts without forking them.

`convoy` addresses these by decoupling rendering, mutation, deployment, and drift detection into independent controllers, storing state externally (S3), deploying via Server-Side Apply, and exposing operations as first-class API subresources.

> **Note:** this document describes the **target architecture**. Much of it is not built yet. See [Implementation Status](#implementation-status) for what exists today; sections describing unbuilt parts are marked _Status: planned_.

---

## Implementation Status

convoy is at proof-of-concept stage. The core delivery path works end to end: a Git source is fetched, rendered, and applied to target clusters across two connection modes.

**Implemented**

- Kubernetes **aggregated API server** serving all seven kinds with the `/status` subresource.
- `convoy-controller-manager` running three controllers:
  - **Cluster** — connects to a target (`inCluster` self-management, or `agentless` + `kubeconfig`) and records reachability and discovered version in status.
  - **GitRepository** — fetches public repositories (auth `none`), resolves the tracked ref, and publishes the content to an in-process artifact store.
  - **Release** — renders `manifests` content from the source artifact and applies it to the target cluster via Server-Side Apply.
- In-memory artifact store shared between the source and release controllers (lost on restart; repopulated on the next source reconcile).

**Planned (described below, not built yet)**

- External (S3) state storage, history, and versioning — the artifact store is in-memory only today.
- The mutation pipeline, sync waves, and lifecycle hooks.
- Drift detection (Sync controller), the `InSync`/`Healthy` conditions, and health-rule evaluation (the rule *types* are served, but nothing evaluates them).
- All subresources (`/sync`, `/webhooks`, `/mutations`, `/preview`, `/diff`, `/pin`, `/history`, `/lifecycle`) and the internal `*SyncRequest` resources.
- The `agent` (pull) connection mode and a dedicated executor — delivery is control-plane **push** only today.
- `HelmRegistry`/`OCIRegistry` source controllers and the `helm`/`kustomize`/`jsonnet`/`cue` content types (only `manifests` renders).
- Pin/rollback and notifications.

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

Rendered artifacts and deployed state are stored in S3-compatible storage, not in etcd. This provides:

- No Helm release Secrets — no object size limit.
- Built-in history and versioning (S3 object versioning).
- Natural rollback and pin support.
- Multi-cluster state in a single store (namespaced by cluster).

Controllers cache state on local filesystem after writing to S3. On restart, state is hydrated from S3.

### Delivery & Connection Modes

`Cluster.spec.connectionMode` selects how convoy reaches a target:

- **`inCluster`** — convoy manages the cluster it runs in, using its own in-cluster ServiceAccount identity. No endpoint, CA, or credentials are configured.
- **`agentless`** — the control plane connects **out** to a remote cluster's API server using per-cluster credentials (e.g. a `kubeconfig`) and applies workloads directly via Server-Side Apply (push model).
- **`agent`** *(planned)* — an executor runs inside the target cluster and **pulls** work from the control plane over outbound HTTPS, applying locally. This suits NAT/firewalled/air-gapped clusters the control plane cannot reach; the control plane would proxy S3 content so executors need no direct S3 access.

_Status: `inCluster` and `agentless` are implemented as control-plane **push** — the Cluster controller probes the target and the Release controller applies to it. The `agent` (pull) mode, a dedicated executor binary, control-plane S3 proxying, and a separate health loop are planned. Today all controllers run in one `convoy-controller-manager` binary._

---

## Controllers

> _The PoC runs three controllers — **Cluster**, **GitRepository**, and **Release** — in `convoy-controller-manager`. The decomposition below is the target; today **render and apply are combined in the Release controller**, and the Sync and Notification controllers do not exist._

### Cluster Controller

Watches `Cluster` resources. Connects to each target according to its `connectionMode`/auth, then records reachability, the discovered Kubernetes version, and served API versions in the Cluster's status. Other controllers gate on this status before acting on a cluster.

_Status: implemented for the `inCluster` and `agentless` + `kubeconfig` cases._

### Source Controller

Watches `GitRepository`, `HelmRegistry`, and `OCIRegistry` resources. Change detection is via **periodic polling** or **webhooks** (configured via the `/webhooks` subresource on each source).

On change, sets the source's `/sync` subresource state to `NotSynced`.

_Status: the **GitRepository** controller is implemented — change detection via periodic resync; webhooks and the `/sync` subresource are planned. The **HelmRegistry** and **OCIRegistry** controllers are planned._

### Render Controller

Watches source resources and reacts when the `/sync` subresource state transitions to `NotSynced`. Executes the rendering pipeline:

1. Fetch source artifact (Helm chart, Kustomize overlay, raw manifests, etc.)
2. Render to a manifest set
3. Apply mutations in order (helmValues, goTemplate, jsonPatch, strategicMergePatch, celTransform)
4. Store final artifact to S3 (content-addressed)
5. Hand off to Executor

Rendered artifacts are stored separately from deployed state — this decouples render failures from deploy failures and enables render caching.

_Status: planned as a separate controller. Today the Release controller renders inline (manifests only); the `/sync`-driven trigger and mutation pipeline are not implemented._

### Executor

Applies the rendered artifact to the target cluster via SSA. After successful apply:

- Stores deployed state (resource metadata + managed spec) to S3.
- Updates local FS cache.
- Emits events for health checking and notifications.
- Evaluates resource health using built-in rules, `KindHealthRule`, and `ResourceHealthRule` resources.

Applies resources in sync wave order when wave annotations are present. See [Sync Waves](#sync-waves).

Runs lifecycle hooks at defined phases around the sync pipeline. See [Lifecycle Hooks](#lifecycle-hooks).

_Status: the apply step is implemented in the Release controller (Server-Side Apply). S3 state storage, event emission, health evaluation, sync-wave ordering, and lifecycle hooks are planned._

### Sync Controller

Periodically polls the target cluster for managed resources. Compares live cluster state against local FS cache (falling back to S3 if cache is cold).

Diff is computed only over **managed fields** — fields not owned by convoy and fields listed in `ignoreFields` are excluded. Live cluster noise (`resourceVersion`, `uid`, `managedFields`, etc.) is stripped before diffing.

On drift detection, sets the `Release /sync` subresource state to `NotSynced`. The Executor reacts and re-applies the stored rendered artifact without re-rendering.

_Status: planned. No drift detection today — a Release is (re-)applied on resync, not on observed drift._

### Notification Controller *(future)*

Subscribes to events emitted by all other controllers. Sends notifications to configured destinations (Slack, webhook, PagerDuty, etc.) based on `NotificationConfig` resources.

All controllers emit structured events from day one so that plugging in the notification controller requires no changes to existing controllers.

---

## Sync States

_Status: planned. No `/sync` subresources or sync-state machine exist yet._

Sync signaling is state-based, carried on subresources rather than separate objects. Both source resources and `Release` expose a `/sync` subresource with the following states:

| State       | Meaning                                        |
|-------------|------------------------------------------------|
| `Synced`    | Last sync completed successfully               |
| `NotSynced` | Sync required — set by controllers or user     |
| `Syncing`   | Sync in progress                               |
| `Failed`    | Last sync failed — includes reason and message |

**Source `/sync`** is set to `NotSynced` by the Source Controller on detected change, or manually to force re-fetch and re-render.

**Release `/sync`** is set to `NotSynced` by the Sync Controller on detected drift, or manually to force drift correction. Re-applies the stored rendered artifact without re-rendering.

A source sync supersedes a pending release drift sync — a source sync produces a new artifact and deploys it, correcting drift in the process.

---

## Release State Model

A deployed `Release` has three orthogonal state signals:

| Condition | Question answered                                                            |
|-----------|------------------------------------------------------------------------------|
| `Applied` | Did the controller successfully apply the rendered manifests to the cluster? |
| `InSync`  | Does the cluster currently match the rendered manifests?                     |
| `Healthy` | Are the deployed workloads functioning correctly per health rules?           |

These aggregate into a composite `Ready` condition. `Applied` reflects the controller's past action; `InSync` reflects present cluster state; `Healthy` reflects runtime workload state. They are independent — a Release can be `Applied: True`, `InSync: True`, and `Healthy: False` simultaneously.

_Status: only `Applied`/`Ready` are implemented — the Release controller sets a single `Ready` condition on a successful apply. `InSync` (needs drift detection) and `Healthy` (needs health evaluation) are planned._

See [`docs/design/api/core/release.md`](docs/design/api/core/release.md) for the Release design.

---

## Health Checking

_Status: planned. `KindHealthRule` and `ResourceHealthRule` are served by the API server, but nothing evaluates them yet and there is no health loop._

Health assessment runs inside the executor alongside the deploy loop. Two layers, unified under CEL evaluation:

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

Resources within the same wave apply in parallel. Between waves, the Executor waits based on the configured transition strategy in `Release.spec.syncPolicy.waveStrategy`:

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

- `GitRepository` — defines a Git repository source. Reusable across releases.
- `HelmRegistry` — defines a Helm chart registry source. Reusable across releases.
- `OCIRegistry` — defines an OCI registry source. Reusable across releases.
- `KindHealthRule` — cluster-scoped, defines CEL-based health evaluation rules for all resources of a given kind.
- `ResourceHealthRule` — namespaced, defines CEL-based health evaluation rules for a specific resource instance identified by `(apiGroup, kind, namespace, name)`.
- `Release` — the deployment unit. Binds one source + one `Cluster` + a content interpretation (`helm`, `kustomize`, `manifests`, `jsonnet`, `cue`) with an ordered mutation pipeline. The `mutations` field is a top-level field on `Release` (not in `spec`), separately addressable via the `/mutations` subresource for field-level RBAC and audit.
- `Cluster` — registers a target cluster with connection info, auth, and free-form metadata.
- `NotificationConfig` *(future)* — defines notification destinations and event filters.
- `ReleaseSet` *(future)* — higher-level resource generating per-cluster `Release` objects from a template and cluster selector.

_Status: every resource except `NotificationConfig` and `ReleaseSet` is served by the API server. The `mutations` top-level field/subresource is planned; of the content types, only `manifests` renders today._

### Subresources

_Status: planned. Only the standard `/status` subresource is served today; none of the below are implemented._

| Resource                                       | Subresource  | Verbs                   | Purpose                                                       |
|------------------------------------------------|--------------|-------------------------|---------------------------------------------------------------|
| `GitRepository`, `HelmRegistry`, `OCIRegistry` | `/sync`      | POST                    | Trigger source fetch + render                                 |
| `GitRepository`, `HelmRegistry`, `OCIRegistry` | `/webhooks`  | GET, PUT, PATCH, DELETE | Manage webhook registrations                                  |
| `Release`                                      | `/mutations` | GET, PUT, PATCH         | Manage the ordered mutation pipeline                          |
| `Release`                                      | `/sync`      | POST                    | Trigger drift correction                                      |
| `Release`                                      | `/preview`   | GET, POST               | Return rendered manifest set (what would deploy)              |
| `Release`                                      | `/diff`      | GET, POST               | Return diff between rendered and live/pinned/historical state |
| `Release`                                      | `/pin`       | GET, POST, DELETE       | Freeze or roll back to a specific render                      |
| `Release`                                      | `/history`   | GET                     | Paginated list of past render records                         |
| `Release`                                      | `/lifecycle` | GET, PUT, PATCH, DELETE | Manage lifecycle hook configuration                           |

See [Sync States](#sync-states) for the state machine carried by `/sync` subresources.

### Internal Resources

_Status: planned._

`SourceSyncRequest` and `ReleaseSyncRequest` exist as internal AA resources (etcd-backed, not user-facing) used for communication between the API server subresource handlers and controllers. Controllers watch them via standard informers.

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

_Status: planned (depends on external state/history)._

A `Release` can be pinned to a specific historical render via the `/pin` subresource. Two intents:

- `freeze` — stop applying spec/mutation changes until unpinned. Used during incidents.
- `rollback` — re-apply a previous render. Used to recover from a bad deploy.

While pinned, spec and mutation changes are accepted but not applied. A `SpecDriftFromPin` condition is set to warn the user. Unpinning (DELETE `/pin`) resumes normal reconciliation, applying any pending changes.

---

## RBAC

Access control is delegated to native Kubernetes RBAC. Users define `Role`/`ClusterRole` bindings on convoy resources as they would for any other Kubernetes resource. The `/mutations` subresource is separately addressable, enabling teams to grant mutation-edit rights without full Release edit rights. No custom RBAC layer is introduced.

_Status: native RBAC delegation is implemented — the aggregated API server delegates authz to host-cluster RBAC (`SubjectAccessReview`). The separately-addressable `/mutations` subresource is planned._

---

## Non-Goals

- **Secret management** — sensitive values should come from a secrets manager (External Secrets Operator, Vault, etc.), not stored in Git or S3 unencrypted.
- **Progressive delivery** — canary/blue-green rollouts are out of scope. convoy handles deployment; progressive delivery strategies are delegated to external tooling.
- **Non-Kubernetes workloads** — convoy targets Kubernetes exclusively.
