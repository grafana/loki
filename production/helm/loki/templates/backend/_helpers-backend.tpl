{{/*
backend fullname
*/}}
{{- define "loki.backendFullname" -}}
{{ include "loki.name" . }}-backend
{{- end }}

{{/*
backend common labels
*/}}
{{- define "loki.backendLabels" -}}
{{ include "loki.labels" . }}
app.kubernetes.io/component: backend
{{- end }}

{{/*
backend selector labels
*/}}
{{- define "loki.backendSelectorLabels" -}}
{{ include "loki.selectorLabels" . }}
app.kubernetes.io/component: backend
{{- end }}

{{/*
backend priority class name
*/}}
{{- define "loki.backendPriorityClassName" -}}
{{- $pcn := coalesce .Values.global.priorityClassName .Values.backend.priorityClassName -}}
{{- if $pcn }}
priorityClassName: {{ $pcn }}
{{- end }}
{{- end }}

{{/*
backend readiness probe
*/}}
{{- define "loki.backend.readinessProbe" }}
{{- with .Values.backend.readinessProbe | default .Values.loki.readinessProbe }}
readinessProbe:
  {{- toYaml . | nindent 2 }}
{{- end }}
{{- end }}

{{/*
backend liveness probe
*/}}
{{- define "loki.backend.livenessProbe" }}
{{- with .Values.backend.livenessProbe | default .Values.loki.livenessProbe }}
livenessProbe:
  {{- toYaml . | nindent 2 }}
{{- end }}
{{- end }}
