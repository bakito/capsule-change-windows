---
title: Overview
weight: 10
---

Schedule Kubernetes resource changes and maintenance restrictions with one
cluster-scoped resource. **ChangeWindow** is a standalone Capsule addon:
Capsule core and Tenant objects are not required.

Select namespaces and resource kinds, define recurring windows, then choose what
happens inside them. Scale development workloads down overnight and restore their
replicas in the morning. Freeze writes during maintenance. Warn users before a
window starts, when they interact with affected resources.

| Capability | What it does |
| --- | --- |
| [Schedules]({{< relref "/docs/guides/schedules" >}}) | Combine cron windows across named time zones. |
| [Resource actions]({{< relref "/docs/guides/actions" >}}) | Apply templated changes at the start and end, including `/scale`. |
| [Admission rules]({{< relref "/docs/guides/admission" >}}) | Deny selected writes during or outside a window, with a custom message. |
| [Warnings]({{< relref "/docs/guides/admission#warning-periods" >}}) | Return advance, active, and ending-window notices on API writes. |
| [Operations]({{< relref "/docs/guides/operations" >}}) | Inspect readiness, pending restoration, and Prometheus metrics. |

Start with [installation]({{< relref "/docs/getting-started/installation" >}}), try the [maintenance example]({{< relref "/docs/getting-started/installation#first-maintenance-window" >}}),
or browse the [generated API reference]({{< relref "/docs/reference/api" >}}). The current API is
`addons.projectcapsule.dev/v1alpha1`.

## How a window works

1. Namespace and resource selectors define the scope.
2. Named schedules form a union of overlapping or adjacent windows.
3. On entry, the controller snapshots matching resources, records captures, and
   applies start actions using the configured executor service account.
4. Admission rules and warnings evaluate the current time on each affected write.
5. On exit, end actions restore or change the recorded resources. Failed actions
   retry using the durable journal.

Admission rules can be used on their own, without resource actions. Every window
requires a service account; admission-only identities need no executor RBAC.
Resource actions are transitions: after a successful patch, they do not continuously
enforce the value throughout the window.
