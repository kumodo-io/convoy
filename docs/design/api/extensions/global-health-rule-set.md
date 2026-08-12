<!-- TOC -->
  * [Example](#example)
  * [Spec](#spec)
  * [Matching and evaluation](#matching-and-evaluation)
<!-- TOC -->

`GlobalHealthRuleSet` is a **cluster-scoped** resource in the `extensions.convoy.kumodo.io/v1alpha1` API group. It carries a *list* of health rules (`spec.rules[]`); each rule targets a set of resources by API version + resource name and provides a CEL predicate that decides whether a matched resource is passing. It is part of the health assessment system — see `core/release.md` ("Health Assessment") for how per-resource verdicts aggregate into the Release-level `Healthy` condition.

**Relationship to Release:** a Release opts into a rule set via a **label selector**, `spec.healthAssessment.globalRuleSetSelector`. A `GlobalHealthRuleSet` whose labels don't match any Release's selector is never consulted (opt-in per scope). Namespaced [`HealthRuleSet`](health-rule-set.md)s are selected the same way via `localRuleSetSelector`; rules from both scopes are additive.

**Expression language:** CEL. The only binding is `object` (the live resource). `ruleExpression` must return a **bool** (`true` = the resource passes). The health *state* (`Healthy`/`Progressing`/`Degraded`/`Unknown`) is **derived** from the pass/fail result plus the Release-level progress deadline — the expression never names a state. There is no `now` binding and no `Suspended` state.

## Example

```yaml
apiVersion: extensions.convoy.kumodo.io/v1alpha1
kind: GlobalHealthRuleSet
metadata:
  name: convoy-defaults
  labels:
    convoy.kumodo.io/default: "true"      # a Release's globalRuleSetSelector matches this
spec:
  rules:
    - apiVersion: apps/v1
      resources: [deployments]
      # Predicate: returns true when the Deployment is fully rolled out.
      ruleExpression: >-
        has(object.status) && has(object.status.observedGeneration) &&
        object.status.observedGeneration == object.metadata.generation &&
        (object.spec.replicas == 0 ||
         (has(object.status.availableReplicas) && object.status.availableReplicas == object.spec.replicas))
      messageExpression: >-
        (has(object.status.availableReplicas) ? string(object.status.availableReplicas) : '0') +
        '/' + string(object.spec.replicas) + ' replicas available'

    - apiVersion: cert-manager.io/v1
      resources: [certificates]
      ruleExpression: >-
        has(object.status.conditions) &&
        object.status.conditions.exists(c, c.type == 'Ready' && c.status == 'True')
      messageExpression: >-
        has(object.status.conditions) && object.status.conditions.exists(c, c.type == 'Ready')
          ? object.status.conditions.filter(c, c.type == 'Ready')[0].message
          : 'waiting for Certificate to become ready'
```

## Spec

| Field                | Type              | Required | Description                                                                                                       |
|----------------------|-------------------|----------|-------------------------------------------------------------------------------------------------------------------|
| `rules`              | list              | yes      | The rules carried by this set.                                                                                    |
| `rules[].apiVersion` | string            | yes      | Target API group/version, e.g. `apps/v1` or `v1` (core). Must be non-empty.                                       |
| `rules[].resources`  | list of string    | yes      | Target **resource** names (plural, lowercase), e.g. `[deployments]`. Matching is by resource, not by kind/name.   |
| `rules[].ruleExpression`    | string (CEL) | yes | Predicate returning a **bool** (`true` = passes). Only binding: `object`. Must be non-empty.                       |
| `rules[].messageExpression` | string (CEL) | no  | Returns a human-readable message surfaced in per-resource health.                                                 |

Validation on create/update requires each rule to have a non-empty `apiVersion` and `ruleExpression`. There is no per-rule progress deadline; the deadline is a single Release-level setting (`Release.spec.healthAssessment.deadline`) applied to every assessed resource.

## Matching and evaluation

- **Matching** is by `apiVersion` + one of `resources` — deliberately **name-independent**, so a rule keeps applying across rendering-time renames.
- **Aggregation** is a conjunction: a resource is `Healthy` when *every* applicable rule passes. The first failing rule supplies the message. A resource with **no** applicable rule is `Healthy` (vacuous pass) — there are no built-in rules and no kstatus fallback.
- **State derivation**: a failing predicate is `Progressing` until the Release-level deadline elapses (measured from when the resource first went non-passing), then `Degraded`. An evaluation error (or an unreadable resource) yields `Unknown`. The four states are `Healthy`, `Progressing`, `Degraded`, `Unknown`.
