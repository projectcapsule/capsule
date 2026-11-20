{{/* Build a controller-specific rendering context without changing the core values. */}}
{{- define "capsule.replications.render" -}}
{{- $root := .root -}}
{{- with $root -}}
{{- $replications := dict }}
{{- $controllers := .Values.manager.controllers | default dict }}
{{- range $key, $value := ($controllers.replications | default dict) }}
{{- if ne $value nil }}
{{- $_ := set $replications $key (deepCopy $value) }}
{{- end }}
{{- end }}
{{/* Nullable option defaults express inheritance without swallowing false/zero. */}}
{{- with $replications.options }}
{{- range $key, $value := . }}
{{- if eq $value nil }}
{{- $_ := unset $replications.options $key }}
{{- end }}
{{- end }}
{{- end }}
{{- $values := deepCopy .Values }}
{{- $_ := set $values "manager" (mergeOverwrite (deepCopy .Values.manager) $replications) }}
{{/* These complete maps replace inherited maps, including explicit {}. */}}
{{- range $key := list "resources" "deploymentStrategy" "daemonsetStrategy" }}
{{- if hasKey $replications $key }}
{{- $_ := set $values.manager $key (get $replications $key) }}
{{- end }}
{{- end }}
{{- range $key := list "replicaCount" "podLabels" "podAnnotations" "nodeSelector" "tolerations" "affinity" "antiAffinity" "topologySpreadConstraints" "priorityClassName" "podDisruptionBudget" "imagePullSecrets" }}
{{- if hasKey $replications $key }}
{{- $_ := set $values $key (get $replications $key) }}
{{- end }}
{{- end }}
{{- with $replications.podSecurityContext }}
{{- $_ := set $values "podSecurityContext" (mergeOverwrite (deepCopy $values.podSecurityContext) .) }}
{{- end }}
{{/* Both controllers can share a node when host networking is inherited. */}}
{{- if $values.manager.hostNetwork }}
{{- range $key := list "livenessProbe" "readinessProbe" }}
{{- $probe := get $values.manager $key }}
{{- if and $probe.httpGet (eq (toString $probe.httpGet.port) "10080") }}
{{- $_ := set $probe.httpGet "port" 10081 }}
{{- end }}
{{- end }}
{{- end }}
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
{{/* Explicit inherited selectors must still identify the replication Service. */}}
{{- $selector := include "capsule.selectorLabels" $context | fromYaml }}
{{- $monitoring := $values.monitoring | default dict }}
{{- range $monitor := list ($monitoring.serviceMonitor | default dict) ($values.serviceMonitor | default dict) }}
{{- with $monitor.matchLabels }}
{{- $_ := set $monitor "matchLabels" (mergeOverwrite (deepCopy .) $selector) }}
{{- end }}
{{- end }}
{{- include $.template $context }}
{{- end }}
{{- end -}}
