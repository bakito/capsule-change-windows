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

`kubectl get changewindows` shows **Ready**, **State**, **Next Transition** and
**Age**. `kubectl get changewindows -o wide` also shows the **Active** condition
and the number of **Pending** resource actions.

The two standard Kubernetes conditions describe different facts:

- **Ready=True**, reason `Succeeded`: schedules and resource actions reconciled
  successfully for `observedGeneration`. A failure sets `Ready=False` with reason
  `ReconcileError`; it does not imply that an active maintenance restriction stopped.
- **Active=True**, reason `Active`: the current time falls inside a scheduled
  window. Outside a window it is `False`, with the lifecycle reason below.
  Invalid policies produce `Active=Unknown`.

| State (Active condition reason) | Meaning |
| --- | --- |
| `Planned` | Waiting for the first window observed by this controller. |
| `Active` | A scheduled window is currently active, even if an action failed. |
| `Finishing` | The window ended, but resource restoration remains outstanding. |
| `Finished` | The last observed window and its resource actions finished. This persists between recurring windows until the next window starts. |
| `Unknown` | The policy could not be compiled, so the window state cannot be determined. |
| `Unscheduled` | The cron schedules have no upcoming occurrence and no window was observed. |

`Next Transition` remains populated after `Finished`: it is the next scheduled
start. During `Active`, it reports the current end. It is displayed as an absolute
UTC timestamp. An absent end can mean a continuous window extends beyond the
bounded scheduling horizon.

```shell
kubectl wait changewindow/development-downtime --for=condition=Ready --timeout=60s
kubectl wait changewindow/development-downtime --for=condition=Active --timeout=1h
kubectl get changewindow development-downtime -o wide
```

Check each condition's `observedGeneration` against `metadata.generation` when
using status for automation. `lastTransitionTime` changes when the condition's
True/False/Unknown value changes. The existing `status.active` boolean remains
available for compatibility; conditions distinguish an unknown schedule from an
inactive one. `windowStart` and `windowEnd` describe the active or next window,
while `pendingResources` counts journal entries still requiring completion.
`Finished` reports a controller-observed cycle, not a historical audit of windows
that elapsed while the controller was unavailable.

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
