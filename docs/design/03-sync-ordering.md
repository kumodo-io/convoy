**Sync Waves**

<!-- TOC -->
  * [Purpose](#purpose)
  * [Scope](#scope)
  * [Wave Specification via Annotations](#wave-specification-via-annotations)
    * [Wave Numbering](#wave-numbering)
    * [Annotations Recognized](#annotations-recognized)
    * [Helm Hook Compatibility](#helm-hook-compatibility)
  * [Wave Transition Strategies](#wave-transition-strategies)
    * [Why Wave Strategies Matter](#why-wave-strategies-matter)
  * [Status Reflection](#status-reflection)
  * [Resource Selection by Wave](#resource-selection-by-wave)
  * [Interaction with `/preview` and `/diff`](#interaction-with-preview-and-diff)
  * [Mutation Pipeline Interaction](#mutation-pipeline-interaction)
  * [What's Not Addressed Here](#whats-not-addressed-here)
  * [Summary Table](#summary-table)
<!-- TOC -->

## Purpose

Sync waves provide ordered application of resources within a single Release. They solve real-world deployment dependencies where some resources must exist or be ready before others can be applied successfully.

Common use cases:

- **CRDs before CRs.** Custom resource definitions must be registered with the API server before custom resources of those kinds can be applied.
- **Namespaces before namespaced resources.** A `Namespace` must exist before resources are placed in it.
- **ConfigMaps and Secrets before workloads.** Avoids transient failure states when Pods reference configs that don't yet exist.
- **PVC and StorageClass relationships.** Storage must be bound before workloads consume it.
- **Operators before their CRs.** An operator's controller must be running before its custom resources can be reconciled.
- **Service before Ingress.** Some ingress controllers require the referenced Service to exist.

Without ordering, users hit these problems repeatedly and resort to splitting deployments across multiple Releases, manual coordination, or reliance on eventual consistency through retries — all of which introduce operational friction.

## Scope

Sync waves are **resource-level ordering within a single Release**. Cross-Release ordering (e.g., "deploy infra Release before app Release") is a separate concern handled at the fleet level (`ReleaseSet`) and is not addressed here.

## Wave Specification via Annotations

Wave assignment is expressed as annotations on individual manifests. The annotation appears in the rendered output (after mutations are applied) and is read by the sync engine.

```yaml
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: certificates.cert-manager.io
  annotations:
    convoy.kumodo.io/sync-wave: "-10"     # apply early
spec: ...
```

```yaml
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: payments-tls
  annotations:
    convoy.kumodo.io/sync-wave: "10"      # apply late
spec: ...
```

### Wave Numbering

- Wave numbers are signed integers
- Lower numbers apply earlier; higher numbers apply later
- Negative numbers are valid and commonly used for "bootstrap" resources
- Default wave (no annotation) is `0`
- Resources within the same wave apply in parallel
- Between waves: the next wave begins only after the current wave's transition condition is met (see Wave Transition Strategies below)

### Annotations Recognized

| Annotation                     | Purpose                                                                                                                                                                      |
|--------------------------------|------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `convoy.kumodo.io/sync-wave`       | Native annotation. Always preferred when set.                                                                                                                                |
| `argocd.argoproj.io/sync-wave` | Recognized for ecosystem compatibility. Used when the native annotation is absent. Allows Helm charts and bundles already annotated for ArgoCD to work without modification. |
| `helm.sh/hook-weight`          | Recognized **only** for resources also marked with `helm.sh/hook` (see Helm Hook Compatibility below). Maps Helm's hook ordering into the wave system.                       |

When multiple annotations are present, precedence is: `convoy.kumodo.io/sync-wave` > `argocd.argoproj.io/sync-wave`. Helm hook annotations apply only to Helm-marked hook resources and don't conflict with the wave annotations on regular resources.

### Helm Hook Compatibility

Helm charts use `helm.sh/hook` annotations to mark resources as hooks (e.g., `pre-install`, `post-install`, `pre-upgrade`). The sync engine recognizes these and translates them into wave behavior:

| Helm hook                              | Treated as                           | Notes                                                             |
|----------------------------------------|--------------------------------------|-------------------------------------------------------------------|
| `pre-install`, `pre-upgrade`           | Pre-sync hook (negative early waves) | Runs before regular manifests                                     |
| `post-install`, `post-upgrade`         | Post-sync hook (positive late waves) | Runs after regular manifests                                      |
| `pre-delete`, `post-delete`            | Currently ignored                    | Helm-specific; not yet integrated with Release deletion semantics |
| `test`, `test-success`, `test-failure` | Currently ignored                    | Helm test framework; not part of sync semantics                   |

Resources with `helm.sh/hook` annotations are processed as hooks; their `helm.sh/hook-weight` controls ordering within the hook phase. Resources without `helm.sh/hook` go through the regular wave pipeline.

This means standard Helm charts using hook annotations work without modification when deployed via this tool.

## Wave Transition Strategies

When a wave finishes, the sync engine decides when to start the next wave. The strategy is configured per-Release:

```yaml
spec:
  syncPolicy:
    waveStrategy:
      strategy: waitForApplied      # | waitForHealthy | waitForHealthyWithTimeout
      waveTimeout: 10m              # used only with waitForHealthyWithTimeout
```

| Strategy                    | Wave transition condition                                                                                                                                                                                                                                                                  |
|-----------------------------|--------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `waitForApplied`            | All resources in the wave have been accepted by the kube-apiserver. Default. Fast; appropriate when resources don't have meaningful runtime dependencies between waves.                                                                                                                    |
| `waitForHealthy`            | All resources in the wave have reached `Healthy` (or `Suspended`) per the health assessment rules. Necessary when subsequent waves depend on runtime state — e.g., CRD must be fully registered before CRs of that kind can apply, operator must be Running before its CRs are reconciled. |
| `waitForHealthyWithTimeout` | Same as `waitForHealthy`, but if `waveTimeout` elapses without the wave reaching healthy, sync proceeds to the next wave with a warning recorded in status. Fallback for slow-starting workloads where waiting indefinitely would block deploys.                                           |

### Why Wave Strategies Matter

`waitForApplied` is fast but insufficient for ordering that depends on runtime readiness. A common failure: applying a CRD then immediately applying a CR of that kind — the CR application can fail because the API server hasn't yet finished registering the CRD's API. `waitForHealthy` is necessary for these cases.

`waitForHealthyWithTimeout` is the practical compromise for production deploys where some workloads (databases, slow init containers) might legitimately take 10+ minutes to be healthy but blocking deploys indefinitely is unacceptable.

## Status Reflection

Per-wave progress is reported in `Release.status.waves`:

```yaml
status:
  waves:
    - number: -10
      resources: 1                      # CRD
      state: Completed
      startedAt: 2026-04-29T10:00:00Z
      completedAt: 2026-04-29T10:00:15Z
      transitionReason: AppliedAndHealthy
    
    - number: 0
      resources: 12                     # most resources
      state: Completed
      startedAt: 2026-04-29T10:00:15Z
      completedAt: 2026-04-29T10:00:45Z
      transitionReason: AppliedAndHealthy
    
    - number: 10
      resources: 1                      # Certificate (CR of the CRD above)
      state: InProgress
      startedAt: 2026-04-29T10:00:45Z
      transitionReason: ""
  
  conditions:
    - type: Applied
      status: "Unknown"
      reason: WaveInProgress
      message: "Wave 10 of 10 in progress (1 resource)"
```

| Field              | Description                                                                                                         |
|--------------------|---------------------------------------------------------------------------------------------------------------------|
| `number`           | Wave number from manifest annotation.                                                                               |
| `resources`        | Count of manifests in this wave.                                                                                    |
| `state`            | `Pending`, `InProgress`, `Completed`, `Failed`, `TimedOut`.                                                         |
| `startedAt`        | When the wave's first resource was applied.                                                                         |
| `completedAt`      | When the wave's transition condition was met.                                                                       |
| `transitionReason` | Why the wave transitioned: `AppliedAndHealthy`, `Applied` (when strategy is `waitForApplied`), `Timeout`, `Failed`. |

While a sync is in progress, the relevant `Applied` and `Healthy` conditions reflect intermediate state — `status: "Unknown"` rather than `True` or `False` — making clear that the verdict is pending wave completion.

## Resource Selection by Wave

Resources without an explicit wave annotation default to wave `0`. A Release with no annotated resources runs as a single wave (effectively no ordering), behaving identically to a non-wave deploy.

A Release with annotated resources runs through waves in order:

1. Sort waves ascending by number
2. For each wave: apply all resources in parallel; wait for transition condition
3. Move to next wave; repeat
4. After last wave: report final sync state

If any resource in a wave fails to apply, the wave fails. By default, this fails the whole sync — subsequent waves don't run, status reflects the failure. Future enhancement: configurable `continueOnWaveFailure` — not in v1.

## Interaction with `/preview` and `/diff`

The `/preview` subresource (when implemented) shows the rendered manifest set with wave annotations preserved. Tooling can group resources by wave for display.

The `/diff` subresource shows wave-by-wave diff when both sides have wave annotations, allowing operators to see "what's in wave 0 that's changing" rather than a single flat diff.

## Mutation Pipeline Interaction

Wave annotations are part of the rendered manifest set. They flow through the mutation pipeline naturally — mutations can add, modify, or remove wave annotations:

- A `goTemplate` mutation could add wave annotations based on cluster metadata
- A `jsonPatch` mutation could override a chart's default wave for a specific resource
- A `strategicMergePatch` mutation could add wave annotations to resources that don't have them

This means waves are not a separate concept layered on top of the rendering pipeline — they're a property of rendered manifests, set by whatever combination of source content and mutations produced the final output. The sync engine reads them at the end, after all rendering is complete.

## What's Not Addressed Here

Three related concepts are deliberately out of scope for sync waves and will be designed separately:

- **Pre-sync, post-sync, sync-fail hooks** — actions (typically Jobs) that run at specific points in the sync lifecycle. Helm's `pre-install`/`post-install` hooks map to the wave system as described above; the broader hook concept (with explicit hook resource definitions, hook success criteria, hook timeout) is a future enhancement.
- **Cross-Release ordering** — fleet-level deployment dependencies. Belongs to the `ReleaseSet` design.
- **Conditional waves** — running a wave only if a condition is met (e.g., "run migration wave only on first deploy, not on updates"). Future enhancement.

## Summary Table

| Aspect                | Decision                                                                              |
|-----------------------|---------------------------------------------------------------------------------------|
| Ordering scope        | Within a single Release (resource-level)                                              |
| Specification         | Annotation on individual manifests                                                    |
| Native annotation     | `convoy.kumodo.io/sync-wave`                                                              |
| Recognized aliases    | `argocd.argoproj.io/sync-wave` (compatibility)                                        |
| Helm compatibility    | `helm.sh/hook` annotations recognized; `helm.sh/hook-weight` orders within hook phase |
| Default wave          | `0`                                                                                   |
| Within-wave behavior  | Parallel application                                                                  |
| Between-wave behavior | Strategy-controlled: `waitForApplied`, `waitForHealthy`, `waitForHealthyWithTimeout`  |
| Default strategy      | `waitForApplied`                                                                      |
| Status                | Per-wave progress in `status.waves[]`                                                 |
| Failure handling      | Wave failure fails the sync (no subsequent waves)                                     |