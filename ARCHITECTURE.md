# kumo-cd Architecture

## Motivation

Existing CD solutions (Flux, ArgoCD) share fundamental limitations:

- **Helm state stored in Secrets** — Kubernetes has a ~1MB object size limit. Large charts with many manifests hit this ceiling.
- **Patching is awkward** — applying JSON patches or Kustomize overlays on top of a Helm chart is either unsupported (ArgoCD) or buried (Flux `postRenderers`).
- **Inflexible rendering pipeline** — sources, renderers, and patch mechanisms are tightly coupled.

kumo-cd addresses these by decoupling rendering, patching, deployment, and drift detection into independent controllers, storing state externally (S3), and deploying via Server-Side Apply.

---

## Core Concepts

### Rendering Pipeline

Rendering is treated as a pipeline:

```
Source → Render → Template patches → Apply patches → Artifact (stored in S3)
```

Each stage is independent and composable. Multiple source types and patch mechanisms are supported without coupling them to the deploy mechanism.

### Server-Side Apply

All resources are applied via Kubernetes Server-Side Apply (SSA). This means:

- No Helm release Secrets — no object size limit.
- Field ownership is tracked by the API server — only managed fields are owned by kumo-cd.
- `ignoreFields` maps directly to SSA field manager semantics.

### External State Storage

Deployed state (metadata + spec of applied resources) is stored in S3-compatible storage, not in etcd. This provides:

- No size constraints.
- Built-in history and versioning (S3 object versioning).
- Natural rollback support.
- Multi-cluster state in a single store (namespaced by cluster).

Controllers cache state on local filesystem after writing to S3. On restart, state is hydrated from S3. This reduces S3 read traffic during normal operation.

---

## Controllers

### Source Controller

Watches for changes in external sources:

- Git repositories
- Helm/OCI registries
- Raw manifest sources

Change detection is via **periodic polling** or **webhooks** where the source supports them.

On change, sets the `Source /sync` subresource state to `NotSynced`.

### Render Controller

Watches `Source` resources and reacts when the `/sync` subresource state transitions to `NotSynced`. Executes the rendering pipeline:

1. Fetch source artifact (Helm chart, raw manifests, Kustomization, etc.)
2. Render to a manifest set
3. Execute Go template rendering on referenced `Patch` objects (inject cluster, app, and custom values)
4. Apply rendered patches (JSON patches, strategic merge patches, Kustomize overlays)
5. Store final artifact to S3 (content-addressed)
6. Hand off to Deploy Controller

Rendered artifacts are stored separately from deployed state — this decouples render failures from deploy failures and enables render caching.

### Deploy Controller

Applies the rendered artifact to the target cluster via SSA. After successful apply:

- Stores deployed state (resource metadata + managed spec) to S3.
- Updates local FS cache.
- Emits events for health checking and notifications.

Evaluates resource health after deploy using the configured health rules (see [Health Checking](#health-checking)).

### Sync Controller

Periodically polls the target cluster for managed resources. Compares live cluster state against local FS cache (falling back to S3 if cache is cold).

Diff is computed only over **rendered fields** — fields listed in `ignoreFields` are excluded. Live cluster noise (`resourceVersion`, `uid`, `managedFields`, controller-set annotations) is stripped before diffing.

On drift detection, sets the `Application /sync` subresource state to `NotSynced`. The Deploy Controller watches `Application` resources and reacts to this state — **skipping re-rendering** and re-applying the stored rendered artifact from local FS (or S3 if cache is cold) directly.

### Notification Controller *(future)*

Subscribes to events emitted by all other controllers. Sends notifications to configured destinations (Slack, webhook, PagerDuty, etc.) based on `NotificationConfig` resources.

All controllers emit structured events from day one so that plugging in the notification controller requires no changes to existing controllers.

---

## Sync States

Sync signaling is state-based, carried on subresources rather than separate objects. Both `Source` and `Application` expose a `/sync` subresource with the following states:

| State | Meaning |
|---|---|
| `Synced` | Last sync completed successfully |
| `NotSynced` | Sync required — set by controllers or user |
| `Syncing` | Sync in progress |
| `Failed` | Last sync failed — includes reason and message |

**Source `/sync`** is set to `NotSynced` by the Source Controller on detected change, or by the user to force a re-fetch and re-render. The Render Controller reacts and sets it to `Syncing` then `Synced` (or `Failed`).

**Application `/sync`** is set to `NotSynced` by the Sync Controller on detected drift, or by the user to force drift correction. The Deploy Controller reacts and re-applies the stored rendered artifact without re-rendering.

A source sync supersedes a pending application sync for the same application — a source sync produces a new artifact and deploys it, which also corrects any drift.

---

## Health Checking

Two layers, unified under CEL evaluation:

1. **Built-in rules** — predefined CEL expressions for standard Kubernetes resources (Deployment, StatefulSet, DaemonSet, Pod, PVC, Job, etc.). Applied automatically, no user configuration required.
2. **Custom rules** — user-defined CEL expressions for non-standard or custom resources (CRDs). Defined per resource kind in the `Application` spec.

Users can override built-in rules per resource kind if the defaults are not suitable.

A deploy is not marked successful until all managed resources pass their health rules.

---

## Multi-Cluster

Clusters are registered by creating a resource containing credentials to access the target cluster. Auth is pluggable:

- **Kubeconfig** — static credentials stored in a Secret.
- **Workload Identity** — IRSA (AWS), Workload Identity (GKE), or equivalent. Preferred for cloud environments.
- Additional providers can be added via the pluggable auth interface.

S3 state is namespaced per cluster, enabling a single control plane to manage many clusters.

---

## API Design

kumo-cd is implemented as a Kubernetes Aggregation API server. This gives full control over the API surface, enables subresources as first-class operations, and avoids the semantic mismatch of using CRs for imperative actions.

### User-Facing Resources

All resources are backed by etcd via the generic registry, preserving standard watch/informer semantics for controllers.

- `Source` — defines a source artifact: Git repository, Helm/OCI chart, raw manifests, etc. Reusable across applications.
- `Patch` — defines a patch set: JSON patches, strategic merge patches, Kustomize overlays. Supports Go template syntax for dynamic values (see [Patch Templating](#patch-templating)). Reusable across applications.
- `Application` — composes a reference to a `Source`, an ordered list of `Patch` references with optional per-reference values, and a target `ClusterRegistration`. Also defines sync policy, health rules, and ignore fields.
- `ClusterRegistration` — registers a target cluster with its auth configuration.
- `NotificationConfig` *(future)* — defines notification destinations and event filters.

Separating `Source` and `Patch` from `Application` allows them to be shared across multiple applications — a common Helm chart source or a standard labeling patch can be defined once and referenced many times.

### Subresources

Sync signaling and imperative operations are exposed as subresources. Controllers watch the parent resources and react to subresource state changes — no separate internal objects required.

| Resource | Subresource | Method | Purpose |
|---|---|---|---|
| `Source` | `/sync` | POST | Trigger source fetch + render |
| `Application` | `/sync` | POST | Trigger drift correction |
| `Application` | `/diff` | GET | Return live computed diff |
| `Application` | `/rollback` | POST | Roll back to a previous artifact version |

See [Sync States](#sync-states) for the state machine carried by `/sync` subresources.

---

## Storage Layout (S3)

```
{bucket}/
  {cluster-id}/
    rendered/
      {app-name}/
        {content-hash}.yaml       # rendered+patched artifact
    state/
      {app-name}/
        current.yaml              # last successfully deployed state
        {timestamp}.yaml          # historical versions (via S3 versioning)
```

---

## Patch Templating

`Patch` resources support Go template syntax in patch values. Before patches are applied, the Render Controller executes template rendering with a well-defined context:

| Variable | Source |
|---|---|
| `.Cluster.*` | `ClusterRegistration` metadata (name, environment, region, labels, etc.) |
| `.App.*` | `Application` metadata (name, namespace, labels, etc.) |
| `.Values.*` | Custom values passed per-patch-reference in the `Application` spec |

Example — a shared labeling patch reused across clusters:

```yaml
kind: Patch
metadata:
  name: standard-labels
spec:
  patches:
    - target:
        kind: Deployment
      patch: |
        - op: add
          path: /metadata/labels/cluster
          value: {{ .Cluster.Name }}
        - op: add
          path: /metadata/labels/team
          value: {{ .Values.team }}
```

Referenced from an `Application` with per-reference values:

```yaml
kind: Application
spec:
  patches:
    - ref: standard-labels
      values:
        team: platform
```

This allows a single `Patch` object to serve many applications and clusters without duplication.

---

## Rollback

Rollback is triggered via the `Application /rollback` subresource. The request body specifies a target artifact version (S3 object version or timestamp). The Deploy Controller re-applies the referenced artifact directly, skipping the render pipeline. On success, `current.yaml` in S3 is updated to the rolled-back state.

---

## RBAC

Access control is delegated to native Kubernetes RBAC. Users define `Role`/`ClusterRole` bindings on kumo-cd resources as they would for any other Kubernetes resource. No custom RBAC layer is introduced.

---

## Non-Goals

- **Secret management** — sensitive values should come from a secrets manager (External Secrets Operator, Vault, etc.), not stored in Git or S3 unencrypted.
- **Progressive delivery** — canary/blue-green rollouts are out of scope. kumo-cd handles deployment; progressive delivery strategies are delegated to external tooling.
- **Non-Kubernetes workloads** — kumo-cd targets Kubernetes exclusively.
