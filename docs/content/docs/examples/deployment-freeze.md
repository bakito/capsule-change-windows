---
title: Freeze deployment changes
weight: 30
---

Block Deployment writes during a weekly maintenance window and warn users in
advance. This policy is admission-only: it does not modify workloads or need an
executor ServiceAccount.

## Complete example

{{< example "maintenance-only.yaml" >}}

Namespace selectors choose the affected namespaces. Both parent writes and
`scale` requests are covered. Omitting `audience` and `matchConditions` applies
the entry to every caller and matching write during its configured period.
A custom denial message tells users why their request was rejected.

For a narrower freeze, see the
[replica-change example]({{< relref "/docs/examples/conditional-changes" >}}).
