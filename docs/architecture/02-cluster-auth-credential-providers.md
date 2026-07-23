**Cluster Auth — Out-of-Process Credential Providers (`execPlugin`)**

<!-- TOC -->
  * [Problem](#problem)
  * [Why Not Local Exec](#why-not-local-exec)
  * [Design: Credential-Provider Registry](#design-credential-provider-registry)
    * [Components](#components)
    * [Auth Flow](#auth-flow)
    * [The Registry Is Stateless](#the-registry-is-stateless)
  * [What the Plugin Returns](#what-the-plugin-returns)
  * [The Plugin Holds the Cloud Identity](#the-plugin-holds-the-cloud-identity)
  * [Token Caching & Refresh](#token-caching--refresh)
  * [Security](#security)
    * [Registration Is the Trust Boundary](#registration-is-the-trust-boundary)
    * [Transport](#transport)
    * [What Convoy Sends](#what-convoy-sends)
  * [Failure Modes](#failure-modes)
  * [Relationship to Agent (Pull) Mode](#relationship-to-agent-pull-mode)
  * [Summary](#summary)
<!-- TOC -->

This document describes how convoy authenticates to a target `Cluster` whose auth type is `execPlugin`, without embedding third-party auth binaries in convoy's own images. It complements [00-overview](00-overview.md) (component model) and the security decisions on least-privilege identity.

> **Status: planned — none of this is built yet.** `Cluster.spec.agentless.auth.type` accepts `execPlugin` in the API, but no credential-provider registry, plugin protocol, or plugin workloads exist. Today only `kubeconfig` auth is implemented (`token`/`clientCert`/`execPlugin`/`oidc` are defined in the API but not wired). Read this as a design proposal.

## Problem

An exec credential plugin (kubeconfig `users[].user.exec`) is **not compiled into the client** — it names an external executable (`aws`, `gke-gcloud-auth-plugin`, `az`, `aws-iam-authenticator`, …) that client-go forks as a subprocess to obtain a credential. Supporting `execPlugin` the naive way therefore means **every** such binary must be present in the `convoy-controller-manager` image and on `PATH`.

That is untenable: we cannot bake an open-ended, ever-growing set of third-party cloud binaries into our image, version and patch them, and rebuild whenever a new provider appears.

## Why Not Local Exec

Even setting aside image bloat, letting client-go fork the command named in a `Cluster` is a **remote-code-execution surface in the control plane**: whoever can create/edit a `Cluster` could set `command: /bin/sh, args: [-c, "…"]` and have the controller-manager execute arbitrary code with the control plane's privileges and network position. client-go does not allowlist commands. Unrestricted local exec is therefore not an option for convoy.

## Design: Credential-Provider Registry

Instead of forking a local binary, convoy calls an **out-of-process credential provider** over the network. Auth plugins run as **separate workloads** (each image contains exactly one auth binary), self-register with the control plane, and mint credentials on request.

This is a custom credential provider — it deliberately does **not** use client-go's exec authenticator (which only forks a local subprocess). Convoy calls the plugin, receives an `ExecCredential`-shaped response, and builds a `rest.Config` from the returned credential itself.

### Components

- **Registry** (in the control plane) — an in-memory map: `plugin name → endpoint`. Not persisted.
- **Auth-plugin workloads** — one per provider (e.g. `aws`, `gke`, `azure`). Each contains its auth binary and runs with its own cloud identity. On startup (and periodically) it self-registers: *"I am plugin `X`, my endpoint is `…`."*

### Auth Flow

The Cluster reconciler, processing a `Cluster` with `execPlugin` auth:

1. reads the plugin **name** from the Cluster's auth spec;
2. looks the name up in the **registry** → resolves the plugin **endpoint**;
3. sends the **exec config** (args / env / cluster info — never secrets) to the endpoint;
4. receives a **credential** (token or client cert) + expiry in response;
5. builds a `rest.Config` with that credential and connects to the target cluster.

### The Registry Is Stateless

Plugins keep themselves registered via **heartbeats** (repeated `POST /register`, or an equivalent keep-alive). Consequences:

- If the control plane restarts and the in-memory registry is wiped, running plugins simply re-register on their next heartbeat — no persistence needed.
- Entries carry a **TTL**; a plugin that stops heart-beating is evicted, so a dead plugin doesn't linger as a resolvable-but-broken endpoint.

_This is a service-discovery pattern; the cold-start window (empty registry after restart) is handled by failing soft — see [Failure Modes](#failure-modes)._

## What the Plugin Returns

The response mirrors Kubernetes' `ExecCredentialStatus`: a credential that is **either** a bearer `token` **or** a client certificate/key pair, plus an `expirationTimestamp`. Convoy must support **both** shapes — a cert credential means a TLS-client `rest.Config`, which cannot be swapped per-request as cheaply as a bearer token.

## The Plugin Holds the Cloud Identity

A cloud auth plugin does not mint a credential from nothing — e.g. the EKS `aws` plugin uses **its pod's** AWS identity (IRSA / instance role) to call STS and produce a token. Therefore:

- Each plugin workload must run with the cloud identity authorized to obtain credentials for its target clusters.
- **Convoy never holds cloud credentials.** They are isolated per plugin workload.

This isolation is a deliberate benefit of the design: a compromise of the control plane does not expose any cloud provider credential, and each plugin can be scoped to exactly the clusters it serves.

## Token Caching & Refresh

Credentials expire (an EKS token lives ~15 minutes). Because convoy re-renders and re-applies on intervals, it must have a valid credential at each apply. Convoy therefore:

- **caches** the credential per Cluster with its `expirationTimestamp`;
- **refreshes** by re-calling the plugin endpoint before expiry;
- treats a refresh failure as a transient reachability problem (requeue), not a hard error.

This cache/refresh loop is the main body of implementation work; it is bounded and per-Cluster.

## Security

Moving from local exec to a registered network service **removes the arbitrary-command RCE** (a `Cluster` can only name a plugin an operator has deployed; users cannot make convoy execute anything). It introduces new boundaries that must be secured.

### Registration Is the Trust Boundary

Self-registration means anything that can reach the register endpoint could claim to be plugin `aws` and register a **malicious endpoint**; convoy would then send cluster info there and trust the returned credential (credential interception / MITM). Registration therefore requires:

- **authn on `/register`** — only trusted workloads (verified ServiceAccount / namespace) may register a name;
- ideally **endpoint validation** — a given plugin name may only resolve to an expected namespace/ServiceAccount.

### Transport

Calls between convoy and plugin endpoints use **mTLS** (or at minimum in-cluster TLS + ServiceAccount-token auth) so credentials are not intercepted in transit.

### What Convoy Sends

Convoy sends only the **exec config** — `args`, `env`, and (when requested) the target cluster's server URL/CA. It must **not** send secret material: the plugin supplies its own identity (see above). This keeps secrets off the wire and out of the registry path.

## Failure Modes

- **Cold start.** After a control-plane restart the registry is empty until heartbeats repopulate it. A Cluster reconcile that cannot resolve its plugin must **fail soft** — requeue with a clear condition (e.g. `AuthPluginUnavailable: waiting for plugin "X" to register`), never hard-fail.
- **Plugin down.** If the plugin workload is unavailable, the target cluster cannot be authenticated → its Releases cannot apply. This is inherent (a missing local exec binary would fail the same way) but is now a visible network dependency; surface it as a `Cluster`/`Release` condition rather than an opaque error.
- **Stale entry.** TTL/heartbeat eviction prevents convoy from repeatedly dialing a dead endpoint.

## Relationship to Agent (Pull) Mode

For agent (pull) clusters the problem does not arise: the in-cluster **agent** authenticates with its **own ServiceAccount** locally — there is no cross-cluster credential to acquire, so no credential provider is involved. The registry pattern is specifically the **push/`agentless`** answer for clusters that need provider-specific auth; agent mode is the long-term escape hatch for the hardest auth environments.

## Summary

| Aspect | Local exec (rejected) | Credential-provider registry (this design) |
| --- | --- | --- |
| Auth binaries | baked into convoy image | isolated, one per plugin workload |
| New provider | rebuild convoy image | deploy a new plugin workload |
| Arbitrary-command RCE | yes (any `Cluster` creator) | no (only registered plugins callable) |
| Cloud credentials | would live in control plane | isolated per plugin workload |
| Trust boundary | the forked command | plugin **registration** + transport |
| Credential acquisition | client-go forks locally | convoy calls endpoint, builds `rest.Config` |

**Net:** `execPlugin` is served by an out-of-process credential-provider registry — plugin workloads self-register (stateless registry + heartbeats), hold their own cloud identity, and return `ExecCredential`-shaped credentials that convoy caches/refreshes. It removes third-party binaries from convoy's image and eliminates the exec RCE, at the cost of implementing credential caching/refresh and securing registration (authn) + transport (mTLS).
