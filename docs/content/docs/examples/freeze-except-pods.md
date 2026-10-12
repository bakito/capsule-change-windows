---
title: Freeze all changes except Pods
weight: 35
---

Freeze writes to every namespaced resource kind for tenant users during maintenance,
while allowing Pod lifecycle operations. This includes custom resources, even
those installed after the ChangeWindow was created.

## Complete example

{{< example "freeze-except-pods.yaml" >}}

Label the affected namespaces `maintenance=enabled` and replace `tenant-developers`
with the group used by your identity provider. The window runs on Sundays at
02:00 in `Europe/Zurich` for two elapsed hours. It needs no executor ServiceAccount.

## What is blocked

For members of that group, CREATE, UPDATE, and DELETE are denied for every
non-Pod resource in selected namespaces while the window is active. Patches and
server-side apply are covered by CREATE or UPDATE admission. `subresources: ["", "*"]`
includes parent writes and endpoints such as `/scale` and `/status`.

The exception checks the **parent API resource**, so it also allows Pod binding,
eviction, status, and other Pod subresource writes. Checking only `object.kind`
would miss payloads such as `Binding` and `Eviction`. Existing RBAC and other
admission policies still apply; the exception grants no new permissions.

Unselected namespaces, protected infrastructure namespaces, and cluster-scoped
resources are outside this freeze. GET, LIST, WATCH, and CONNECT operations such
as exec are unaffected. For Node changes, use a separate
[Node target]({{< relref "/docs/examples/node-pool" >}}).

## Keep workload recovery working

The audience restricts the freeze to tenant users. Controllers and the scheduler
outside that group can continue reconciliation, including replacement Pods and
related resource/status updates. This is what lets maintenance protect configuration
changes while workloads recover.

Removing `audience` applies the rule to everyone. That also blocks controller
writes to non-Pod resources such as ReplicaSets, EndpointSlices, and PVCs, and can
interrupt recovery even though Pod writes themselves remain allowed. Include any
tenant automation accounts you intend to freeze explicitly in the audience.

See [GVK patterns]({{< relref "/docs/guides/schedules#gvk-patterns" >}}) to narrow
the freeze to particular API groups or kind patterns.
