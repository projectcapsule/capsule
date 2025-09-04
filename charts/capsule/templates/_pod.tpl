{{- define "capsule.pod" -}}
metadata:
  {{- with .Values.podAnnotations }}
  annotations:
    {{- toYaml . | nindent 4 }}
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
  hostUsers: {{ .Values.manager.hostUsers }}
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
        - --webhook-port={{ .Values.manager.webhookPort }}
        - --zap-log-level={{ default 4 .Values.manager.options.logLevel }}
        - --configuration-name={{ .Values.manager.options.capsuleConfiguration }}
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
      {{- with .Values.manager.env }}
        {{- toYaml . | nindent 6 }}
      {{- end }}
      ports:
        {{- if not (.Values.manager.hostNetwork) }}
        - name: webhook-server
          containerPort: {{ .Values.manager.webhookPort }}
          protocol: TCP
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
