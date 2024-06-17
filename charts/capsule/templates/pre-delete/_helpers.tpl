{{- define "capsule.pre-delete.name" -}}
{{- printf "%s-pre-delete" (include "capsule.name" $) -}}
{{- end }}

{{- define "capsule.pre-delete.annotations" -}}
"helm.sh/hook": pre-delete
  {{- with $.Values.jobs.annotations }}
    {{- . | toYaml | nindent 0 }}
  {{- end }}
{{- end }}

{{- define "capsule.pre-delete.component" -}}
pre-delete-hook
{{- end }}

