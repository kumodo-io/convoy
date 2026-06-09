<!-- TOC -->
  * [Example](#example)
  * [Spec](#spec)
  * [Ref](#ref)
    * [branch](#branch)
    * [tag](#tag)
    * [commit](#commit)
    * [semver](#semver)
  * [Authentication Methods](#authentication-methods)
    * [basic](#basic)
    * [sshKey](#sshkey)
    * [token](#token)
    * [githubApp](#githubapp)
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
      * [`GitRepository` webhooks](#gitrepository-webhooks)
    * [Status Reflection](#status-reflection)
    * [Status Fields per Entry](#status-fields-per-entry)
    * [RBAC](#rbac)
    * [Defaults and Auto-Generation](#defaults-and-auto-generation)
    * [URL Generation](#url-generation)
<!-- TOC -->

Represents a Git repository as a content source for the continuous delivery pipeline. It tracks a specified ref (branch, tag, commit, or semver constraint) at a configurable interval, fetching content for use by `Release` objects (as the source of charts, manifests, kustomizations, etc.) or as mutation content (values files, patches).

`GitRepository` is the most common source kind. Authentication supports a wide range of methods including SSH keys, HTTP basic auth, tokens, GitHub App installation, and cloud workload identity.

**Subresources**

- `/status` — observed reconciliation state
- `/sync` — POST to force re-fetch from upstream. Body: `{ force?: bool, cascade?: bool }`. Subject to `minSyncInterval`.
- `/webhooks` — GET / POST / DELETE webhook configuration (provider, secret reference, event filters, payload filters). The generated webhook URL is returned in responses and surfaced in status. See [Webhooks](#webhooks).

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
  
  # What to track — exactly one of {branch, tag, commit, semver} must be specified.
  # See "Ref" below.
  ref:
    # Track a branch (resolves to its tip on each fetch).
    branch: main
    
    # tag: v1.2.3                # exact tag
    # commit: abc123def456...    # pinned commit (immutable resolution)
    # semver: ">=1.2.0 <2.0.0"   # for tag-based semver tracking; resolves to highest matching tag
  
  # Authentication for private repos.
  # Optional — omit for public repos.
  # Discriminated union on auth.type. See "Authentication Methods" below.
  auth:
    # type: basic | sshKey | token | githubApp | oidc | workloadIdentity
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

    # type: workloadIdentity — controller uses cloud-native identity for cloud-hosted Git
    # (e.g., AWS CodeCommit via IRSA). No static credentials stored.
  
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
  
  # Last successful fetch info — drives Release.status.resolvedSource.
  lastFetched:
    # Resolved revision (Git SHA) at fetch time.
    revision: abc123def456789aabbccddeeff001122334455
    # Tree hash for content-addressed dedup.
    treeHash: sha256:7d8e9f0a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e
    fetchedAt: 2026-04-29T09:59:00Z
    # Resolved ref details (e.g., for semver: which tag matched).
    resolvedRef: main
  
  # Number of Releases referencing this Source.
  releaseCount: 4

  # Webhook status. See "Webhooks" below.
  webhooks:
    - name: github-primary
      url: https://convoy.svc.cluster.local/webhooks/8f3a2b1c...
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
      url: https://convoy.svc.cluster.local/webhooks/2a4b6c8d...
      lastReceived: 2026-04-29T09:50:00Z
      lastTriggered: 2026-04-29T09:50:00Z
      receivedCount: 18
      triggersCount: 18
      rejectedCount: 0
      filteredCount: 0

  conditions:
    - type: Ready
      status: "True"
      lastTransitionTime: 2026-04-29T09:59:00Z
      reason: FetchSucceeded
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

| Field                   | Type     | Required            | Default | Description                                                                                                                        |
|-------------------------|----------|---------------------|---------|------------------------------------------------------------------------------------------------------------------------------------|
| `url`                   | string   | yes                 | —       | Repository URL. Supports `https://`, `git://`, `ssh://` schemes.                                                                   |
| `ref`                   | object   | yes                 | —       | What to track — discriminated union. Exactly one sub-field is set. See [Ref](#ref).                                                |
| `ref.branch`            | string   | one-of              | —       | Track a branch; resolves to its tip on each fetch.                                                                                 |
| `ref.tag`               | string   | one-of              | —       | Track a fixed tag.                                                                                                                 |
| `ref.commit`            | string   | one-of              | —       | Pin to a specific commit SHA (immutable resolution).                                                                               |
| `ref.semver`            | string   | one-of              | —       | Semver constraint resolved against tags; selects highest matching.                                                                 |
| `auth`                  | object   | no                  | —       | Authentication — discriminated union on `auth.type`. Omit for public repos. See [Authentication Methods](#authentication-methods). |
| `auth.type`             | enum     | yes (if `auth` set) | —       | One of `basic`, `sshKey`, `token`, `githubApp`, `oidc`, `workloadIdentity`.                                                        |
| `auth.basic`            | object   | one-of              | —       | HTTP basic auth (when `type: basic`).                                                                                              |
| `auth.sshKey`           | object   | one-of              | —       | SSH private key auth (when `type: sshKey`).                                                                                        |
| `auth.token`            | object   | one-of              | —       | Bearer token auth (when `type: token`).                                                                                            |
| `auth.githubApp`        | object   | one-of              | —       | GitHub App installation auth (when `type: githubApp`).                                                                             |
| `auth.oidc`             | object   | one-of              | —       | OIDC client-credentials flow (when `type: oidc`).                                                                                  |
| `auth.workloadIdentity` | object   | one-of              | —       | Cloud workload identity (when `type: workloadIdentity`); for cloud-hosted Git like CodeCommit.                                     |
| `interval`              | duration | no                  | `1m`    | Polling interval for ref resolution and content fetching.                                                                          |
| `minSyncInterval`       | duration | no                  | `10s`   | Minimum interval between manual/webhook-triggered syncs. Rate-limits the `/sync` subresource.                                      |
| `includePaths`          | []string | no                  | —       | Path patterns; changes outside these paths don't trigger downstream reconciliation.                                                |
| `ignorePaths`           | []string | no                  | —       | Path patterns; changes matching these are ignored even if inside `includePaths`.                                                   |

## Ref

`spec.ref` is a discriminated union. Exactly one of `branch` / `tag` / `commit` / `semver` is set; it determines how the repository revision is resolved on each fetch.

### branch

Track a branch. Resolves to the branch tip on each fetch, so new commits are picked up automatically.

```yaml
spec:
  ref:
    branch: main
```

### tag

Track a fixed tag. Resolves to whatever commit the tag points at; stable unless the tag is moved upstream.

```yaml
spec:
  ref:
    tag: v1.2.3
```

### commit

Pin to a specific commit SHA. Fully immutable — the resolved revision never changes.

```yaml
spec:
  ref:
    commit: abc123def456789aabbccddeeff001122334455
```

### semver

A semver constraint matched against the repository's tags. Resolves to the highest tag satisfying the constraint, so new releases are picked up automatically while staying within the range.

```yaml
spec:
  ref:
    semver: ">=1.2.0 <2.0.0"
```

## Authentication Methods

`spec.auth` is a discriminated union on `auth.type`. The active method's block is set; all others are omitted. Omit `auth` entirely for public repositories.

### basic

HTTP basic auth — a username plus password or personal access token, each read from a Secret.

```yaml
spec:
  auth:
    type: basic
    basic:
      usernameRef: { secretRef: { name: payments-git-basic, key: username } }
      passwordRef: { secretRef: { name: payments-git-basic, key: password } }
```

### sshKey

An SSH private key stored in a Secret, for `ssh://` and `git@` remotes. Optionally pin a `known_hosts` entry for strict host key checking (recommended for production).

```yaml
spec:
  auth:
    type: sshKey
    sshKey:
      secretRef:
        name: payments-git-ssh
        key: id_rsa
      knownHostsRef:
        configMapRef:
          name: github-known-hosts
          key: known_hosts
```

### token

A bearer token (e.g. a GitHub PAT or GitLab token) stored in a Secret.

```yaml
spec:
  auth:
    type: token
    token:
      secretRef: { name: payments-git-token, key: token }
```

### githubApp

GitHub App installation auth (JWT-based). Finer-grained permissions than a PAT and not tied to an individual user. The controller mints short-lived installation tokens from the app's private key.

```yaml
spec:
  auth:
    type: githubApp
    githubApp:
      appID: "123456"
      installationID: "7890123"
      privateKeyRef: { secretRef: { name: github-app-key, key: key.pem } }
```

### oidc

OIDC client-credentials flow — the controller discovers the token endpoint from the issuer's `/.well-known/openid-configuration` and exchanges client credentials for a token. Client authentication is a nested discriminated union (`clientAuth.type`): `clientSecret` (shared secret), `privateKeyJWT` (signed JWT assertion), or `tlsClientAuth` (mTLS).

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
```

### workloadIdentity

The controller pod uses its own cloud-native identity (e.g. IRSA on EKS) to authenticate to cloud-hosted Git such as AWS CodeCommit. No static credentials are stored.

```yaml
spec:
  auth:
    type: workloadIdentity
```

## Status

| Field                     | Type        | Description                                                                                           |
|---------------------------|-------------|-------------------------------------------------------------------------------------------------------|
| `observedGeneration`      | integer     | Generation of `spec` last reconciled.                                                                 |
| `lastFetched`             | object      | Last successful fetch result.                                                                         |
| `lastFetched.revision`    | string      | Resolved Git SHA.                                                                                     |
| `lastFetched.treeHash`    | string      | Content-addressed tree hash.                                                                          |
| `lastFetched.fetchedAt`   | timestamp   | When the fetch completed.                                                                             |
| `lastFetched.resolvedRef` | string      | Resolved ref details (e.g., for semver, which tag matched).                                           |
| `releaseCount`            | integer     | Number of `Release` objects referencing this source.                                                  |
| `webhooks`                | object      | Webhook operational state, mirrored from the `/webhooks` subresource. See [Webhooks](#webhooks).      |
| `conditions`              | []Condition | See [Conditions](#conditions).                                                                        |

## Conditions

`GitRepository` reports the following standard k8s conditions in `status.conditions`.

| Condition           | What `True` means                                                                                                                                                                    |
|---------------------|--------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `Ready`             | Composite: the repository is reachable, authenticated, and content was fetched successfully. The signal most tooling should watch. Requires `Reachable: True` and `AuthValid: True`. |
| `Reachable`         | Network-level reachability of the Git remote succeeded.                                                                                                                              |
| `AuthValid`         | The most recent authentication attempt succeeded. See [AuthValid](#authvalid) for full semantics.                                                                                    |
| `WebhookConfigured` | At least one webhook is configured and receiving events. See [Webhooks](#webhooks).                                                                                                  |

### AuthValid

`AuthValid` reflects the state of authenticating to the Git remote. It is always present, even when no explicit credentials are configured (public repos report `True` with `reason: NoAuthRequired`).

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
| `True`    | `TokenAcquired`          | Succeeded via explicit token acquisition (GitHub App, workload identity).         |
| `True`    | `CredentialsAccepted`    | Succeeded with simple credentials (basic auth, SSH key, token).                   |
| `True`    | `NoAuthRequired`         | No authentication is configured; repository is public.                            |
| `False`   | `InvalidCredentials`     | Credentials rejected by the remote (401-class).                                   |
| `False`   | `TokenAcquisitionFailed` | GitHub App or workload-identity token exchange failed.                            |
| `False`   | `SecretNotFound`         | Referenced Secret or ConfigMap does not exist.                                    |
| `False`   | `SecretMalformed`        | Secret exists but content is invalid (bad base64, wrong format, missing keys).    |
| `False`   | `PermissionDenied`       | Credentials valid but lack required permissions (403-class).                      |
| `False`   | `TokenExpired`           | Known-expired token; refresh attempt failed.                                      |
| `False`   | `AuthMisconfigured`      | Config error detected without contacting the remote (e.g. invalid GitHub App ID). |
| `Unknown` | `NotYetAttempted`        | Repository is new; auth has not yet been tested.                                  |
| `Unknown` | `RemoteUnreachable`      | Could not contact the remote to test auth (network, DNS, firewall).               |

**Triage with `Reachable`:**

| `Reachable` | `AuthValid` | Action                                                                              |
|-------------|-------------|-------------------------------------------------------------------------------------|
| `False`     | `Unknown`   | Fix network, DNS, firewall, or remote endpoint availability.                        |
| `True`      | `False`     | Fix credentials or auth configuration.                                              |
| `True`      | `True`      | If `Ready: False`, look beyond auth (rate limits, content errors).                  |

`AuthValid: False` always forces `Ready: False`, with `Ready.reason: AuthFailed`.

**Example — successful SSH key auth:**

```yaml
status:
  conditions:
    - type: Ready
      status: "True"
      reason: FetchSucceeded
    - type: Reachable
      status: "True"
    - type: AuthValid
      status: "True"
      reason: CredentialsAccepted
      message: "SSH key authentication successful"
```

**Example — secret missing:**

```yaml
status:
  conditions:
    - type: Ready
      status: "False"
      reason: AuthFailed
    - type: Reachable
      status: "Unknown"
      reason: AuthRequired
    - type: AuthValid
      status: "False"
      reason: SecretNotFound
      message: "Secret 'payments-git-ssh' not found in namespace 'payments-team'"
```

## Webhooks

### Purpose

The `/webhooks` subresource manages webhook configurations for the Source, enabling push-based notifications from the upstream Git provider that trigger source refreshes. Multiple webhooks can be configured per Source, supporting scenarios like multi-mirror Git setups, provider migrations, and chained CI/CD pipelines.

Webhook configuration lives behind a subresource (rather than inline in `spec`) for several reasons:
- Different RBAC than the rest of the Source (platform team manages source definitions; app teams configure their webhook integrations)
- Different lifecycle (webhooks added/removed independently of Source changes)
- Different sensitivity (webhook secrets are credentials)
- Audit clarity (webhook config changes distinguishable from Source spec changes)

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
