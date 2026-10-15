---
title: Temporary playground
description: Launch a disposable Kind cluster with Change Windows, Headlamp and the current plugin for up to 30 minutes.
weight: 20
---

The playground builds the current checkout's operator with ko and its Headlamp
plugin, then installs both in a new Kind cluster. Flux manages Headlamp through
a HelmRelease. A demo workload and ChangeWindow let you explore the calendar,
maintenance
rules and automatic downscaling. No registry publication or existing cluster is
involved.

## Start the playground

With Docker running and the locked [development tools]({{< relref "/docs/development/contributing" >}}) available:

```sh
devbox run -- task playground
```

Open `http://127.0.0.1:18081` after **Playground ready**. Setup time is separate
from the 30-minute usable lifetime. The task uses a private kubeconfig, refuses
to reuse an existing cluster named `change-windows-playground`, and never switches
your current context. Ctrl+C deletes the playground early.

```sh
devbox run -- task playground PLAYGROUND_MINUTES=10 PLAYGROUND_PORT=18082
```

`PLAYGROUND_MINUTES` accepts 1–30. `PLAYGROUND_PORT` selects a free localhost port;
`PLAYGROUND_CLUSTER` selects a different **new** Kind cluster name. Dependencies
use pinned versions; the operator and plugin come from the current working tree.

## Configuration and manifests

All playground inputs live under `playground/`:

- `kind.config.yaml` declares the Kind node and read-only plugin mount. The task
  stages the current plugin in a private directory and runs Kind from there, so
  the relative `./plugins` mount works without generating configuration.
- `installation/headlamp/` contains the Flux HelmRepository and HelmRelease.
  The source and Helm controllers run in `flux-system`; Headlamp runs in
  `kube-system`. Its chart version and values are declared in `release.flux.yaml`.
- `installation/change-windows/values.yaml` configures the local operator's Helm
  release, including admission selection for the `development` namespace.
- `manifests/demo/` holds the demo resources and maintenance window.
  `manifests/examples/` holds the individual documented examples, shared directly
  with the documentation and tests. `manifests/dev/` holds the dry-run admission
  probe used by development deployment.

The task applies `playground/manifests/kustomization.yaml`, which selects only the
runnable demo. Its window manifest bundles the corresponding ServiceAccount and
executor RBAC. Every other documented example also includes its own account;
action examples include their executor permissions. Other examples are opt-in; do not recursively
apply every sample because their schedules and policies may overlap.

Preview the manifests without installing anything:

```sh
kubectl kustomize playground/installation/headlamp
kubectl kustomize playground/manifests
```

## Explore the demo

Open **Change Windows** in Headlamp's navigation. The `playground` window selects
the demo Deployment in `development`, scales it from two replicas to zero every
ten minutes for two minutes, then restores the captured count. Replica changes
are denied while the window is active; other edits warn during the minute before
it begins. Follow the cycle in the calendar, resource details, and replica counts.
Use Headlamp's ordinary resource editor to experiment with the ChangeWindow.

Headlamp uses a shared demo service account behind a localhost-only listener. It
can view normal resources, edit ChangeWindows, and edit Deployments in
`development`. It cannot read journal Secrets, impersonate users, change RBAC, or
edit infrastructure workloads. No Kubernetes login is required. Never expose this
playground's Headlamp service through an unauthenticated public endpoint.

## Cleanup

Expiry, failure, or terminal Ctrl+C removes the Kind cluster, staged plugin files,
private kubeconfig and port-forward. State is intentionally discarded. When
terminating from another process, signal the whole process group: Task does not
forward signals sent only to its own PID. A forced
process kill or host failure can prevent shell cleanup; delete only the named
playground cluster with `kind delete cluster --name change-windows-playground`
after such an interruption. Normal cleanup leaves existing clusters untouched.
