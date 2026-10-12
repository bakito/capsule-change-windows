---
title: Schedules and selection
description: Select namespaces and resources for ChangeWindows, understand protected namespaces, and configure schedules, time zones, and overlapping windows.
weight: 30
---

```yaml
namespaceSelectors:
  - matchLabels:
      environment: development
  - matchLabels:
      environment: staging
targets:
  - name: deployments
    apiVersion: apps/v1
    kind: Deployment
    selector:
      matchLabels:
        downscaling: enabled
schedules:
  - name: weekday-nights
    cron: "0 19 * * 1-5"
    timeZone: Europe/Zurich
    duration: 12h
  - name: weekend
    cron: "0 7 * * 6"
    timeZone: Europe/Zurich
    duration: 48h
```

These fields belong under `spec`. Arrays of namespace selectors use
**OR**: a namespace matching any entry is selected. Within an entry, Kubernetes
label selector requirements use **AND**. Target selectors then match labels on
resources in those namespaces. Every action or admission rule refers to targets
by their unique names.

An explicit `namespaceSelectors: [{}]` selects all eligible namespaces, subject to
the protected namespace exclusions below. Namespaced targets require at least one
namespace selector. Selection never grants the executor additional RBAC.

For admission rules and warnings, targets may also select cluster-scoped `v1`
Nodes by their own labels. Omit `namespaceSelectors` for Node-only windows. If a
window combines Nodes and namespaced targets, namespace selectors apply only to
the namespaced targets. Other cluster-scoped kinds and scheduled actions on Nodes
are rejected. See the [node-pool example]({{< relref "/docs/examples/node-pool" >}}).

## Protected namespaces

The following namespaces are always excluded from scheduled actions, admission
denials, and admission warnings:

- `kube-system`
- `kube-public`
- `kube-node-lease`
- The controller's own namespace: the Helm release namespace (`capsule-system`
  in the installation guide).

These exclusions apply even when namespace labels match a selector, when using
`namespaceSelectors: [{}]`, or when targeting all namespaced kinds with wildcard
GVKs. A ChangeWindow cannot override them. This keeps the controller's private
capture journals, TLS resources, and Kubernetes control-plane recovery outside
maintenance restrictions and scheduled mutations.

## GVK patterns

Admission targets reuse Capsule core's `VersionKind` matcher. API version and kind
patterns are case-sensitive; `*` matches any sequence of characters. This is not
a regular expression language.

| `apiVersion` | `kind` | Matches |
| --- | --- | --- |
| omitted, `""`, or `v1` | `ConfigMap` | Core v1 ConfigMaps. |
| `apps/v1` | `Deployment` | That exact GVK. |
| `apps` or `apps/*` | `Deployment` | Deployments in any apps API version. |
| `apps/v1` | `*Set` | Apps v1 kinds ending in Set. |
| `*.example.com/v1*` | `*` | Kinds in matching groups and versions. |
| `*` | `*` | Every namespaced kind, including future custom resources. |

Patterns apply to both denial rules and warnings. Namespace and target label
selectors still apply. They never select cluster-scoped resources; Node protection
requires a concrete `apiVersion: v1`, `kind: Node` target. For subresource requests,
GVK matching uses the **parent kind** (for example Deployment for `/scale`), while
CEL `object` remains the actual admission payload (Scale).

Scheduled actions require a concrete namespaced GVK. Their capture, enumeration,
and writes do not expand wildcard targets. See the
[freeze-except-Pods example]({{< relref "/docs/examples/freeze-except-pods" >}})
and [after-hours scaling]({{< relref "/docs/examples/after-hours" >}}).

## Cron and time zones

Use five-field cron: minute, hour, day of month, month, day of week. Seconds and
inline `TZ=` or `CRON_TZ=` prefixes are not supported. Set an IANA zone in
`timeZone`; the default is `UTC`.

Durations are positive elapsed time, up to `168h`. Daylight-saving transitions
follow the selected zone: nonexistent local start times are skipped, and repeated
times may produce overlapping occurrences. A duration of `24h` means 24 elapsed
hours, which may end at a different local hour across a clock change.

## Overlapping windows

Start boundaries are inclusive and end boundaries are exclusive. Overlapping and
adjacent schedules in one ChangeWindow merge into one continuous window. The
example joins Friday night to the weekend: workloads are not briefly restored at
07:00 on Saturday.

Evaluation uses a bounded 14-day lookback and lookahead. If a continuous union
extends beyond that horizon, `status.windowEnd` is absent and `.window.end` is an
empty string. Avoid messages or templates that assume every window has a known
end. The journal preserves the known start of the current occurrence.

## Selection over time

Admission uses current cached namespace and resource labels and evaluates the
clock directly. It does not wait for `status.active` to change. On UPDATE, both
old and new resource labels are considered so removing a matching label cannot
bypass a restriction.

Resource actions take a snapshot on the first reconciliation inside a window.
Newly created or newly selected resources join the next window. End actions use
recorded resource UIDs even if labels change. Deleted resources and replacements
with the same name are skipped.

If the controller misses an entire window, it does not replay an obsolete start
action. Pending restoration from a previous window completes before the next
window starts executing actions.
