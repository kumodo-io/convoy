# kumo-cd Architecture

## Motivation

Existing CD solutions (Flux, ArgoCD) share fundamental limitations:

- **Helm state stored in Secrets** — Kubernetes has a ~1MB object size limit. Large charts with many manifests hit this ceiling.
- **Patching is awkward** — applying JSON patches or Kustomize overlays on top of a Helm chart is either unsupported (ArgoCD) or buried (Flux `postRenderers`).
- **Inflexible rendering pipeline** — sources, renderers, and mutation mechanisms are tightly coupled.
- **Lifecycle hooks are chart-bound** — hooks (migrations, smoke tests, alerts) live inside charts as annotations. Deploying teams cannot add hooks to vendored charts without forking them.

kumo-cd addresses these by decoupling rendering, mutation, deployment, and drift detection into independent controllers, storing state externally (S3), deploying via Server-Side Apply, and exposing operations as first-class API subresources.

---

## Core Concepts

### Rendering Pipeline

Rendering is treated as a pipeline:

```
Source → Render → Apply mutations → Artifact (stored in S3)
```

Each stage is independent and composable. Multiple source types and mutation mechanisms are supported without coupling them to the deploy mechanism.

Mutation types: `helmValues`, `goTemplate`, `jsonPatch`, `strategicMergePatch`, `celTransform`.

### Server-Side Apply

All resources are applied via Kubernetes Server-Side Apply (SSA). This means:

- No Helm release Secrets — no object size limit.
- Field ownership is tracked by the API server — only managed fields are owned by kumo-cd.
- `ignoreFields` maps directly to SSA field manager semantics.

### External State Storage

Rendered artifacts and deployed state are stored in S3-compatible storage, not in etcd. This provides:

- No size constraints.
- Built-in history and versioning (S3 object versioning).
- Natural rollback and pin support.
- Multi-cluster state in a single store (namespaced by cluster).

Controllers cache state on local filesystem after writing to S3. On restart, state is hydrated from S3.

### Executor

A single `executor` binary handles deployment and health assessment for target clusters. It runs in two topologies determined by `Cluster.spec.connectionMode`:

- **Agentless** — a central deployment handles many clusters using per-cluster credentials provided by the control plane.
- **Agent** — the executor runs inside the target cluster using in-cluster service account credentials.

Both modes run the same binary; only configuration differs. The executor runs two parallel internal loops: a **deploy loop** and a **health loop**.

Communication is **agent-initiated PULL**: the executor makes outbound HTTPS requests to the control plane. The control plane never opens connections to executors. This works in any network topology — NAT, firewalls, air-gapped clusters.

The control plane **proxies S3 content** to executors. Executors never access S3 directly, avoiding cross-cloud authentication complexity and restrictive egress policies.

See [`docs/architecture/health-assessment.md`](docs/architecture/health-assessment.md) for executor architecture details.

---

## Controllers

### Source Controller

Watches `GitSource`, `HelmRegistry`, and `OCIRegistry` resources. Change detection is via **periodic polling** or **webhooks** (configured via the `/webhooks` subresource on each source).

On change, sets the source's `/sync` subresource state to `NotSynced`.

### Render Controller

Watches source resources and reacts when the `/sync` subresource state transitions to `NotSynced`. Executes the rendering pipeline:

1. Fetch source artifact (Helm chart, Kustomize overlay, raw manifests, etc.)
2. Render to a manifest set
3. Apply mutations in order (helmValues, goTemplate, jsonPatch, strategicMergePatch, celTransform)
4. Store final artifact to S3 (content-addressed)
5. Hand off to Executor

Rendered artifacts are stored separately from deployed state — this decouples render failures from deploy failures and enables render caching.

### Executor

Applies the rendered artifact to the target cluster via SSA. After successful apply:

- Stores deployed state (resource metadata + managed spec) to S3.
- Updates local FS cache.
- Emits events for health checking and notifications.
- Evaluates resource health using built-in rules and `HealthRule` resources.

Applies resources in sync wave order when wave annotations are present. See [Sync Waves](#sync-waves).

Runs lifecycle hooks at defined phases around the sync pipeline. See [Lifecycle Hooks](#lifecycle-hooks).

### Sync Controller

Periodically polls the target cluster for managed resources. Compares live cluster state against local FS cache (falling back to S3 if cache is cold).

Diff is computed only over **managed fields** — fields not owned by kumo-cd and fields listed in `ignoreFields` are excluded. Live cluster noise (`resourceVersion`, `uid`, `managedFields`, etc.) is stripped before diffing.

On drift detection, sets the `Release /sync` subresource state to `NotSynced`. The Executor reacts and re-applies the stored rendered artifact without re-rendering.

### Notification Controller *(future)*

Subscribes to events emitted by all other controllers. Sends notifications to configured destinations (Slack, webhook, PagerDuty, etc.) based on `NotificationConfig` resources.

All controllers emit structured events from day one so that plugging in the notification controller requires no changes to existing controllers.

---

## Sync States

Sync signaling is state-based, carried on subresources rather than separate objects. Both source resources and `Release` expose a `/sync` subresource with the following states:

| State | Meaning |
|---|---|
| `Synced` | Last sync completed successfully |
| `NotSynced` | Sync required — set by controllers or user |
| `Syncing` | Sync in progress |
| `Failed` | Last sync failed — includes reason and message |

**Source `/sync`** is set to `NotSynced` by the Source Controller on detected change, or manually to force re-fetch and re-render.

**Release `/sync`** is set to `NotSynced` by the Sync Controller on detected drift, or manually to force drift correction. Re-applies the stored rendered artifact without re-rendering.

A source sync supersedes a pending release drift sync — a source sync produces a new artifact and deploys it, correcting drift in the process.

---

## Release State Model

A deployed `Release` has three orthogonal state signals:

| Condition | Question answered |
|---|---|
| `Applied` | Did the controller successfully apply the rendered manifests to the cluster? |
| `InSync` | Does the cluster currently match the rendered manifests? |
| `Healthy` | Are the deployed workloads functioning correctly per health rules? |

These aggregate into a composite `Ready` condition. `Applied` reflects the controller's past action; `InSync` reflects present cluster state; `Healthy` reflects runtime workload state. They are independent — a Release can be `Applied: True`, `InSync: True`, and `Healthy: False` simultaneously.

See [`docs/design/api-types.md`](docs/design/api-types.md) for the full state model.

---

## Health Checking

Health assessment runs inside the executor alongside the deploy loop. Two layers, unified under CEL evaluation:

1. **Built-in rules** — predefined CEL expressions for standard Kubernetes resources (Deployment, StatefulSet, DaemonSet, Pod, PVC, Job, etc.). Applied automatically, no configuration required.
2. **`HealthRule` resources** — user-defined CEL expressions for non-standard or custom resources. Defined as standalone `HealthRule` objects referenced from a `Release` or applied cluster-wide.

Users can override built-in rules per resource kind. Health states per resource: `Healthy`, `Progressing`, `Degraded`, `Suspended`, `Unknown`. Release-level health aggregates per-resource states into `Release.status.health`.

See [`docs/design/api-types.md`](docs/design/api-types.md) for the full health assessment design including `HealthRule` schema and aggregation rules.

---

## Sync Waves

Resources within a Release can be ordered using wave annotations:

```yaml
metadata:
  annotations:
    cd.kumodo.io/sync-wave: "-10"   # apply early (e.g., CRDs)
    cd.kumodo.io/sync-wave: "10"    # apply late (e.g., CRs of the above CRD)
```

Resources within the same wave apply in parallel. Between waves, the Executor waits based on the configured transition strategy in `Release.spec.syncPolicy.waveStrategy`:

| Strategy | Transition condition |
|---|---|
| `waitForApplied` | All resources accepted by the API server (default) |
| `waitForHealthy` | All resources reach `Healthy` per health rules |
| `waitForHealthyWithTimeout` | `waitForHealthy` with a timeout fallback |

ArgoCD's `argocd.argoproj.io/sync-wave` annotation is recognized for ecosystem compatibility. Helm `helm.sh/hook` annotations are translated into wave behavior automatically.

See [`docs/design/sync-ordering.md`](docs/design/sync-ordering.md) for the full design.

---

## Lifecycle Hooks

Hooks extend the sync pipeline with side-effect operations (migrations, smoke tests, alerts, backups) configured via the `/lifecycle` subresource on each `Release`. They are decoupled from chart content — deploying teams own hooks independently of vendored charts.

Phases:

| Phase | When |
|---|---|
| `PreRender` | Before mutations are applied |
| `PostRender` | After rendering, before applying |
| `PreSync` | Before any wave applies |
| `PostSync` | After all waves complete |
| `SyncFail` | When sync fails at any point |
| `PreDelete` | Before workloads are pruned on Release deletion |
| `PostDelete` | After workloads are pruned |

Hook types: `Job`, `Pod`. Hooks support CEL `when` conditions and per-phase bindings (`.Release`, `.Cluster`, `.Source`, `.Sync`). Helm `helm.sh/hook` annotations are translated into lifecycle entries automatically.

See [`docs/design/release-lifecycle.md`](docs/design/release-lifecycle.md) for the full design.

---

## Multi-Cluster

Clusters are registered as `Cluster` resources. Auth is pluggable: `kubeconfig`, `token`, `clientCert`, `execPlugin`, `workloadIdentity`, `oidcClientCredentials`.

`Cluster.spec.metadata` holds free-form per-cluster metadata (region, tier, cloud, etc.) available in mutation templates and lifecycle hook conditions as `.Cluster.metadata.<key>`.

S3 state is namespaced per cluster, enabling a single control plane to manage many clusters.

---

## API Design

kumo-cd is implemented as a Kubernetes Aggregation API server. This gives full control over the API surface, enables subresources as first-class operations, and avoids the semantic mismatch of using CRs for imperative actions.

### User-Facing Resources

All resources are backed by etcd via the generic registry, preserving standard watch/informer semantics for controllers.

- `GitSource` — defines a Git repository source. Reusable across releases.
- `HelmRegistry` — defines a Helm chart registry source. Reusable across releases.
- `OCIRegistry` — defines an OCI registry source. Reusable across releases.
- `HealthRule` — defines CEL-based health evaluation rules for a resource kind.
- `Release` — the deployment unit. Binds one source + one `Cluster` + a content interpretation (`helm`, `kustomize`, `manifests`, `jsonnet`, `cue`) with an ordered mutation pipeline. The `mutations` field is a top-level field on `Release` (not in `spec`), separately addressable via the `/mutations` subresource for field-level RBAC and audit.
- `Cluster` — registers a target cluster with connection info, auth, and free-form metadata.
- `NotificationConfig` *(future)* — defines notification destinations and event filters.
- `ReleaseSet` *(future)* — higher-level resource generating per-cluster `Release` objects from a template and cluster selector.

### Subresources

| Resource | Subresource | Verbs | Purpose |
|---|---|---|---|
| `GitSource`, `HelmRegistry`, `OCIRegistry` | `/sync` | POST | Trigger source fetch + render |
| `GitSource`, `HelmRegistry`, `OCIRegistry` | `/webhooks` | GET, PUT, PATCH, DELETE | Manage webhook registrations |
| `Release` | `/mutations` | GET, PUT, PATCH | Manage the ordered mutation pipeline |
| `Release` | `/sync` | POST | Trigger drift correction |
| `Release` | `/preview` | GET, POST | Return rendered manifest set (what would deploy) |
| `Release` | `/diff` | GET, POST | Return diff between rendered and live/pinned/historical state |
| `Release` | `/pin` | GET, POST, DELETE | Freeze or roll back to a specific render |
| `Release` | `/history` | GET | Paginated list of past render records |
| `Release` | `/lifecycle` | GET, PUT, PATCH, DELETE | Manage lifecycle hook configuration |

See [Sync States](#sync-states) for the state machine carried by `/sync` subresources.
See [`docs/design/preview-and-diff.md`](docs/design/preview-and-diff.md) for `/preview` and `/diff` design.
See [`docs/design/release-lifecycle.md`](docs/design/release-lifecycle.md) for `/lifecycle` design.

### Internal Resources

`SourceSyncRequest` and `ReleaseSyncRequest` exist as internal AA resources (etcd-backed, not user-facing) used for communication between the API server subresource handlers and controllers. Controllers watch them via standard informers.

---

## Storage Layout (S3)

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

A `Release` can be pinned to a specific historical render via the `/pin` subresource. Two intents:

- `freeze` — stop applying spec/mutation changes until unpinned. Used during incidents.
- `rollback` — re-apply a previous render. Used to recover from a bad deploy.

While pinned, spec and mutation changes are accepted but not applied. A `SpecDriftFromPin` condition is set to warn the user. Unpinning (DELETE `/pin`) resumes normal reconciliation, applying any pending changes.

---

## RBAC

Access control is delegated to native Kubernetes RBAC. Users define `Role`/`ClusterRole` bindings on kumo-cd resources as they would for any other Kubernetes resource. The `/mutations` subresource is separately addressable, enabling teams to grant mutation-edit rights without full Release edit rights. No custom RBAC layer is introduced.

---

## Non-Goals

- **Secret management** — sensitive values should come from a secrets manager (External Secrets Operator, Vault, etc.), not stored in Git or S3 unencrypted.
- **Progressive delivery** — canary/blue-green rollouts are out of scope. kumo-cd handles deployment; progressive delivery strategies are delegated to external tooling.
- **Non-Kubernetes workloads** — kumo-cd targets Kubernetes exclusively.
