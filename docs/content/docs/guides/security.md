---
title: Security and permissions
weight: 60
---

ChangeWindows are cluster scoped and can restrict writes across namespaces and
to selected Nodes.
Only trusted cluster administrators should create or modify them. Namespace
selectors establish scope; they do not authorize access to selected workloads.

## Executor permissions

Resource actions require `spec.serviceAccount`. The controller impersonates that
identity for workload reads and patches. Admission-only policies do not need an
executor.

Grant two separate permissions: the manager may impersonate the specific approved
service account, and that executor may read and patch the selected workload
resources in the intended namespaces. The default installation grants neither
arbitrary service-account impersonation nor workload write access.

The samples assume an existing `development` namespace. Before applying them,
adapt the controller identity to your Helm release. For the release used in the
[installation guide]({{< relref "/docs/getting-started/installation" >}}), find the
manager service account with:

```shell
kubectl -n capsule-system get pods \
  -l app.kubernetes.io/instance=scheduled-action \
  -o jsonpath='{.items[0].spec.serviceAccountName}{"\n"}'
```

In the RBAC sample, replace every `schedule-action-controller-system` namespace
with `capsule-system` and the impersonation RoleBinding's
`schedule-action-controller-controller-manager` subject name with that command's
output. Set the ChangeWindow's `spec.serviceAccount.namespace` to `capsule-system`
and keep its name equal to the executor ServiceAccount's name. The manager and
executor are separate identities.

{{< example "executor-rbac.yaml" >}}

Repeat the workload Role and RoleBinding in each permitted namespace; avoid broad
cluster grants merely to satisfy a selector. If you choose another Helm release
name or namespace, use those values when looking up the manager and adapting the
samples.

## Admission availability

The webhook uses `failurePolicy: Fail` for namespaced writes outside the protected
infrastructure namespaces, and for CREATE, UPDATE, and DELETE on core v1 Nodes.
Node status and lease updates are not intercepted. Unsynchronized or invalid
policy caches fail closed, including when no policy currently selects a Node.
Plan webhook availability accordingly: every controller replica serves admission,
while leader election restricts resource actions to one replica.

Policies and namespace labels follow informer consistency. The indexed cache
avoids recompiling policies for each request. For requests such as `/scale`,
admission may need to read the parent to evaluate labels. The default controller
RBAC permits those reads for Deployments, StatefulSets, and ReplicaSets. Other
target kinds can require explicit additional controller read permissions. Node
selection reads the admission payload and needs no additional Node read RBAC.

Audience exclusions grant no permissions. Evaluate their interaction with other
policies before relying on a recovery identity. CEL programs are cached by policy
generation and have bounded evaluation costs; request objects and authorization
decisions are never stored in that compiled cache. A denial's evaluation error
fails closed, while a warning's evaluation error returns a diagnostic warning.

## Journals and object identity

Capture Secrets contain original values and rendered patches. Restrict access to
the controller namespace and use Kubernetes encryption at rest where required.
Do not put captured data in logs, status, or metrics.

UID and resource-version preconditions prevent patches from silently applying to
a replacement object or ignoring concurrent changes. Restores use the recorded
UID; deleted workloads are not recreated. Retain the journal and finalizer until
required end actions have completed.
