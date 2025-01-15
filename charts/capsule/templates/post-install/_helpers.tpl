{{- define "capsule.post-install.name" -}}
{{- printf "%s-post-install" (include "capsule.name" $) -}}
{{- end }}

{{- define "capsule.post-install.annotations" -}}
"helm.sh/hook": post-install
{{- end }}

{{- define "capsule.post-install.component" -}}
post-install-hook
{{- end }}

