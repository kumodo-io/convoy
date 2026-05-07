**`/preview` and `/diff` Subresources**

<!-- TOC -->
  * [/preview](#preview)
    * [Behavior](#behavior)
    * [Output](#output)
    * [Pin handling](#pin-handling)
    * [Query parameters](#query-parameters)
    * [POST with mutation overrides](#post-with-mutation-overrides)
    * [Common usage patterns](#common-usage-patterns)
    * [RBAC](#rbac)
  * [/diff](#diff)
    * [Behavior](#behavior-1)
    * [Comparison axes](#comparison-axes)
    * [Query parameters](#query-parameters-1)
    * [Common usage](#common-usage)
    * [Pin interaction](#pin-interaction)
    * [POST with mutation overrides](#post-with-mutation-overrides-1)
    * [Output format](#output-format)
    * [Managed-fields filtering](#managed-fields-filtering)
    * [Drift categorization](#drift-categorization)
    * [RBAC](#rbac-1)
  * [Summary](#summary)
<!-- TOC -->

`/preview` and `/diff` are read-only subresources on `Release` that answer two operational questions: "what would deploy?" and "what would change?". Both are essential for safe deployment workflows — letting operators verify intent before triggering a sync.

## /preview

Returns the rendered manifest set that would result from a sync, given the Release's current configuration (or specified overrides). The output is consumable directly: pipeable to `kubectl apply`, validators (`kubeval`, `conftest`, OPA), or human review.

### Behavior

| Verb | Path                       | Purpose                                          |
|------|----------------------------|--------------------------------------------------|
| GET  | `/releases/<name>/preview` | Return rendered manifests                        |
| POST | `/releases/<name>/preview` | Return rendered manifests with overrides applied |

### Output

A multi-document YAML stream (default) or JSON list. The output is the post-mutation manifest set — exactly what would be applied to the cluster on sync. Nothing else: no lifecycle hooks (accessed via `/lifecycle`), no resolved-source metadata (accessed via `/status`), no diff annotations.

### Pin handling

`/preview` always answers "what would `/sync` apply right now?" When pinned, that's the pinned content. When not pinned, it's a fresh render from current spec.

| Release state | Default behavior             |
|---------------|------------------------------|
| Not pinned    | Render from current spec     |
| Pinned        | Return pinned render content |

The default is operationally consistent: `/preview` always shows what the next sync would deploy.

### Query parameters

| Parameter          | Values           | Default                 | Description                                                                                                                                                                                        |
|--------------------|------------------|-------------------------|----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `source`           | `spec`, `pinned` | (smart)                 | Override the default pin handling. `spec` forces fresh render from current spec, ignoring pin. `pinned` requires the Release to be pinned and returns pinned content; returns 400 when not pinned. |
| `cluster`          | cluster name     | Release's bound cluster | Override the cluster context for templating, capabilities, and cluster-specific value selection. The Release itself is not modified.                                                               |
| `clusterNamespace` | namespace        | Release's namespace     | Override the cluster reference's namespace for cross-namespace resolution.                                                                                                                         |

### POST with mutation overrides

For "what would happen if I changed these mutations?" without committing to the Release. Lets users iterate safely — change a value file reference, an inline patch, a templated condition — and see the resulting render before persisting.

```
POST /releases/<name>/preview
Content-Type: application/json
Body: {
  "mutations": [
  {
    "type": "helmValues",
    "from": {
      "sourceRef": { "name": "experimental-values", "path": "test/values.yaml" }
    }
  },
  {
    "type": "jsonPatch",
      "inline": [
        { "op": "replace", "path": "/spec/replicas", "value": 5 }
      ]
    }
  ]
}
```

The provided mutations replace the Release's mutations for this preview only. Query parameters (`source`, `cluster`, `clusterNamespace`) compose with POST overrides:

```
POST /releases/<name>/preview?cluster=prod-staging
Body: { 
  "mutations": [...]
}
```

Returns the render that would result if the Release used these mutations and was deployed to the specified cluster. Useful for "is my mutation change correct, and does it work right in staging?" without two separate operations.

### Common usage patterns

```bash
# What would deploy if I synced right now?
kubectl get release/payments-prod-eu --raw "/.../preview"

# What would deploy if I synced this Release to staging instead?
kubectl get ... --raw "/.../preview?cluster=staging-eu"

# What would deploy if I unpinned?
kubectl get ... --raw "/.../preview?source=spec"

# What if I changed my mutations?
curl -X POST "/.../preview" -d '{"mutations": [...]}'

# Validate against policies
kubectl get ... --raw "/.../preview" | conftest test -
```

### RBAC

| Verb | Permission                 |
|------|----------------------------|
| GET  | `releases/preview: get`    |
| POST | `releases/preview: create` |

When `?cluster=<name>` is used, additionally requires `clusters: get` on the referenced cluster (standard k8s reference-resolution semantics).

## /diff

Returns the differences between two states. The "what would change?" complement to `/preview`'s "what would deploy?". Several comparison axes are supported via the `against` parameter.

### Behavior

| Verb | Path                    | Purpose                                     |
|------|-------------------------|---------------------------------------------|
| GET  | `/releases/<name>/diff` | Return diff between specified states        |
| POST | `/releases/<name>/diff` | Return diff with mutation overrides applied |

### Comparison axes

The `against` parameter controls what's being compared.

| `against`        | Compares                                                                            | Common use case                      |
|------------------|-------------------------------------------------------------------------------------|--------------------------------------|
| `live` (default) | Current rendered output vs live cluster state                                       | "What would change if I synced now?" |
| `pin`            | Current rendered output vs pinned content                                           | "What would change if I unpinned?"   |
| `render`         | Current rendered output vs a specific historical render (or two historical renders) | "What changed between deploys?"      |

### Query parameters

| Parameter          | Used with        | Description                                                                                                                                    |
|--------------------|------------------|------------------------------------------------------------------------------------------------------------------------------------------------|
| `against`          | always           | Comparison axis: `live`, `pin`, or `render`. Default: `live`.                                                                                  |
| `cluster`          | `against=live`   | Compare against the live state of a specified cluster instead of the Release's bound cluster. Useful for cross-cluster comparison.             |
| `clusterNamespace` | `against=live`   | Override cluster reference's namespace.                                                                                                        |
| `renderRef`        | `against=render` | Hash of the render to compare against. Required when `against=render`.                                                                         |
| `compareTo`        | `against=render` | Optional second render hash. When provided, compares the two specified historical renders directly (Release's current render is not involved). |
| `format`           | always           | Output format: `structural` (default), `text` (unified diff).                                                                                  |

### Common usage

```
GET /diff                                                       # against bound cluster's live state
GET /diff?against=live&cluster=staging-eu                       # against staging's live state
GET /diff?against=pin                                           # current render vs pinned content
GET /diff?against=render&renderRef=sha256:abc...                # current render vs historical
GET /diff?against=render&renderRef=sha256:abc...&compareTo=sha256:def...   # two historicals
```

### Pin interaction

When the Release is pinned, "current rendered output" in the comparison defaults to the **pinned content** (matching `/preview`'s default). To diff against the would-be-rendered content from current spec, combine with `/preview?source=spec` reasoning — implementation-side, the user requests `against=pin`, which gives "what would change if I unpinned and synced."

### POST with mutation overrides

Same pattern as `/preview`. Lets users see what diff their mutation changes would produce before committing.

```
POST /releases/<name>/diff?against=live
Content-Type: application/json
Body: {
  "mutations": [...]   // overrides Release's mutations for this diff
}
```

Returns the diff between the specified comparison target and a render produced with the override mutations. Combinable with all GET query parameters.

### Output format

**Structural diff (default):** A JSON document describing changes per resource. Format:

```json
{
  "resources": [
    {
      "kind": "Deployment",
      "apiGroup": "apps",
      "version": "v1",
      "name": "payments-api",
      "namespace": "payments-team",
      "changeType": "Modified",
      "fieldChanges": [
        { "path": "/spec/replicas", "before": 3, "after": 5 },
        { "path": "/spec/template/spec/containers/0/image", "before": "...:v1.2.0", "after": "...:v1.3.0" }
      ]
    },
    {
      "kind": "ConfigMap",
      "apiGroup": "",
      "version": "v1",
      "name": "payments-config",
      "namespace": "payments-team",
      "changeType": "Added",
      "manifest": { ... }
    }
  ],
  "summary": {
    "added": 1,
    "modified": 1,
    "deleted": 0,
    "unchanged": 12
  }
}
```

`changeType` values: `Added`, `Modified`, `Deleted`, `Unchanged` (omitted from output by default; included if requested via `?include=unchanged`).

**Text diff (`?format=text`):** Unified diff of YAML representations, one block per modified resource. Easier for human consumption; pipeable to standard diff-viewer tools.

### Managed-fields filtering

The diff filters out fields that aren't owned by the Release, to avoid showing noise that the user didn't cause. Specifically:

- Fields populated by other field managers (HPA scaling `replicas`, defaulted by admission, set by other controllers) are excluded
- Fields the apiserver sets at apply time (`creationTimestamp`, `resourceVersion`, `uid`) are excluded
- Status fields are excluded (status is observed state, not desired state)

This makes the diff output reflect "differences caused by this Release's intent," not "all bytes that differ between rendered and live."

### Drift categorization

When `against=live` and field-level drift exists, the diff categorizes:

- **Drift** — field is managed by this Release but the live value differs (someone manually edited the resource, or a controller fighting with field ownership)
- **External** — field is not managed by this Release but exists in the live state (HPA-managed replicas, etc.) — informational only, not flagged as drift

Categorization appears in the structural output as `changeReason` per field change.

### RBAC

| Verb | Permission              |
|------|-------------------------|
| GET  | `releases/diff: get`    |
| POST | `releases/diff: create` |

When `?cluster=<name>` is used (with `against=live`), additionally requires `clusters: get` on the referenced cluster.

## Summary

| Aspect            | `/preview`                         | `/diff`                                                     |
|-------------------|------------------------------------|-------------------------------------------------------------|
| Purpose           | "What would deploy?"               | "What would change?"                                        |
| Verbs             | GET, POST                          | GET, POST                                                   |
| Default behavior  | Render with current config         | Diff current render vs live cluster                         |
| Pin handling      | Default returns pinned when pinned | Default uses pinned content as "current render" when pinned |
| Cluster override  | `?cluster=<name>`                  | `?cluster=<name>` (with `against=live`)                     |
| Mutation override | POST with `mutations` body         | POST with `mutations` body                                  |
| Lifecycle hooks   | Not included                       | Not included                                                |
| Output            | YAML stream / JSON list            | Structural JSON (default) / text diff                       |
| Field filtering   | n/a                                | Managed-fields filtering; drift categorization              |
| Format options    | YAML (default), JSON               | structural (default), text                                  |

Both subresources share the same parameter conventions (`?cluster=`, mutation override via POST), making them ergonomically consistent. `/preview` is for content; `/diff` is for differences. They compose: render via `/preview`, validate via `/diff` against live or pin, then `/sync` when satisfied.


