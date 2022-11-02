{{/*
Client definition for LogsInstance
*/}}
{{- define "loki.logsInstanceClient" -}}
{{- $isSingleBinary := eq (include "loki.deployment.isSingleBinary" .) "true" -}}
{{- $url := printf "http://%s.%s.svc.%s:3100/loki/api/v1/push" (include "loki.writeFullname" .) .Release.Namespace .Values.global.clusterDomain }}
{{- if $isSingleBinary  }}
  {{- $url = printf "http://%s.%s.svc.%s:3100/loki/api/v1/push" (include "loki.singleBinaryFullname" .) .Release.Namespace .Values.global.clusterDomain }}
{{- else if .Values.gateway.enabled -}}
  {{- $url = printf "http://%s.%s.svc.%s/loki/api/v1/push" (include "loki.gatewayFullname" .) .Release.Namespace .Values.global.clusterDomain }}
{{- end -}}
- url: {{ $url }}
  externalLabels:
    cluster: {{ include "loki.fullname" . }}
  {{- if .Values.enterprise.enabled }}
  basicAuth:
    username:
      name: {{ include "enterprise-logs.canarySecret" . }}
      key: username
    password:
      name: {{ include "enterprise-logs.canarySecret" . }}
      key: password
  {{- else if .Values.loki.auth_enabled }}
  tenantId: {{ .Values.monitoring.selfMonitoring.tenant }}
  {{- end }}
{{- end -}}

{{/*
Convert a recording rule group to yaml
*/}}
{{- define "loki.ruleGroupToYaml" -}}
{{- range . }}
- name: {{ .name }}
  rules:
    {{- toYaml .rules | nindent 4 }}
{{- end }}
{{- end }}
