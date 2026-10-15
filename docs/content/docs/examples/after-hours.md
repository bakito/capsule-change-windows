---
title: Downscale after work and resume in the morning
weight: 45
---

Scale every Deployment and StatefulSet in selected development namespaces to zero
after work. Restore each workload's original replica count in the morning.
This example only schedules actions; it adds no admission restrictions or warnings.

## Complete example

{{< example "after-hours.yaml" >}}

The window starts every day at **19:00 in Europe/Zurich** and lasts **12 elapsed
hours**, normally restoring workloads at **07:00**. `timeZone` is set **per schedule**
within the ChangeWindow, so different windows can use different IANA zones without
changing controller configuration. Its default is `UTC`.

On a night when clocks change for daylight saving, twelve elapsed hours can end
at 06:00 or 08:00 local time. The API specifies a duration, not a separate local
end time. See [time zones]({{< relref "/docs/guides/schedules#cron-and-time-zones" >}}).

## Workloads and namespace scope

Label the affected namespaces `environment=development`. Omitting target label
selectors selects all Deployments and StatefulSets in those namespaces. Use
`namespaceSelectors: [{}]` to include every eligible namespace, and grant executor
permissions in each one. Protected infrastructure namespaces remain excluded.

These workloads expose a `/scale` endpoint. DaemonSets, Jobs, CronJobs, and standalone
Pods do not share that scaling behavior and are not selected. ReplicaSets managed
by Deployments scale through their owning Deployment.

Each workload's replica count is captured before downscaling. A workload with
three replicas resumes with three; one already at zero stays at zero. Captures
survive controller restarts. Newly created workloads join the next window, and
deleted workloads or replacements with a different UID are not restored.

## Executor permissions

The complete YAML includes `after-hours-executor`, its impersonation permissions,
and workload RBAC in the existing `development` namespace. Repeat the
workload Role and RoleBinding for each selected namespace. Adjust the controller
namespace and manager ServiceAccount subject for your Helm release, following
the [executor setup instructions]({{< relref "/docs/guides/security#executor-permissions" >}}).
Set `spec.serviceAccount.namespace` in the ChangeWindow to the same executor
namespace and retain the executor name `after-hours-executor`.

Apply the complete example file to deploy the account, permissions, and window
together.

The start action explicitly takes ownership of `spec.replicas` using `force`.
Coordinate with HPAs, GitOps reconciliation, and other replica writers: this
action runs once at the start and end, rather than continuously holding replicas
at zero. See [resource actions]({{< relref "/docs/guides/actions" >}}).
