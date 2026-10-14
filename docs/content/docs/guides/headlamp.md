---
title: Headlamp
weight: 80
---

The companion plugin adds **Change Windows** to Headlamp's main navigation. Its
overview combines weekly, monthly and yearly calendar views with a searchable
table of ChangeWindows. Use **Previous**, **Next** and **Today** to navigate the
selected period. In the yearly view, select a month to open its calendar or a
highlighted day to open its week. Monthly cells show up to three entries, with a
link to the week when more windows overlap.
The table shows controller readiness and lifecycle separately from the wall-clock
preview. Open an entry for Headlamp's standard metadata panel, including labels,
annotations, lifecycle and next transition. Conditions and events follow it,
then matching namespaces, a calendar for that window, schedules, targets, actions
and rules. **View YAML**
opens the resource using Headlamp's standard viewer.

Weekly and monthly calendars use a compact grid. On mobile, the week becomes a
list of days and the month scrolls horizontally. Both the overview and individual
window calendars support all three views.

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

[![Artifact Hub](https://img.shields.io/endpoint?url=https://artifacthub.io/badge/repository/change-windows-headlamp)](https://artifacthub.io/packages/search?repo=change-windows-headlamp)

Installation is supported via Plugin Manager for Headlamp.

```yaml
  enabled: true
  configContent: |
    plugins:
      - name: capsule
        source: https://artifacthub.io/packages/headlamp/change-windows-headlamp/capsule-change-windows
        version: 0.1.0
    installOptions:
      parallel: true
      maxConcurrent: 2
```

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

## Try a temporary playground

The [playground]({{< relref "/docs/development/playground" >}}) installs the
operator, Headlamp and this plugin together in a disposable Kind cluster. Run it
locally for up to 30 minutes; it cleans itself up when the timer expires.
