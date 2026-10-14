# Change Windows

## Purpose

Change Windows is a standalone Project Capsule addon for time-based Kubernetes
operations. Its cluster-scoped `ChangeWindow` API uses
`addons.projectcapsule.dev/v1alpha1` for maintenance admission rules, advance
warnings, and scheduled actions such as downscaling and restoring workloads.
It works with or without Capsule and never requires Tenant objects.

## General repository rules

These rules are language-independent and can be reused in other repositories.

- Read the contribution/development guides, task definitions, dependency locks,
  hook configuration, and CI workflows before editing. Preserve unrelated work.
- Reuse maintained upstream tools and existing repository implementations before
  adding dependencies, wrappers, or frameworks. Keep task commands direct and
  inline; avoid helper tasks that only dispatch to other tasks or shell scripts.
- Keep development installations and executable examples together in `playground/`,
  with separate `installation/` and `manifests/` directories. Put generator inputs
  beside the owning code. Do not reintroduce `hack/` or a root `examples/` tree,
  or accumulate loose Go/JS maintenance scripts. Prefer upstream CLIs over wrappers.
- Prefer predefined prek hooks, especially native builtins, over local hooks.
  Use local hooks only for project-specific composition or the shared tool
  environment; those should call existing Task targets through Devbox. Keep the
  configuration in `prek.toml`, without a second hook configuration or recursion.
  Hooks and CI must not require Python.
- Keep tool versions in Devbox configuration and locks. Tasks consume tools from
  PATH; avoid repeated version checks, bespoke installers, and fallback toolchains.
  For a tool absent from the package index, provision a pinned upstream CLI in
  Devbox's ignored environment, without adding a wrapper or separate module.
- Use tools' native validation, linting, and dry runs for configuration changes.
  Do not add loose Go or JS tests that assert Taskfile text, dependency versions,
  workflow strings, or repository layout. Tests must exercise product behavior
  and live alongside the owning code or in its existing integration/browser suite.
- CI calls the same Task targets as local development. Preserve ordered generation
  before consumers; Task dependencies run concurrently, `cmds` task calls do not.
  Quote configurable values, propagate failures, and clean up temporary files and
  child processes. Keep publishing explicit and separate from local builds.
- Run the checks appropriate to the change and report unrun or unavailable checks.
  Review security, performance, and bounded resource use. Do not create artificial
  test harnesses merely to claim coverage of documentation or tooling changes.
- Never reuse or delete unrelated clusters or credentials. Scope development
  operations to an explicitly selected context. Do not commit or publish unless
  requested; preview release commands with a dry run first.

## Go codebase rules

These rules apply to Go implementation and tests, not to repository maintenance.

- Follow `go.mod`, `.golangci.yml`, neighboring package conventions, and the
  Apache-2.0 source header. Keep domain logic out of entry points. Format tests too.
- Pass context through API operations, preserve useful errors, and bound retries.
  Controllers must reconcile idempotently across retries and leader changes.
- Keep shared compiled objects immutable and synchronized. Define invalidation
  for spec/UID changes and deletion. Reuse indexed lookups and existing caches;
  avoid repeated compilation, cluster-wide scans, and unnecessary API reads.
- Protect object identity with UID checks and optimistic concurrency. Surface SSA
  conflicts instead of silently forcing ownership. Use finite metric labels,
  never object names, namespaces, identities, or error text.
- Place unit tests beside the implementation they exercise. Test observable
  behavior, relevant errors, and boundaries. Use real API-server tests for SSA and
  schema behavior and scoped e2e tests for runtime, admission, and RBAC changes.
- Test concurrent access with `-race`. Add allocation-reporting benchmarks for
  new or materially changed scheduling, admission, and cache paths. Compilation
  alone does not establish an integration or e2e pass.
- Generate APIs and manifests with the existing tasks; never hand-edit generated
  DeepCopy methods, CRDs, or RBAC. Preserve Kubebuilder metadata and scaffold markers.

## Change Windows project rules

The following sections are specific to this addon.

### Prefer Capsule imports

**Always prefer importing and reusing code from `github.com/projectcapsule/capsule`
whenever a suitable implementation is available.** Before implementing functionality,
search Capsule core for existing packages, API types, interfaces, validators,
matchers, templating, and helpers. Direct imports take precedence over copying code,
reimplementing behavior locally, or introducing another dependency for the same job.
The existing `pkg/api/runtime.VersionKind` import is an example of this approach.
If no suitable implementation exists in Capsule, consider suggesting a feature to the core, if suitable. Ask for feedback from the managing entity.

Preserve Capsule's established semantics and use a compatible, pinned module version.
A local alternative requires a documented, concrete reason why the available Capsule
implementation cannot be imported or reused. Importing Capsule code must preserve
this addon's ability to run without a Capsule installation or Tenant objects.

### API and behavior

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

### Development map

- `api/v1alpha1`: API types and validation markers; generated DeepCopy code.
- `internal/schedule`: bounded cron-window evaluation with cached boundaries.
- `internal/policy`: selector/template compilation and immutable indexed policy store.
- `internal/executor`: impersonated resource reads and identity-constrained SSA.
- `internal/controller`: journal lifecycle, finalizers, capture/start/end reconciliation.
- `internal/admission`: policy validation, matching, denials, warnings and cache wiring.
- `internal/metrics`: Prometheus metrics registered with controller-runtime.
- `cmd/main.go`: manager, dependencies, TLS, metrics and webhook registration.
- `playground/manifests`: demo resources, executable ChangeWindow/RBAC examples, and development probes.
- `charts/change-windows`: Helm chart, generated CRDs/RBAC, and enterprise releases.
- `e2e`: isolated Kind integration tests.
- `playground/installation`: Helm values and the Flux HelmRelease for Headlamp; `playground/kind.config.yaml` configures Kind.
- `docs`: Hugo/Doks site, feature guides and generated API reference.
- `headlamp`: read-only Change Windows UI, resource maintenance sections and browser tests.

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

### Execution and security

Journal data is sensitive: store it only in controller-namespace Secrets, never
status, metrics, events, or logs. Bound journal size and participant counts.
Preserve originals across retries; keep restoration finalizers until required
end actions succeed.

Executor service accounts require explicit, narrowly scoped RBAC. Do not grant
arbitrary impersonation, workload write access, or cluster-admin by default.
Only the authenticated executor marker bypasses its own policy; other windows
must still apply. Admission must skip irrelevant operations before API reads.

### Development and validation

Enter `devbox shell`, run `task fmt generate manifests`, then `task check`.
Use native validators and Task dry runs for tooling changes. `task lint:files`
selects the builtin `files` hook group; project hooks must not join that group.
Actionlint also checks workflow shell commands with ShellCheck.

Unit tests cover executable examples through the policy compiler. For scheduling,
admission, and RBAC changes, cover allowed/denied requests, selected/unselected
namespaces, multiple policies, dry runs, subresources, retries, deletion and
replacement, conflicts, and capture retention where relevant.

`task e2e` builds current source and creates its own Kind cluster with a private
kubeconfig. Preserve logs before cleanup, observe the run, then diagnose failures.
`dev:setup` and `dev:deploy` mutate only the explicit development context;
`dev:deploy` builds with ko and installs through Helm. `enterprise:release` and
`enterprise:prerelease` publish; inspect their dry runs before release work.

### Documentation and Headlamp

Run `task docs:generate docs:check` for API/documentation changes. The pinned
upstream `fybrik.io/crdoc` CLI generates `docs/content/docs/reference/api.md`
directly from Helm CRDs. Use its native frontmatter template and TOC configuration;
do not add a Go wrapper, separate tool module, or output-rewriting script.
Devbox provisions crdoc; `docs:check` checks drift, builds root/subpath sites, and
validates local links and fragments with lychee.

Keep Doks/Sass locked in `docs/package-lock.json`; use npm ci --ignore-scripts.
Keep `docs/go.mod` as a boundary excluding npm dependencies from Go tooling.
Share executable examples through Hugo mounts and use relref between pages.
Preserve the default description cascade in `docs/hugo.toml` to avoid Hugo's
automatic Summary panic on live rebuilds. Check incremental previews when changing it.

The homepage YAML explorer uses `playground/manifests/examples/quickstart.yaml` with annotations in
`docs/data/window_example.yaml`. Preserve exact copy/download content and native
links without JavaScript. Run `task docs:test:browser` for interaction changes;
tests live under `docs/tests/` and cover desktop and mobile viewports.

For plugin changes, read `headlamp/README.md` and run `task headlamp:check`.
Use public Headlamp APIs and shared React/MUI libraries. Preserve cluster scope,
selector semantics, owner UID checks, bounded projections, and explicit forbidden
or unavailable states. Never infer completed actions from current labels or read
private capture journals. Browser tests belong in `headlamp/e2e/`, use intercepted
API fixtures, and never create credentials or cluster resources.
