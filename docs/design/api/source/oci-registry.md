<!-- TOC -->
  * [Example](#example)
  * [Spec](#spec)
  * [Authentication Methods](#authentication-methods)
    * [dockerConfig](#dockerconfig)
    * [basic](#basic)
    * [token](#token)
    * [oidcClientCredentials](#oidcclientcredentials)
    * [workloadIdentity](#workloadidentity)
  * [Content Types](#content-types)
  * [Status](#status)
  * [Conditions](#conditions)
    * [AuthValid](#authvalid)
  * [Webhooks](#webhooks)
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

Represents an OCI registry that may hold Helm charts (OCI-packaged), raw manifest bundles, or Kustomize bundles. OCI is a generic artifact format; the registry doesn't dictate content type, so a `defaultContentType` is configured on the registry and may be overridden per-`Release`. OCI registries are increasingly common for chart distribution (Harbor, ghcr.io, GitLab, ECR, ACR, GAR) and are the modern alternative to HTTP Helm registries.

**Subresources**

- `/status` — observed reconciliation state
- `/sync` — POST to force a registry re-probe (subject to `minSyncInterval`)
- `/webhooks` — GET / POST / DELETE webhook configuration. See [Webhooks](#webhooks).

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
    # type: dockerConfig | basic | token | oidcClientCredentials | workloadIdentity
    type: dockerConfig
    
    # type: dockerConfig — standard kubelet-style image pull secret.
    # Most flexible; handles registry-specific auth flows.
    dockerConfig:
      secretRef:
        name: ghcr-pull-secret
        key: .dockerconfigjson

    # --------------------------------
    # type: basic
    # basic:
    #   usernameRef: { secretRef: { name: ghcr-creds, key: username } }
    #   passwordRef: { secretRef: { name: ghcr-creds, key: password } }

    # --------------------------------
    # type: token — bearer token (e.g., GitHub PAT for ghcr.io).
    # token:
    #   secretRef: { name: ghcr-token, key: token }

    # --------------------------------
    # type: workloadIdentity — controller pod has cloud-native identity
    # (IRSA for ECR, Workload Identity for GAR, managed identity for ACR).
    # workloadIdentity: {}

    # --------------------------------
    # type: oidcClientCredentials:
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
  
  # How often to check for new tags / artifact updates.
  interval: 5m
  
  # Optional: TLS config for self-signed registries.
  # tls:
  #   caBundleRef: { configMapRef: { name: corporate-ca, key: ca.crt } }
  #   insecureSkipVerify: false
  
  # Default content type for artifacts in this registry. See "Content Types" below.
  # OCI is a generic artifact format — could be Helm charts, Kustomize bundles,
  # or arbitrary manifest archives.
  # - helm:      OCI-packaged Helm charts (mediaType: application/vnd.cncf.helm.config.v1+json)
  # - manifests: tarballs of raw YAML
  # - kustomize: Kustomize bundles
  # Can be overridden per-Release in spec.content.type.
  defaultContentType: helm

webhooks:
  - name: ghcr
    provider: ghcr
    secretRef:
      name: ghcr-webhook-secret
      key: secret
    events:
      - package-published
    filters:
      repositories:
        - payments-api
        - payments-worker
      tags:
        - "v*"

status:
  observedGeneration: 2
  
  # Last successful registry probe.
  lastChecked:
    checkedAt: 2026-04-29T09:55:00Z
  
  releaseCount: 8

  # Webhook status
  webhooks:
    - name: harbor
      url: https://convoy.svc.cluster.local/webhooks/8f3a2b1c-4d5e-6f7a-8b9c-0d1e2f3a4b5c
      lastReceived: 2026-04-29T10:00:00Z
      lastTriggered: 2026-04-29T10:00:00Z
      receivedCount: 142
      triggeredCount: 87
      rejectedCount: 3
      filteredCount: 52
      lastRejection:
        at: 2026-04-29T08:30:00Z
        reason: SignatureMismatch
        message: "HMAC signature did not match expected value"

  conditions:
    - type: Ready
      status: "True"
      lastTransitionTime: 2026-04-29T09:55:00Z
      reason: RegistryReachable
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

| Field                        | Type     | Required            | Default | Description                                                                                                 |
|------------------------------|----------|---------------------|---------|-------------------------------------------------------------------------------------------------------------|
| `url`                        | string   | yes                 | —       | OCI registry URL with `oci://` scheme.                                                                      |
| `auth`                       | object   | no                  | —       | Authentication — discriminated union on `auth.type`. See [Authentication Methods](#authentication-methods). |
| `auth.type`                  | enum     | yes (if `auth` set) | —       | One of `dockerConfig`, `basic`, `token`, `oidcClientCredentials`, `workloadIdentity`.                       |
| `auth.dockerConfig`          | object   | one-of              | —       | Standard kubelet-style image pull secret (when `type: dockerConfig`).                                       |
| `auth.basic`                 | object   | one-of              | —       | HTTP basic auth (when `type: basic`).                                                                       |
| `auth.token`                 | object   | one-of              | —       | Bearer token auth (when `type: token`).                                                                     |
| `auth.oidcClientCredentials` | object   | one-of              | —       | OIDC client credentials flow (when `type: oidcClientCredentials`).                                          |
| `auth.workloadIdentity`      | object   | one-of              | —       | Cloud workload identity (when `type: workloadIdentity`); for ECR (IRSA), GAR (GCP Workload Identity), ACR.  |
| `interval`                   | duration | no                  | `5m`    | Polling interval for tag/digest checks.                                                                     |
| `minSyncInterval`            | duration | no                  | `10s`   | Minimum interval between manual/webhook-triggered syncs.                                                    |
| `defaultContentType`         | enum     | no                  | `helm`  | Default interpretation of artifacts. See [Content Types](#content-types). Overridable per Release.          |
| `tls`                        | object   | no                  | —       | TLS config for the registry.                                                                                |
| `tls.caBundleRef`            | object   | no                  | —       | Reference to a ConfigMap containing the CA bundle.                                                          |
| `tls.insecureSkipVerify`     | bool     | no                  | `false` | Skip TLS verification (not recommended).                                                                    |

## Authentication Methods

`spec.auth` is a discriminated union on `auth.type`. The active method's block is set; all others are omitted. Most OCI registries require auth; omit `auth` entirely for public registries.

### dockerConfig

A standard kubelet-style image pull secret (a `.dockerconfigjson` blob in a Secret). The most flexible option — it handles registry-specific auth flows and works with the same secrets used for pulling images.

```yaml
spec:
  auth:
    type: dockerConfig
    dockerConfig:
      secretRef:
        name: ghcr-pull-secret
        key: .dockerconfigjson
```

### basic

HTTP basic authentication with a username and password drawn from a Secret.

```yaml
spec:
  auth:
    type: basic
    basic:
      usernameRef: { secretRef: { name: ghcr-creds, key: username } }
      passwordRef: { secretRef: { name: ghcr-creds, key: password } }
```

### token

A static bearer token (e.g. a GitHub PAT for ghcr.io). Simple, but the token does not rotate automatically.

```yaml
spec:
  auth:
    type: token
    token:
      secretRef: { name: ghcr-token, key: token }
```

### oidcClientCredentials

OIDC client-credentials flow. The controller discovers the token endpoint via `/.well-known/openid-configuration` on the issuer and exchanges client credentials for a token. Client authentication itself is a nested discriminated union (`clientAuth.type`): `clientSecret` (shared secret, most common), `privateKeyJWT` (signed JWT assertion), or `tlsClientAuth` (mTLS).

```yaml
spec:
  auth:
    type: oidcClientCredentials
    oidcClientCredentials:
      issuerURL: https://auth.example.com/realms/internal
      clientId: cd-controller
      clientAuth:
        type: clientSecret           # | privateKeyJWT | tlsClientAuth
        clientSecret:
          secretRef: { name: oidc-client, key: client-secret }
      scopes: [openid]
      audience: oci-registry
      # Optional TLS config for a self-signed corporate IdP:
      # tls:
      #   caBundleRef: { name: corporate-ca, key: ca.crt }
      #   insecureSkipVerify: false
```

### workloadIdentity

The controller pod uses its own cloud-native identity (IRSA for ECR, Workload Identity for GAR, managed identity for ACR). No static credentials are stored.

```yaml
spec:
  auth:
    type: workloadIdentity
    workloadIdentity: {}
```

## Content Types

`spec.defaultContentType` is an enum selecting how artifacts in this registry are interpreted by default. OCI is a generic artifact format, so the registry itself does not dictate content type. The default may be overridden per-`Release` in `spec.content.type`.

- `helm` (default) — OCI-packaged Helm charts (`mediaType: application/vnd.cncf.helm.config.v1+json`).
- `manifests` — tarballs of raw YAML manifests.
- `kustomize` — Kustomize bundles.

```yaml
spec:
  defaultContentType: helm   # | manifests | kustomize
```

## Status

| Field                   | Type        | Description                                                                                           |
|-------------------------|-------------|-------------------------------------------------------------------------------------------------------|
| `observedGeneration`    | integer     | Generation of `spec` last reconciled.                                                                 |
| `lastChecked`           | object      | Last successful registry probe.                                                                       |
| `lastChecked.checkedAt` | timestamp   | When the check completed.                                                                             |
| `releaseCount`          | integer     | Number of `Release` objects referencing this registry.                                                |
| `webhooks`              | object      | Webhook operational state, mirrored from the `/webhooks` subresource. See [Webhooks](#webhooks).      |
| `conditions`            | []Condition | See [Conditions](#conditions).                                                                        |

## Conditions

`OCIRegistry` reports the following standard k8s conditions in `status.conditions`.

| Condition           | What `True` means                                                                                                                                                                                  |
|---------------------|----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `Ready`             | Composite: the registry is reachable, authenticated, and any configured webhooks are active. The signal most tooling should watch. Requires `Reachable: True` and `AuthValid: True`.               |
| `Reachable`         | Network-level reachability of the registry endpoint succeeded.                                                                                                                                     |
| `AuthValid`         | The most recent authentication attempt succeeded. See [AuthValid](#authvalid) for full semantics.                                                                                                  |
| `WebhookConfigured` | At least one webhook is configured and receiving events. `True` when no webhooks are configured is not reported as a fault; per-entry health is in `status.webhooks[]`. See [Webhooks](#webhooks). |

### AuthValid

`AuthValid` reflects the state of authenticating to the registry. It is always present, even when no explicit credentials are configured (public registries report `True` with `reason: NoAuthRequired`).

**Status semantics:**

| Status    | Meaning                                                                                                     |
|-----------|-------------------------------------------------------------------------------------------------------------|
| `True`    | Most recent authentication attempt succeeded. Credentials are working.                                      |
| `False`   | Most recent authentication attempt failed for an auth-specific reason. Actionable.                          |
| `Unknown` | Authentication has not been attempted yet, OR a non-auth failure (network, timeout) prevented testing auth. |

`Unknown` must not be conflated with `False`: "credentials are bad" is a different problem from "we couldn't test the credentials."

**Reason codes:**

| Status    | Reason                   | Meaning                                                                          |
|-----------|--------------------------|----------------------------------------------------------------------------------|
| `True`    | `TokenAcquired`          | Succeeded via explicit token acquisition (OIDC, workload identity).              |
| `True`    | `CredentialsAccepted`    | Succeeded with simple credentials (dockerConfig, basic, token).                  |
| `True`    | `NoAuthRequired`         | No authentication is configured; registry is public.                             |
| `False`   | `InvalidCredentials`     | Credentials rejected by the registry (401-class).                                |
| `False`   | `TokenAcquisitionFailed` | OIDC or workload-identity token exchange failed.                                 |
| `False`   | `SecretNotFound`         | Referenced Secret or ConfigMap does not exist.                                   |
| `False`   | `SecretMalformed`        | Secret exists but content is invalid (bad base64, wrong format, missing keys).   |
| `False`   | `PermissionDenied`       | Credentials valid but lack required permissions (403-class).                     |
| `False`   | `TokenExpired`           | Known-expired token; refresh attempt failed.                                     |
| `False`   | `AuthMisconfigured`      | Config error detected without contacting the registry (e.g. invalid issuer URL). |
| `Unknown` | `NotYetAttempted`        | Registry is new; auth has not yet been tested.                                   |
| `Unknown` | `RemoteUnreachable`      | Could not contact the registry to test auth (network, DNS, firewall).            |

**Triage with `Reachable`:**

| `Reachable` | `AuthValid` | Action                                                                              |
|-------------|-------------|-------------------------------------------------------------------------------------|
| `False`     | `Unknown`   | Fix network, DNS, firewall, or registry endpoint availability.                      |
| `True`      | `False`     | Fix credentials or auth configuration.                                              |
| `True`      | `True`      | If `Ready: False`, look beyond auth (rate limits, content errors).                  |

`AuthValid: False` always forces `Ready: False`, with `Ready.reason: AuthFailed`.

**Example — public registry, no auth:**

```yaml
status:
  conditions:
    - type: Ready
      status: "True"
      reason: RegistryReachable
    - type: Reachable
      status: "True"
    - type: AuthValid
      status: "True"
      reason: NoAuthRequired
      message: "Registry is public; no authentication configured"
```

## Webhooks

### Purpose

The `/webhooks` subresource manages webhook configurations for the registry, enabling push-based notifications from upstream that trigger source refreshes. Multiple webhooks can be configured, supporting scenarios like multi-mirror setups, provider migrations, and chained CI/CD pipelines.

Webhook configuration lives behind a subresource (rather than inline in `spec`) for several reasons:
- Different RBAC than the rest of the Source (platform team manages source definitions; app teams configure their webhook integrations)
- Different lifecycle (webhooks added/removed independently of Source changes)
- Different sensitivity (webhook secrets are credentials)
- Audit clarity (webhook config changes distinguishable from Source spec changes)

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
      triggeredCount: 87
      rejectedCount: 3
      filteredCount: 52
      lastRejection:
        at: 2026-04-29T08:30:00Z
        reason: SignatureMismatch
        message: "HMAC signature did not match expected value"
    
    - name: harbor-mirror
      url: https://convoy.svc.cluster.local/webhooks/2a4b6c8d-1e3f-5a7b-9c0d-2e4f6a8b0c1d
      lastReceived: 2026-04-29T09:50:00Z
      lastTriggered: 2026-04-29T09:50:00Z
      receivedCount: 18
      triggeredCount: 18
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

| Field                   | Type      | Description                                                                                        |
|-------------------------|-----------|----------------------------------------------------------------------------------------------------|
| `name`                  | string    | Matches the entry's name in the subresource.                                                       |
| `url`                   | string    | Endpoint URL (mirrored from subresource for visibility).                                           |
| `lastReceived`          | timestamp | Most recent webhook delivery received (regardless of outcome).                                     |
| `lastTriggered`         | timestamp | Most recent delivery that passed verification and filters and triggered sync.                      |
| `receivedCount`         | integer   | Total deliveries received since last status reset.                                                 |
| `triggeredCount`        | integer   | Deliveries that resulted in a sync.                                                                |
| `rejectedCount`         | integer   | Deliveries rejected (signature failures, malformed payloads).                                      |
| `filteredCount`         | integer   | Deliveries that passed verification but were filtered out (events/repositories/tags didn't match). |
| `lastRejection`         | object    | Details of the most recent rejection, if any.                                                      |
| `lastRejection.at`      | timestamp | When the rejection occurred.                                                                       |
| `lastRejection.reason`  | string    | Standardized reason: `SignatureMismatch`, `MalformedPayload`, `UnknownEvent`, `SecretNotFound`.    |
| `lastRejection.message` | string    | Human-readable detail.                                                                             |

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
