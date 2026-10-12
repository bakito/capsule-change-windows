# Scheduled Action Controller

## Purpose

This repository is a dedicated Capsule addon for time-based Kubernetes resource
behavior. Its cluster-scoped `ChangeWindow` API uses the group
`addons.projectcapsule.dev/v1alpha1`. It combines maintenance admission rules,
advance warnings, and scheduled server-side apply actions such as downscaling
Deployments and restoring their original replica counts. It is independent of
Capsule core and does not require Tenant objects.

## API and behavior

- Namespace selectors form an OR; each selector retains Kubernetes AND semantics.
  An explicit `[{}]` selects all namespaces except protected infrastructure namespaces.
- Named schedules use five-field cron, IANA time zones, and positive elapsed
  durations. Overlapping and adjacent windows within a ChangeWindow are merged.
- Resource targets name a namespaced GVK and label selector. Namespace scope must
  be enforced for every read and mutation. Cluster-scoped v1 Nodes are an
  admission-only exception, selected by Node labels with no namespace lookup.
  Node-only windows omit namespaceSelectors; Node actions/subresources and other
  cluster-scoped kinds are rejected. Never intercept Node status or lease updates.
- Admission GVK patterns reuse Capsule core's `pkg/api/runtime.VersionKind` matcher.
  Patterns cover namespaced resources only; actions require concrete GVKs. Keep
  exact GVR and pattern indexes immutable, merged in name order without duplicate
  policies or per-request candidate allocations. Do not enumerate discovery to
  expand wildcards: new CRDs must match immediately. Subresource GVK selection
  uses the parent kind; CEL keeps the actual admission payload. Read parent objects
  only when labels or template context require them, without widening default RBAC.
- Every denial and warning supports its own audience include/exclude and named
  CEL matchConditions. Compile once per generation in the policy Store, check
  audience before API reads, and keep request activations private. CEL sees actual
  subresource payloads. Evaluation work is bounded; denial errors fail closed,
  warning errors produce a diagnostic warning without blocking the request.
- Actions use Go templates and SSA. Subresources are real Kubernetes endpoints;
  `/scale` is supported when advertised by discovery, `/rollout` is not a Deployment
  endpoint. Never silently substitute a parent-resource patch.
- Capture precedes mutation and is durable. Preserve originals across retries and
  restarts. End actions use recorded resource UIDs, including after label changes;
  never recreate deleted workloads or restore a replacement with the same name.
- Resource selection is a snapshot at window start. New matching resources join the
  next window. The controller namespace, `kube-system`, `kube-public`, and `kube-node-lease` are
  excluded to keep journals, TLS and control-plane recovery outside maintenance.
  Keep controller selection, admission and installation exclusions consistent.
- Any matching deny wins. Warning/deny templates must not access the network.
  Admission uses wall-clock schedules, not potentially stale status. Dry-run
  requests receive the same decisions and warnings without side effects.

## Development map

- `api/v1alpha1`: API types and validation markers; generated DeepCopy code.
- `internal/schedule`: bounded cron-window evaluation with cached boundaries.
- `internal/policy`: selector/template compilation and immutable indexed policy store.
- `internal/executor`: impersonated resource reads and identity-constrained SSA.
- `internal/controller`: journal lifecycle, finalizers, capture/start/end reconciliation.
- `internal/admission`: policy validation, matching, denials, warnings and cache wiring.
- `internal/metrics`: Prometheus metrics registered with controller-runtime.
- `cmd/main.go`: manager, dependencies, TLS, metrics and webhook registration.
- `examples`: executable ChangeWindow and executor RBAC examples.
- `charts/change-windows`: Helm chart, generated CRDs/RBAC, and enterprise releases.
- `e2e`, `hack`: isolated Kind testing and build/release tooling.
- `docs`: Hugo/Doks site, feature guides and generated API reference.
- `headlamp`: read-only Change Windows UI, resource maintenance sections and browser tests.

Read `CONTRIBUTING.md`, `DEVELOPMENT.md`, `Taskfile.yml`, `devbox.json`, `devbox.lock`,
`prek.toml`, `go.mod`, `.golangci.yml`,
and `.github/workflows` before editing. Preserve the Kubebuilder `PROJECT` and
scaffold markers. Reuse existing packages rather than adding parallel frameworks.

Before adding code, inspect the main `github.com/projectcapsule/capsule` project
for reusable packages, API types, and helpers. Reuse them whenever possible instead
of duplicating their implementation, as with the existing
`pkg/api/runtime.VersionKind` matcher. Preserve Capsule's established semantics and
use a compatible, pinned module version. If direct reuse is unsuitable, document
the reason before introducing a local alternative. Reusing Capsule code must keep
this addon usable without a running Capsule installation or Tenant objects.

Helm is the only supported installation method. Keep user-facing installation,
operations, and executor RBAC guidance aligned with the Helm chart's release
namespace and service accounts. Generate CRDs and RBAC directly into the chart;
do not reintroduce a `config/` tree or a separate installation path.

Build container images through the ko-backed Task targets. `image:build` creates a
local archive without a daemon or registry push; `image:push` explicitly builds and
publishes multi-platform images. Kind consumes the archive. Do not add Dockerfile
builds or duplicate build/test commands in workflows. GitHub Actions should call
Task targets with the shared locked Devbox setup; publishing and signing stay
explicit and restricted to release jobs.

## Correctness, security and scale

- Pass context through API operations, preserve useful errors, and use bounded
  retries. Reconciliation must be idempotent and survive leader changes.
- Journal data is sensitive: store it only in controller-namespace Secrets, never
  status, metrics, events or logs. Keep journal size and participant counts bounded.
- Executor service accounts require explicit, narrowly scoped RBAC. Do not grant
  arbitrary service-account impersonation, workload write access, or cluster-admin
  by default. Only the authenticated executor marker bypasses its own policy;
  other ChangeWindows must still apply.
- Protect object identity with UID and optimistic concurrency. Surface SSA ownership
  conflicts rather than silently enabling force. Do not release a restoration
  finalizer while required end actions remain unsuccessful.
- Keep shared compiled objects immutable and synchronized. Invalidate on spec/UID
  changes and deletion. Admission should use indexed candidates, compile nothing
  per request, and avoid API reads on irrelevant operations/resources.
- Use finite metric labels, never object names, namespaces, identities or error text.
  Review every change for security, bounded work, API round trips and memory growth.

## Required checks

Enter `devbox shell`, run `task fmt generate manifests`, then `task check`.
Never hand-edit generated CRDs, RBAC or DeepCopy methods. `task manifests` also
refreshes chart CRDs and manager RBAC. The exact development tools belong in
`devbox.json` and `devbox.lock`; tasks consume tools from PATH, without downloading
tool binaries into the repository. Keep hooks and CI free of Python tooling:
use prek's native builtins, yamlfmt, ShellCheck, actionlint and Cocogitto.
Use `task test:tooling` for Task/hook/release-helper changes. Preserve serialized
generation before consumers; Task dependencies run concurrently, while `cmds`
task calls are ordered. Pass configurable values via quoted environment variables.
Keep task implementations inline in `Taskfile.yml`; do not add shell wrappers
under `hack/`. Keep concrete targets and avoid mode-dispatch helper tasks.
GitHub-specific output formatting belongs in workflows. All prek hooks must call Task targets through Devbox. Native prek
checks belong in `hack/prek-builtins.toml`, which must not call Task recursively.
Keep inline commands covered by `task lint:shell` and behavior tests. Preserve
signal handling, temporary-file cleanup, and explicit publishing boundaries.
For documentation/API/sample changes, run `task docs:generate docs:check`.
Keep `docs/content/docs/reference/api.md` generated from the Go types. Share executable
examples through the Hugo mounts of `examples/`. The documentation generator
is a pinned Go tool in `hack/tools/`; its dependencies stay out of the controller
module. Doks and its Sass dependencies are pinned in docs/package-lock.json;
documentation tasks install them with npm ci --ignore-scripts. Keep docs/go.mod
as a module boundary so Go tools do not traverse npm dependencies. Use relref
links between documentation pages and verify root and subpath builds.
Keep the default description cascade in docs/hugo.toml and add page-specific
descriptions where useful. This keeps SEO, Open Graph, and Twitter metadata from
invoking Hugo's automatic Summary, which can panic during live content rebuilds.
Check incremental preview updates as well as clean builds when changing this path.
The homepage YAML explorer uses examples/quickstart.yaml and annotations in
docs/data/window_example.yaml. Preserve exact copy/download content and native
links without JavaScript. For interaction changes, install Playwright Chromium
and run task docs:test:browser; CI runs it on desktop and mobile viewports.
For plugin changes, read `headlamp/README.md` and run `task headlamp:check`.
Use Headlamp's public APIs and shared React/MUI libraries. Preserve cluster scope,
Kubernetes selector semantics, owner UID checks, bounded projections and explicit
unavailable/forbidden states. Never infer completed actions from current labels
or read private capture journals. Browser tests use an existing Headlamp frontend
with intercepted API fixtures, and never create credentials or cluster resources.

Add meaningful unit tests for changed behavior, real API-server tests for SSA and
schema behavior, and scoped e2e coverage for scheduling/admission/RBAC changes.
Cover allowed and denied requests, selected and unselected namespaces, different
policies, dry runs, subresources, restart/retry, deletion/replacement, conflicts,
and capture retention where relevant. Test concurrent cache access with `-race`.
Add allocation-reporting benchmarks for new scheduling/admission/cache paths.
Report unavailable or unrun checks explicitly; compilation is not an e2e pass.

`task e2e` builds the current controller and creates its own Kind cluster with a
private kubeconfig. Never reuse/delete the developer's existing clusters. Preserve
logs before cleanup. Observe runs first, then diagnose and fix after they finish.

Use conventional commits and the Apache-2.0 Go header. Do not commit or publish
unless requested. `enterprise:release` and `enterprise:prerelease` publish images
and Helm charts; inspect a dry run first. Enterprise release dry runs print the
publication and deployment commands. `dev:setup` and `dev:deploy` apply resources
to the explicitly selected development context; `dev:deploy` builds current source
with ko and installs through Helm. `task e2e` exercises that path in its own disposable cluster.
