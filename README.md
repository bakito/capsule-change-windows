# Scheduled Action Controller

A standalone Capsule addon for scheduled Kubernetes resource changes and maintenance
admission rules. The cluster-scoped `ChangeWindow` API belongs to
`addons.projectcapsule.dev/v1alpha1`. Capsule core and Tenant objects are not required.

Select namespaces and namespaced resource kinds, define cron windows, then apply
Go-templated patches at the start and end. Capture existing values before mutation
to restore replicas afterward. Admission can deny writes during or outside windows
and return advance or active-window warnings. Each denial and warning supports
User/Group/ServiceAccount audiences and CEL match conditions. Admission can also
protect Node pools selected by Node labels.

See the [complete ChangeWindow example](examples/addons_v1alpha1_changewindow.yaml)
and [executor RBAC example](examples/executor-rbac.yaml).

## Documentation

The [Hugo/Doks documentation](docs/content/docs/getting-started/overview.md) includes installation, scheduling,
resource actions, admission and warnings, operations, a dedicated
[Examples section](docs/content/docs/examples/_index.md), a development guide, and a
[generated API reference](docs/content/docs/reference/api.md). Preview it with
`devbox run -- task docs:serve`, then open <http://localhost:1313>.
Use `task docs:build` for static output in `dist/docs/` and `task docs:check` to
validate the reference, examples, and rendered links.

The site requires **Hugo Extended 0.166.0 or newer** and **Node 24**.
Documentation tasks install the locked Doks dependencies automatically. Use the pinned Devbox
environment; a system/Homebrew Hugo earlier on `PATH` can otherwise cause a
misleading `template for shortcode "example" not found` error. Documentation
tasks check the Hugo version before generating or building anything.

## Headlamp plugin

The [plugin in headlamp/](headlamp/README.md) adds **Change Windows** to Headlamp's
main navigation: a weekly calendar, searchable action table, detail pages, and a
maintenance section on namespaced resources, including Pods through verified
workload owner references. Run `task headlamp:install headlamp:check` in Devbox.
The plugin uses the viewer's existing Kubernetes permissions and does not execute
actions. See the [Headlamp guide](docs/content/docs/guides/headlamp.md) for installation and RBAC.

## Semantics

The alpha API is now **ChangeWindow** (`changewindows`, short name `cw`), replacing
`ScheduledAction`. This is a breaking rename, not an additional served kind.
Before upgrading an existing installation, use the old controller to finish or
delete old ScheduledActions and wait for restoration/finalizers to complete.
Save their specs, update the kind to `ChangeWindow`, upgrade the CRDs/controller,
then reapply them. Do not remove the old CRD or force-remove finalizers while
restorations are pending. Helm does not upgrade or remove CRDs automatically.
Update RBAC and clients to `changewindows`; metrics now use `change_window_`.
The repository, module, chart, and controller deployment names are unchanged.

- `namespaceSelectors` are ORed. An explicit `[{}]` selects every namespace except
  the controller namespace and `kube-system`, `kube-public`, and `kube-node-lease`.
  These namespaces keep control-plane startup and recovery outside maintenance. Resource selectors use standard Kubernetes label semantics.
- Node targets are admission-only and use labels on Nodes. Omit namespace selectors
  for Node-only windows; other cluster-scoped targets and actions on Nodes are rejected.
- Admission targets reuse Capsule core GVK patterns: `apps`, `apps/*`, `*Set`, or
  `apiVersion: "*"` with `kind: "*"`. Patterns select namespaced resources, including
  new CRDs, and do not require Capsule to be installed. Actions use concrete GVKs.
  Admission `subresources: ["", "*"]` covers parent and all subresource writes.
- `schedules` use five-field cron and IANA time zones (UTC by default). Durations
  are elapsed time, greater than zero and at most 168h. Starts are inclusive and
  ends exclusive. Overlapping and adjacent schedules within one CR form one window.
  DST follows the named timezone: nonexistent local times are skipped and repeated
  times may produce overlapping occurrences.
- Windows use bounded 14-day lookback/lookahead. `window.end`/status end are absent
  or empty when a continuous union extends beyond lookahead. Start is the earliest
  known start within lookback, retained in the journal for that occurrence.
- Actions snapshot matching objects at the first reconciliation in an active
  window. Newly matching resources join the next window. Entirely missed windows
  do not replay obsolete start actions. Restores pending from an earlier window
  finish before a new window can begin.
- Capture uses RFC 6901 JSON pointers and is persisted in a private Secret before
  any mutation. A window supports at most 256 action/resource participants and a
  512 KiB journal. Limits fail before workload mutation. Captures and rendered
  patches are retained only while required for retry/restore.
- Starts and ends retry until successful, then stop. The controller does not
  continuously enforce the patched value. A crash after an accepted write may
  repeat the same SSA patch. Deletion triggers restoration through a finalizer;
  failed restores retain their journal and block deletion. Spec edits are rejected
  while the restoration finalizer is present.
- End actions use recorded UIDs regardless of label changes. Missing resources and
  replacements are skipped. UID/resourceVersion checks guard concurrent writes.
  `force` defaults to false: ownership conflicts are errors, never implicit force.
  Restoring a value does not restore its previous SSA ownership. Different CRs are
  independent; avoid overlapping forced writes to the same fields.

## Templates and subresources

Templates use Go `text/template` with `missingkey=error`, standard template
functions, `toJson`, and `quote`. Context includes `.target` (the parent object),
`.captured.<name>`, `.window.start`, `.window.end`, `.window.startsIn`, and
`.window.phase`. Action manifests are rendered once when the journal is prepared,
so retries reuse stable content. Templates cannot redirect identity or overwrite
UID/resourceVersion preconditions. Output is bounded; no network or shell functions
are exposed. This addon does not implement Capsule resource-loading template context.

Both capture and apply accept `subresource`. Discovery determines the payload kind:
Deployment `/scale` receives `autoscaling/v1 Scale`. Unsupported endpoints and
unsupported SSA return errors without falling back to the parent. Deployment
rollout restart is a parent patch to `spec.template.metadata.annotations`, not
`/rollout`; use `.window.start` as a stable restart timestamp.

## Admission and security

Admission rules support `when: Active` or `Inactive`, `action: Deny`, explicit
operations, target references, subresources, and a custom templated message. Any
deny wins. Parent resources are the default; include `scale` explicitly to cover
scaling. Old and new labels are checked on updates to prevent label removal from
bypassing a restriction. Invalid message rendering still denies the request.

Every denial and warning can independently define `audience.include` and
`audience.exclude` (User, Group, or ServiceAccount), plus named CEL
`matchConditions`. Includes use OR; exclusions win. Conditions use AND and receive
`object`, `oldObject`, `request`, and `namespaceObject`. Omitted filters preserve
existing behavior. Denial evaluation errors fail closed; warning errors produce
diagnostic warnings. See the [admission guide](docs/content/docs/guides/admission.md)
and [node-pool example](docs/content/docs/examples/node-pool.md).

Warnings support `BeforeStart`, `Active`, and `BeforeEnd`. BeforeStart/BeforeEnd
require positive `within` (at most 168h). They are Kubernetes admission warnings on
writes, including dry runs and denied requests, not proactive notifications or
GET/LIST/WATCH responses. Up to eight distinct messages of 256 bytes are returned.

The policy cache is compiled on spec changes and indexed by parent GVR. Namespace
labels and policy updates follow informer consistency. Admission evaluates the
clock directly and does not rely on status to activate a restriction. Unsynchronized
or invalid policy caches fail closed. The webhook uses `failurePolicy: Fail` and is
installed for namespaced writes cluster-wide, except these protected namespaces,
and parent Node writes (not Node status or leases).
Run multiple replicas for availability; every replica serves admission and only
the leader executes actions.

Only cluster administrators should manage ChangeWindows. Resource actions run
as the explicitly configured service account. Grant the controller permission to
impersonate only approved executors, and give those executors workload permissions
only in intended namespaces. The default installation grants no arbitrary executor
impersonation or workload writes. The controller namespace holds sensitive capture
Secrets; restrict access and enable Kubernetes encryption at rest as appropriate.

The authenticated executor marker bypasses only its own ChangeWindow; using the
same service account directly is insufficient. Other policies still apply. Parent
reads for admission to `/scale` are restricted to Deployments, StatefulSets and
ReplicaSets by default; other target kinds require explicit controller read RBAC.

## Install and develop

Helm is the only supported installation method. Install
[cert-manager](https://cert-manager.io/docs/installation/) first, then use the
local chart with an image available to your cluster:

```sh
helm upgrade --install scheduled-action charts/change-windows \
  --namespace capsule-system --create-namespace \
  --set image.registry=registry.example.com \
  --set image.repository=scheduled-action \
  --set image.tag=dev
```

Follow the [installation guide](docs/content/docs/getting-started/installation.md)
to build the image and verify the release. Configure
[executor RBAC](docs/content/docs/guides/security.md#executor-permissions) for your
Helm release before creating action-bearing ChangeWindows. Admission-only CRs do
not need a service account.

For development:

```sh
devbox shell
task build test lint
task e2e IMG=ghcr.io/projectcapsule/change-windows:e2e
```

See [DEVELOPMENT.md](DEVELOPMENT.md),
[CONTRIBUTING.md](CONTRIBUTING.md), and [AGENT.md](AGENT.md) for project conventions.

## Metrics

Metrics are served on authenticated HTTPS port 8443. Grant a Prometheus service
account access to the `/metrics` non-resource URL and configure its bearer token.
Controller-runtime workqueue/reconcile metrics are included. Addon metrics use only
finite phase/result/decision labels:

| Metric | Meaning |
| --- | --- |
| `change_window_transitions_total{phase}` | Observed start/end transitions persisted in status |
| `change_window_executions_total{phase,result}` | SSA attempts, including retries and missing/replaced skips |
| `change_window_execution_duration_seconds{phase}` | SSA attempt duration including API reads/writes |
| `change_window_admission_requests_total{decision}` | Allow, deny and error responses |
| `change_window_admission_warnings_total` | Deduplicated emitted warnings |
| `change_window_admission_duration_seconds` | Admission handler duration |

Counters reset on process restart and are operational telemetry, not an exactly-once
audit ledger. No captured values, resource names or namespace names are metric labels.

## License

Apache-2.0. See [LICENSE](LICENSE).
