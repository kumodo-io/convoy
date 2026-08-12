<!-- TOC -->
  * [Example](#example)
  * [Spec](#spec)
  * [Authentication Methods](#authentication-methods)
    * [basic](#basic)
    * [sshKey](#sshkey)
    * [token](#token)
    * [githubApp](#githubapp)
    * [oidc](#oidc)
    * [cloudIdentity](#cloudidentity)
  * [Status](#status)
  * [Conditions](#conditions)
    * [AuthValid](#authvalid)
  * [Webhooks](#webhooks)
    * [Purpose](#purpose)
    * [Schema](#schema)
    * [Verbs](#verbs)
    * [Common Fields](#common-fields)
    * [Per-Kind Variations](#per-kind-variations)
      * [`GitRepository` webhooks](#gitrepository-webhooks)
    * [Status Reflection](#status-reflection)
    * [Status Fields per Entry](#status-fields-per-entry)
    * [RBAC](#rbac)
    * [Defaults and Auto-Generation](#defaults-and-auto-generation)
    * [URL Generation](#url-generation)
<!-- TOC -->

Represents a Git repository as a **reusable, credentialed handle** to a content source for the continuous delivery pipeline: a URL plus authentication (and auxiliary options like path filters and TLS). It does **not** pin a ref — *which* revision to fetch (branch, tag, commit, or semver constraint) is specified per `Release` in `spec.source.gitRepository.ref`. One `GitRepository` is therefore shared across many `Release`s that pull different refs of the same repo without duplicating credentials.

The controller's job is to verify the repository is reachable and that the configured credentials are accepted; content for a specific ref is fetched on demand when a `Release` references it.

`GitRepository` is the most common source kind. Implemented authentication methods are `none`, `basic` (HTTP), `token`, and `sshKey`; `githubApp`, `oidc`, and `cloudIdentity` are defined in the API but return `Unsupported` (planned).

**Subresources**

- `/status` — observed reachability state (the only subresource). Re-verification is driven by the controller's interval/resync, not an on-demand subresource. (A `/sync` subresource exists only on `Release`, not on source kinds; `/webhooks` is not implemented — see [Webhooks](#webhooks).)

## Example

```yaml
apiVersion: source.convoy.kumodo.io/v1alpha1
kind: GitRepository
metadata:
  name: payments-charts-repo
  namespace: payments-team
  labels:
    team: payments

spec:
  # Repository URL. Supports https://, git://, ssh:// schemes.
  url: https://github.com/example/payments-charts

  # NOTE: no ref here. The ref to fetch (branch/tag/commit/semver) is set per
  # Release in spec.source.gitRepository.ref, so this repo is reusable across Releases.

  # Authentication for private repos.
  # Optional — omit for public repos.
  # Discriminated union on auth.type. See "Authentication Methods" below.
  auth:
    # type: none | basic | sshKey | token | githubApp | oidc | cloudIdentity
    type: sshKey
    
    # type: sshKey — SSH private key in a Secret.
    sshKey:
      secretRef:
        name: payments-git-ssh
        key: id_rsa
      # Optional known_hosts for strict host key checking (recommended for production).
      knownHostsRef:
        configMapRef:
          name: github-known-hosts
          key: known_hosts
    
    # type: basic — HTTP basic auth (username + password/PAT).
    # basic:
    #   usernameRef: { secretRef: { name: payments-git-basic, key: username } }
    #   passwordRef: { secretRef: { name: payments-git-basic, key: password } }
    
    # type: token — bearer token (e.g., GitHub PAT, GitLab token).
    # token:
    #   secretRef: { name: payments-git-token, key: token }
    
    # type: githubApp — GitHub App authentication (JWT-based, finer-grained permissions).
    # githubApp:
    #   appID: "123456"
    #   installationID: "7890123"
    #   privateKeyRef: { secretRef: { name: github-app-key, key: key.pem } }

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

    # type: cloudIdentity — controller uses cloud-native identity for cloud-hosted Git
    # (e.g., AWS CodeCommit via IRSA). No static credentials stored.
    # cloudIdentity:
    #   provider: aws | azure | gcp
  
  # Polling interval for ref resolution and content fetching.
  # Lower = faster updates, higher git load. Webhooks can supplement.
  interval: 1m

  minSyncInterval: 10s
  
  # Optional: path prefix filter — only consider changes under these paths
  # for triggering re-render. Reduces noise from unrelated commits.
  includePaths:
    - charts/
    - templates/
  
  # Optional: ignore changes matching these paths (regex or glob, depending on impl).
  # ignorePaths:
  #   - "**/*.md"
  #   - docs/
  
  # Optional: TLS configuration
  tls:
    caPublicCertRef:
      # config map name containing the CA public cert
      name: payments-git-ca-cert
      key: ca.crt
    insecureSkipVerify: false
  
webhook:
  - name: github-primary
    provider: github
    secretRef:
      name: payments-github-webhook
      key: secret
    events:
      - push
      - tag-created
    filters:
      refs:
        - main
        - releases/*
      paths:
        - charts/payments/**
  - name: gitea-mirror
    provider: gitea
    secretRef:
      name: payments-gitea-webhook
      key: secret
    events:
      - push

status:
  observedGeneration: 7

  # Last successful reachability/verification probe.
  lastSuccessfulProbeAt: 2026-04-29T09:55:00Z

  # A single Ready condition is written. Reasons: Verified | VerifyFailed | Unsupported.
  conditions:
    - type: Ready
      status: "True"
      lastTransitionTime: 2026-04-29T09:59:00Z
      reason: Verified
```

## Spec

| Field                | Type     | Required            | Default | Description                                                                                                                                         |
|----------------------|----------|---------------------|---------|-----------------------------------------------------------------------------------------------------------------------------------------------------|
| `url`                | string   | yes                 | —       | Repository URL. Supports `https://`, `git://`, `ssh://` schemes. The ref to fetch is set per `Release` (`spec.source.gitRepository.ref`), not here. |
| `auth`               | object   | yes                 | —       | Authentication — discriminated union on `auth.type`. Use `type: none` for public repos. See [Authentication Methods](#authentication-methods).      |
| `auth.type`          | enum     | yes                 | —       | One of `none`, `basic`, `sshKey`, `token`, `githubApp`, `oidc`, `cloudIdentity`. Only `none`/`basic`/`sshKey`/`token` are implemented.               |
| `auth.basic`         | object   | one-of              | —       | HTTP basic auth (when `type: basic`).                                                                                                               |
| `auth.sshKey`        | object   | one-of              | —       | SSH private key auth (when `type: sshKey`).                                                                                                         |
| `auth.token`         | object   | one-of              | —       | Bearer token auth (when `type: token`).                                                                                                             |
| `auth.githubApp`     | object   | one-of              | —       | GitHub App installation auth (when `type: githubApp`).                                                                                              |
| `auth.oidc`          | object   | one-of              | —       | OIDC client-credentials flow (when `type: oidc`).                                                                                                   |
| `auth.cloudIdentity` | object   | one-of              | —       | Cloud workload identity (when `type: cloudIdentity`); for cloud-hosted Git like CodeCommit.                                                         |
| `interval`           | duration | no                  | —       | Polling interval for verification. No server-side default is applied (zero if unset).                                                               |
| `minSyncInterval`    | duration | no                  | —       | Defined in the API but currently unused (no `/sync` subresource on source kinds). No default.                                                       |
| `includePaths`       | []string | no                  | —       | Path patterns; changes outside these paths don't trigger downstream reconciliation.                                                                 |
| `ignorePaths`        | []string | no                  | —       | Path patterns; changes matching these are ignored even if inside `includePaths`.                                                                    |

The ref selector (`branch` / `tag` / `commit`) lives on the consuming `Release` as `spec.source.gitRepository.ref` — see the [Release](../core/release.md) doc. This is what makes a single `GitRepository` reusable: staging can track `branch: main` and prod can pin `tag: v1.2.3` against the same repo + credentials. (A `semver` ref kind is defined in the API but its resolution is not yet implemented — it returns `Unsupported`; use `branch`/`tag`/`commit`.)

## Authentication Methods

`spec.auth` is **required** (a discriminated union on `auth.type`); for a public repository set `type: none`. Implemented types: `none`, `basic`, `token`, `sshKey`. `githubApp`, `oidc`, `cloudIdentity` are defined in the API but return `Unsupported` (planned).

### basic

HTTP basic auth — a `username` (literal string) plus a `passwordRef` (a Secret key selector, i.e. `{name, key}`).

```yaml
spec:
  auth:
    type: basic
    basic:
      username: git
      passwordRef: { name: payments-git-basic, key: password }
```

### sshKey

An SSH private key (`sshKeyRef`) for `ssh://` / `git@` remotes. `knownHostsRef` is **required** (host-key verification). Only plaintext `known_hosts` entries are supported (hashed `|1|` entries are rejected; `@cert-authority` lines are skipped).

```yaml
spec:
  auth:
    type: sshKey
    sshKey:
      sshKeyRef: { name: payments-git-ssh, key: id_rsa }
      knownHostsRef: { name: github-known-hosts, key: known_hosts }
```

### token

A bearer token (e.g. a GitHub PAT or GitLab token). The field is `tokenRef` (a Secret key selector).

```yaml
spec:
  auth:
    type: token
    token:
      tokenRef: { name: payments-git-token, key: token }
```

### githubApp

_Planned — not implemented (returns `Unsupported`)._ GitHub App installation auth. `appID`/`installationID` are integers; `privateKeyRef` is a flat Secret key selector.

```yaml
spec:
  auth:
    type: githubApp
    githubApp:
      appID: 123456
      installationID: 7890123
      privateKeyRef: { name: github-app-key, key: key.pem }
```

### oidc

_Planned — not implemented (returns `Unsupported`)._ OIDC client-credentials flow. Client auth is a nested union (`clientAuth.type`: `clientSecret`/`privateKeyJWT`/`tlsClientAuth`). Note the JSON tag `clientID`, and the secret nests under `clientSecret.clientSecretRef`.

```yaml
spec:
  auth:
    type: oidc
    oidc:
      issuerURL: https://auth.example.com/realms/internal
      clientID: cd-controller
      clientAuth:
        type: clientSecret           # | privateKeyJWT | tlsClientAuth
        clientSecret:
          clientSecretRef: { name: oidc-client, key: client-secret }
```

### cloudIdentity

_Planned — not implemented (returns `Unsupported`)._ The controller pod uses its own cloud-native identity (e.g. IRSA on EKS) to authenticate to cloud-hosted Git. No static credentials are stored.

```yaml
spec:
  auth:
    type: cloudIdentity
    cloudIdentity:
      provider: aws   # | gcp | azure
```

## Status

| Field                     | Type        | Description                                                                                           |
|---------------------------|-------------|-------------------------------------------------------------------------------------------------------|
| `observedGeneration`      | integer     | Generation of `spec` last reconciled.                                                                 |
| `lastSuccessfulProbeAt`   | timestamp   | Time of the last successful reachability/verification probe.                                          |
| `conditions`              | []Condition | See [Conditions](#conditions).                                                                        |

(The API type also has a `releaseCount` field, but the controller does not currently populate it.)

## Conditions

`GitRepository` reports a **single** condition in `status.conditions`: `Ready`. (There are no separate `Reachable`/`AuthValid`/`WebhookConfigured` conditions — those are not implemented.)

| Condition | What `True` means                                                        | Reasons                                                                       |
|-----------|--------------------------------------------------------------------------|-------------------------------------------------------------------------------|
| `Ready`   | The remote was reached and its configured credentials verified, so Releases may fetch from it. | `Verified` (success); `VerifyFailed` (unreachable / auth rejected / other error); `Unsupported` (auth method or ref kind not implemented). |

An authentication failure surfaces as `Ready: False` with reason `VerifyFailed` — there is no separate auth condition or auth-specific reason code.

### AuthValid (not implemented)

There is no `AuthValid` condition today. A verification failure (unreachable remote, rejected credentials, missing Secret) is reported on the single `Ready` condition as `status: "False"`, `reason: VerifyFailed`, with a human-readable message. Example:

```yaml
status:
  lastSuccessfulProbeAt: 2026-04-29T10:00:00Z
  conditions:
    - type: Ready
      status: "False"
      reason: VerifyFailed
      message: "Secret 'payments-git-ssh' not found in namespace 'payments-team'"
```

## Webhooks

> **Status: planned — not implemented.** There is no `/webhooks` subresource and no webhook receiver: no per-entry URL generation, no operational status counters, no provider/event/filter processing. Webhook configuration is expressed today as an **inline `webhook` array field** on the object (a sibling of `spec`/`status`); the `WebhookStatus` types exist but the controller does not populate them, and no endpoint receives webhook deliveries. The rest of this section describes the intended design.

### Purpose

The webhook design enables push-based notifications from the upstream Git provider that trigger source refreshes, supporting scenarios like multi-mirror Git setups, provider migrations, and chained CI/CD pipelines.

### Schema

The subresource holds a list of named webhook entries under `items`.

```yaml
# GET /apis/convoy.kumodo.io/sources/v1alpha1/namespaces/payments-team/gitrepositories/payments-charts-repo/webhooks
items:
  - name: github-primary
    provider: github
    secretRef:
      name: payments-github-webhook
      key: secret
    events:
      - push
      - tag-created
    filters:
      refs:
        - main
        - releases/*
      paths:
        - charts/payments/**
    url: https://convoy.svc.cluster.local/webhooks/8f3a2b1c-4d5e-6f7a-8b9c-0d1e2f3a4b5c
  
  - name: gitea-mirror
    provider: gitea
    secretRef:
      name: payments-gitea-webhook
      key: secret
    events:
      - push
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
| `provider`       | enum     | yes                  | —                                   | Provider type. Drives signature verification scheme and payload parser. Enum values listed below.                                                   |
| `secretRef`      | object   | yes (most providers) | —                                   | Reference to a Secret containing the shared secret used for HMAC verification (or bearer token for `generic` provider).                             |
| `secretRef.name` | string   | yes                  | —                                   | Secret name in the same namespace as the Source.                                                                                                    |
| `secretRef.key`  | string   | yes                  | —                                   | Key within the Secret holding the secret value.                                                                                                     |
| `events`         | []string | no                   | provider-specific                   | Provider event types that trigger sync. Defaults vary per provider.                                                                                 |
| `filters`        | object   | no                   | —                                   | Optional payload-based filtering.                                                                                                                   |
| `url`            | string   | server-generated     | —                                   | Endpoint URL for the upstream provider to call. Returned in GET/POST responses; mirrored in `status.webhooks[].url`. Each entry has a distinct URL. |

### Per-Kind Variations

#### `GitRepository` webhooks

**Provider enum values:**

| Value             | Description                                                                              |
|-------------------|------------------------------------------------------------------------------------------|
| `github`          | GitHub / GitHub Enterprise. HMAC-SHA256 via `X-Hub-Signature-256`.                       |
| `gitlab`          | GitLab. Token via `X-Gitlab-Token` (HMAC opt-in).                                        |
| `bitbucket`       | Bitbucket Cloud. HMAC-SHA256 via `X-Hub-Signature`.                                      |
| `bitbucketServer` | Bitbucket Data Center / Server. Provider-specific headers.                               |
| `gitea`           | Gitea / Forgejo. HMAC via `X-Gitea-Signature`.                                           |
| `azure`           | Azure DevOps Service Hooks.                                                              |
| `generic`         | Bearer token via `Authorization: Bearer <secret>`, or no auth if `secretRef` is omitted. |

**Filter fields:**

| Field           | Type     | Description                                                                                                                                   |
|-----------------|----------|-----------------------------------------------------------------------------------------------------------------------------------------------|
| `filters.refs`  | []string | Only trigger sync when the push affects matching refs. Supports glob patterns (e.g., `releases/*`).                                           |
| `filters.paths` | []string | Only trigger sync when changed paths intersect these patterns. Composes with `spec.includePaths` and `spec.ignorePaths` on the Source itself. |

**Default events:** `["push"]`

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
    
    - name: gitea-mirror
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

| Field                      | Type      | Description                                                                                     |
|----------------------------|-----------|-------------------------------------------------------------------------------------------------|
| `name`                     | string    | Matches the entry's name in the subresource.                                                    |
| `url`                      | string    | Endpoint URL (mirrored from subresource for visibility).                                        |
| `lastReceived`             | timestamp | Most recent webhook delivery received (regardless of outcome).                                  |
| `lastTriggered`            | timestamp | Most recent delivery that passed verification and filters and triggered sync.                   |
| `receivedCount`            | integer   | Total deliveries received since last status reset.                                              |
| `triggersCount`            | integer   | Deliveries that resulted in a sync.                                                             |
| `rejectedCount`            | integer   | Deliveries rejected (signature failures, malformed payloads).                                   |
| `filteredCount`            | integer   | Deliveries that passed verification but were filtered out (events/refs/paths didn't match).     |
| `lastRejection`            | object    | Details of the most recent rejection, if any.                                                   |
| `lastRejection.rejectedAt` | timestamp | When the rejection occurred.                                                                    |
| `lastRejection.reason`     | string    | Standardized reason: `SignatureMismatch`, `MalformedPayload`, `UnknownEvent`, `SecretNotFound`. |
| `lastRejection.message`    | string    | Human-readable detail.                                                                          |

### RBAC

Standard Kubernetes RBAC operates at the subresource level — `gitrepositories/webhooks` — not at individual entry granularity. Granting webhook-edit permissions on a Source allows managing all webhook entries on that Source.

```yaml
# Platform team — manages source definitions but not webhook config
- apiGroups: ["source.convoy.kumodo.io"]
  resources: ["gitrepositories"]
  verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]

# App team — manages webhook integrations only
- apiGroups: ["source.convoy.kumodo.io"]
  resources: ["gitrepositories/webhooks"]
  verbs: ["get", "create", "update", "delete"]
```

For per-webhook access control (rare), users can:
- Use separate Sources (recommended for cleanly isolated webhooks).
- Apply `ValidatingAdmissionPolicy` with CEL expressions on the subresource (Kubernetes 1.30+).

### Defaults and Auto-Generation

To avoid penalizing the common case of one webhook per Source:

- POST without a `name` auto-generates a name based on `provider` (e.g., `github-1`). Subsequent POSTs of the same provider increment the suffix.
- Single-webhook users can ignore names entirely and treat the subresource as effectively singular by addressing the auto-generated entry.

### URL Generation

Webhook URLs use opaque, server-generated identifiers: `https://<control-plane-host>/webhooks/<webhook-uuid>`

The UUID is generated when the webhook entry is created and remains stable for the lifetime of that entry. Deletion and recreation of the same-named entry produces a new UUID and new URL — users must reconfigure the upstream provider with the new URL.

This pattern is preferred over Source-identifying URLs (e.g., `/webhooks/sources/<namespace>/<source-name>/<webhook-name>`) because:
- It avoids leaking namespace/Source naming structure to the public webhook endpoint.
- Webhook URL rotation (regenerating the UUID) doesn't require changing Source or webhook names.
- Each entry has a distinct URL, mapping cleanly to upstream provider configurations.
