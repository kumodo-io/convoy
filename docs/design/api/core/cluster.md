<!-- TOC -->
  * [Example](#example)
  * [Spec](#spec)
  * [Connection Modes](#connection-modes)
    * [agentless](#agentless)
    * [agent](#agent)
  * [Authentication Methods](#authentication-methods)
    * [execPlugin](#execplugin)
    * [kubeconfig](#kubeconfig)
    * [token](#token)
    * [clientCert](#clientcert)
    * [workloadIdentity](#workloadidentity)
    * [oidc](#oidc)
  * [Deletion Policy](#deletion-policy)
  * [Status](#status)
  * [Conditions](#conditions)
    * [AuthValid](#authvalid)
<!-- TOC -->

Represents a target Kubernetes cluster: connection info, authentication, free-form metadata for templating, and declarative deletion policy. Supports both agentless (push) and agent (pull) connection modes.

**Subresources**

- `/status` — observed reconciliation state
- `/sync` — POST to force re-run health/discovery probe

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
      type: execPlugin
      
      # type: execPlugin — for cloud-provider auth (EKS, GKE, AKS).
      # Token rotation is delegated to the plugin (e.g., aws eks get-token).
      execPlugin:
        command: aws
        args:
          - eks
          - get-token
          - --cluster-name
          - prod-eu
          - --region
          - eu-west-1
        # Optional env vars passed to the plugin
        env:
          - name: AWS_PROFILE
            value: cd-controller
        # Optional reference to credentials the plugin needs (e.g., AWS IAM role)
        credentialsRef:
          secretRef:
            name: aws-cd-creds

      # --------------------------------
      # type: kubeconfig — full kubeconfig blob in a Secret.
      # Convenient but coarse-grained.
      # kubeconfig:
      #   secretRef: { name: prod-eu-kubeconfig, key: config }

      # --------------------------------
      # type: token — bearer token (e.g., service account token).
      # token:
      #   secretRef: { name: prod-eu-sa-token, key: token }

      # --------------------------------
      # type: clientCert — mTLS auth for clusters using cert-based auth.
      # clientCert:
      #   certRef: { name: prod-eu-client-cert, key: tls.crt }
      #   keyRef:  { name: prod-eu-client-cert, key: tls.key }

      # --------------------------------
      # type: workloadIdentity — controller pod has cloud-native identity
      # (IRSA on EKS, Workload Identity on GKE). No static creds needed.

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
  
  # Free-form metadata used in mutation templates (goTemplate / celTransform).
  # Available in templates as {{ .Cluster.metadata.<key> }}.
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
  
  # Number of Releases currently referencing this Cluster.
  # Useful for understanding deletion impact at a glance.
  releaseCount: 23
  
  # For agent mode — populated when connectionMode: agent.
  # agentStatus:
  #   agentVersion: v1.0.3
  #   registeredAt: 2026-03-15T08:00:00Z
  #   lastHeartbeat: 2026-04-29T10:00:30Z
  
  conditions:
    - type: Ready
      status: "True"
      lastTransitionTime: 2026-04-29T10:00:00Z
      reason: ConnectionEstablished
      message: "Cluster reachable, capabilities discovered"
    - type: ConfigValid
      status: "True"
      lastTransitionTime: 2026-04-29T09:55:00Z
    - type: Reachable
      status: "True"
      lastTransitionTime: 2026-04-29T10:00:00Z
    - type: AuthValid
      status: "True"
      lastTransitionTime: 2026-04-29T10:00:00Z
      reason: TokenAcquired
```

## Spec

| Field                            | Type   | Required | Default    | Description                                                                                                        |
|----------------------------------|--------|----------|------------|--------------------------------------------------------------------------------------------------------------------|
| `connectionMode`                 | enum   | yes      | —          | `agentless` or `agent`. Selects which connection block applies. See [Connection Modes](#connection-modes).         |
| `agentless`                      | object | one-of   | —          | Direct API-server access config (when `connectionMode: agentless`).                                                |
| `agentless.server`               | string | yes      | —          | Target cluster's kube-apiserver URL.                                                                               |
| `agentless.caPublicCertRef`      | object | no       | —          | Reference to the CA cert (ConfigMap) used to verify the API server's TLS cert.                                     |
| `agentless.auth`                 | object | yes      | —          | Authentication method — discriminated union on `auth.type`. See [Authentication Methods](#authentication-methods). |
| `agentless.connectionParameters` | object | no       | —          | Client tuning: `qps`, `burst`, `timeout`.                                                                          |
| `agent`                          | object | one-of   | —          | Agent pull-mode config (when `connectionMode: agent`).                                                             |
| `metadata`                       | map    | no       | —          | Free-form per-cluster metadata for templating; exposed as `{{ .Cluster.metadata.<key> }}`. String values.          |
| `capabilities`                   | object | no       | discovered | Cluster capabilities (`serverSideApply`, `apiVersions`) used by the renderer. Auto-discovered if omitted.          |
| `deletionPolicy`                 | object | no       | —          | Declarative deletion behavior. See [Deletion Policy](#deletion-policy).                                            |

## Connection Modes

`spec.connectionMode` is a discriminated union. Exactly one of `spec.agentless` / `spec.agent` is set, matching the mode.

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

An in-cluster agent pulls desired state from the control plane and applies it locally. Used for clusters that can't be reached directly from the control plane (private/airgapped networks, egress-only connectivity).

```yaml
spec:
  connectionMode: agent
  agent:
    # Token the in-cluster agent uses to register and authenticate with the control plane.
    registrationTokenRef:
      secretRef: { name: prod-eu-agent-registration, key: token }
    # Optional: pin the expected agent identity to reject spoofed agents.
    expectedAgentID: prod-eu-agent
```

## Authentication Methods

`spec.agentless.auth` is a discriminated union on `auth.type`. The active method's block is set; all others are omitted.

### execPlugin

Delegates token acquisition to an external command (e.g. `aws eks get-token`). Best for cloud-provider auth (EKS, GKE, AKS) where token rotation is handled by the plugin.

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
        credentialsRef:
          secretRef: { name: aws-cd-creds }
```

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

A static bearer token (e.g. a long-lived ServiceAccount token). Simple, but the token does not rotate automatically.

```yaml
spec:
  agentless:
    auth:
      type: token
      token:
        secretRef: { name: prod-eu-sa-token, key: token }
```

### clientCert

mTLS client-certificate authentication, for clusters configured with cert-based auth.

```yaml
spec:
  agentless:
    auth:
      type: clientCert
      clientCert:
        certRef: { name: prod-eu-client-cert, key: tls.crt }
        keyRef:  { name: prod-eu-client-cert, key: tls.key }
```

### workloadIdentity

The controller pod uses its own cloud-native identity (IRSA on EKS, Workload Identity on GKE). No static credentials are stored.

```yaml
spec:
  agentless:
    auth:
      type: workloadIdentity
```

### oidc

OIDC client-credentials flow. The controller discovers the token endpoint via `/.well-known/openid-configuration` on the issuer and exchanges client credentials for a token. Client authentication itself is a nested discriminated union (`clientAuth.type`): `clientSecret` (shared secret, most common), `privateKeyJWT` (signed JWT assertion), or `tlsClientAuth` (mTLS).

```yaml
spec:
  agentless:
    auth:
      type: oidc
      oidc:
        issuerURL: https://auth.example.com/realms/internal
        clientId: cd-controller
        clientAuth:
          type: clientSecret           # | privateKeyJWT | tlsClientAuth
          clientSecret:
            secretRef: { name: oidc-client, key: client-secret }
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
| `releaseCount`          | integer     | Number of `Release` objects referencing this Cluster (deletion-impact at a glance).     |
| `agentStatus`           | object      | Agent version / registration / heartbeat — populated only when `connectionMode: agent`. |
| `conditions`            | []Condition | See [Conditions](#conditions).                                                          |

## Conditions

`Cluster` reports the following standard k8s conditions in `status.conditions`.

| Condition     | What `True` means                                                                                                                                                                              |
|---------------|------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `Ready`       | Composite: the cluster is reachable, authenticated, and its capabilities/version have been discovered. The signal most tooling should watch. Requires `Reachable: True` and `AuthValid: True`. |
| `ConfigValid` | The `spec` is internally consistent (connection mode matches the populated block, auth config is well-formed) — validated without contacting the cluster.                                      |
| `Reachable`   | Network-level reachability of the target kube-apiserver succeeded.                                                                                                                             |
| `AuthValid`   | The most recent authentication attempt succeeded. See [AuthValid](#authvalid) for full semantics.                                                                                              |

### AuthValid

`AuthValid` reflects the state of authenticating to the target cluster. It is always present, even when no explicit credentials are configured.

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
| `True`    | `TokenAcquired`          | Succeeded via explicit token acquisition (execPlugin, workload identity, OIDC).   |
| `True`    | `CredentialsAccepted`    | Succeeded with simple credentials (token, client cert, kubeconfig).               |
| `False`   | `InvalidCredentials`     | Credentials rejected by the cluster (401-class).                                  |
| `False`   | `TokenAcquisitionFailed` | OIDC or workload-identity token exchange failed.                                  |
| `False`   | `SecretNotFound`         | Referenced Secret or ConfigMap does not exist.                                    |
| `False`   | `SecretMalformed`        | Secret exists but content is invalid (bad base64, wrong format, missing keys).    |
| `False`   | `PermissionDenied`       | Credentials valid but lack required permissions (403-class).                      |
| `False`   | `TokenExpired`           | Known-expired token; refresh attempt failed.                                      |
| `False`   | `AuthMisconfigured`      | Config error detected without contacting the cluster (e.g. invalid issuer URL).   |
| `Unknown` | `NotYetAttempted`        | Cluster is new; auth has not yet been tested.                                     |
| `Unknown` | `RemoteUnreachable`      | Could not contact the cluster to test auth (network, DNS, firewall).              |

**Triage with `Reachable`:**

| `Reachable` | `AuthValid` | Action                                                                              |
|-------------|-------------|-------------------------------------------------------------------------------------|
| `False`     | `Unknown`   | Fix network, DNS, firewall, or API-server availability.                             |
| `True`      | `False`     | Fix credentials or auth configuration.                                              |
| `True`      | `True`      | If `Ready: False`, look beyond auth (capability discovery, rate limits).            |

`AuthValid: False` always forces `Ready: False`, with `Ready.reason: AuthFailed`.

**Example — workload identity success:**

```yaml
status:
  conditions:
    - type: Ready
      status: "True"
      reason: ConnectionEstablished
    - type: Reachable
      status: "True"
    - type: AuthValid
      status: "True"
      reason: TokenAcquired
      message: "EKS GetToken succeeded via IRSA role arn:aws:iam::123456789012:role/cd-eks-deployer"
```
