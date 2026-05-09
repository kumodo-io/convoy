**Release Lifecycle Hooks**

<!-- TOC -->
  * [Purpose](#purpose)
  * [Why a Subresource, Not Annotations](#why-a-subresource-not-annotations)
  * [Phases](#phases)
  * [Hook Schema](#hook-schema)
    * [Hook fields](#hook-fields)
  * [CEL `when` Conditions](#cel-when-conditions)
    * [Phase-specific bindings](#phase-specific-bindings)
    * [Common patterns](#common-patterns)
    * [Evaluation semantics](#evaluation-semantics)
  * [Failure Handling](#failure-handling)
  * [Cleanup](#cleanup)
    * [`deleteBeforeCreation`](#deletebeforecreation)
    * [`cleanupOnCompletion`](#cleanuponcompletion)
  * [Ownership Tracking](#ownership-tracking)
  * [Lifecycle Retention Policy](#lifecycle-retention-policy)
    * [Interaction with per-hook policy](#interaction-with-per-hook-policy)
    * [When GC runs](#when-gc-runs)
  * [Helm Chart Compatibility](#helm-chart-compatibility)
  * [Status Reflection](#status-reflection)
    * [Per-hook status fields](#per-hook-status-fields)
  * [Interaction with Sync Waves](#interaction-with-sync-waves)
  * [Mutation Pipeline Interaction](#mutation-pipeline-interaction)
  * [Subresource](#subresource)
  * [Summary Table](#summary-table)
  * [Binding Model](#binding-model)
  * [`.Release` Bindings](#release-bindings)
    * [Release aliases](#release-aliases)
  * [`.Cluster` Bindings](#cluster-bindings)
    * [Cluster aliases](#cluster-aliases)
  * [`.Source` Bindings](#source-bindings)
    * [Source aliases](#source-aliases)
  * [`.Sync` Bindings](#sync-bindings)
    * [`.Sync.failure` (SyncFail only)](#syncfailure-syncfail-only)
  * [Phase Availability Summary](#phase-availability-summary)
  * [Common Patterns](#common-patterns-1)
    * [First-deploy vs. update](#first-deploy-vs-update)
    * [Skip on no-op syncs](#skip-on-no-op-syncs)
    * [Environment-conditional](#environment-conditional)
    * [Region-conditional](#region-conditional)
    * [Failure-stage-conditional](#failure-stage-conditional)
    * [Combining conditions](#combining-conditions)
    * [Reference render hash in template](#reference-render-hash-in-template)
  * [Evaluation Semantics](#evaluation-semantics-1)
  * [Validation](#validation)
<!-- TOC -->

## Purpose

Lifecycle hooks extend the Release sync pipeline with operations that aren't part of the deployed manifest set. They execute at well-defined points in the deploy lifecycle and handle side-effect work: database migrations, smoke tests, cache warming, deploy notifications, backups, alerts on failure.

Hooks live behind the `/lifecycle` subresource on each Release. The subresource is the canonical source of hook configuration; status mirrors hook execution state for visibility.

## Why a Subresource, Not Annotations

Hooks could be expressed as annotations on individual manifests (the ArgoCD/Helm pattern). The subresource model was chosen instead because:

- **Separates concerns.** Manifests describe what to deploy; lifecycle describes what to do at deploy time. Mixing them in annotations conflates two different things.
- **Decouples hooks from charts.** With annotations, hooks live inside the chart. To add a smoke test for a vendored chart, users would have to fork it. Subresource-defined hooks are owned by the deploying team.
- **Templating with cluster metadata is natural.** Hooks in the subresource use the same template engine as mutations and have direct access to per-cluster context.
- **Phase vocabulary is unconstrained.** Not bound to ArgoCD or Helm phase names; can include phases (like `PreRender` and `PostRender`) that don't exist in either ecosystem.
- **Audit and RBAC.** Subresource means standard k8s RBAC; distinct audit events for hook configuration changes.
- **Helm chart compatibility happens at translation time.** When sources have `helm.sh/hook` annotations, the renderer translates them into lifecycle entries automatically. Existing Helm charts with hooks work without modification.

## Phases

| Phase        | When it runs                                      | Common uses                                              |
|--------------|---------------------------------------------------|----------------------------------------------------------|
| `PreRender`  | Before mutations are applied                      | Validation of source content; environment checks         |
| `PostRender` | After rendering, before applying                  | Manifest validation; security scanning; signing          |
| `PreSync`    | Before any wave applies                           | Database migrations; pre-deploy checks; resource cleanup |
| `PostSync`   | After all waves complete (and optionally healthy) | Smoke tests; cache warming; deploy notifications         |
| `SyncFail`   | When sync fails at any point                      | Alerts; partial-state cleanup; rollback triggers         |
| `PreDelete`  | Before workloads are pruned on Release deletion   | Backups; graceful shutdown; external cleanup             |
| `PostDelete` | After workloads are pruned                        | Final notifications; resource accounting                 |

## Hook Schema

Hooks are typed units — each declares its kind via `type` and provides a Kubernetes-native template for that kind.

```yaml
# /releases/payments-api-prod-eu/lifecycle subresource

preSync:
  - name: db-migration
    type: Job                       # Job | Pod (v1)
    
    # Optional CEL condition. If absent, hook always runs.
    # If present and evaluates to false (or errors), hook is skipped.
    when: '.Cluster.metadata.tier == "production"'
    
    # k8s-native template (here: JobTemplateSpec, similar to CronJob.spec.jobTemplate)
    jobTemplate:
      spec:
        template:
          spec:
            serviceAccountName: payments-migrator
            restartPolicy: Never
            containers:
              - name: migrate
                image: registry.example.com/payments-migration:v1.2.3
                command: ["./migrate.sh"]
                env:
                  - name: TARGET_REGION
                    # Templated against per-Release context at sync time
                    value: "{{ .Cluster.metadata.region }}"
        backoffLimit: 0
    
    # Ordering within the phase (signed integer, parallel within same weight)
    weight: 0
    
    # Hook timeout — overrides any phase-level default
    timeout: 10m
    
    # What happens if the hook fails
    onFailure: AbortSync            # | Continue | AbortDelete (PreDelete only)
    
    # Cleanup behavior
    deleteBeforeCreation: true
    cleanupOnCompletion: OnSuccess  # | OnCompletion | Never

postSync:
  - name: smoke-test
    type: Job
    jobTemplate: { ... }
    onFailure: Continue
    cleanupOnCompletion: OnCompletion

syncFail:
  - name: alert-oncall
    type: Job
    jobTemplate:
      spec:
        template:
          spec:
            restartPolicy: Never
            containers:
              - name: alert
                image: alerting-tool:v1
                env:
                  - name: RELEASE_NAME
                    value: "{{ .Release.name }}"
                  - name: FAILURE_STAGE
                    value: "{{ .Sync.failure.stage }}"
    cleanupOnCompletion: OnCompletion

preDelete:
  - name: backup-database
    type: Job
    jobTemplate: { ... }
    timeout: 30m
    onFailure: AbortDelete
    cleanupOnCompletion: Never      # retain for compliance audit
```

### Hook fields

| Field                  | Type     | Required      | Default       | Description                                                                        |
|------------------------|----------|---------------|---------------|------------------------------------------------------------------------------------|
| `name`                 | string   | yes           | —             | Unique within the Release. Identifies the hook for status reporting and updates.   |
| `type`                 | enum     | yes           | —             | One of `Job`, `Pod`. Determines which template field is used.                      |
| `when`                 | string   | no            | —             | CEL expression. Hook runs only when expression is `true`. Errors treat as `false`. |
| `jobTemplate`          | object   | when type=Job | —             | Standard k8s `JobTemplateSpec`. Templated against phase-specific context.          |
| `podTemplate`          | object   | when type=Pod | —             | Standard k8s `PodTemplateSpec`.                                                    |
| `weight`               | integer  | no            | `0`           | Ordering within phase. Lower runs first; same weight runs in parallel.             |
| `timeout`              | duration | no            | phase default | Maximum time to wait for hook completion before marking it `TimedOut`.             |
| `onFailure`            | enum     | no            | phase default | `AbortSync`, `AbortDelete`, or `Continue`.                                         |
| `deleteBeforeCreation` | bool     | no            | `true`        | Delete any existing hook resource of the same name before creating a new one.      |
| `cleanupOnCompletion`  | enum     | no            | phase default | `OnSuccess`, `OnCompletion`, or `Never`.                                           |

## CEL `when` Conditions

The optional `when` field on each hook makes hook execution conditional. When absent, the hook always runs. When present, the expression evaluates against phase-specific bindings; the hook runs only if the expression is `true`.

### Phase-specific bindings

| Phase        | Bindings available                                                                              |
|--------------|-------------------------------------------------------------------------------------------------|
| `PreRender`  | `.Release`, `.Cluster`, `.Source`                                                               |
| `PostRender` | `.Release`, `.Cluster`, `.Source`, `.Render.manifestsRef`                                       |
| `PreSync`    | `.Release`, `.Cluster`, `.Render.manifestsRef`, `.Release.previousRender`                       |
| `PostSync`   | `.Release`, `.Cluster`, `.Render.manifestsRef`, `.Sync.completedAt`                             |
| `SyncFail`   | `.Release`, `.Cluster`, `.Render.manifestsRef`, `.Sync.failure.{stage, message, mutationIndex}` |
| `PreDelete`  | `.Release`, `.Cluster`                                                                          |
| `PostDelete` | `.Release`, `.Cluster`                                                                          |

`.Release.previousRender` is `null` on first deploy and populated otherwise — enables "first-deploy vs update" conditional logic without separate phases.

### Common patterns

```yaml
# Production-only
when: '.Cluster.metadata.tier == "production"'

# First deploy only
when: '.Release.firstDeploy'

# Region-specific
when: '.Cluster.metadata.region in ["eu-west-1", "eu-central-1"]'

# Specific failure stage
when: '.Sync.failure.stage == "Rendering"'

# Combination
when: '.Cluster.metadata.tier == "production" && .Release.firstDeploy'
```

### Evaluation semantics

- Validation at admission time: each `when` expression is compiled and field references are validated; invalid expressions are rejected on `/lifecycle` update
- Errors at runtime treat the condition as `false`; hook is skipped with a warning recorded in status
- Skipped hooks appear in status with `state: Skipped` and a reason

## Failure Handling

Each hook's `onFailure` controls behavior when the hook fails. Defaults vary by phase based on stake:

| Phase        | Default `onFailure` |
|--------------|---------------------|
| `PreRender`  | `AbortSync`         |
| `PostRender` | `AbortSync`         |
| `PreSync`    | `AbortSync`         |
| `PostSync`   | `Continue`          |
| `SyncFail`   | `Continue`          |
| `PreDelete`  | `AbortDelete`       |
| `PostDelete` | `Continue`          |

| Value         | Behavior                                                                                                                            |
|---------------|-------------------------------------------------------------------------------------------------------------------------------------|
| `AbortSync`   | Stop the sync. Set `Applied: False` with reason `HookFailed`. Run `SyncFail` hooks. Don't apply remaining waves or post-sync hooks. |
| `AbortDelete` | Stop the deletion. Preserve workloads. Release stays in deleting state until policy or hook is changed.                             |
| `Continue`    | Log the failure, continue with the rest of the sync. Failure is recorded in status but doesn't halt progress.                       |

## Cleanup

Two orthogonal axes control hook resource lifecycle.

### `deleteBeforeCreation`

Whether to delete any existing same-named hook resource before creating a new one for the current sync.

| Value            | Behavior                                                                                                                                  |
|------------------|-------------------------------------------------------------------------------------------------------------------------------------------|
| `true` (default) | Delete leftover hook resource before creating new one. Required for re-runnable hooks (migrations re-running on each sync).               |
| `false`          | Allow accumulation. Each sync creates a new hook resource (typically with a generated suffix). Subject to `maxRetainedPerHook` retention. |

### `cleanupOnCompletion`

When to delete the hook resource after it completes.

| Value          | Behavior                                                                                             |
|----------------|------------------------------------------------------------------------------------------------------|
| `OnSuccess`    | Delete on success; retain on failure (for debugging).                                                |
| `OnCompletion` | Delete after completion regardless of success/failure.                                               |
| `Never`        | Retain indefinitely. User responsible for manual cleanup. Compliance use case (audit jobs, backups). |

Defaults per phase:

| Phase                     | Default `cleanupOnCompletion` |
|---------------------------|-------------------------------|
| `PreRender`, `PostRender` | `OnSuccess`                   |
| `PreSync`, `PostSync`     | `OnSuccess`                   |
| `SyncFail`                | `OnCompletion`                |
| `PreDelete`               | `Never`                       |
| `PostDelete`              | `OnCompletion`                |

`deleteBeforeCreation: true` is the default for all phases.

## Ownership Tracking

Hook resources live in target clusters; the Release object lives in the control plane cluster. Cross-cluster `ownerReferences` don't work (the target cluster's GC can't resolve references to resources in another cluster), so hooks use **labels-only ownership tracking**.

Every hook resource created by the controller carries:

```yaml
metadata:
  labels:
    convoy.kumodo.io/release-name: payments-api-prod-eu
    convoy.kumodo.io/release-namespace: payments-team
    convoy.kumodo.io/release-uid: 8f3a2b1c-...
    convoy.kumodo.io/hook-phase: PreSync
    convoy.kumodo.io/hook-name: db-migration
    convoy.kumodo.io/managed-by: convoy
  annotations:
    convoy.kumodo.io/created-at: "2026-04-29T09:59:30Z"
    convoy.kumodo.io/sync-id: <sync-uuid>
```

The controller queries by label selector to discover owned resources. All cleanup is controller-driven (sync-time and periodic GC sweep).

If the controller is permanently lost or disconnected, hook resources in target clusters become orphaned. Operators discover this via target-cluster inspection (`kubectl get jobs -l convoy.kumodo.io/managed-by=convoy`) and clean up manually. This is a property of any cross-cluster managed system; the cure is operational practice (monitoring controller health), not a technical mechanism.

## Lifecycle Retention Policy

`Release.spec.lifecycleRetentionPolicy` provides a backstop for stale hook resources beyond per-hook cleanup behavior. Catches edge cases: orphans from removed hooks, leftovers from controller crashes, accumulation under `cleanupOnCompletion: Never`.

```yaml
spec:
  lifecycleRetentionPolicy:
    maxAge: 30d
    onHookRemoved: prune
    overrideNeverCleanup: false
    maxRetainedPerHook: 5
```

| Field                  | Type     | Default | Description                                                                                                                                       |
|------------------------|----------|---------|---------------------------------------------------------------------------------------------------------------------------------------------------|
| `maxAge`               | duration | `30d`   | Maximum age for any retained hook resource. Older resources are deleted by GC sweep. Set to `0` to disable.                                       |
| `onHookRemoved`        | enum     | `prune` | When a hook is removed from `/lifecycle`, what to do with existing resources. `prune` deletes them; `retain` leaves them to age out via `maxAge`. |
| `overrideNeverCleanup` | bool     | `false` | Whether `maxAge` applies even to hooks with `cleanupOnCompletion: Never`. Default `false` respects compliance intent.                             |
| `maxRetainedPerHook`   | integer  | `5`     | Maximum retained resources per hook name when `deleteBeforeCreation: false`.                                                                      |

### Interaction with per-hook policy

| Per-hook config                                             | `maxAge` behavior                              |
|-------------------------------------------------------------|------------------------------------------------|
| `cleanupOnCompletion: OnSuccess`, succeeded                 | Already cleaned; nothing to do                 |
| `cleanupOnCompletion: OnSuccess`, failed                    | Retained; deleted when older than `maxAge`     |
| `cleanupOnCompletion: OnCompletion`                         | Already cleaned; nothing to do                 |
| `cleanupOnCompletion: Never`, `overrideNeverCleanup: false` | Retained indefinitely; `maxAge` does not apply |
| `cleanupOnCompletion: Never`, `overrideNeverCleanup: true`  | Deleted when older than `maxAge`               |

### When GC runs

- **Sync-time:** cleanup runs as part of each sync, after applying manifests and running hooks
- **Periodic background sweep:** controller scans all Releases periodically (e.g., hourly) for stale resources, regardless of sync activity
- Both modes apply the same retention policy

## Helm Chart Compatibility

Helm charts use `helm.sh/hook` annotations to mark resources as hooks. The renderer recognizes these and translates them into lifecycle entries automatically:

| Helm hook                                          | Translated to                                               | Notes                              |
|----------------------------------------------------|-------------------------------------------------------------|------------------------------------|
| `pre-install`, `pre-upgrade`                       | `PreSync` entry                                             | Treated as a regular PreSync hook  |
| `post-install`, `post-upgrade`                     | `PostSync` entry                                            |                                    |
| `pre-delete`                                       | `PreDelete` entry                                           |                                    |
| `post-delete`                                      | `PostDelete` entry                                          |                                    |
| `helm.sh/hook-weight`                              | `weight` field                                              |                                    |
| `helm.sh/hook-delete-policy: hook-succeeded`       | `cleanupOnCompletion: OnSuccess`                            |                                    |
| `helm.sh/hook-delete-policy: hook-failed`          | `cleanupOnCompletion: Never` (retain on failure equivalent) |                                    |
| `helm.sh/hook-delete-policy: before-hook-creation` | `deleteBeforeCreation: true`                                |                                    |
| `test`, `test-success`, `test-failure`             | Currently ignored                                           | Helm test framework not integrated |

User-defined hooks (in `/lifecycle`) and chart-translated hooks coexist. Chart hooks appear in status marked with `source: chart`; user hooks with `source: lifecycle`. Within a phase, ordering is by `weight` regardless of source.

This means standard Helm charts with hook annotations work without modification when deployed via this tool — chart authors don't have to do anything special, and deploying teams don't have to translate hooks manually.

## Status Reflection

Hook execution state is reported per-phase in `Release.status.lifecycle`:

```yaml
status:
  lifecycle:
    preSync:
      - name: db-migration
        type: Job
        source: lifecycle              # | chart
        kind: Job
        apiGroup: batch
        version: v1
        namespace: payments-team
        resourceName: db-migration-1745928000
        state: Succeeded
        startedAt: 2026-04-29T09:59:30Z
        completedAt: 2026-04-29T10:00:00Z
        cleanedUpAt: 2026-04-29T10:00:05Z
        message: "Job completed successfully (1/1 succeeded). Resource cleaned up."
    
    postSync:
      - name: smoke-test
        type: Job
        source: lifecycle
        state: Failed
        startedAt: 2026-04-29T10:00:50Z
        completedAt: 2026-04-29T10:01:30Z
        message: "Job failed (backoff limit exceeded; container exit 1)"
        followUpAction: Continue
      
      - name: payments-helm-post-install
        type: Job
        source: chart                   # came from helm.sh/hook annotation
        state: Skipped
        skipReason: Condition
        message: "when condition evaluated to false"
  
  lifecycleRetention:
    lastSweepAt: 2026-04-29T10:00:00Z
    resourcesDeleted: 3
    resourcesRetained: 5
  
  resolvedLifecycleRetentionPolicy:
    maxAge: 30d
    onHookRemoved: prune
    overrideNeverCleanup: false
    maxRetainedPerHook: 5
```

### Per-hook status fields

| Field                         | Description                                                                                           |
|-------------------------------|-------------------------------------------------------------------------------------------------------|
| `name`                        | Matches the hook's name in the subresource.                                                           |
| `type`                        | Hook type (`Job`, `Pod`).                                                                             |
| `source`                      | `lifecycle` (user-defined) or `chart` (translated from Helm hook annotation).                         |
| `kind`, `apiGroup`, `version` | Resource kind of the created hook.                                                                    |
| `namespace`, `resourceName`   | Identity of the actual created resource.                                                              |
| `state`                       | `Pending`, `Running`, `Succeeded`, `Failed`, `TimedOut`, `Skipped`.                                   |
| `skipReason`                  | When `state: Skipped`: `Condition` (when=false) or `EvaluationError`.                                 |
| `startedAt`                   | When the hook resource was created and began running.                                                 |
| `completedAt`                 | When the hook reached terminal state (success/failure/timeout).                                       |
| `cleanedUpAt`                 | When the hook resource was deleted by cleanup logic. Absent when retained.                            |
| `message`                     | Human-readable detail.                                                                                |
| `followUpAction`              | What the controller did after the hook completed: `None`, `Continue`, `AbortedSync`, `AbortedDelete`. |

## Interaction with Sync Waves

Hooks and waves operate at different points in the sync pipeline and don't mix at the resource level:

- `PreSync` hooks run before any wave starts
- All waves apply through their own ordering and transition strategies
- `PostSync` hooks run after all waves complete

Within a phase, hooks order by `weight` (not by `sync-wave`). Ordering mechanisms are scoped: waves order regular manifests; hook weights order hooks within a phase. They don't interact.

## Mutation Pipeline Interaction

Hook templates are part of the rendered configuration and flow through mutations naturally — `goTemplate` and `celTransform` mutations can modify hook templates the same way they modify any other resource. Cluster metadata, Release context, and other bindings are available in hook templates per the phase binding rules above.

This keeps the rendering pipeline uniform; hooks are not a special case at render time.

## Subresource

| Verb   | Path                         | Purpose                                        |
|--------|------------------------------|------------------------------------------------|
| GET    | `/releases/<name>/lifecycle` | Read full lifecycle config                     |
| PUT    | `/releases/<name>/lifecycle` | Replace entire lifecycle config                |
| PATCH  | `/releases/<name>/lifecycle` | Partial update                                 |
| DELETE | `/releases/<name>/lifecycle` | Remove all lifecycle config (clear all phases) |

Per-phase or per-hook addressing is not supported in v1; the entire lifecycle config is small enough to PUT atomically.

## Summary Table

| Aspect                | Decision                                                                                |
|-----------------------|-----------------------------------------------------------------------------------------|
| Expression            | `/lifecycle` subresource with phase-keyed hook lists                                    |
| Phases                | `PreRender`, `PostRender`, `PreSync`, `PostSync`, `SyncFail`, `PreDelete`, `PostDelete` |
| Hook types            | `Job` and `Pod` for v1                                                                  |
| Hook template         | k8s-native template per type (e.g., `JobTemplateSpec`)                                  |
| Templating context    | Phase-specific bindings (`.Release`, `.Cluster`, `.Render`, `.Sync`, etc.)              |
| Conditional execution | Optional CEL `when` field per hook                                                      |
| Completion criteria   | Reuses `HealthRule` machinery — hook is done when its resource is `Healthy`             |
| Failure handling      | Per-hook `onFailure`; phase-appropriate defaults                                        |
| Ordering              | Per-hook `weight` within phase; parallel within same weight                             |
| Cleanup               | Per-hook `deleteBeforeCreation` and `cleanupOnCompletion`                               |
| Retention backstop    | Release-level `lifecycleRetentionPolicy`                                                |
| Ownership tracking    | Labels-only (cross-cluster `ownerReferences` don't work)                                |
| Helm compatibility    | `helm.sh/hook` annotations translated automatically; coexist with user hooks            |
| Status                | Per-phase, per-hook execution state in `status.lifecycle`                               |

## Binding Model

The context exposes three top-level objects, each a projection of an underlying API resource, plus a small set of computed aliases for common patterns.

| Object     | Backing resource                                            | Available in phases                                              |
|------------|-------------------------------------------------------------|------------------------------------------------------------------|
| `.Release` | The Release object itself                                   | All phases                                                       |
| `.Cluster` | The Cluster object referenced by the Release                | All phases                                                       |
| `.Source`  | The Source object referenced by the Release                 | `PreRender`, `PostRender` only                                   |
| `.Sync`    | Computed projection of the in-flight or just-completed sync | `PostRender` onward (with phase-specific availability per field) |

Direct access to underlying resource fields (`.Release.status.lastAttempted.renderedRef`, etc.) is fully supported. Aliases exist purely for ergonomic access to commonly-referenced information; they are read-only projections that present the right value for the hook's phase.

When the underlying status structure evolves between API versions, aliases preserve their documented semantics. Users referencing aliases are insulated from internal status reorganization; users using direct status access bear the responsibility of tracking schema changes.

## `.Release` Bindings

Direct projection of the Release object. Available in all phases.

| Path                   | Backing                | Notes                                                               |
|------------------------|------------------------|---------------------------------------------------------------------|
| `.Release.name`        | `metadata.name`        | Release object name                                                 |
| `.Release.namespace`   | `metadata.namespace`   | Release object namespace                                            |
| `.Release.uid`         | `metadata.uid`         | Stable identity across renames                                      |
| `.Release.generation`  | `metadata.generation`  | Spec generation at hook execution time                              |
| `.Release.labels`      | `metadata.labels`      | Release labels                                                      |
| `.Release.annotations` | `metadata.annotations` | Release annotations                                                 |
| `.Release.spec.*`      | `spec.*`               | Full Release spec — source, cluster, content, syncPolicy, etc.      |
| `.Release.status.*`    | `status.*`             | Full Release status — for direct access when an alias doesn't exist |

### Release aliases

| Alias                              | Backing                                          | Notes                                                                   |
|------------------------------------|--------------------------------------------------|-------------------------------------------------------------------------|
| `.Release.firstDeploy`             | computed: `!has(.Release.status.lastSuccessful)` | `true` on the first deploy of this Release; `false` thereafter.         |
| `.Release.lastSuccessfulRenderRef` | `.Release.status.lastSuccessful.renderedRef`     | Hash of currently deployed render. `null` when `firstDeploy` is `true`. |
| `.Release.lastSuccessfulSyncedAt`  | `.Release.status.lastSuccessful.syncedAt`        | When the previously deployed render was synced. `null` on first deploy. |

## `.Cluster` Bindings

Direct projection of the Cluster object. Available in all phases.

| Path                 | Backing              | Notes                                     |
|----------------------|----------------------|-------------------------------------------|
| `.Cluster.name`      | `metadata.name`      | Cluster object name                       |
| `.Cluster.namespace` | `metadata.namespace` | Cluster object namespace                  |
| `.Cluster.uid`       | `metadata.uid`       | Stable cluster identity                   |
| `.Cluster.labels`    | `metadata.labels`    | Cluster labels (used for fleet selection) |
| `.Cluster.spec.*`    | `spec.*`             | Full Cluster spec                         |
| `.Cluster.status.*`  | `status.*`           | Full Cluster status                       |

### Cluster aliases

| Alias                        | Backing                             | Notes                                                                                                                                                                                |
|------------------------------|-------------------------------------|--------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `.Cluster.metadata`          | `.Cluster.spec.metadata`            | Per-cluster free-form metadata. **Note:** this aliases the spec field, not the k8s `metadata` block. The k8s metadata fields are exposed as `.Cluster.name`, `.Cluster.labels`, etc. |
| `.Cluster.discoveredVersion` | `.Cluster.status.discoveredVersion` | Discovered Kubernetes version (e.g., `v1.29.4`).                                                                                                                                     |

The `.Cluster.metadata` alias intentionally surfaces the most commonly-referenced field (per-cluster templating metadata) at a convenient path. Users referencing the k8s metadata block directly use `.Cluster.name`, `.Cluster.labels`, etc.

## `.Source` Bindings

Direct projection of the Source object referenced by `Release.spec.source`. Available only in `PreRender` and `PostRender` phases — by `PreSync` onward, the rendered output is what matters and source-specific information is no longer relevant.

| Path                | Backing                     | Notes                                             |
|---------------------|-----------------------------|---------------------------------------------------|
| `.Source.kind`      | source kind                 | One of `GitSource`, `HelmRegistry`, `OCIRegistry` |
| `.Source.name`      | source `metadata.name`      |                                                   |
| `.Source.namespace` | source `metadata.namespace` |                                                   |
| `.Source.spec.*`    | source `spec.*`             | Full source spec                                  |
| `.Source.status.*`  | source `status.*`           | Full source status                                |

### Source aliases

| Alias                         | Backing                               | Notes                                                    |
|-------------------------------|---------------------------------------|----------------------------------------------------------|
| `.Source.lastFetchedRevision` | `.Source.status.lastFetched.revision` | Resolved revision (e.g., Git SHA, chart version digest). |

## `.Sync` Bindings

Computed projection of the sync attempt the hook is part of. Aliases consolidate render and sync information that would otherwise require navigating `lastAttempted` and `lastSuccessful` separately.

`.Sync.*` is **phase-dependent**: the underlying field that backs each alias varies by phase, so the alias presents the value meaningful for that phase without users needing to know which status field to read.

| Alias                       | Backing (phase-dependent)                                                                                                                      | Available in                            | Notes                                                                                                                        |
|-----------------------------|------------------------------------------------------------------------------------------------------------------------------------------------|-----------------------------------------|------------------------------------------------------------------------------------------------------------------------------|
| `.Sync.renderRef`           | `.Release.status.lastAttempted.renderedRef` (PostRender, PreSync, PostSync, SyncFail); equals `lastSuccessful.renderedRef` once sync completes | PostRender, PreSync, PostSync, SyncFail | Hash of the render being applied (or just applied). May be `null` in `SyncFail` if failure occurred before render completed. |
| `.Sync.renderCompletedAt`   | `.Release.status.lastAttempted.renderCompletedAt`                                                                                              | PostRender, PreSync, PostSync           | When rendering finished for the current attempt.                                                                             |
| `.Sync.attemptedAt`         | `.Release.status.lastAttempted.attemptedAt`                                                                                                    | PreSync, PostSync, SyncFail             | When the current sync attempt started.                                                                                       |
| `.Sync.completedAt`         | `.Release.status.lastSuccessful.syncedAt`                                                                                                      | PostSync                                | When sync finished.                                                                                                          |
| `.Sync.appliedResources`    | `.Release.status.lastSuccessful.appliedResources`                                                                                              | PostSync                                | Count of resources applied in the just-completed sync.                                                                       |
| `.Sync.changedFromPrevious` | computed: `lastAttempted.renderedRef != lastSuccessful.renderedRef` (or `true` if `lastSuccessful` is `null`)                                  | PostSync                                | `true` if this sync changed the deployed render hash; `false` if the same content was reapplied.                             |

### `.Sync.failure` (SyncFail only)

| Alias                         | Backing                                               | Notes                                                                                  |
|-------------------------------|-------------------------------------------------------|----------------------------------------------------------------------------------------|
| `.Sync.failure.stage`         | `.Release.status.lastAttempted.failure.stage`         | One of `SourceResolution`, `MutationResolution`, `Rendering`, `Syncing`, `HookFailed`  |
| `.Sync.failure.message`       | `.Release.status.lastAttempted.failure.message`       | Human-readable failure detail                                                          |
| `.Sync.failure.mutationIndex` | `.Release.status.lastAttempted.failure.mutationIndex` | When `stage: MutationResolution`, the index of the failing mutation. `null` otherwise. |
| `.Sync.failure.waveNumber`    | `.Release.status.lastAttempted.failure.waveNumber`    | When `stage: Syncing`, the wave number where failure occurred. `null` otherwise.       |
| `.Sync.failure.hookName`      | `.Release.status.lastAttempted.failure.hookName`      | When `stage: HookFailed`, the name of the failing hook. `null` otherwise.              |
| `.Sync.failure.hookPhase`     | `.Release.status.lastAttempted.failure.hookPhase`     | When `stage: HookFailed`, the phase of the failing hook. `null` otherwise.             |

## Phase Availability Summary

| Binding                     | PreRender | PostRender | PreSync | PostSync | SyncFail        | PreDelete | PostDelete |
|-----------------------------|-----------|------------|---------|----------|-----------------|-----------|------------|
| `.Release.*`                | ✓         | ✓          | ✓       | ✓        | ✓               | ✓         | ✓          |
| `.Cluster.*`                | ✓         | ✓          | ✓       | ✓        | ✓               | ✓         | ✓          |
| `.Source.*`                 | ✓         | ✓          | —       | —        | —               | —         | —          |
| `.Sync.renderRef`           | —         | ✓          | ✓       | ✓        | ✓ (may be null) | —         | —          |
| `.Sync.renderCompletedAt`   | —         | ✓          | ✓       | ✓        | —               | —         | —          |
| `.Sync.attemptedAt`         | —         | —          | ✓       | ✓        | ✓               | —         | —          |
| `.Sync.completedAt`         | —         | —          | —       | ✓        | —               | —         | —          |
| `.Sync.appliedResources`    | —         | —          | —       | ✓        | —               | —         | —          |
| `.Sync.changedFromPrevious` | —         | —          | —       | ✓        | —               | —         | —          |
| `.Sync.failure.*`           | —         | —          | —       | —        | ✓               | —         | —          |

## Common Patterns

### First-deploy vs. update

```yaml
when: '.Release.firstDeploy'
```

Runs only on the first successful deploy of this Release. Useful for one-time initialization (database seeds, namespace setup, initial configuration).

### Skip on no-op syncs

```yaml
when: '.Sync.changedFromPrevious'
```

In `PostSync`, runs the hook only when the sync actually changed something. Avoids re-running smoke tests when reconciliation reapplies an unchanged manifest set.

### Environment-conditional

```yaml
when: '.Cluster.metadata.tier == "production"'
```

Production-only operations. The user populates `Cluster.spec.metadata.tier` during cluster registration; the alias makes templating concise.

### Region-conditional

```yaml
when: '.Cluster.metadata.region in ["eu-west-1", "eu-central-1"]'
```

Region-specific operations (e.g., GDPR-related cleanup in EU regions).

### Failure-stage-conditional

```yaml
when: '.Sync.failure.stage == "Rendering"'
```

In `SyncFail`, route different alerts depending on what stage failed. Render failures are config issues; sync failures are runtime issues; they typically need different escalation paths.

### Combining conditions

```yaml
when: '.Cluster.metadata.tier == "production" && .Release.firstDeploy'
```

Production, first deploy only. Common pattern for one-time production setup that should never run in dev or staging.

### Reference render hash in template

```yaml
env:
  - name: RENDER_HASH
    value: "{{ .Sync.renderRef }}"
  - name: REGION
    value: "{{ .Cluster.metadata.region }}"
```

## Evaluation Semantics

- Bindings are evaluated at hook execution time, immediately before the controller decides whether to run the hook (for `when`) or templates the hook resource (for template bodies).
- Values reflect the Release's state at that exact moment. They are not snapshotted earlier in the sync.
- Aliases are resolved against their backing fields at evaluation time. If an aliased field is `null` (e.g., `.Release.lastSuccessfulRenderRef` on first deploy), the alias resolves to `null`.
- Expressions referencing potentially-null bindings must handle `null` explicitly using CEL's `has()` operator or null comparisons. Errors during evaluation treat the condition as `false` — the hook is skipped with a warning recorded in status.

## Validation

When `/lifecycle` is updated:

- Each `when` expression is compiled as CEL and field references are validated against the bindings available for the hook's phase.
- Each template body's go-template syntax is parsed; field references against the bindings are checked.
- Invalid expressions or out-of-phase binding references are rejected at admission time with a message identifying the offending hook and expression.

This catches typos and binding-availability mistakes before they cause runtime failures or skipped hooks.
