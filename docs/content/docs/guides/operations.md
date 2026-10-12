---
title: Operations
weight: 70
---

```shell
kubectl get changewindows
kubectl describe changewindow development-downtime
kubectl get changewindow development-downtime -o yaml
kubectl -n capsule-system logs \
  -l app.kubernetes.io/instance=scheduled-action --all-containers=true
```

The log command uses the release name and namespace from the
[installation guide]({{< relref "/docs/getting-started/installation" >}}).

Check `status.observedGeneration` alongside the `Ready` condition. `active`
reports the reconciled window state; `windowStart`, `windowEnd`, and
`nextTransition` report known boundaries. An absent end can mean a continuous
window extends beyond the scheduling horizon. `pendingResources` counts journal
entries still requiring completion.

Admission evaluates wall-clock schedules itself. A status update is not the
trigger that enables or disables restrictions.

## Prometheus metrics

Metrics are served on authenticated HTTPS port **8443**, alongside standard
controller-runtime reconciliation and workqueue metrics. Give the scraping
service account `get` access to the `/metrics` non-resource URL and configure a
bearer token and the serving certificate's CA.

The Helm chart can create a ServiceMonitor with
`metrics.serviceMonitor.enabled=true`. Set
`metrics.serviceMonitor.tokenSecret` to a Secret in the release namespace with a
`token` key. The ServiceMonitor uses the chart's TLS Secret as its CA. Provision
and rotate the scrape credential using your cluster's identity tooling.

| Metric | Labels | Meaning |
| --- | --- | --- |
| `change_window_transitions_total` | `phase`: start, end | Durably recorded window transitions. |
| `change_window_executions_total` | `phase`: start, end; `result`: success, error, gone | SSA attempts, including retries and missing/replaced skips. |
| `change_window_execution_duration_seconds` | `phase`: start, end | Histogram of SSA attempt duration, including API latency. |
| `change_window_admission_requests_total` | `decision`: allow, deny, error | Admission responses. |
| `change_window_admission_warnings_total` | none | Deduplicated warning messages emitted. |
| `change_window_admission_duration_seconds` | none | Histogram of admission handler duration. |

```promql
# Failed action attempts per second, across controller replicas.
sum(rate(change_window_executions_total{result="error"}[5m]))

# Admission p99 latency in seconds.
histogram_quantile(0.99,
  sum by (le) (rate(change_window_admission_duration_seconds_bucket[5m]))
)
```

Counters reset on restart and are operational telemetry, not an exactly-once audit
ledger. Metric labels contain no policy names, namespace names, or captured values.

## Troubleshooting

| Symptom | Check |
| --- | --- |
| `Ready=False` / `ReconcileError` | Controller logs, target discovery, executor impersonation, and workload RBAC. |
| SSA conflict | Identify the competing field manager. Coordinate ownership before explicitly using `force`. |
| Actions did not select a new object | Selection is a snapshot at window entry; the object joins the next window. |
| An affected write is unexpectedly allowed | Namespace labels, resource labels, operations, subresource list, time zone, and whether the namespace is protected. |
| A write fails with the webhook unavailable | Controller readiness, certificates, Service endpoints, and API-server connectivity. The failure policy is closed. |
| Deletion is pending | Required restoration has not completed. Fix RBAC or patch conflicts and let the finalizer finish. |
| Capture exceeds a bound | Reduce action/resource participants or captured data, then retry after addressing the failed window. |

## Upgrade and removal

Review API and deployment changes before upgrading. Refresh CRDs explicitly when
using Helm: Helm does not automatically upgrade existing CRDs from a chart's
`crds/` directory. Use the CRDs from the same checkout as the controller image.

Before uninstalling, delete ChangeWindows and wait for restoration and finalizer
completion while the controller and executor permissions are still available.
Removing the controller or CRDs first prevents normal restoration.

## Migrating from ScheduledAction

The alpha API was renamed from `ScheduledAction` to `ChangeWindow`, with
resource name `changewindows` and short name `cw`. There is no compatibility
alias or automatic conversion. The group and version remain
`addons.projectcapsule.dev/v1alpha1`.

Before upgrading, save existing manifests and let the old controller finish
restoration when deleting old resources. Wait until finalizers complete; do not
force them off or delete the old CRD while restoration is pending. Update the
controller and CRDs, change saved manifests to `kind: ChangeWindow`, then reapply.
Helm does not upgrade or remove CRDs automatically. Update RBAC, clients, and
metric names to `change_window_*`. The repository, image, and chart names remain
`schedule-action-controller`.
