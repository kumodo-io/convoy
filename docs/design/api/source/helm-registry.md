<!-- TOC -->
  * [Example](#example)
  * [Spec](#spec)
  * [Authentication Methods](#authentication-methods)
    * [basic](#basic)
    * [token](#token)
    * [oidc](#oidc)
    * [workloadIdentity](#workloadidentity)
  * [Status](#status)
  * [Conditions](#conditions)
    * [AuthValid](#authvalid)
  * [Webhooks](#webhooks)
    * [Purpose](#purpose)
    * [Schema](#schema)
    * [Verbs](#verbs)
    * [Common Fields](#common-fields)
    * [Per-Kind Variations](#per-kind-variations)
      * [`HelmRegistry` webhooks](#helmregistry-webhooks)
    * [Status Reflection](#status-reflection)
    * [Status Fields per Entry](#status-fields-per-entry)
    * [RBAC](#rbac)
    * [Defaults and Auto-Generation](#defaults-and-auto-generation)
    * [URL Generation](#url-generation)
<!-- TOC -->

Represents a Helm HTTP chart repository — a server providing a chart index (`index.yaml`) and chart artifacts. The controller refreshes the index periodically so that `Release` objects can pull charts by name and version constraint. Intended for HTTP-based Helm repositories (the original Helm registry format); for OCI-based Helm chart distribution, use `OCIRegistry` instead.

**Subresources**

- `/status` — observed reconciliation state
- `/sync` — POST to force re-fetch of the chart index (subject to `minSyncInterval`)
- `/webhooks` — GET / POST / DELETE webhook configuration; see [Webhooks](#webhooks)

## Example

```yaml
apiVersion: source.convoy.kumodo.io/v1alpha1
kind: HelmRegistry
metadata:
  name: bitnami
  namespace: payments-team

spec:
  # Helm HTTP repository URL (the index.yaml location).
  url: https://charts.bitnami.com/bitnami
  
  # Authentication — optional, for private repos.
  # Discriminated union on auth.type. See "Authentication Methods" below.
  auth:
    # type: basic | token | oidc | workloadIdentity
    type: basic
    
    basic:
      usernameRef: { secretRef: { name: bitnami-creds, key: username } }
      passwordRef: { secretRef: { name: bitnami-creds, key: password } }

    # --------------------------------
    # type: token — bearer token auth, less common for HelmRegistry but supported.
    # token:
    #   secretRef: { name: bitnami-token, key: token }

    # --------------------------------
    # oidc:
    # OIDC issuer URL — the controller does discovery at /.well-known/openid-configuration
    # to find the actual token endpoint. Standard OIDC.
    # issuerURL: https://auth.example.com/realms/internal

    # Client ID is non-sensitive; typically OK in spec
    # clientId: cd-controller

    # Client authentication method (RFC 6749 + OIDC core).
    # - clientSecret:    standard shared secret (most common)
    # - privateKeyJWT:   client signs a JWT assertion (more secure, used in regulated envs)
    # - tlsClientAuth:   mTLS-based client auth (rare)
    # clientAuth:
    #   type: clientSecret      # | privateKeyJWT | tlsClientAuth
    #   clientSecret:
    #   secretRef:
    #     name: oidc-client
    #     key: client-secret
    #   Alternative methods:
    #   privateKeyJWT:
    #     privateKeyRef: { secretRef: { name: oidc-client-key, key: key.pem } }
    #   tlsClientAuth:
    #     certRef: { secretRef: { name: oidc-mtls, key: tls.crt } }
    #     keyRef:  { secretRef: { name: oidc-mtls, key: tls.key } }
  
  # How often to refresh the chart index.
  # Helm registries change less frequently than Git; longer intervals are fine.
  interval: 5m
  
  # Optional: TLS config for self-signed or custom CA registries.
  # tls:
  #   caBundleRef: { configMapRef: { name: corporate-ca, key: ca.crt } }
  #   insecureSkipVerify: false

webhook:
  - name: harbor
    provider: harbor
    secretRef:
      name: harbor-webhook-secret
      key: secret
    events:
      - chart-uploaded
    filters:
      charts:
        - payments-api
        - payments-worker

status:
  observedGeneration: 2
  
  # Last successful index fetch.
  lastFetched:
    fetchedAt: 2026-04-29T09:55:00Z
    # Number of charts available in the index (informational).
    chartCount: 87
  
  releaseCount: 12

  # Webhook operational state — see "Webhooks" below.
  webhooks:
    - name: harbor
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

  conditions:
    - type: Ready
      status: "True"
      lastTransitionTime: 2026-04-29T09:55:00Z
      reason: IndexFetched
    - type: Reachable
      status: "True"
    - type: AuthValid
      status: "True"
      reason: CredentialsAccepted
    - type: WebhookConfigured
      status: "True"
      reason: WebhookActive
```

## Spec

| Field                    | Type     | Required            | Default | Description                                                                                                                        |
|--------------------------|----------|---------------------|---------|------------------------------------------------------------------------------------------------------------------------------------|
| `url`                    | string   | yes                 | —       | Helm HTTP repository URL serving `index.yaml`.                                                                                     |
| `auth`                   | object   | no                  | —       | Authentication — discriminated union on `auth.type`. Omit for public repos. See [Authentication Methods](#authentication-methods). |
| `auth.type`              | enum     | yes (if `auth` set) | —       | One of `basic`, `token`, `oidc`, `workloadIdentity`.                                                              |
| `interval`               | duration | no                  | `5m`    | Polling interval for index refresh.                                                                                                |
| `minSyncInterval`        | duration | no                  | `10s`   | Minimum interval between manual/webhook-triggered syncs.                                                                           |
| `tls`                    | object   | no                  | —       | TLS config for the registry.                                                                                                       |
| `tls.caBundleRef`        | object   | no                  | —       | Reference to a ConfigMap containing the CA bundle.                                                                                 |
| `tls.insecureSkipVerify` | bool     | no                  | `false` | Skip TLS verification (not recommended).                                                                                           |

## Authentication Methods

`spec.auth` is a discriminated union on `auth.type`. The active method's block is set; all others are omitted. Omit `auth` entirely for public repositories.

### basic

HTTP basic auth — username and password from a Secret. The most common method for private Helm HTTP repositories.

```yaml
spec:
  auth:
    type: basic
    basic:
      usernameRef: { secretRef: { name: bitnami-creds, key: username } }
      passwordRef: { secretRef: { name: bitnami-creds, key: password } }
```

### token

A static bearer token sent in the `Authorization` header. Less common for Helm HTTP repositories but supported.

```yaml
spec:
  auth:
    type: token
    token:
      secretRef: { name: bitnami-token, key: token }
```

### oidc

OIDC client-credentials flow. The controller discovers the token endpoint via `/.well-known/openid-configuration` on the issuer and exchanges client credentials for a token. Client authentication itself is a nested discriminated union (`clientAuth.type`): `clientSecret` (shared secret, most common), `privateKeyJWT` (signed JWT assertion, used in regulated envs), or `tlsClientAuth` (mTLS, rare).

```yaml
spec:
  auth:
    type: oidc
    oidc:
      issuerURL: https://auth.example.com/realms/internal
      clientId: cd-controller
      clientAuth:
        type: clientSecret           # | privateKeyJWT | tlsClientAuth
        clientSecret:
          secretRef: { name: oidc-client, key: client-secret }
        # Alternative methods:
        # privateKeyJWT:
        #   privateKeyRef: { secretRef: { name: oidc-client-key, key: key.pem } }
        # tlsClientAuth:
        #   certRef: { secretRef: { name: oidc-mtls, key: tls.crt } }
        #   keyRef:  { secretRef: { name: oidc-mtls, key: tls.key } }
```

### workloadIdentity

The controller pod uses its own cloud-native identity (e.g. Workload Identity on GKE) to authenticate. No static credentials are stored. Used for cloud-hosted Helm registries such as Google Artifact Registry.

```yaml
spec:
  auth:
    type: workloadIdentity
```

## Status

| Field                    | Type        | Description                                                                       |
|--------------------------|-------------|-----------------------------------------------------------------------------------|
| `observedGeneration`     | integer     | Generation of `spec` last reconciled.                                             |
| `lastFetched`            | object      | Last successful index fetch.                                                      |
| `lastFetched.fetchedAt`  | timestamp   | When the index was fetched.                                                       |
| `lastFetched.chartCount` | integer     | Number of charts in the index (informational).                                    |
| `releaseCount`           | integer     | Number of `Release` objects referencing this registry.                            |
| `webhooks`               | []object    | Per-entry webhook operational state. See [Webhooks](#webhooks).                   |
| `conditions`             | []Condition | See [Conditions](#conditions).                                                    |

## Conditions

`HelmRegistry` reports the following standard k8s conditions in `status.conditions`.

| Condition           | What `True` means                                                                                                                                                                  |
|---------------------|------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `Ready`             | Composite: the registry is reachable, authenticated, and its chart index has been fetched. The signal most tooling should watch. Requires `Reachable: True` and `AuthValid: True`. |
| `Reachable`         | Network-level reachability of the registry endpoint succeeded.                                                                                                                     |
| `AuthValid`         | The most recent authentication attempt succeeded. See [AuthValid](#authvalid) for full semantics.                                                                                  |
| `WebhookConfigured` | At least one webhook is configured and accepting events. See [Webhooks](#webhooks).                                                                                                |

### AuthValid

`AuthValid` reflects the state of authenticating to the registry. It is always present, even when no explicit credentials are configured (public repos report `True` with `reason: NoAuthRequired`).

**Status semantics:**

| Status    | Meaning                                                                                                     |
|-----------|-------------------------------------------------------------------------------------------------------------|
| `True`    | Most recent authentication attempt succeeded. Credentials are working.                                      |
| `False`   | Most recent authentication attempt failed for an auth-specific reason. Actionable.                          |
| `Unknown` | Authentication has not been attempted yet, OR a non-auth failure (network, timeout) prevented testing auth. |

`Unknown` must not be conflated with `False`: "credentials are bad" is a different problem from "we couldn't test the credentials."

**Reason codes:**

| Status    | Reason                   | Meaning                                                                           |
|-----------|--------------------------|-----------------------------------------------------------------------------------|
| `True`    | `TokenAcquired`          | Succeeded via explicit token acquisition (OIDC, workload identity).               |
| `True`    | `CredentialsAccepted`    | Succeeded with simple credentials (basic auth, token).                            |
| `True`    | `NoAuthRequired`         | No authentication is configured; the repository is public.                        |
| `False`   | `InvalidCredentials`     | Credentials rejected by the registry (401-class).                                 |
| `False`   | `TokenAcquisitionFailed` | OIDC or workload-identity token exchange failed.                                  |
| `False`   | `SecretNotFound`         | Referenced Secret or ConfigMap does not exist.                                    |
| `False`   | `SecretMalformed`        | Secret exists but content is invalid (bad base64, wrong format, missing keys).    |
| `False`   | `PermissionDenied`       | Credentials valid but lack required permissions (403-class).                      |
| `False`   | `TokenExpired`           | Known-expired token; refresh attempt failed.                                      |
| `False`   | `AuthMisconfigured`      | Config error detected without contacting the registry (e.g. invalid issuer URL).  |
| `Unknown` | `NotYetAttempted`        | Registry is new; auth has not yet been tested.                                    |
| `Unknown` | `RemoteUnreachable`      | Could not contact the registry to test auth (network, DNS, firewall).             |

**Triage with `Reachable`:**

| `Reachable` | `AuthValid` | Action                                                                              |
|-------------|-------------|-------------------------------------------------------------------------------------|
| `False`     | `Unknown`   | Fix network, DNS, firewall, or registry endpoint availability.                      |
| `True`      | `False`     | Fix credentials or auth configuration.                                              |
| `True`      | `True`      | If `Ready: False`, look beyond auth (rate limits, IdP misconfig, content errors).   |

`AuthValid: False` always forces `Ready: False`, with `Ready.reason: AuthFailed`.

**Example — OIDC client credentials failure:**

```yaml
status:
  conditions:
    - type: Ready
      status: "False"
      reason: AuthFailed
    - type: Reachable
      status: "True"
    - type: AuthValid
      status: "False"
      reason: TokenAcquisitionFailed
      message: "Failed to acquire OIDC token from https://auth.example.com/realms/internal: invalid_client (client authentication failed)"
```

## Webhooks

### Purpose

The `/webhooks` subresource manages webhook configurations for the registry, enabling push-based notifications from upstream systems that trigger index refreshes. Multiple webhooks can be configured per registry, supporting scenarios like provider migrations and chained CI/CD pipelines.

Webhook configuration lives behind a subresource (rather than inline in `spec`) for several reasons:
- Different RBAC than the rest of the Source (platform team manages source definitions; app teams configure their webhook integrations)
- Different lifecycle (webhooks added/removed independently of Source changes)
- Different sensitivity (webhook secrets are credentials)
- Audit clarity (webhook config changes distinguishable from Source spec changes)

### Schema

The subresource holds a list of named webhook entries under `items`.

```yaml
# GET /apis/convoy.kumodo.io/sources/v1alpha1/namespaces/payments-team/helmregistries/bitnami/webhooks
items:
  - name: harbor-primary
    provider: harbor
    secretRef:
      name: payments-harbor-webhook
      key: secret
    events:
      - chart-uploaded
    filters:
      charts:
        - payments-api
        - payments-worker
    url: https://convoy.svc.cluster.local/webhooks/8f3a2b1c-4d5e-6f7a-8b9c-0d1e2f3a4b5c
```

Each entry's `url` is server-generated and returned by GET/POST; it is not accepted as input. Users copy the URL into the upstream provider's webhook configuration.

### Verbs

| Path               | Verb   | Purpose                                                                 |
|--------------------|--------|-------------------------------------------------------------------------|
| `/webhooks`        | GET    | List all webhook entries for the registry.                              |
| `/webhooks`        | POST   | Create a new webhook entry. Server may auto-generate a name if omitted. |
| `/webhooks`        | DELETE | Remove all webhook entries (rare but convenient).                       |
| `/webhooks/<name>` | GET    | Read a specific webhook entry.                                          |
| `/webhooks/<name>` | PUT    | Update a specific webhook entry.                                        |
| `/webhooks/<name>` | DELETE | Remove a specific webhook entry.                                        |

### Common Fields

| Field            | Type     | Required             | Default                             | Description                                                                                                                                         |
|------------------|----------|----------------------|-------------------------------------|-----------------------------------------------------------------------------------------------------------------------------------------------------|
| `name`           | string   | yes                  | server-generated on POST if omitted | Unique within the registry. Used for addressability via `/webhooks/<name>`. Stable across edits; does not change once assigned.                     |
| `provider`       | enum     | yes                  | —                                   | Provider type. Drives signature verification scheme and payload parser. See [Per-Kind Variations](#per-kind-variations).                            |
| `secretRef`      | object   | yes (most providers) | —                                   | Reference to a Secret containing the shared secret used for HMAC verification (or bearer token for `generic` provider).                             |
| `secretRef.name` | string   | yes                  | —                                   | Secret name in the same namespace as the registry.                                                                                                  |
| `secretRef.key`  | string   | yes                  | —                                   | Key within the Secret holding the secret value.                                                                                                     |
| `events`         | []string | no                   | provider-specific                   | Provider event types that trigger sync. Defaults vary per provider.                                                                                 |
| `filters`        | object   | no                   | —                                   | Optional payload-based filtering. See [Per-Kind Variations](#per-kind-variations).                                                                  |
| `url`            | string   | server-generated     | —                                   | Endpoint URL for the upstream provider to call. Returned in GET/POST responses; mirrored in `status.webhooks[].url`. Each entry has a distinct URL. |

### Per-Kind Variations

#### `HelmRegistry` webhooks

**Provider enum values:**

| Value         | Description                                           |
|---------------|-------------------------------------------------------|
| `harbor`      | Harbor with Helm support. CloudEvents-style payloads. |
| `artifactory` | JFrog Artifactory. HMAC-based auth.                   |
| `nexus`       | Sonatype Nexus 3 with Helm support.                   |
| `generic`     | Any system that can POST with bearer token or HMAC.   |

Public Helm registries (e.g., `charts.bitnami.com`) typically don't support webhooks; this subresource is primarily for self-hosted enterprise registries.

**Filter fields:**

| Field            | Type     | Description                                             |
|------------------|----------|---------------------------------------------------------|
| `filters.charts` | []string | Only trigger sync when matching chart names are pushed. |

**Default events:** `["chart-uploaded"]`

### Status Reflection

`status.webhooks` mirrors operational state per webhook entry. Sensitive config (secret reference, filters) stays behind the subresource; status surfaces only non-sensitive operational facts.

```yaml
status:
  webhooks:
    - name: harbor-primary
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
  
  conditions:
    - type: WebhookConfigured
      status: "True"
      lastTransitionTime: 2026-04-29T08:00:00Z
      reason: WebhooksActive
      message: "1 webhook configured; receiving events"
```

The `WebhookConfigured` condition is `True` if at least one webhook is configured. Per-entry health is in `status.webhooks[].rejectedCount` and `lastRejection`; aggregate failures across entries trigger condition transitions to `False` with appropriate reasons.

### Status Fields per Entry

| Field                      | Type      | Description                                                                                     |
|----------------------------|-----------|-------------------------------------------------------------------------------------------------|
| `name`                     | string    | Matches the entry's name in the subresource.                                                    |
| `url`                      | string    | Endpoint URL (mirrored from subresource for visibility).                                        |
| `lastReceived`             | timestamp | Most recent webhook delivery received (regardless of outcome).                                  |
| `lastTriggered`            | timestamp | Most recent delivery that passed verification and filters and triggered sync.                   |
| `receivedCount`            | integer   | Total deliveries received since last status reset.                                              |
| `triggersCount`            | integer   | Deliveries that resulted in a sync.                                                             |
| `rejectedCount`            | integer   | Deliveries rejected (signature failures, malformed payloads).                                   |
| `filteredCount`            | integer   | Deliveries that passed verification but were filtered out (events/charts didn't match).         |
| `lastRejection`            | object    | Details of the most recent rejection, if any.                                                   |
| `lastRejection.rejectedAt` | timestamp | When the rejection occurred.                                                                    |
| `lastRejection.reason`     | string    | Standardized reason: `SignatureMismatch`, `MalformedPayload`, `UnknownEvent`, `SecretNotFound`. |
| `lastRejection.message`    | string    | Human-readable detail.                                                                          |

### RBAC

Standard Kubernetes RBAC operates at the subresource level — `helmregistries/webhooks` — not at individual entry granularity. Granting webhook-edit permissions on the registry allows managing all webhook entries on it.

```yaml
# Platform team — manages source definitions but not webhook config
- apiGroups: ["source.convoy.kumodo.io"]
  resources: ["helmregistries"]
  verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]

# App team — manages webhook integrations only
- apiGroups: ["source.convoy.kumodo.io"]
  resources: ["helmregistries/webhooks"]
  verbs: ["get", "create", "update", "delete"]
```

For per-webhook access control (rare), users can:
- Use separate registries (recommended for cleanly isolated webhooks).
- Apply `ValidatingAdmissionPolicy` with CEL expressions on the subresource (Kubernetes 1.30+).

### Defaults and Auto-Generation

To avoid penalizing the common case of one webhook per registry:

- POST without a `name` auto-generates a name based on `provider` (e.g., `harbor-1`). Subsequent POSTs of the same provider increment the suffix.
- Single-webhook users can ignore names entirely and treat the subresource as effectively singular by addressing the auto-generated entry.

### URL Generation

Webhook URLs use opaque, server-generated identifiers: `https://<control-plane-host>/webhooks/<webhook-uuid>`

The UUID is generated when the webhook entry is created and remains stable for the lifetime of that entry. Deletion and recreation of the same-named entry produces a new UUID and new URL — users must reconfigure the upstream provider with the new URL.

This pattern is preferred over Source-identifying URLs (e.g., `/webhooks/sources/<namespace>/<source-name>/<webhook-name>`) because:
- It avoids leaking namespace/Source naming structure to the public webhook endpoint.
- Webhook URL rotation (regenerating the UUID) doesn't require changing Source or webhook names.
- Each entry has a distinct URL, mapping cleanly to upstream provider configurations.
