---
title: Freeze a node pool
weight: 10
---

Prevent selected users from creating, updating, or deleting Nodes in a worker
pool every Sunday from 02:00 to 04:00 UTC. The pool is identified by the Node
label `nodepool.example.com/name: workers`; change that label key and value to
match your cluster.

## Complete example

{{< example "node-pool-maintenance.yaml" >}}

## Who is affected

The denial applies to user `node-operator`, members of group
`platform-developers`, and ServiceAccount `platform/deployer`. The
`break-glass` user is excluded even when they belong to that group. Everyone
still needs normal Kubernetes RBAC permissions; an exclusion grants no access.

The advance warning has its own audience. Members of `platform-developers`
receive it on matching writes in the hour before maintenance, except for
`break-glass`. Audiences are independent on every denial and warning entry.

## What is protected

The target selector matches labels on Nodes themselves. Omit
`namespaceSelectors` for a Node-only window because Nodes have no namespace.
During the active window, affected users cannot cordon a selected Node, change
its taints or labels, or delete it. Both old and new labels are checked, so
removing the pool label does not bypass the rule. Nodes in other pools and users
outside the audience remain unaffected by this ChangeWindow.

This protects writes to Node objects. It does not govern a cloud provider's node
pool API, a separate NodePool custom resource, or Pod eviction requests. Node
status and lease updates are outside its scope. Node targets support admission
rules and warnings; scheduled apply actions on Nodes are rejected.

## Try the policy

After [installing the controller]({{< relref "/docs/getting-started/installation" >}}),
apply the downloaded manifest and inspect readiness:

```shell
kubectl apply -f node-pool-maintenance.yaml
kubectl wait --for=condition=Ready changewindow/worker-pool-maintenance --timeout=60s
kubectl get nodes -l nodepool.example.com/name=workers
```

During a scheduled window, a user included in the audience and authorized to
patch Nodes can verify the denial without changing the Node:

```shell
kubectl patch node <worker-node> --type=merge \
  -p '{"spec":{"unschedulable":true}}' --dry-run=server
```

The response contains `Worker pool maintenance`. The same write to an unselected
Node, or by an excluded user, is allowed by this policy. Dry runs use the same
matching and schedule as real writes. For a local test, temporarily use cron
`* * * * *` and duration `2m` to make the window continuously active, then restore
the intended schedule.
