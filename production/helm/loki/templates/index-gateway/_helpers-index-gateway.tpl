{{/*
index-gateway fullname
*/}}
{{- define "loki.indexGatewayFullname" -}}
{{ include "loki.fullname" . }}-index-gateway
{{- end }}

{{/*
index-gateway common labels
*/}}
{{- define "loki.indexGatewayLabels" -}}
{{ include "loki.labels" . }}
app.kubernetes.io/component: index-gateway
{{- end }}

{{/*
index-gateway selector labels
*/}}
{{- define "loki.indexGatewaySelectorLabels" -}}
{{ include "loki.selectorLabels" . }}
app.kubernetes.io/component: index-gateway
{{- end }}

{{/*
index-gateway image
*/}}
{{- define "loki.indexGatewayImage" -}}
{{- $dict := dict "loki" .Values.loki.image "service" .Values.indexGateway.image "global" .Values.global.image "defaultVersion" .Chart.AppVersion -}}
{{- include "loki.lokiImage" $dict -}}
{{- end }}

{{/*
index-gateway priority class name
*/}}
{{- define "loki.indexGatewayPriorityClassName" -}}
{{- $pcn := coalesce .Values.global.priorityClassName .Values.indexGateway.priorityClassName -}}
{{- if $pcn }}
priorityClassName: {{ $pcn }}
{{- end }}
{{- end }}

{{/*
indexGateway readiness probe
*/}}
{{- define "loki.indexGateway.readinessProbe" }}
{{- with .Values.indexGateway.readinessProbe | default .Values.loki.readinessProbe }}
readinessProbe:
  {{- toYaml . | nindent 2 }}
{{- end }}
{{- end }}

{{/*
indexGateway liveness probe
*/}}
{{- define "loki.indexGateway.livenessProbe" }}
{{- with .Values.indexGateway.livenessProbe | default .Values.loki.livenessProbe }}
livenessProbe:
  {{- toYaml . | nindent 2 }}
{{- end }}
{{- end }}
