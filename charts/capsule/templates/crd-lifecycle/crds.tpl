{{/* CustomResources Lifecycle */}}
{{- if $.Values.crds.install }}
  {{ range $path, $_ :=  .Files.Glob "crds/**.yaml" }}
    {{- with $ }}
      {{- $content := (tpl (.Files.Get $path) $) -}}
      {{- $p := (fromYaml $content) -}}
      {{- if $p.Error }}
        {{- fail (printf "found YAML error in file %s - %s - raw:\n\n%s" $path $p.Error $content) -}}
      {{- end -}}


      {{/* Add Common Lables */}}
      {{- $_ := set $p.metadata "labels" (mergeOverwrite (default dict (get $p.metadata "labels")) (default dict $.Values.crds.labels) (fromYaml (include "capsule.labels" $))) -}}


      {{/* Add Common Lables */}}
      {{- $_ := set $p.metadata "annotations" (mergeOverwrite (default dict (get $p.metadata "annotations")) (default dict $.Values.crds.annotations)) -}}

      {{/* Add Keep annotation to CRDs */}}
      {{- if $.Values.crds.keep }}
        {{- $_ := set $p.metadata.annotations "helm.sh/resource-policy" "keep" -}}
      {{- end }}

      {{/* Add Spec Patches for the CRD */}}
      {{- $patchFile := $path | replace ".yaml" ".patch" }}
      {{- $patchRawContent := (tpl (.Files.Get $patchFile) $) -}}
      {{- if $patchRawContent -}}
        {{- $patchContent := (fromYaml $patchRawContent) -}}
        {{- if $patchContent.Error }}
          {{- fail (printf "found YAML error in patch file %s - %s - raw:\n\n%s" $patchFile $patchContent.Error $patchRawContent) -}}
        {{- end -}}
        {{- $tmp := deepCopy $p | mergeOverwrite $patchContent -}}
        {{- $p = $tmp -}}
      {{- end -}}
      {{- if $p }}
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: {{ include "capsule.crds.name" . }}-{{ $path | base | trimSuffix ".yaml" | regexFind "[^_]+$"  }}
  namespace: {{ .Release.Namespace | quote }}
  annotations:
    # create hook dependencies in the right order
    "helm.sh/hook-weight": "-5"
    {{- include "capsule.crds.annotations" . | nindent 4 }}
  labels:
    app.kubernetes.io/component: {{ include "capsule.crds.component" . | quote }}
    {{- include "capsule.labels" . | nindent 4 }}
data:
  content: |
    {{- printf "---\n%s" (toYaml $p) | nindent 4 }}

      {{- end }}
    {{ end }}
  {{- end }}
{{- end }}