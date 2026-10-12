{{/*
Expand the name of the chart.
*/}}
{{- define "change-windows.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "change-windows.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "change-windows.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "change-windows.labels" -}}
helm.sh/chart: {{ include "change-windows.chart" . }}
{{ include "change-windows.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "change-windows.selectorLabels" -}}
app.kubernetes.io/name: {{ include "change-windows.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "change-windows.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "change-windows.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Determine the Kubernetes version to use for jobsFullyQualifiedDockerImage tag
*/}}
{{- define "helm.jobsTagKubeVersion" -}}
{{- if contains "-eks-" .Capabilities.KubeVersion.GitVersion }}
{{- print "v" .Capabilities.KubeVersion.Major "." (.Capabilities.KubeVersion.Minor | replace "+" "") -}}
{{- else }}
{{- print "v" .Capabilities.KubeVersion.Major "." .Capabilities.KubeVersion.Minor -}}
{{- end }}
{{- end }}

{{/*
Create the jobs fully-qualified Docker image to use
*/}}
{{- define "change-windows.jobsFullyQualifiedDockerImage" -}}
{{- if .Values.global.jobs.kubectl.image.tag }}
{{- printf "%s/%s:%s" .Values.global.jobs.kubectl.image.registry .Values.global.jobs.kubectl.image.repository .Values.global.jobs.kubectl.image.tag -}}
{{- else }}
{{- printf "%s/%s:%s" .Values.global.jobs.kubectl.image.registry .Values.global.jobs.kubectl.image.repository (include "helm.jobsTagKubeVersion" .) -}}
{{- end }}
{{- end }}

{{/*
 Webhook service (Called with $.Path)
*/}}
{{- define "change-windows.webhooks.service" -}}
  {{- include "change-windows.webhooks.cabundle" $.ctx | nindent 0 }}
  {{- if $.ctx.Values.webhooks.service.url }}
url: {{ printf "%s/%s" (trimSuffix "/" $.ctx.Values.webhooks.service.url ) (trimPrefix "/" (required "Path is required for the function" $.path)) | quote }}
  {{- else }}
service:
  name: {{ default (printf "%s-webhook" (include "change-windows.name" $.ctx)) $.ctx.Values.webhooks.service.name | quote }}
  namespace: {{ default $.ctx.Release.Namespace $.ctx.Values.webhooks.service.namespace | quote }}
  port: {{ default 443 $.ctx.Values.webhooks.service.port }}
  path: {{ required "Path is required for the function" $.path }}
  {{- end }}
{{- end }}

{{/*
 Webhook service (Without Path)
*/}}
{{- define "change-windows.webhooks.serviceConfig" -}}
  {{- include "change-windows.webhooks.cabundle" $ | nindent 0 }}
  {{- if $.Values.webhooks.service.url }}
url: {{ trimSuffix "/" $.Values.webhooks.service.url | quote }}
  {{- else }}
service:
  name: {{ default (printf "%s-webhook" (include "change-windows.name" $)) $.Values.webhooks.service.name | quote }}
  namespace: {{ default $.Release.Namespace $.Values.webhooks.service.namespace | quote }}
  port: {{ default 443 $.Values.webhooks.service.port }}
  {{- end }}
{{- end }}

{{/*
 Webhook endpoint CA Bundle
*/}}
{{- define "change-windows.webhooks.cabundle" -}}
  {{- if $.Values.webhooks.service.caBundle -}}
caBundle: {{ $.Values.webhooks.service.caBundle | quote -}}
  {{- end -}}
{{- end -}}

{{- define "change-windows.webhooks.secret" -}}
{{- default (printf "%s-tls" (include "change-windows.name" $)) $.Values.webhooks.certificate.secret.name -}}
{{- end -}}


{{- define "change-windows.webhooks.labels" -}}
  {{- with $.Values.webhooks.labels }}
    {{- toYaml . | nindent 0 }}
  {{- end }}
{{- end }}

{{- define "change-windows.webhooks.annotations" -}}
  {{- if and ($.Values.webhooks.certificate.generate.enabled) (not $.Values.webhooks.service.url) (not $.Values.webhooks.service.caBundle) }}
cert-manager.io/inject-ca-from: {{ $.Release.Namespace }}/{{ include "change-windows.name" $  }}
  {{-  end }}
  {{- with $.Values.webhooks.annotations }}
    {{- toYaml . | nindent 0 }}
  {{- end }}
{{- end }}
