<!-- TOC -->
  * [Example](#example)
  * [Spec](#spec)
<!-- TOC -->

`KindHealthRule` is a **cluster-scoped** resource in the `extensions.convoy.kumodo.io/v1alpha1` API group. It defines a custom health rule that applies to all resources of a given kind across the entire cluster, regardless of which `Release` manages them. It is part of the health assessment system — see `core/release.md` ("Health Assessment") for how rules are resolved (relative to built-in rules, kstatus, and `ResourceHealthRule`) and how per-resource verdicts aggregate into the Release-level `Healthy` condition.

**Targeting:** Each `KindHealthRule` targets exactly one `(apiGroup, kind)`. Validating admission rejects creation of a second `KindHealthRule` with the same target — to change behavior, edit the existing rule.

**Relationship to Release:** None. The controller indexes `KindHealthRule` by `(apiGroup, kind, version)` and looks up the applicable rule by the resource's kind, regardless of which Release manages it. This ensures consistent health verdicts across the system.

**Expression language:** CEL, with bindings for the live resource (`object`) and the current time (`now`). The `expression` must return one of `Healthy`, `Progressing`, `Degraded`, `Suspended`, `Unknown`.

## Example

```yaml
apiVersion: extensions.convoy.kumodo.io/v1alpha1
kind: KindHealthRule
metadata:
  name: certificate-health
spec:
  target:
    apiGroup: cert-manager.io
    kind: Certificate
    version: v1
  
  expression: |
    has(object.status.conditions) && object.status.conditions.exists(c, 
      c.type == "Ready" && c.status == "True"
    ) ? "Healthy" :
    has(object.status.conditions) && object.status.conditions.exists(c,
      c.type == "Ready" && c.status == "False"
    ) ? "Degraded" :
    "Progressing"
  
  progressDeadline: 5m
  
  messageExpression: |
    has(object.status.conditions) && object.status.conditions.exists(c, c.type == "Ready") 
      ? object.status.conditions.filter(c, c.type == "Ready")[0].message
      : "Waiting for Certificate to become ready"
```

## Spec

| Field               | Type         | Required | Description                                                                                                                 |
|---------------------|--------------|----------|-----------------------------------------------------------------------------------------------------------------------------|
| `target.apiGroup`   | string       | yes      | API group of the targeted kind (empty string `""` for core group).                                                          |
| `target.kind`       | string       | yes      | Kind this rule applies to.                                                                                                  |
| `target.version`    | string       | yes      | API version of the targeted kind.                                                                                           |
| `expression`        | string (CEL) | yes      | Health verdict. Must return one of `Healthy`, `Progressing`, `Degraded`, `Suspended`, `Unknown`. Bindings: `object`, `now`. |
| `progressDeadline`  | duration     | no       | How long a non-terminal (`Progressing`) state may persist before the resource is treated as `Degraded`.                     |
| `messageExpression` | string (CEL) | no       | Returns a human-readable status message surfaced in the Release's per-resource health.                                      |
