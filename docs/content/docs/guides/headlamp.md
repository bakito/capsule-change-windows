---
title: Headlamp
weight: 80
---

The companion plugin adds **Change Windows** to Headlamp's main navigation. Its
overview combines a weekly calendar with a searchable table of ChangeWindows.
Open an entry for its schedules, namespace selection, targets, actions and rules.

The calendar displays time in your browser's time zone, while each schedule keeps
its configured zone. Overlapping and adjacent windows are merged. Active state
follows scheduled time; controller readiness and pending resources appear
separately. The preview is not proof that a workload patch has completed.

## Resource maintenance topic

Namespaced resource detail pages include a **Maintenance windows** section. It
shows active windows that select the resource, their end time when known, and
the applicable resource actions and admission rules.

For Pods, the plugin also checks verified controlling owners such as ReplicaSets
and Deployments. It identifies which owner is selected: a restriction on a
Deployment does not automatically restrict Pod writes. Owner labels refresh every
30 seconds. Missing namespace or owner permissions are shown explicitly.

Selection uses current labels. Resources added after a window starts may be
selected by the API definition but are not part of the controller's original
action snapshot. Journal Secrets are never read by the plugin.

## Build and install the plugin

```shell
devbox shell
task headlamp:install
task headlamp:check
task headlamp:package
```

The source is under `headlamp/`. The SDK produces a bundle in `headlamp/dist/` and
a local archive when packaging. Install it as `capsule-change-windows` using your
[Headlamp plugin distribution method](https://headlamp.dev/docs/latest/development/plugins/building/).
It is tested against Headlamp 0.45.0. The controller and CRD are separate
prerequisites; this UI does not install them.

Grant the viewer read access to ChangeWindows and namespaces. Parent workload
reads are needed for owner matching. The repository includes a sample
`headlamp/deploy/viewer-role.yaml`; bind it deliberately and narrow workload reads
to individual namespaces where appropriate. The plugin uses Headlamp's existing
login and never needs a new administrator credential.

When using a custom controller namespace, configure it per Headlamp cluster in
the plugin settings so that infrastructure namespace remains excluded.

## Releases

Pull requests and `main` pushes run the centralized Headlamp test action.
Pushing a `v*` semantic-version tag runs the checks again, then attaches the
plugin archive to the controller release after that release succeeds.
The publish action writes versioned Artifact Hub metadata to the
`headlamp-plugin` branch at `capsule-change-windows/<version>/`.
See `headlamp/README.md` for publication and Artifact Hub registration details.

## Plugin development

```shell
task headlamp:serve
# Against an already reachable Headlamp frontend:
HEADLAMP_URL=http://127.0.0.1:18081 task headlamp:test:browser
```

Devbox pins Node; npm pins the SDK and dependencies. The plugin uses Headlamp's
shared React/MUI components and public extension APIs. Unit tests validate
scheduling, selection, owner identity and UI error states. Browser tests load the
bundle in Headlamp with intercepted fixtures, requiring no policy changes or
credentials. See `headlamp/README.md` for browser setup, permission details, and
performance checks.
