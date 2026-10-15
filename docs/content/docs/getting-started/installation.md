---
title: Installation
weight: 20
---

Helm is the only supported installation method. Install
[cert-manager](https://cert-manager.io/docs/installation/) first; it issues the
webhook and metrics certificates. You need cluster administrator permissions to
install CRDs, admission webhooks, and controller RBAC.

## Install with Helm

[![Helm Chart](https://img.shields.io/endpoint?url=https://artifacthub.io/badge/repository/change-windows)](https://artifacthub.io/packages/search?repo=change-windows)

```shell
helm upgrade --install change-windows charts/change-windows \
  --namespace capsule-system --create-namespace

kubectl -n capsule-system rollout status \
  deployment/change-windows
```

These instructions use the local chart and a self-built image; they do not depend
on a published release. The release name is `change-windows` and its namespace
is `capsule-system`; both are configurable. The release namespace is always
excluded from maintenance selection.

The controller does not receive arbitrary workload write permissions. Before
using resource actions, configure [executor RBAC]({{< relref "/docs/guides/security#executor-permissions" >}})
for the chart's manager service account and make `spec.serviceAccount` identify
the executor you created.

## First maintenance window

```shell
kubectl create namespace maintenance-demo
kubectl label namespace maintenance-demo maintenance=enabled
# Match the bundled account and its reference to this release's namespace.
sed 's/schedule-action-controller-system/capsule-system/g' \
  playground/manifests/examples/maintenance-only.yaml | kubectl apply -f -
kubectl get changewindows
kubectl get changewindow weekly-maintenance -o yaml
```

This example blocks Deployment changes every Sunday from 02:00 to 04:00 UTC. It
warns callers in the preceding hour. The YAML also deploys its required service
account. This admission-only example grants it no workload or impersonation
permissions. The substitution above adapts the playground namespace to this Helm
installation.

{{< example "maintenance-only.yaml" >}}

Wait for the `Ready` condition and check that `status.observedGeneration` matches
`metadata.generation`. Use a server-side dry run of an affected write to observe
warnings and denials without persisting a change. Outside the window the example
allows writes; inside it the custom maintenance message explains the denial.

The [CRD manifest]({{< relurl "crds/addons.projectcapsule.dev_changewindows.yaml" >}})
is available for inspection and CRD lifecycle management alongside the Helm
release. It is not a standalone installer.
