{{- define "capsule.post-delete.name" -}}
{{- printf "%s-post-delete" (include "capsule.fullname" $ | trunc 51 | trimSuffix "-") -}}
{{- end }}

{{- define "capsule.post-delete.annotations" -}}
"helm.sh/hook": post-delete
"helm.sh/hook-delete-policy": before-hook-creation,hook-succeeded
{{- end }}

{{- define "capsule.post-delete.component" -}}
post-delete-hook
{{- end }}
