---
title: Installation
weight: 20
---

Helm is the only supported installation method. Install
[cert-manager](https://cert-manager.io/docs/installation/) first; it issues the
webhook and metrics certificates. You need cluster administrator permissions to
install CRDs, admission webhooks, and controller RBAC.

Build an image from the checkout and make it available to the cluster. These
commands use ko through [Devbox](https://www.jetify.com/docs/devbox/installing-devbox/)
and do not require a container daemon. Replace the example registry with your own.

```shell
git clone https://github.com/projectcapsule/schedule-action-controller.git
cd schedule-action-controller
devbox shell
task image:push IMG=registry.example.com/scheduled-action:dev
```

`image:push` builds and publishes Linux amd64/arm64 images. For a local archive
without publishing, use `task image:build IMG=registry.example.com/scheduled-action:dev`;
the archive is written to `dist/controller.tar` and must be loaded into your
cluster separately.

## Install with Helm

```shell
helm upgrade --install scheduled-action charts/change-windows \
  --namespace capsule-system --create-namespace \
  --set image.registry=registry.example.com \
  --set image.repository=scheduled-action \
  --set image.tag=dev

kubectl -n capsule-system rollout status \
  deployment/scheduled-action-scheduled-action
```

These instructions use the local chart and a self-built image; they do not depend
on a published release. The release name is `scheduled-action` and its namespace
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
kubectl apply -f playground/manifests/examples/maintenance-only.yaml
kubectl get changewindows
kubectl get changewindow weekly-maintenance -o yaml
```

This example blocks Deployment changes every Sunday from 02:00 to 04:00 UTC. It
warns callers in the preceding hour. It does not scale workloads and needs no
executor service account.

{{< example "maintenance-only.yaml" >}}

Wait for the `Ready` condition and check that `status.observedGeneration` matches
`metadata.generation`. Use a server-side dry run of an affected write to observe
warnings and denials without persisting a change. Outside the window the example
allows writes; inside it the custom maintenance message explains the denial.

The [CRD manifest]({{< relurl "crds/addons.projectcapsule.dev_changewindows.yaml" >}})
is available for inspection and CRD lifecycle management alongside the Helm
release. It is not a standalone installer.
