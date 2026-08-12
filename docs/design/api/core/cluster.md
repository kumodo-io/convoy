<!-- TOC -->
  * [Example](#example)
  * [Spec](#spec)
  * [Connection Modes](#connection-modes)
    * [agentless](#agentless)
    * [agent](#agent)
  * [Authentication Methods](#authentication-methods)
    * [kubeconfig](#kubeconfig)
    * [token](#token)
    * [clientCert](#clientcert)
    * [execPlugin](#execplugin)
    * [oidc](#oidc)
  * [Deletion Policy](#deletion-policy)
  * [Status](#status)
  * [Conditions](#conditions)
<!-- TOC -->

Represents a target Kubernetes cluster: connection info, authentication, free-form metadata for templating, and declarative deletion policy. Connection modes are `inCluster` (self-management) and `agentless` (control plane pushes via the target's kube-apiserver); `agent` (pull) is defined in the API but **not implemented** (returns `Unsupported`).

**Subresources**

- `/status` — observed reconciliation state (the only subresource). Re-probing is driven by the controller's resync/requeue, not an on-demand subresource.

## Example

```yaml
apiVersion: core.convoy.kumodo.io/v1alpha1
kind: Cluster
metadata:
  name: prod-eu
  namespace: platform-fleet
  labels:
    env: prod
    region: eu-west-1
    cloud: aws
  annotations:
    convoy.kumodo.io/owner: platform-team@example.com

spec:
  # Discriminated connection mode.
  # - inCluster: this is used to self-management only
  # - agentless: control plane pushes manifests via the cluster's kube-apiserver
  # - agent:     an in-cluster agent pulls desired state and applies locally
  connectionMode: agentless
  
  # connectionMode: agentless
  # Configuration for direct API server access from the control plane.
  agentless:
    # Target cluster's kube-apiserver URL.
    server: https://eks-prod-eu.example.com
    
    # CA certificate for verifying the API server's TLS cert.
    # Either inline (base64-PEM) or via reference. Use reference for rotation-friendliness.
    caPublicCertRef:
      name: prod-eu-ca
      key: ca.crt
    
    # Authentication method — discriminated union. See "Authentication Methods" below.
    auth:
      # type: kubeconfig | token | clientCert | execPlugin | oidc
      # NOTE: only `kubeconfig` is implemented today; the others are defined in the
      # API but return Unsupported at connect time (planned).
      type: kubeconfig
      kubeconfig:
        secretRef: { name: prod-eu-kubeconfig, key: config }

      # --------------------------------
      # type: execPlugin — PLANNED (returns Unsupported). For cloud-provider auth.
      # execPlugin:
      #   command: aws
      #   args: [eks, get-token, --cluster-name, prod-eu, --region, eu-west-1]
      #   env:
      #     - name: AWS_PROFILE
      #       value: cd-controller
      #   # credentialsRef is a flat SecretReference ({name[, namespace]}), not a nested secretRef:
      #   credentialsRef: { name: aws-cd-creds }

      # --------------------------------
      # type: token — PLANNED (Unsupported). Bearer token; field is tokenRef (not secretRef).
      # token:
      #   tokenRef: { name: prod-eu-sa-token, key: token }

      # --------------------------------
      # type: clientCert — PLANNED (Unsupported). mTLS for cert-based clusters.
      # clientCert:
      #   certRef: { name: prod-eu-client-cert, key: tls.crt }
      #   keyRef:  { name: prod-eu-client-cert, key: tls.key }

      # --------------------------------
      # (There is no `workloadIdentity` Cluster auth type — the valid types are
      # kubeconfig | token | clientCert | execPlugin | oidc.)

      # --------------------------------
      # type: oidc — PLANNED (Unsupported)
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
    
    # Connection tuning (sane defaults; override for high-volume or rate-limited clusters).
    connectionParameters:
      qps: 50
      burst: 100
      timeout: 30s
  
  # connectionMode: agent
  # Configuration for agent-based pull mode.
  # Only one of {agentless, agent} should be set, matching connectionMode.
  # agent:
  #   # Token used by the in-cluster agent to register and authenticate with the control plane.
  #   registrationTokenRef:
  #     secretRef: { name: prod-eu-agent-registration, key: token }
  #   
  #   # Optional: pin expected agent identity. If set, control plane rejects connections
  #   # from agents not matching this ID. Useful for preventing agent spoofing.
  #   expectedAgentID: prod-eu-agent
  
  # Free-form metadata used in mutation templates (goTemplate / celTransform) and
  # lifecycle-hook conditions. Keys are flattened directly under `.cluster`, so a key
  # `region` is `{{ .cluster.region }}` (plus `.cluster.name` = the Cluster's name).
  # Keep types as strings (k8s label-style); templates can convert as needed.
  metadata:
    region: eu-west-1
    cloud: aws
    tier: production
    cni: cilium
    k8sVersion: "1.29"
    replicas: "5"
    ingressClass: nginx
  
  # Capabilities the cluster supports — informational, used by the renderer.
  # If omitted, controller discovers these on first successful connection.
  capabilities:
    serverSideApply: true
    # Specific API versions the renderer can rely on (Helm Capabilities.APIVersions).
    apiVersions:
      - apps/v1
      - networking.k8s.io/v1
      - autoscaling/v2
  
  # Declarative deletion behavior. See "Deletion Policy" below.
  deletionPolicy:
    pruneReleases: true | false

status:
  # Observed reality from the controller.
  
  observedGeneration: 3
  
  # Connection health, updated by background probes.
  reachable: true
  lastSeen: 2026-04-29T10:00:30Z
  
  # Discovered cluster info, populated on successful connection.
  # Used by the renderer for Helm Capabilities and API-version checks.
  discoveredVersion: v1.29.4
  discoveredAPIVersions:
    - apps/v1
    - networking.k8s.io/v1
    - autoscaling/v2
    - cilium.io/v2
  
  # releaseCount and agentStatus exist in the API type but are NOT populated by the
  # controller today:
  #   releaseCount: <n>        # number of Releases referencing this Cluster (not written)
  #   agentStatus: { ... }     # agent-mode heartbeat (agent mode not implemented)
  
  # The controller writes exactly ONE condition: Ready. Reasons are Reachable
  # (success), Unsupported (unimplemented mode/auth), or ProbeFailed.
  conditions:
    - type: Ready
      status: "True"
      lastTransitionTime: 2026-04-29T10:00:00Z
      reason: Reachable
      message: "Cluster reachable, capabilities discovered"
```

## Spec

| Field                            | Type   | Required | Default    | Description                                                                                                        |
|----------------------------------|--------|----------|------------|--------------------------------------------------------------------------------------------------------------------|
| `connectionMode`                 | enum   | yes      | —          | `inCluster`, `agentless`, or `agent`. `inCluster` + `agentless` are implemented; `agent` returns `Unsupported`.    |
| `agentless`                      | object | one-of   | —          | Direct API-server access config (when `connectionMode: agentless`).                                                |
| `agentless.server`               | string | yes      | —          | Target cluster's kube-apiserver URL.                                                                               |
| `agentless.caPublicCertRef`      | object | no       | —          | Reference to the CA cert (ConfigMap) used to verify the API server's TLS cert.                                     |
| `agentless.auth`                 | object | yes      | —          | Authentication method — discriminated union on `auth.type`. Only `kubeconfig` is implemented. See [Authentication Methods](#authentication-methods). |
| `agentless.connectionParameters` | object | no       | —          | Client tuning: `qps`, `burst`, `timeout`.                                                                          |
| `agent`                          | object | one-of   | —          | Agent pull-mode config (when `connectionMode: agent`). Not implemented.                                            |
| `metadata`                       | map    | no       | —          | Free-form per-cluster metadata for templating; keys are flattened under `.cluster` (e.g. `{{ .cluster.<key> }}`). String values. |
| `capabilities`                   | object | no       | discovered | Cluster capabilities (`serverSideApply`, `apiVersions`) used by the renderer. Auto-discovered if omitted.          |
| `deletionPolicy`                 | object | no       | —          | Declarative deletion behavior. See [Deletion Policy](#deletion-policy).                                            |

## Connection Modes

`spec.connectionMode` is a discriminated union: `inCluster`, `agentless`, or `agent`. `inCluster` (the control plane manages its own cluster) and `agentless` are implemented; `agent` returns `Unsupported` at connect time (planned).

### agentless

The control plane connects directly to the target cluster's kube-apiserver and pushes manifests. No in-cluster component is required. This is the default and most common mode.

```yaml
spec:
  connectionMode: agentless
  agentless:
    server: https://eks-prod-eu.example.com
    caPublicCertRef:
      name: prod-eu-ca
      key: ca.crt
    auth:
      type: execPlugin
      execPlugin: { ... }
    connectionParameters:
      qps: 50
      burst: 100
      timeout: 30s
```

### agent

_Planned — not implemented (returns `Unsupported`); `spec.agent` is currently an empty struct._ The design: an in-cluster agent pulls desired state from the control plane and applies it locally, for clusters that can't be reached directly (private/airgapped networks, egress-only connectivity).

## Authentication Methods

`spec.agentless.auth` is a discriminated union on `auth.type`. The valid types are `kubeconfig`, `token`, `clientCert`, `execPlugin`, `oidc`. **Only `kubeconfig` is implemented today** — the others are defined in the API but return `Unsupported` at connect time (planned). (There is no `workloadIdentity` type.)

### kubeconfig

A full kubeconfig blob stored in a Secret. Convenient but coarse-grained — the whole file is trusted as-is.

```yaml
spec:
  agentless:
    auth:
      type: kubeconfig
      kubeconfig:
        secretRef: { name: prod-eu-kubeconfig, key: config }
```

### token

_Planned — not implemented (returns `Unsupported`)._ A static bearer token (e.g. a long-lived ServiceAccount token). The field is `tokenRef`:

```yaml
spec:
  agentless:
    auth:
      type: token
      token:
        tokenRef: { name: prod-eu-sa-token, key: token }
```

### clientCert

_Planned — not implemented (returns `Unsupported`)._ mTLS client-certificate authentication:

```yaml
spec:
  agentless:
    auth:
      type: clientCert
      clientCert:
        certRef: { name: prod-eu-client-cert, key: tls.crt }
        keyRef:  { name: prod-eu-client-cert, key: tls.key }
```

### execPlugin

_Planned — not implemented (returns `Unsupported`)._ Delegates token acquisition to an external command (e.g. `aws eks get-token`). `credentialsRef` is a flat `SecretReference` (`{name[, namespace]}`):

```yaml
spec:
  agentless:
    auth:
      type: execPlugin
      execPlugin:
        command: aws
        args: [eks, get-token, --cluster-name, prod-eu, --region, eu-west-1]
        env:
          - name: AWS_PROFILE
            value: cd-controller
        credentialsRef: { name: aws-cd-creds }
```

### oidc

_Planned — not implemented (returns `Unsupported`)._ OIDC client-credentials flow. Client authentication is a nested discriminated union (`clientAuth.type`): `clientSecret`, `privateKeyJWT`, or `tlsClientAuth`. Note the JSON tag is `clientID` (not `clientId`), and the client secret nests under `clientSecret.clientSecretRef`:

```yaml
spec:
  agentless:
    auth:
      type: oidc
      oidc:
        issuerURL: https://auth.example.com/realms/internal
        clientID: cd-controller
        clientAuth:
          type: clientSecret           # | privateKeyJWT | tlsClientAuth
          clientSecret:
            clientSecretRef: { name: oidc-client, key: client-secret }
        scopes: [openid]
        audience: kube-apiserver
        # Optional TLS config for a self-signed corporate IdP:
        # tls:
        #   caPublicCertRef: { name: corporate-ca, key: ca.crt }
        #   insecureSkipVerify: false
```

## Deletion Policy

`spec.deletionPolicy` declaratively controls what happens when the `Cluster` is deleted. The flag `spec.deletionPolicy.pruneReleases`
defines whether to prune `Release` objects referencing the `Cluster`. It default to `false` (zero-value) and in this case the `Release`
objects will be kept in an orphaned state. Otherwise the cascade deletion will be performed.

## Status

| Field                   | Type        | Description                                                                             |
|-------------------------|-------------|-----------------------------------------------------------------------------------------|
| `observedGeneration`    | integer     | `spec` generation last reconciled.                                                      |
| `reachable`             | bool        | Connection health from background probes.                                               |
| `lastSeen`              | timestamp   | Last successful contact with the cluster.                                               |
| `discoveredVersion`     | string      | Discovered Kubernetes version (e.g. `v1.29.4`), populated on successful connection.     |
| `discoveredAPIVersions` | []string    | API versions discovered on the cluster; used by the renderer for Helm Capabilities.     |
| `releaseCount`          | integer     | Defined in the API but **not currently populated** by the controller.                   |
| `agentStatus`           | object      | Defined in the API but **not currently populated** (agent mode is not implemented).     |
| `conditions`            | []Condition | See [Conditions](#conditions).                                                          |

## Conditions

`Cluster` reports a **single** condition in `status.conditions`: `Ready`. (There are no separate `ConfigValid`/`Reachable`/`AuthValid` conditions — those are not implemented.)

| Condition | What `True` means                                                                                     | Reasons                                                                                 |
|-----------|-------------------------------------------------------------------------------------------------------|-----------------------------------------------------------------------------------------|
| `Ready`   | The controller connected to the target and its reachability/version were probed successfully.         | `Reachable` (success); `Unsupported` (mode/auth not implemented); `ProbeFailed` (connect/probe failed, including auth failures). |

Reachability and discovered version are also surfaced directly in `status.reachable` / `status.discoveredVersion`. An authentication failure currently surfaces as `Ready: False` with reason `ProbeFailed` (there is no dedicated auth condition or auth-specific reason code).

**Example:**

```yaml
status:
  reachable: true
  discoveredVersion: v1.29.4
  conditions:
    - type: Ready
      status: "True"
      reason: Reachable
      message: "Cluster reachable, capabilities discovered"
```
