<!-- TOC -->
  * [Example](#example)
  * [Spec](#spec)
<!-- TOC -->

`ResourceHealthRule` is a **namespaced** resource in the `extensions.convoy.kumodo.io/v1alpha1` API group. It defines a custom health rule for a single resource instance, identified by `(apiGroup, kind, namespace, name)`. It takes precedence over `KindHealthRule` when both match. For cluster-scoped target resources, `namespace` is empty. It is part of the health assessment system — see `core/release.md` ("Health Assessment") for resolution order and how per-resource verdicts aggregate into the Release-level `Healthy` condition.

**Relationship to Release:** None. The controller indexes `ResourceHealthRule` by `(apiGroup, kind, version, namespace, name)` and looks up the rule at evaluation time, independently of which Release manages the resource.

**Expression language:** Same CEL bindings as `KindHealthRule` — the live resource (`object`) and current time (`now`); the `expression` returns one of `Healthy`, `Progressing`, `Degraded`, `Suspended`, `Unknown`.

## Example

```yaml
apiVersion: extensions.convoy.kumodo.io/v1alpha1
kind: ResourceHealthRule
metadata:
  name: payments-db-health
  namespace: payments-team
spec:
  target:
    apiGroup: apps
    kind: StatefulSet
    version: v1
    name: payments-db
    namespace: payments-team
  
  expression: |
    object.status.readyReplicas == object.status.replicas &&
    object.status.replicas >= 3
      ? "Healthy"
      : object.status.readyReplicas > 0
        ? "Progressing"
        : "Degraded"
  
  progressDeadline: 15m
  
  messageExpression: |
    string(object.status.readyReplicas) + "/" + string(object.status.replicas) + " replicas ready"
```

Cluster-scoped target example (namespace is empty in the target):

```yaml
apiVersion: extensions.convoy.kumodo.io/v1alpha1
kind: ResourceHealthRule
metadata:
  name: some-ns-health
  namespace: platform-team
spec:
  target:
    apiGroup: ""
    kind: Namespace
    name: some-ns
    namespace: ""
  
  expression: |
    object.status.phase == "Active" ? "Healthy" : "Degraded"
```

## Spec

| Field               | Type         | Required | Description                                                                                                                 |
|---------------------|--------------|----------|-----------------------------------------------------------------------------------------------------------------------------|
| `target.apiGroup`   | string       | yes      | API group of the targeted resource (empty string `""` for core group).                                                      |
| `target.kind`       | string       | yes      | Kind of the targeted resource.                                                                                              |
| `target.version`    | string       | yes      | API version of the targeted resource.                                                                                       |
| `target.name`       | string       | yes      | Name of the specific resource instance.                                                                                     |
| `target.namespace`  | string       | yes      | Namespace of the targeted resource; empty `""` for cluster-scoped kinds.                                                    |
| `expression`        | string (CEL) | yes      | Health verdict. Must return one of `Healthy`, `Progressing`, `Degraded`, `Suspended`, `Unknown`. Bindings: `object`, `now`. |
| `progressDeadline`  | duration     | no       | How long a non-terminal (`Progressing`) state may persist before the resource is treated as `Degraded`.                     |
| `messageExpression` | string (CEL) | no       | Returns a human-readable status message surfaced in the Release's per-resource health.                                      |
