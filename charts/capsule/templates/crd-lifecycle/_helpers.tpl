{{- define "capsule.crds.name" -}}
{{- printf "%s-crds" (include "capsule.name" $) -}}
{{- end }}

{{- define "capsule.crds.annotations" -}}
"helm.sh/hook": "pre-install,pre-upgrade"
{{- end }}

{{- define "capsule.crds.component" -}}
crd-install-hook
{{- end }}

{{- define "capsule.crds.regexReplace" -}}
{{- printf "%s" ($ | base | trimSuffix ".yaml" | regexReplaceAll "[_.]" "-") -}}
{{- end }}


