---
title: Restrict replica changes with CEL
weight: 20
---

Freeze Deployment replica changes in production namespaces during Sunday
maintenance, while allowing other Deployment updates. The same condition works
for parent Deployment updates and the `scale` subresource because both payloads
contain `spec.replicas`.

## Complete example

{{< example "conditional-maintenance.yaml" >}}

The manifest also deploys the window's required ServiceAccount. This admission-only
identity has no workload or impersonation permissions.

## Expected behavior

During the active window, members of `application-developers` and the
`production/deployer` ServiceAccount cannot change replica counts in namespaces
labeled `environment: production`. A Deployment image update that preserves
replicas remains allowed by this rule. The excluded `break-glass` user is
unaffected by the denial. Other namespaces retain their existing behavior.

The warning entry independently selects the developers group and returns a
notice on replica updates in the hour before the next window. The ServiceAccount
is not included in that warning unless it also belongs to the selected group.

This example matches UPDATE only, so both objects are present. Conditions for
CREATE or DELETE must account for null `oldObject` or `object` respectively.
For subresource writes, CEL receives the actual `Scale` objects; target label
selection uses the parent Deployment. See the
[matching reference]({{< relref "/docs/guides/admission#match-conditions" >}})
for variables, limits, and error behavior.
