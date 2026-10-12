---
title: Admission and warnings
weight: 50
---

```yaml
admission:
  - name: maintenance-freeze
    targets: [deployments]
    operations: [CREATE, UPDATE, DELETE]
    subresources: ["", scale]
    when: Active
    action: Deny
    message: >-
      Maintenance is active for {{ .target.metadata.namespace }}.
      {{ if .window.end }}Retry after {{ .window.end }}.{{ end }}
```

Admission rules belong under `spec.admission`. They select named targets,
operations, and optionally subresources. `when: Active` denies inside the union
of scheduled windows. Use `when: Inactive` to permit changes only during those
windows. `action: Deny` is currently the only action; defaults are `Active` and
`Deny`.

Every matching deny wins, including a deny from another ChangeWindow. A
custom message uses the target and window template context. If the message fails
to render, the request is still denied. Browse the
[complete examples]({{< relref "/docs/examples" >}}) for ready-to-adapt manifests.

## Audiences

Every entry under `spec.admission` and `spec.config.admission.warnings` can
define its own `audience` and `matchConditions`. They combine with the entry's
schedule, target, operation, and subresource filters using AND.

```yaml
audience:
  include:
    - kind: User
      name: alice
    - kind: Group
      name: developers
    - kind: ServiceAccount
      name: deployer
      namespace: team-a
  exclude:
    - kind: User
      name: break-glass
```

Includes use OR: any matching subject selects the caller. An omitted or empty
include list selects everyone. Any matching exclusion takes precedence. With no
audience, the entry applies to all callers. Subject names are exact,
case-sensitive matches, with no wildcard expansion.

`User` matches the authenticated username, `Group` matches any authenticated
group, and `ServiceAccount` matches the exact namespace/name identity. Use
`kind: ServiceAccount` (not `SA`) and always specify its namespace. Namespace
must be omitted for User and Group subjects. Each include/exclude list permits
at most 64 subjects. Audiences filter this entry; they do not grant RBAC
permissions or exempt the caller from other entries or ChangeWindows.

## Match conditions

```yaml
matchConditions:
  - name: replicas-changed
    expression: >-
      object != null && oldObject != null &&
      object.spec.replicas != oldObject.spec.replicas
```

Conditions are named CEL expressions that return a boolean. Every condition
must be true for the entry to match. An empty list matches without extra
conditions. Names must be unique DNS labels. Each entry allows up to 64
conditions, with at most 4096 expression bytes each.

| Variable | Value |
| --- | --- |
| `object` | The new admission object; null on DELETE. |
| `oldObject` | The previous admission object; null on CREATE. |
| `request` | Admission request metadata, including `operation`, `namespace`, `subResource`, `dryRun`, and `userInfo`. Raw objects are exposed through their separate variables. |
| `namespaceObject` | The namespace used for selection, including labels; null for Nodes. |

For `/scale`, `object` and `oldObject` are the actual Scale payloads. Target
selection still uses the parent resource's labels. CEL variables are dynamic:
the expression is checked for a boolean result when the policy is accepted, but
field presence and types depend on the request. Guard optional fields with
`has()` and guard null objects before reading them. No `authorizer`, API reads,
network functions, or Capsule template helpers are available in CEL.

A false condition skips the entry, including when another condition encountered
an ordinary evaluation error. If no condition is false and evaluation fails,
a denial entry rejects the request with a diagnostic error. A warning entry
returns a diagnostic warning and does not block the request. Invalid expressions
are rejected when the ChangeWindow is created or updated. Runtime diagnostics
identify the policy and entry without exposing evaluated object values.

Expressions are compiled once per policy generation and reused. Each expression
has a 10,000-unit runtime cost limit; an admission request has separate
100,000-unit budgets for denials and warnings. Request-budget exhaustion or
cancellation stops evaluation. Keep conditions small and predictable, and test
them with server-side dry runs before depending on a freeze.

## Parent resources and subresources

An omitted or empty `subresources` list matches only the parent resource. Include
`scale` to cover scaling and `""` to retain parent writes. Use endpoint names
without a leading slash. `"*"` matches every named subresource;
`subresources: ["", "*"]` covers both parent and subresource writes. Partial
subresource patterns such as `sta*` are not supported. This option applies to
denials and warnings; action endpoints must remain concrete. GET, LIST, WATCH,
and CONNECT are not blocked by these rules.

Parent labels are fetched only when needed for a subresource label selector or
message's target context. A rule selecting all labels and rendering only window
information needs no parent-object read. Custom subresource targets using parent
labels or `.target` need explicit parent-resource `get` permissions for the
controller; no broad read permission is granted automatically.

The controller's own authenticated action requests bypass only their originating
ChangeWindow. Other policies still apply. Calling Kubernetes directly as the
executor service account does not grant this bypass.

## Warning periods

```yaml
config:
  admission:
    warnings:
      - name: upcoming
        targets: [deployments]
        operations: [CREATE, UPDATE, DELETE]
        subresources: ["", scale]
        when: BeforeStart
        within: 1h
        message: "Maintenance begins at {{ .window.start }} (in {{ .window.startsIn }})."
      - name: ending
        targets: [deployments]
        operations: [UPDATE]
        subresources: ["", scale]
        when: BeforeEnd
        within: 15m
        message: "Maintenance ends at {{ .window.end }}."
```

Warnings live under `spec.config.admission.warnings`. They are Kubernetes admission
warnings returned to clients such as `kubectl` when users write affected resources.
They are not background notifications.

| `when` | Notice period | `within` |
| --- | --- | --- |
| `BeforeStart` | Before the next window starts. | Required, positive, at most `168h`. |
| `Active` | Throughout the active window. | Must be omitted. |
| `BeforeEnd` | Before the known end of the active window. | Required, positive, at most `168h`. |

Warnings are also returned on dry runs and denied writes. Responses contain at
most eight distinct warnings, each limited to 256 bytes. Keep messages concise
and avoid sensitive values. For a warning-only policy, omit `actions` and
`admission`, retaining targets, schedules, and the warnings configuration.
