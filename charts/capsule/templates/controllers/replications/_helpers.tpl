{{/* Build a controller-specific rendering context without changing the core values. */}}
{{- define "capsule.replications.render" -}}
{{- $root := .root -}}
{{- with $root -}}
{{- $replications := .Values.controllers.replications }}
{{- $values := deepCopy .Values }}
{{- $_ := set $values "manager" (mergeOverwrite (deepCopy .Values.manager) (deepCopy $replications)) }}
{{- $_ := set $values.manager "kind" "Deployment" }}
{{- range $key := list "replicaCount" "podLabels" "podAnnotations" "nodeSelector" "tolerations" "affinity" "topologySpreadConstraints" "priorityClassName" }}
{{- $_ := set $values $key (get $replications $key) }}
{{- end }}
{{- $_ := set $values "podSecurityContext" (mergeOverwrite (deepCopy .Values.podSecurityContext) $replications.podSecurityContext) }}
{{/* Keep selectors disjoint even when a long name already ends in the suffix. */}}
{{- $coreName := include "capsule.name" . }}
{{- $replicationName := printf "%s-replications" ($coreName | trunc 50 | trimSuffix "-") }}
{{- if eq $replicationName $coreName }}
{{- $replicationName = printf "%s-worker" ($coreName | trunc 50 | trimSuffix "-") }}
{{- end }}
{{- $_ := set $values "nameOverride" $replicationName }}
{{- $fullname := include "capsule.fullname" . }}
{{- if gt (len $fullname) 30 }}
{{- $fullname = printf "%s-%s" ($fullname | trunc 19 | trimSuffix "-") ($fullname | sha256sum | trunc 10) }}
{{- end }}
{{- $_ := set $values "fullnameOverride" (printf "%s-replications" $fullname) }}
{{/* Retain the existing trusted identity, including custom ServiceAccount names. */}}
{{- $_ := set $values.serviceAccount "name" (include "capsule.serviceAccountName" .) }}
{{- $context := dict "Values" $values "Release" .Release "Chart" .Chart "Capabilities" .Capabilities "Template" .Template "Files" .Files "replications" true }}
{{- include $.template $context }}
{{- end }}
{{- end -}}
