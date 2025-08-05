{{/*
query-frontend fullname
*/}}
{{- define "loki.queryFrontendFullname" -}}
{{ include "loki.fullname" . }}-query-frontend
{{- end }}

{{/*
query-frontend common labels
*/}}
{{- define "loki.queryFrontendLabels" -}}
{{ include "loki.labels" . }}
app.kubernetes.io/component: query-frontend
{{- end }}

{{/*
query-frontend selector labels
*/}}
{{- define "loki.queryFrontendSelectorLabels" -}}
{{ include "loki.selectorLabels" . }}
app.kubernetes.io/component: query-frontend
{{- end }}

{{/*
query-frontend priority class name
*/}}
{{- define "loki.queryFrontendPriorityClassName" -}}
{{- $pcn := coalesce .Values.global.priorityClassName .Values.queryFrontend.priorityClassName -}}
{{- if $pcn }}
priorityClassName: {{ $pcn }}
{{- end }}
{{- end }}

{{/*
queryFrontend readiness probe
*/}}
{{- define "loki.queryFrontend.readinessProbe" }}
{{- with .Values.queryFrontend.readinessProbe | default .Values.loki.readinessProbe }}
readinessProbe:
  {{- toYaml . | nindent 2 }}
{{- end }}
{{- end }}

{{/*
queryFrontend liveness probe
*/}}
{{- define "loki.queryFrontend.livenessProbe" }}
{{- with .Values.queryFrontend.livenessProbe | default .Values.loki.livenessProbe }}
livenessProbe:
  {{- toYaml . | nindent 2 }}
{{- end }}
{{- end }}
