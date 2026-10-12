---
title: Resource actions
weight: 40
---

An action names its targets and defines `onStart`, `onEnd`, or both. Each step
applies a partial YAML or JSON object using server-side apply (SSA). The controller
supplies identity and concurrency preconditions; templates cannot redirect a
patch to another object.

## Downscale and restore

The complete example captures each Deployment's replica count from `/scale`,
scales it to zero overnight, and restores the captured value at the end. It also
blocks user writes and provides advance warnings.

{{< example "addons_v1alpha1_changewindow.yaml" >}}

Set up [executor permissions]({{< relref "/docs/guides/security#executor-permissions" >}}) before applying this example.
The included RBAC grants access only to an existing namespace named `development`;
additional selected namespaces need their own grants.

**The start patch explicitly uses `force: true`.** This transfers conflicting SSA
field ownership. It is useful when taking control of replicas managed by another
actor, but requires deliberate coordination with HPAs and GitOps controllers.
The default is `false`, so ownership conflicts normally fail. Restoring replicas
restores their value, not the previous field manager's ownership.

## Capture and retry

Capture paths are RFC 6901 JSON pointers, for example `/spec/replicas`. Escape `/`
inside a key as `~1`, and `~` as `~0`. Capture only values needed by the action;
they may contain sensitive configuration.

Before any workload mutation, captures and rendered patches are persisted in a
private Secret in the controller namespace. A window is limited to 256
action/resource participants and a 512 KiB journal. Exceeding those limits fails
before workload mutation.

Failed start and end steps retry; successful steps stop. A crash after the API
server accepts a patch can cause that same patch to be repeated. Use idempotent
templates. Different ChangeWindows execute independently, so avoid competing
forced writes to the same fields.

## Templates and subresources

| Value | Meaning |
| --- | --- |
| `.target` | The selected parent Kubernetes object. |
| `.captured.NAME` | A named captured value for that resource. |
| `.window.start` | Window start timestamp. |
| `.window.end` | Known end timestamp, or an empty string beyond the scheduling horizon. |
| `.window.startsIn` | Time until the upcoming window starts. |
| `.window.phase` | Current template window phase. |

Templates use Go `text/template`, standard template functions, `toJson`, and
`quote`. Missing keys cause errors. Action templates render once when the journal
is prepared, keeping retry content stable. No shell, network, or Capsule
resource-loading functions are available.

Capture and apply accept a real Kubernetes `subresource`, such as `scale`.
Discovery determines the payload kind: Deployment `/scale` accepts
`autoscaling/v1 Scale`. Unsupported endpoints or endpoints that reject SSA return
an error; the controller does not silently patch the parent instead.

## Rollout restart

```yaml
actions:
  - name: restart
    targets: [deployments]
    onStart:
      apply:
        template: |
          spec:
            template:
              metadata:
                annotations:
                  kubectl.kubernetes.io/restartedAt: {{ .window.start | quote }}
```

A Deployment has no `/rollout` subresource. Restart it by patching its pod template
on the parent resource. The stable window timestamp prevents retries from
continually triggering new rollouts. The executor needs `get`, `list`, and `patch`
on Deployments for this example.

## Editing and deleting an active action

Spec edits are rejected while the restoration finalizer is present. Wait for
pending actions to finish before changing the spec. Deleting a ChangeWindow
triggers its required end actions through the finalizer. A failed restoration
keeps the journal and blocks deletion; resolve the underlying permissions or
ownership error instead of stripping the finalizer.
