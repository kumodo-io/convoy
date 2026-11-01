# convoy

**convoy** is a Kubernetes-native continuous delivery system. You describe *what*
to deploy, *from where*, and *to which cluster* as Kubernetes objects; convoy
fetches the source, renders it, applies it with Server-Side Apply, and
continuously reports whether the live state still matches (drift) and whether the
workloads are healthy.

It is built as an **aggregated API server** plus controllers — so its resources
are real Kubernetes API objects you manage with `kubectl`, RBAC, and admission,
not CRDs or an external control plane.

## Documentation

The documentation is at <https://convoy.kumodo.io/documentation/>.

- [Overview](https://convoy.kumodo.io/documentation/) — the delivery flow and the components.
- [Concepts](https://convoy.kumodo.io/documentation/concepts/) — sources, rendering, mutations, sync, drift, health, rollouts, hooks, pins and deletion.
- [Guides](https://convoy.kumodo.io/documentation/guides/) — step-by-step tasks.
- [API Reference](https://convoy.kumodo.io/documentation/reference/) — the fields, conditions and subresources of each kind, and the `kubectl convoy` CLI.
- [Architecture](https://convoy.kumodo.io/documentation/architecture/) and [Operations](https://convoy.kumodo.io/documentation/operations/) — how convoy is built and how to run it.
- [Roadmap](https://convoy.kumodo.io/documentation/roadmap/) — what is built and what is planned.
- [Known Bugs](https://convoy.kumodo.io/documentation/known-bugs/) — known defects, with workarounds.

The documentation source is in [kumodo-io/convoy-docs](https://github.com/kumodo-io/convoy-docs).

## Components

| Binary | Role |
|---|---|
| `convoy-apiserver` | Serves the convoy API groups as an aggregated API server. |
| `convoy-controller-manager` | Fetches, renders, mutates and applies each `Release`. |
| `convoy-observer` | Detects drift, assesses health, and serves `/preview`, `/diff`, `/health` and `/history`. |
| `kubectl-convoy` | The `kubectl convoy` plugin for the `Release` subresources. |

## Development

The tools and tasks are in [mise](https://mise.jdx.dev) (`mise.toml`, `.mise/conf.d/`). Run
`mise tasks` to list them. The common ones:

- `mise run build`, `mise run lint`, `mise run test` — build, lint and test the code.
- `mise run dev-up` — create the kind clusters for a full local environment.
- `mise run tilt-up` — run convoy in the kind cluster with Tilt.

## License

[Apache License 2.0](LICENSE).
