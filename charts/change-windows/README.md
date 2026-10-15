# Change Windows

Change Windows schedules Kubernetes resource actions and protects maintenance
windows with admission rules and advance warnings. Helm is the supported
installation method; cert-manager must be installed first.

## Installation

See the [installation guide](https://projectcapsule.github.io/schedule-action-controller/docs/getting-started/installation/)
for building an image and installing the chart from a checkout.

For a published release, set the chart version explicitly:

```shell
helm upgrade --install change-windows oci://ghcr.io/projectcapsule/charts/change-windows \
  --namespace capsule-system --create-namespace --version VERSION
helm status change-windows --namespace capsule-system
```

Before uninstalling, delete ChangeWindows and allow pending restorations and
finalizers to complete while the controller is still running.

```shell
helm uninstall change-windows --namespace capsule-system
```

## Admission configuration

The chart installs two validating webhooks. Configure request filtering through
`webhooks.hooks.resources` for resource denials and warnings, and
`webhooks.hooks.changewindows` for validation of the ChangeWindow API itself.

| Property | Purpose |
| --- | --- |
| `rules` | Select API groups, versions, resources, operations, and resource scope. |
| `namespaceSelector` | Filter namespaced requests using labels on their Namespace. |
| `objectSelector` | Further filter requests using object labels. |
| `matchConditions` | CEL conditions that must all evaluate to true before the webhook is called. |
| `failurePolicy` | Reject requests on webhook call failures (`Fail`, the default), or allow admission to continue (`Ignore`). |
| `timeoutSeconds`, `matchPolicy` | Configure the call timeout and API version matching. |

The API server applies these filters before sending a request to the controller.
A matching ChangeWindow must also select the resource through its namespace
selectors, targets, audience, conditions, and schedule. Widening a ChangeWindow's
selectors cannot override the Helm webhook filters. See the
[Kubernetes webhook reference](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#webhook-configuration).

### Selecting namespaces outside Capsule tenants

By default, the resource webhook selects namespaced requests only when the
Namespace has the `capsule.clastix.io/tenant` label:

```yaml
webhooks:
  hooks:
    resources:
      namespaceSelector:
        matchExpressions:
          - key: capsule.clastix.io/tenant
            operator: Exists
```

**To include other namespaces, provide your own
`webhooks.hooks.resources.namespaceSelector.matchExpressions`, replacing the
tenant-label expression.** For example, this selects tenant and non-tenant
namespaces while excluding infrastructure namespaces and the Helm release
namespace:

```yaml
webhooks:
  hooks:
    resources:
      namespaceSelector:
        matchExpressions:
          - key: kubernetes.io/metadata.name
            operator: NotIn
            values:
              - '{{ .Release.Namespace }}'
              - kube-system
              - kube-public
              - kube-node-lease
```

Save the override with your installation values and apply it with Helm:

```shell
helm upgrade --install change-windows oci://ghcr.io/projectcapsule/charts/change-windows \
  --namespace capsule-system --create-namespace --version VERSION --values values.yaml
```

Helm replaces the entire `matchExpressions` list. Adding another expression
alongside the tenant-label expression still requires the tenant label, because
selector requirements are combined with AND. Setting only
`namespaceSelector: {}` retains the default expression through Helm's map
merging; `namespaceSelector: {matchExpressions: []}` removes it. Clear any
additional `matchLabels` constraints in your own values as well.

The controller works with or without Capsule. This tenant-only selector is an
installation default, not a requirement for enforcing ChangeWindows. The
controller's protected namespaces still apply: its own namespace, `kube-system`,
`kube-public`, and `kube-node-lease` cannot be selected for admission or actions.
Scheduled actions use ChangeWindow selection and executor RBAC independently of
the webhook's request filters.

The default resource rules also include a separate cluster-scoped `nodes` rule.
A `namespaceSelector` does not filter Nodes; Node maintenance uses the
ChangeWindow's Node target selector. To intercept only namespaced resources,
replace `webhooks.hooks.resources.rules` with:

```yaml
webhooks:
  hooks:
    resources:
      rules:
        - apiGroups: ["*"]
          apiVersions: ["*"]
          operations: [CREATE, UPDATE, DELETE]
          resources: ["*/*"]
          scope: Namespaced
```

The `changewindows` hook validates cluster-scoped ChangeWindow objects and has
independent filters. Broadening resource admission increases webhook traffic and
extends the effect of its default `failurePolicy: Fail` to those requests.

### Templating webhook properties

String values and map keys throughout `webhooks` support Helm templates using the
chart's root context, including `.Release`, `.Values`, and chart helpers. This
covers both hooks' selectors, rules, conditions and policies, as well as webhook
labels, annotations, service settings, and certificate settings. Keep maps and
lists as YAML structures and quote strings containing templates.

Boolean settings such as `webhooks.enabled`, `webhooks.exclusive`, and
`webhooks.certificate.generate.enabled` can use templates producing `true` or
`false`. Port and timeout templates must produce integers. For example:

```yaml
webhooks:
  labels:
    app.kubernetes.io/instance: '{{ .Release.Name }}'
  service:
    port: '{{ add 400 43 }}'
  certificate:
    secret:
      name: '{{ .Release.Name }}-serving-tls'
  hooks:
    resources:
      failurePolicy: '{{ print "Fail" }}'
      timeoutSeconds: '{{ add 2 3 }}'
      matchConditions:
        - name: exclude-controller-namespace
          expression: 'request.namespace != {{ .Release.Namespace | quote }}'
```

Templating runs during Helm rendering and adds no work to the admission request
path. Template output is not evaluated recursively, and ChangeWindow runtime
templates elsewhere in your values are preserved. Use deterministic expressions
for settings shared by multiple manifests, such as the serving Secret name.
These templates are supplied by the chart installer and use Helm's
[`tpl` function](https://helm.sh/docs/howto/charts_tips_and_tricks/#using-the-tpl-function).

## Values

The following Values are available for this chart.

### Global Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| global.jobs.kubectl.affinity | object | `{}` | Set affinity rules |
| global.jobs.kubectl.annotations | object | `{}` | Annotations to add to the job. |
| global.jobs.kubectl.backoffLimit | int | `4` | Backofflimit for jobs |
| global.jobs.kubectl.image.pullPolicy | string | `"IfNotPresent"` | Set the image pull policy of the helm chart job |
| global.jobs.kubectl.image.registry | string | `"docker.io"` | Set the image repository of the helm chart job |
| global.jobs.kubectl.image.repository | string | `"clastix/kubectl"` | Set the image repository of the helm chart job |
| global.jobs.kubectl.image.tag | string | `""` | Set the image tag of the helm chart job |
| global.jobs.kubectl.imagePullSecrets | list | `[]` | ImagePullSecrets |
| global.jobs.kubectl.labels | object | `{}` | Labels to add to the job. |
| global.jobs.kubectl.nodeSelector | object | `{}` | Set the node selector |
| global.jobs.kubectl.podAnnotations | object | `{}` | Annotations to add to the job pod |
| global.jobs.kubectl.podLabels | object | `{}` | Labels to add to the job pod |
| global.jobs.kubectl.podSecurityContext | object | `{"enabled":true,"seccompProfile":{"type":"RuntimeDefault"}}` | Security context for the job pods. |
| global.jobs.kubectl.priorityClassName | string | `""` | Set a pod priorityClassName |
| global.jobs.kubectl.resources | object | `{}` | Job resources |
| global.jobs.kubectl.restartPolicy | string | `"Never"` | Set the restartPolicy |
| global.jobs.kubectl.securityContext | object | `{"allowPrivilegeEscalation":false,"capabilities":{"drop":["ALL"]},"enabled":true,"readOnlyRootFilesystem":true,"runAsGroup":1002,"runAsNonRoot":true,"runAsUser":1002}` | Security context for the job containers. |
| global.jobs.kubectl.tolerations | list | `[]` | Set list of tolerations |
| global.jobs.kubectl.topologySpreadConstraints | list | `[]` | Set Topology Spread Constraints |
| global.jobs.kubectl.ttlSecondsAfterFinished | int | `60` | Sets the ttl in seconds after a finished certgen job is deleted. Set to -1 to never delete. |

### CustomResourceDefinition Lifecycle

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| crds.annotations | object | `{}` | Extra Annotations for CRDs |
| crds.exclusive | bool | `false` | Only install the CRDs, no other primitives |
| crds.inline | bool | `false` |  |
| crds.install | bool | `true` | Install the CustomResourceDefinitions (This also manages the lifecycle of the CRDs for update operations) |
| crds.keep | bool | `true` | Retain CRDs and their resources when an inline installation is uninstalled |
| crds.labels | object | `{}` | Extra Labels for CRDs |

### General Parameters

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| manager.affinity | object | `{}` | Set affinity rules |
| manager.args.extra | list | `["--leader-elect"]` | Extra arguments |
| manager.args.kubeApiBurst | int | `100` | Kubernetes client burst |
| manager.args.kubeApiQPS | int | `50` | Kubernetes client QPS |
| manager.args.workers | int | `1` | Maximum concurrent ChangeWindow reconciliations |
| manager.env | list | `[]` | Environment variables |
| manager.hostUsers | bool | `false` | Do not use Host Users (User Namespaces) |
| manager.image.pullPolicy | string | `"IfNotPresent"` | Set the image pull policy. |
| manager.image.reference | string | `""` | Complete image reference, including an optional digest; overrides registry, repository, and tag |
| manager.image.registry | string | `"ghcr.io"` | Set the image registry |
| manager.image.repository | string | `"projectcapsule/change-windows"` | Set the image repository |
| manager.image.tag | string | `""` | Overrides the image tag whose default is the chart appVersion. |
| manager.livenessProbe | object | `{"httpGet":{"path":"/healthz","port":8081}}` | Configure the liveness probe using Deployment probe spec |
| manager.nodeSelector | object | `{}` | Set the node selector |
| manager.podSecurityContext | object | `{"enabled":true,"seccompProfile":{"type":"RuntimeDefault"}}` | Set the securityContext |
| manager.priorityClassName | string | `""` | Set the priority class name of the Capsule pod |
| manager.readinessProbe | object | `{"httpGet":{"path":"/readyz","port":8081}}` | Configure the readiness probe using Deployment probe spec |
| manager.replicaCount | int | `1` | Amount of replicas |
| manager.resources | object | `{}` | Set the resource requests/limits |
| manager.securityContext | object | `{"allowPrivilegeEscalation":false,"capabilities":{"drop":["ALL"]},"enabled":true,"readOnlyRootFilesystem":true,"runAsNonRoot":true,"runAsUser":1000}` | Set the securityContext for the container |
| manager.tolerations | list | `[]` | Set list of tolerations |
| manager.topologySpreadConstraints | list | `[]` | Set topology spread constraints |
| metrics.serviceMonitor.enabled | bool | `false` |  |
| metrics.serviceMonitor.tokenSecret | string | `""` |  |
| serviceAccount.annotations | object | `{}` | Annotations to add to the service account. |
| serviceAccount.create | bool | `true` | Specifies whether a service account should be created. |
| serviceAccount.imagePullSecrets | list | `[]` | Image pull secrets for the service account. |
| serviceAccount.name | string | `""` | The name of the service account to use. |
| webhooks.annotations | object | `{}` | Additional Annotations for all webhooks |
| webhooks.certificate.generate.dnsNames | list | `[]` | Additional DNS Names to include in certificate |
| webhooks.certificate.generate.duration | string | `""` | Duration of validity for the CA certificate (e.g. 2160h for 90 days) |
| webhooks.certificate.generate.enabled | bool | `true` | Enable cert-manager generated certificates for the webhook service |
| webhooks.certificate.generate.ipAddresses | list | `[]` | Additional IP Addresses to include in certificate |
| webhooks.certificate.generate.renewBefore | string | `""` | Renew the CA certificate before its expiration time (e.g. 360h for 15 days) |
| webhooks.certificate.generate.uris | list | `[]` | Additional URIs to include in certificate |
| webhooks.certificate.secret.name | string | `""` | Name of the secret containing the TLS certificate for the webhook service |
| webhooks.enabled | bool | `true` | Specifies whether the admission webhooks are enabled |
| webhooks.exclusive | bool | `false` | When `crds.exclusive` is `true` the webhooks will be installed |
| webhooks.hooks.changewindows.failurePolicy | string | `"Fail"` | [FailurePolicy](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#failure-policy) |
| webhooks.hooks.changewindows.matchConditions | list | `[]` | [MatchConditions](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#matching-requests-matchpolicy) |
| webhooks.hooks.changewindows.matchPolicy | string | `"Exact"` | [MatchPolicy](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#matching-requests-matchpolicy) |
| webhooks.hooks.changewindows.namespaceSelector | object | `{}` | [NamespaceSelector](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#matching-requests-namespaceselector) |
| webhooks.hooks.changewindows.objectSelector | object | `{}` | [ObjectSelector](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#matching-requests-objectselector) |
| webhooks.hooks.changewindows.rules | list | `[{"apiGroups":["addons.projectcapsule.dev"],"apiVersions":["v1alpha1"],"operations":["CREATE","UPDATE"],"resources":["changewindows"],"scope":"Cluster"}]` | [Rules](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#matching-requests-rules) |
| webhooks.hooks.changewindows.timeoutSeconds | int | `5` | [TimeoutSeconds](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#timeouts) |
| webhooks.hooks.resources.failurePolicy | string | `"Fail"` | [FailurePolicy](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#failure-policy) |
| webhooks.hooks.resources.matchConditions | list | `[]` | [MatchConditions](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#matching-requests-matchpolicy) |
| webhooks.hooks.resources.matchPolicy | string | `"Exact"` | [MatchPolicy](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#matching-requests-matchpolicy) |
| webhooks.hooks.resources.namespaceSelector | object | `{"matchExpressions":[{"key":"capsule.clastix.io/tenant","operator":"Exists"}]}` | [NamespaceSelector](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#matching-requests-namespaceselector) |
| webhooks.hooks.resources.objectSelector | object | `{}` | [ObjectSelector](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#matching-requests-objectselector) |
| webhooks.hooks.resources.rules | list | `[{"apiGroups":["*"],"apiVersions":["*"],"operations":["CREATE","UPDATE","DELETE"],"resources":["*/*"],"scope":"Namespaced"},{"apiGroups":[""],"apiVersions":["v1"],"operations":["CREATE","UPDATE","DELETE"],"resources":["nodes"],"scope":"Cluster"}]` | [Rules](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#matching-requests-rules) |
| webhooks.hooks.resources.timeoutSeconds | int | `5` | [TimeoutSeconds](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#timeouts) |
| webhooks.labels | object | `{}` | Additional Labels for all webhooks |
| webhooks.service.caBundle | string | `""` | CABundle for the webhook service |
| webhooks.service.name | string | `""` | Custom service name for the webhook service |
| webhooks.service.namespace | string | `""` | Custom service namespace for the webhook service |
| webhooks.service.port | integer, null | `nil` | Custom service port for the webhook service |
| webhooks.service.url | string | `""` | The URL where the capsule webhook services are running (Overwrites cluster scoped service definition) |

### Monitoring Parameters

| Key | Type | Default | Description |
|-----|------|---------|-------------|
