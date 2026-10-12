{{- define "change-windows.crds.name" -}}
{{- printf "%s-crds" (include "change-windows.name" $) -}}
{{- end }}

{{- define "change-windows.crds.annotations" -}}
"helm.sh/hook": "pre-install,pre-upgrade"
{{- end }}

{{- define "change-windows.crds.component" -}}
crd-install-hook
{{- end }}

{{- define "change-windows.crds.regexReplace" -}}
{{- printf "%s" ($ | base | trimSuffix ".yaml" | regexReplaceAll "[_.]" "-") -}}
{{- end }}

{{- define "change-windows.crds.hash" -}}
{{- $paths := list -}}
{{- range $p, $_ := .Files.Glob "crds/**.yaml" }}
  {{- $paths = append $paths $p -}}
{{- end -}}
{{- $paths = sortAlpha $paths -}}

{{- $sizes := list -}}
{{- range $paths }}
  {{- $sizes = append $sizes (len ($.Files.Get .)) -}}
{{- end -}}

{{- $joined := join "," $sizes -}}
{{- sha256sum $joined -}}
{{- end -}}
