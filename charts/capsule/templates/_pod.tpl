{{- define "capsule.pod" -}}
metadata:
  annotations:
    {{- with .Values.podAnnotations }}
      {{- toYaml . | nindent 4 }}
    {{- end }}
    {{- if .Values.crds.install }}
    projectcapsule.dev/crds-size-hash: {{ include "capsule.crdsSizeHash" . | quote }}
    {{- end }}
  labels:
    {{- include "capsule.labels" . | nindent 4 }}
    {{- with .Values.podLabels }}
      {{- toYaml . | nindent 4 }}
    {{- end }}
spec:
  {{- with .Values.imagePullSecrets }}
  imagePullSecrets:
    {{- toYaml . | nindent 4 }}
  {{- end }}
  serviceAccountName: {{ include "capsule.serviceAccountName" . }}
  {{- if .Values.podSecurityContext.enabled }}
  securityContext: {{- omit .Values.podSecurityContext "enabled" | toYaml | nindent 4 }}
  {{- end }}
  {{- if not .Values.manager.hostUsers }}
  hostUsers: {{ .Values.manager.hostUsers }}
  {{- end }}
  {{- if .Values.manager.hostNetwork }}
  hostNetwork: true
  dnsPolicy: ClusterFirstWithHostNet
  {{- end }}
  {{- if .Values.manager.hostPID }}
  hostPID: {{ .Values.manager.hostPID }}
  {{- else }}
  hostPID: false
  {{- end }}
  priorityClassName: {{ .Values.priorityClassName }}
  {{- with .Values.nodeSelector }}
  nodeSelector:
    {{- toYaml . | nindent 4 }}
  {{- end }}
  {{- with .Values.tolerations }}
  tolerations:
    {{- toYaml . | nindent 4 }}
  {{- end }}
  {{- with .Values.affinity }}
  affinity:
    {{- toYaml . | nindent 4 }}
  {{- end }}
  {{- with .Values.topologySpreadConstraints }}
  topologySpreadConstraints:
    {{- toYaml . | nindent 4 }}
  {{- end }}
  volumes:
    - name: cert
      secret:
        defaultMode: 420
        secretName: {{ include "capsule.secretTlsName" . }}
    {{- if .Values.manager.volumes }}
      {{- toYaml .Values.manager.volumes | nindent 4 }}
    {{- end }}
  containers:
    - name: manager
      args:
        - --webhook-port={{ default $.Values.manager.webhookPort $.Values.webhooks.service.port }}
        - --zap-log-level={{ default 4 .Values.manager.options.logLevel }}
        - --configuration-name={{ .Values.manager.options.capsuleConfiguration }}
        - --workers={{ .Values.manager.options.workers }}
        - --client-connection-qps={{ .Values.manager.options.clientConnectionQPS }}
        - --client-connection-burst={{ .Values.manager.options.clientConnectionBurst }}
        {{- with .Values.manager.options.cacheSyncTimeout }}
        - --cache-sync-timeout={{ . }}
        {{- end }}
        - --enable-leader-election={{ .Values.manager.options.leaderElection.enabled }}
        {{- with .Values.manager.options.leaderElection.leaseDuration }}
        - --leader-election-lease-duration={{ . }}
        {{- end }}
        {{- with .Values.manager.options.leaderElection.renewDeadline }}
        - --leader-election-renew-deadline={{ . }}
        {{- end }}
        {{- with .Values.manager.options.leaderElection.retryPeriod }}
        - --leader-election-retry-period={{ . }}
        {{- end }}
        {{- if .Values.manager.options.tracing.enabled }}
        - --enable-tracing=true
        {{- with .Values.manager.options.tracing.endpoint }}
        - --tracing-otlp-endpoint={{ . }}
        {{- end }}
        - --tracing-otlp-insecure={{ .Values.manager.options.tracing.insecure }}
        - --tracing-sample-ratio={{ .Values.manager.options.tracing.sampleRatio }}
        {{- range $key, $value := .Values.manager.options.tracing.headers }}
        - --tracing-otlp-header={{ $key }}={{ $value }}
        {{- end }}
        {{- with .Values.manager.options.tracing.timeout }}
        - --tracing-otlp-timeout={{ . }}
        {{- end }}
        {{- with .Values.manager.options.tracing.compression }}
        - --tracing-otlp-compression={{ . }}
        {{- end }}
        {{- with .Values.manager.options.tracing.tls.serverName }}
        - --tracing-otlp-tls-server-name={{ . }}
        {{- end }}
        - --tracing-otlp-tls-insecure-skip-verify={{ .Values.manager.options.tracing.tls.insecureSkipVerify }}
        {{- end }}
        {{- with .Values.manager.extraArgs }}
          {{- toYaml . | nindent 8 }}
        {{- end }}
      image: {{ include "capsule.managerFullyQualifiedDockerImage" . }}
      imagePullPolicy: {{ .Values.manager.image.pullPolicy }}
      env:
      - name: NAMESPACE
        valueFrom:
          fieldRef:
            fieldPath: metadata.namespace
      - name: SERVICE_ACCOUNT
        valueFrom:
          fieldRef:
            fieldPath: spec.serviceAccountName
      {{- if .Values.manager.options.tracing.enabled }}
      {{- if or .Values.manager.options.tracing.basicAuth.username .Values.manager.options.tracing.basicAuth.existingSecret.name }}
      - name: CAPSULE_TRACING_OTLP_BASIC_AUTH_USERNAME
        {{- if .Values.manager.options.tracing.basicAuth.existingSecret.name }}
        valueFrom:
          secretKeyRef:
            name: {{ .Values.manager.options.tracing.basicAuth.existingSecret.name }}
            key: {{ .Values.manager.options.tracing.basicAuth.existingSecret.usernameKey }}
        {{- else }}
        value: {{ .Values.manager.options.tracing.basicAuth.username | quote }}
        {{- end }}
      {{- end }}
      {{- if or .Values.manager.options.tracing.basicAuth.password .Values.manager.options.tracing.basicAuth.existingSecret.name }}
      - name: CAPSULE_TRACING_OTLP_BASIC_AUTH_PASSWORD
        {{- if .Values.manager.options.tracing.basicAuth.existingSecret.name }}
        valueFrom:
          secretKeyRef:
            name: {{ .Values.manager.options.tracing.basicAuth.existingSecret.name }}
            key: {{ .Values.manager.options.tracing.basicAuth.existingSecret.passwordKey }}
        {{- else }}
        value: {{ .Values.manager.options.tracing.basicAuth.password | quote }}
        {{- end }}
      {{- end }}
      {{- end }}
      {{- with .Values.manager.env }}
      {{- toYaml . | nindent 6 }}
      {{- end }}
      ports:
        {{- if not (.Values.manager.hostNetwork) }}
        - name: admission
          protocol: TCP
          containerPort: {{ default $.Values.manager.webhookPort $.Values.webhooks.service.port }}
        - name: metrics
          containerPort: 8080
          protocol: TCP
        - name: health-api
          containerPort: 10080
          protocol: TCP
        {{- end }}
        {{- with .Values.manager.ports }}
          {{- . | nindent 8 }}
        {{- end }}
      livenessProbe:
        {{- toYaml .Values.manager.livenessProbe | nindent 8 }}
      readinessProbe:
        {{- toYaml .Values.manager.readinessProbe | nindent 8 }}
      volumeMounts:
        - mountPath: /tmp/k8s-webhook-server/serving-certs
          name: cert
          readOnly: true
        {{- if .Values.manager.volumeMounts }}
          {{- toYaml .Values.manager.volumeMounts | nindent 8 }}
        {{- end }}
      resources:
        {{- toYaml .Values.manager.resources | nindent 8 }}
      {{- if .Values.manager.securityContext }}
      securityContext:
        {{- omit .Values.manager.securityContext "enabled" | toYaml | nindent 8 }}
      {{- else if .Values.securityContext.enabled }}
      securityContext:
        {{- omit .Values.securityContext "enabled" | toYaml | nindent 8 }}
      {{- end }}
{{- end -}}
