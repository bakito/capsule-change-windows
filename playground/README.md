# Change Windows playground

Build the current controller and Headlamp plugin in a disposable Kind cluster:

```sh
# From the repository root, with Docker running:
devbox run -- task playground
```

Headlamp is available at <http://127.0.0.1:18081> after setup. Flux installs and
reconciles Headlamp through a HelmRelease; the operator uses the local Helm chart
and a ko image loaded into Kind. The task deletes its cluster after 30 minutes or
when stopped with Ctrl+C, preserving other clusters and your kubeconfig context.
Use `PLAYGROUND_MINUTES=10`, `PLAYGROUND_PORT=18082`, or
`PLAYGROUND_CLUSTER=my-playground` to customize the run.

## Layout

| Path | Purpose |
| --- | --- |
| `kind.config.yaml` | Kind configuration, including the read-only local plugin mount. |
| `installation/headlamp/` | Flux HelmRepository, HelmRelease, and Kustomization. |
| `installation/change-windows/values.yaml` | Standalone demo Helm values; admission selects only `development`. |
| `manifests/kustomization.yaml` | Selects the runnable demo and its executor permissions. |
| `manifests/demo/` | Demo namespace, workload, Headlamp permissions, and cycling ChangeWindow. |
| `manifests/examples/` | Individual documented ChangeWindows and executor RBAC examples. |
| `manifests/dev/` | Dry-run admission probe used by `task dev:deploy`. |

Kind resolves `hostPath: ./plugins` against its working directory. The task stages
`headlamp/dist` as `plugins/capsule-change-windows/` in a private temporary directory
and runs Kind there with the checked-in configuration. No generated Kind template
or host-path substitution is needed. Flux's source and Helm controllers are installed
in `flux-system`; the Headlamp release runs in `kube-system`.

Preview the resources without a cluster:

```sh
kubectl kustomize playground/installation/headlamp
kubectl kustomize playground/manifests
```

Apply the demo through its Kustomization, rather than recursively applying every
sample. The other examples are opt-in and can have overlapping schedules or
policies. The documentation mounts `manifests/examples/` directly, so examples
remain the same files used by the policy tests and documentation downloads.

Headlamp uses a shared, limited demo identity over a localhost-only connection.
It can edit ChangeWindows and the demo workloads but cannot read controller journal
Secrets or administer RBAC. Keep this playground private.

See the [playground guide](../docs/content/docs/development/playground.md) for the
maintenance cycle, permissions, and cleanup behavior.
