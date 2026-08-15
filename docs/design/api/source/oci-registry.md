<!-- TOC -->
  * [Example](#example)
  * [Spec](#spec)
  * [Authentication Methods](#authentication-methods)
    * [basic](#basic)
    * [token](#token)
    * [dockerConfig](#dockerconfig)
    * [oidc](#oidc)
    * [cloudIdentity](#cloudidentity)
  * [Content Types](#content-types)
  * [Status](#status)
  * [Conditions](#conditions)
  * [Webhooks](#webhooks) — planned
    * [Purpose](#purpose)
    * [Schema](#schema)
    * [Verbs](#verbs)
    * [Common Fields](#common-fields)
    * [Per-Kind Variations](#per-kind-variations)
      * [`OCIRegistry` webhooks](#ociregistry-webhooks)
    * [Status Reflection](#status-reflection)
    * [Status Fields per Entry](#status-fields-per-entry)
    * [RBAC](#rbac)
    * [Defaults and Auto-Generation](#defaults-and-auto-generation)
    * [URL Generation](#url-generation)
<!-- TOC -->

Represents an OCI registry holding Helm charts packaged as OCI artifacts. OCI is a generic artifact format, and the API's `defaultContentType` enumerates `helm`/`manifests`/`kustomize`, but **only `helm` content is fetchable today** — the fetcher always pulls a Helm chart; `manifests`/`kustomize` are accepted by the API but not yet implemented. OCI registries are common for chart distribution (Harbor, ghcr.io, GitLab, ECR, ACR, GAR).

**Subresources**

- `/status` — observed reconciliation state (the only subresource). Re-probing is driven by the controller's interval/resync. (`/sync` exists only on `Release`; `/webhooks` is not implemented — see [Webhooks](#webhooks).)

## Example

```yaml
apiVersion: source.convoy.kumodo.io/v1alpha1
kind: OCIRegistry
metadata:
  name: ghcr-payments
  namespace: payments-team

spec:
  # OCI registry URL. Use the oci:// scheme.
  # Can point to a registry root or a specific repository.
  url: oci://ghcr.io/example/charts
  
  # Authentication — most OCI registries require auth.
  # Discriminated union on auth.type. See "Authentication Methods" below.
  auth:
    # type: none | basic | token | dockerConfig | oidc | cloudIdentity
    # Only none/basic/token are implemented; dockerConfig/oidc/cloudIdentity → Unsupported.
    type: basic
    basic:
      username: deploy
      passwordRef: { name: ghcr-creds, key: password }

    # --------------------------------
    # type: token — bearer token (sent as the basic-auth password to the registry).
    # token:
    #   tokenRef: { name: ghcr-token, key: token }

    # --------------------------------
    # type: dockerConfig — PLANNED (Unsupported). Kubelet-style image pull secret.
    # dockerConfig:
    #   secretRef: { name: ghcr-pull-secret, key: .dockerconfigjson }

    # --------------------------------
    # type: cloudIdentity — PLANNED (Unsupported). Cloud-native identity
    # (IRSA for ECR, Workload Identity for GAR, managed identity for ACR).
    # cloudIdentity:
    #   provider: aws | azure | gcp

    # --------------------------------
    # type: oidc
    # oidc:
    #   OIDC issuer URL — the controller does discovery at /.well-known/openid-configuration
    #   to find the actual token endpoint. Standard OIDC.
    #   issuerURL: https://auth.example.com/realms/internal
    #
    #   Client ID is non-sensitive; typically OK in spec
    #   clientId: cd-controller
    #
    #   Client authentication method (RFC 6749 + OIDC core).
    #   - clientSecret:    standard shared secret (most common)
    #   - privateKeyJWT:   client signs a JWT assertion (more secure, used in regulated envs)
    #   - tlsClientAuth:   mTLS-based client auth (rare)
    #   clientAuth:
    #     type: clientSecret      # | privateKeyJWT | tlsClientAuth
    #     clientSecret:
    #     secretRef:
    #       name: oidc-client
    #       key: client-secret
    #
    #     Alternative methods:
    #     privateKeyJWT:
    #       privateKeyRef: { name: oidc-client-key, key: key.pem }
    #     tlsClientAuth:
    #       certRef: { name: oidc-mtls, key: tls.crt }
    #       keyRef:  { name: oidc-mtls, key: tls.key }
    #
    #   Optional scopes requested in the token. Whether these are honored depends on IdP config.
    #   scopes:
    #     - openid
    #     - read:charts
    #
    #   Optional audience claim. Many IdPs require this to scope tokens to specific resources.
    #   audience: helm-registry
    #
    #   Optional: TLS config for the IdP itself, useful for self-signed corporate IdPs.
    #   tls:
    #     caPublicCertRef: { name: corporate-ca, key: ca.crt }
    #     insecureSkipVerify: false
  
  # How often to check for new tags / artifact updates.
  interval: 5m
  
  minSyncInterval: 10s
  
  # Optional: TLS config for self-signed registries.
  # tls:
  #   caPublicCertRef: { name: corporate-ca, key: ca.crt }
  #   insecureSkipVerify: false
  
  # Default content type for artifacts in this registry. See "Content Types" below.
  # OCI is a generic artifact format — could be Helm charts, Kustomize bundles,
  # or arbitrary manifest archives.
  # - helm:      OCI-packaged Helm charts (mediaType: application/vnd.cncf.helm.config.v1+json)
  # - manifests: tarballs of raw YAML
  # - kustomize: Kustomize bundles
  # Can be overridden per-Release in spec.content.type.
  defaultContentType: helm

# Webhook config is an inline `webhook` array (singular key), a sibling of spec/status.
# NOTE: webhook processing is not implemented (planned) — see "Webhooks".
webhook:
  - name: ghcr
    provider: ghcr
    secretRef: { name: ghcr-webhook-secret, key: secret }
    events: [package-published]

status:
  observedGeneration: 2

  # Last successful reachability/verification probe.
  lastSuccessfulProbeAt: 2026-04-29T09:55:00Z

  # A single Ready condition is written. Reasons: Verified | VerifyFailed | Unsupported.
  conditions:
    - type: Ready
      status: "True"
      lastTransitionTime: 2026-04-29T09:55:00Z
      reason: Verified
```

## Spec

| Field                    | Type     | Required            | Default | Description                                                                                                 |
|--------------------------|----------|---------------------|---------|-------------------------------------------------------------------------------------------------------------|
| `url`                    | string   | yes                 | —       | OCI registry URL with `oci://` scheme.                                                                      |
| `auth`                   | object   | yes                 | —       | Authentication — discriminated union on `auth.type`. Use `type: none` for public registries. See [Authentication Methods](#authentication-methods). |
| `auth.type`              | enum     | yes                 | —       | One of `none`, `basic`, `token`, `dockerConfig`, `oidc`, `cloudIdentity`. Only `none`/`basic`/`token` are implemented. |
| `auth.basic`             | object   | one-of              | —       | HTTP basic auth (when `type: basic`).                                                                       |
| `auth.token`             | object   | one-of              | —       | Bearer token auth (when `type: token`).                                                                     |
| `auth.dockerConfig`      | object   | one-of              | —       | Image pull secret (when `type: dockerConfig`). Not implemented (planned).                                   |
| `auth.oidc`              | object   | one-of              | —       | OIDC client credentials flow (when `type: oidc`). Not implemented (planned).                                |
| `auth.cloudIdentity`     | object   | one-of              | —       | Cloud workload identity (when `type: cloudIdentity`); for ECR/GAR/ACR. Not implemented (planned).           |
| `interval`               | duration | no                  | —       | Polling interval for verification. No server-side default (zero if unset).                                  |
| `minSyncInterval`        | duration | no                  | —       | Defined in the API but currently unused. No default.                                                        |
| `defaultContentType`     | enum     | no                  | —       | `helm`/`manifests`/`kustomize`. Only `helm` is fetchable; no server-side default. See [Content Types](#content-types). |
| `tls`                    | object   | no                  | —       | TLS config for the registry.                                                                                |
| `tls.caPublicCertRef`    | object   | no                  | —       | CA bundle reference (`{name, key}`). **Not yet wired** — only `insecureSkipVerify` is honored today.        |
| `tls.insecureSkipVerify` | bool     | no                  | `false` | Skip TLS verification (not recommended).                                                                    |

## Authentication Methods

`spec.auth` is **required** (a discriminated union on `auth.type`); for a public registry set `type: none`. Implemented types: `none`, `basic`, `token`. `dockerConfig`, `oidc`, and `cloudIdentity` are defined in the API but return `Unsupported` (planned). Note: `token` is sent to the registry as the basic-auth password.

### basic

HTTP basic auth — a `username` (literal string) plus a `passwordRef` (`{name, key}`).

```yaml
spec:
  auth:
    type: basic
    basic:
      username: deploy
      passwordRef: { name: ghcr-creds, key: password }
```

### token

A static bearer token (`tokenRef`), sent to the registry as the basic-auth password.

```yaml
spec:
  auth:
    type: token
    token:
      tokenRef: { name: ghcr-token, key: token }
```

### dockerConfig

_Planned — not implemented (returns `Unsupported`)._ A kubelet-style image pull secret (a `.dockerconfigjson` blob in a Secret).

```yaml
spec:
  auth:
    type: dockerConfig
    dockerConfig:
      secretRef: { name: ghcr-pull-secret, key: .dockerconfigjson }
```

### oidc

_Planned — not implemented (returns `Unsupported`)._ OIDC client-credentials flow (`clientID`, `clientAuth.clientSecret.clientSecretRef`, etc.).

### cloudIdentity

_Planned — not implemented (returns `Unsupported`)._ The controller pod uses its own cloud-native identity (IRSA for ECR, Workload Identity for GAR, managed identity for ACR). No static credentials are stored.

```yaml
spec:
  auth:
    type: cloudIdentity
    cloudIdentity:
      provider: aws   # | gcp | azure
```

## Content Types

`spec.defaultContentType` is an enum (`helm`/`manifests`/`kustomize`), overridable per-`Release` in `spec.content.type`. **Only `helm` is implemented** — the fetcher always pulls a Helm chart packaged as an OCI artifact (`mediaType: application/vnd.cncf.helm.config.v1+json`). `manifests` and `kustomize` are accepted by the API but not yet fetchable. There is no server-side default applied; the fetcher assumes `helm`.

```yaml
spec:
  defaultContentType: helm   # manifests | kustomize accepted but not yet implemented
```

## Status

| Field                   | Type        | Description                                                                                           |
|-------------------------|-------------|-------------------------------------------------------------------------------------------------------|
| `observedGeneration`    | integer     | Generation of `spec` last reconciled.                                                                 |
| `lastSuccessfulProbeAt` | timestamp   | Time of the last successful reachability/verification probe.                                          |
| `conditions`            | []Condition | See [Conditions](#conditions).                                                                        |

(The API type also has a `releaseCount` field, not currently populated. There is no `lastChecked` status.)

## Conditions

`OCIRegistry` reports a **single** condition in `status.conditions`: `Ready`. (There are no separate `Reachable`/`AuthValid`/`WebhookConfigured` conditions — those are not implemented.)

| Condition | What `True` means                                                        | Reasons                                                                       |
|-----------|--------------------------------------------------------------------------|-------------------------------------------------------------------------------|
| `Ready`   | The registry's `/v2/` endpoint was reached successfully (200 or 401). Note this reflects **reachability**, not verified credentials. | `Verified` (reachable); `VerifyFailed` (unreachable / other error); `Unsupported` (auth method or content type not implemented). |

An authentication failure surfaces as `Ready: False` with reason `VerifyFailed` — there is no separate auth condition or auth-specific reason code. Example:

```yaml
status:
  lastSuccessfulProbeAt: 2026-04-29T09:55:00Z
  conditions:
    - type: Ready
      status: "True"
      reason: Verified
```

## Webhooks

> **Status: planned — not implemented.** There is no `/webhooks` subresource and no webhook receiver (no per-entry URL generation, operational status, or provider/event/filter processing). Webhook configuration is expressed today as an inline `webhook` array field (singular key) on the object; the controller does not populate any webhook status. The rest of this section describes the intended design.

### Purpose

The webhook design enables push-based notifications from upstream that trigger source refreshes, supporting scenarios like multi-mirror setups, provider migrations, and chained CI/CD pipelines.

### Schema

The subresource holds a list of named webhook entries under `items`.

```yaml
# GET /apis/convoy.kumodo.io/sources/v1alpha1/namespaces/payments-team/ociregistries/ghcr-payments/webhooks
items:
  - name: github-primary
    provider: ghcr
    secretRef:
      name: payments-github-webhook
      key: secret
    events:
      - package-published
    filters:
      repositories:
        - payments-api
      tags:
        - "v*"
    url: https://convoy.svc.cluster.local/webhooks/8f3a2b1c-4d5e-6f7a-8b9c-0d1e2f3a4b5c
  
  - name: harbor-mirror
    provider: harbor
    secretRef:
      name: payments-harbor-webhook
      key: secret
    events:
      - PUSH_ARTIFACT
    url: https://convoy.svc.cluster.local/webhooks/2a4b6c8d-1e3f-5a7b-9c0d-2e4f6a8b0c1d
```

Each entry's `url` is server-generated and returned by GET/POST; it is not accepted as input. Users copy the URL into the upstream provider's webhook configuration.

### Verbs

| Path               | Verb   | Purpose                                                                 |
|--------------------|--------|-------------------------------------------------------------------------|
| `/webhooks`        | GET    | List all webhook entries for the Source.                                |
| `/webhooks`        | POST   | Create a new webhook entry. Server may auto-generate a name if omitted. |
| `/webhooks`        | DELETE | Remove all webhook entries (rare but convenient).                       |
| `/webhooks/<name>` | GET    | Read a specific webhook entry.                                          |
| `/webhooks/<name>` | PUT    | Update a specific webhook entry.                                        |
| `/webhooks/<name>` | DELETE | Remove a specific webhook entry.                                        |

### Common Fields

| Field            | Type     | Required             | Default                             | Description                                                                                                                                         |
|------------------|----------|----------------------|-------------------------------------|-----------------------------------------------------------------------------------------------------------------------------------------------------|
| `name`           | string   | yes                  | server-generated on POST if omitted | Unique within the Source. Used for addressability via `/webhooks/<name>`. Stable across edits; does not change once assigned.                       |
| `provider`       | enum     | yes                  | —                                   | Provider type. Drives signature verification scheme and payload parser. Per-kind enum values listed below.                                          |
| `secretRef`      | object   | yes (most providers) | —                                   | Reference to a Secret containing the shared secret used for HMAC verification (or bearer token for `generic` provider).                             |
| `secretRef.name` | string   | yes                  | —                                   | Secret name in the same namespace as the Source.                                                                                                    |
| `secretRef.key`  | string   | yes                  | —                                   | Key within the Secret holding the secret value.                                                                                                     |
| `events`         | []string | no                   | provider-specific                   | Provider event types that trigger sync. Defaults vary per provider.                                                                                 |
| `filters`        | object   | no                   | —                                   | Optional payload-based filtering. Specific fields vary by Source kind.                                                                              |
| `url`            | string   | server-generated     | —                                   | Endpoint URL for the upstream provider to call. Returned in GET/POST responses; mirrored in `status.webhooks[].url`. Each entry has a distinct URL. |

### Per-Kind Variations

#### `OCIRegistry` webhooks

**Provider enum values:**

| Value         | Description                                                                                            |
|---------------|--------------------------------------------------------------------------------------------------------|
| `harbor`      | Harbor. CloudEvents-style payloads.                                                                    |
| `acr`         | Azure Container Registry. HTTP webhook support.                                                        |
| `dockerHub`   | Docker Hub. Per-repository webhook config.                                                             |
| `quay`        | Quay.io. Notification system supporting webhooks.                                                      |
| `ghcr`        | GitHub Container Registry (via GitHub `package` events).                                               |
| `gitlab`      | GitLab Container Registry (via GitLab generic webhook system).                                         |
| `artifactory` | JFrog Artifactory with OCI support.                                                                    |
| `nexus`       | Sonatype Nexus 3 with Docker/OCI support.                                                              |
| `generic`     | Any system that can POST. ECR and GAR are typically served via a bridge component using this provider. |

**Filter fields:**

| Field                  | Type     | Description                                                    |
|------------------------|----------|----------------------------------------------------------------|
| `filters.repositories` | []string | Only trigger sync when matching repository names are affected. |
| `filters.tags`         | []string | Only trigger sync for matching tag patterns (supports globs).  |

**Note on ECR / GAR:** AWS ECR uses EventBridge; Google Artifact Registry uses Pub/Sub. Neither sends direct HTTP webhooks. A small bridge component (Lambda or Cloud Function) consumes events from these systems and POSTs to the `generic` webhook endpoint. Bridge implementation is provided as an optional, separately-installable helper.

### Status Reflection

`status.webhooks` mirrors operational state per webhook entry. Sensitive config (secret reference, filters) stays behind the subresource; status surfaces only non-sensitive operational facts.

```yaml
status:
  webhooks:
    - name: github-primary
      url: https://convoy.svc.cluster.local/webhooks/8f3a2b1c-4d5e-6f7a-8b9c-0d1e2f3a4b5c
      lastReceived: 2026-04-29T10:00:00Z
      lastTriggered: 2026-04-29T10:00:00Z
      receivedCount: 142
      triggersCount: 87
      rejectedCount: 3
      filteredCount: 52
      lastRejection:
        rejectedAt: 2026-04-29T08:30:00Z
        reason: SignatureMismatch
        message: "HMAC signature did not match expected value"
    
    - name: harbor-mirror
      url: https://convoy.svc.cluster.local/webhooks/2a4b6c8d-1e3f-5a7b-9c0d-2e4f6a8b0c1d
      lastReceived: 2026-04-29T09:50:00Z
      lastTriggered: 2026-04-29T09:50:00Z
      receivedCount: 18
      triggersCount: 18
      rejectedCount: 0
      filteredCount: 0
  
  conditions:
    - type: WebhookConfigured
      status: "True"
      lastTransitionTime: 2026-04-29T08:00:00Z
      reason: WebhooksActive
      message: "2 webhooks configured; all receiving events"
```

The `WebhookConfigured` condition is `True` if at least one webhook is configured. Per-entry health is in `status.webhooks[].rejectedCount` and `lastRejection`; aggregate failures across entries trigger condition transitions to `False` with appropriate reasons.

### Status Fields per Entry

| Field                      | Type      | Description                                                                                        |
|----------------------------|-----------|----------------------------------------------------------------------------------------------------|
| `name`                     | string    | Matches the entry's name in the subresource.                                                       |
| `url`                      | string    | Endpoint URL (mirrored from subresource for visibility).                                           |
| `lastReceived`             | timestamp | Most recent webhook delivery received (regardless of outcome).                                     |
| `lastTriggered`            | timestamp | Most recent delivery that passed verification and filters and triggered sync.                      |
| `receivedCount`            | integer   | Total deliveries received since last status reset.                                                 |
| `triggersCount`            | integer   | Deliveries that resulted in a sync.                                                                |
| `rejectedCount`            | integer   | Deliveries rejected (signature failures, malformed payloads).                                      |
| `filteredCount`            | integer   | Deliveries that passed verification but were filtered out (events/repositories/tags didn't match). |
| `lastRejection`            | object    | Details of the most recent rejection, if any.                                                      |
| `lastRejection.rejectedAt` | timestamp | When the rejection occurred.                                                                       |
| `lastRejection.reason`     | string    | Standardized reason: `SignatureMismatch`, `MalformedPayload`, `UnknownEvent`, `SecretNotFound`.    |
| `lastRejection.message`    | string    | Human-readable detail.                                                                             |

### RBAC

Standard Kubernetes RBAC operates at the subresource level — `ociregistries/webhooks` — not at individual entry granularity. Granting webhook-edit permissions on a Source allows managing all webhook entries on that Source.

```yaml
# Platform team — manages source definitions but not webhook config
- apiGroups: ["source.convoy.kumodo.io"]
  resources: ["ociregistries"]
  verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]

# App team — manages webhook integrations only
- apiGroups: ["source.convoy.kumodo.io"]
  resources:
    - "ociregistries/webhooks"
  verbs: ["get", "create", "update", "delete"]
```

For per-webhook access control (rare), users can:
- Use separate Sources (recommended for cleanly isolated webhooks).
- Apply `ValidatingAdmissionPolicy` with CEL expressions on the subresource (Kubernetes 1.30+).

### Defaults and Auto-Generation

To avoid penalizing the common case of one webhook per Source:

- POST without a `name` auto-generates a name based on `provider` (e.g., `harbor-1`, `ghcr-1`). Subsequent POSTs of the same provider increment the suffix.
- Single-webhook users can ignore names entirely and treat the subresource as effectively singular by addressing the auto-generated entry.

### URL Generation

Webhook URLs use opaque, server-generated identifiers: `https://<control-plane-host>/webhooks/<webhook-uuid>`

The UUID is generated when the webhook entry is created and remains stable for the lifetime of that entry. Deletion and recreation of the same-named entry produces a new UUID and new URL — users must reconfigure the upstream provider with the new URL.

This pattern is preferred over Source-identifying URLs (e.g., `/webhooks/sources/<namespace>/<source-name>/<webhook-name>`) because:
- It avoids leaking namespace/Source naming structure to the public webhook endpoint.
- Webhook URL rotation (regenerating the UUID) doesn't require changing Source or webhook names.
- Each entry has a distinct URL, mapping cleanly to upstream provider configurations.
