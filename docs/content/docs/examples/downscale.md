---
title: Downscale and restore workloads
weight: 40
---

Reduce selected Deployments to zero replicas overnight and restore the captured
replica count when the window ends. Overlapping schedules merge into one window,
so workloads remain downscaled across adjoining periods.

## Complete example

{{< example "addons_v1alpha1_changewindow.yaml" >}}

## Executor permissions

The complete YAML includes its executor ServiceAccount and narrowly scoped RBAC.
Follow the [executor setup instructions]({{< relref "/docs/guides/security#executor-permissions" >}})
to adapt the sample to your Helm release. Repeat the workload Role and RoleBinding
in every permitted namespace the window selects.
The example's `serviceAccount` must match the account granted these permissions.

Apply the complete example file so the account and permissions are deployed with
the ChangeWindow.

Start actions capture state before applying changes. Captures survive controller
restarts. End actions restore the recorded resource UID, avoiding replacements
that happen to reuse a deleted Deployment's name. See
[resource actions]({{< relref "/docs/guides/actions" >}}) for SSA ownership,
subresources, restoration, and failure handling.
