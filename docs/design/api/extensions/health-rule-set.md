<!-- TOC -->
  * [Example](#example)
  * [Spec](#spec)
<!-- TOC -->

`HealthRuleSet` is a **namespaced** resource in the `extensions.convoy.kumodo.io/v1alpha1` API group. It is the namespaced counterpart of the cluster-scoped [`GlobalHealthRuleSet`](global-health-rule-set.md): same shape (`spec.rules[]`), same evaluation model, but scoped to a namespace so a team can define health rules alongside the Releases in their own namespace.

**Relationship to Release:** a Release in the same namespace opts in via a **label selector**, `spec.healthAssessment.localRuleSetSelector`. Rules from selected `HealthRuleSet`s (namespaced) and `GlobalHealthRuleSet`s (cluster-scoped) are **additive** — they are concatenated into one flat list and evaluated together. Neither scope overrides the other; there is no precedence.

**Expression language:** identical to `GlobalHealthRuleSet` — CEL predicates over the single `object` binding, returning a bool; state is derived (`Healthy`/`Progressing`/`Degraded`/`Unknown`) from pass/fail plus the Release-level deadline. No `now` binding, no `Suspended` state, no per-rule deadline.

## Example

```yaml
apiVersion: extensions.convoy.kumodo.io/v1alpha1
kind: HealthRuleSet
metadata:
  name: payments-rules
  namespace: payments-team
  labels:
    team: payments                        # a Release's localRuleSetSelector matches this
spec:
  rules:
    - apiVersion: kafka.strimzi.io/v1beta2
      resources: [kafkas]
      ruleExpression: >-
        has(object.status.conditions) &&
        object.status.conditions.exists(c, c.type == 'Ready' && c.status == 'True')
      messageExpression: "'waiting for Kafka to become ready'"
```

## Spec

Identical to [`GlobalHealthRuleSet`](global-health-rule-set.md#spec):

| Field                | Type              | Required | Description                                                                                                     |
|----------------------|-------------------|----------|---------------------------------------------------------------------------------------------------------------|
| `rules`              | list              | yes      | The rules carried by this set.                                                                                 |
| `rules[].apiVersion` | string            | yes      | Target API group/version. Must be non-empty.                                                                  |
| `rules[].resources`  | list of string    | yes      | Target resource names (plural, lowercase). Matching is by resource, not by kind/name.                         |
| `rules[].ruleExpression`    | string (CEL) | yes | Predicate returning a **bool**. Only binding: `object`. Must be non-empty.                                     |
| `rules[].messageExpression` | string (CEL) | no  | Returns a human-readable message surfaced in per-resource health.                                             |

Matching, aggregation (conjunction; unmatched resources are `Healthy`), and state derivation are exactly as documented for [`GlobalHealthRuleSet`](global-health-rule-set.md#matching-and-evaluation).
