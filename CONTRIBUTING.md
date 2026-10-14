# Contributing

1. Create a focused branch from `main`.
2. Preserve the Kubebuilder layout and existing API group/resource scope.
3. Add tests for changed behavior and regenerate API artifacts where needed.
4. Enter `devbox shell` and run `task check`.
5. Run `task e2e` for controller, API, RBAC, or deployment changes.
6. Use Conventional Commit messages, for example `feat: add schedule validation`.

Go files use the Project Capsule copyright and Apache-2.0 SPDX header from
`api/boilerplate.go.txt`. Never edit generated DeepCopy methods or CRDs by hand.

See [DEVELOPMENT.md](DEVELOPMENT.md) for tooling, layout, and runtime behavior.
Install Git hooks with `devbox run -- task hooks:install`. Hooks use prek and
native tools; no Python or Node.js setup is required.
