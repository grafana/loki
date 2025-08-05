{{/*
querier fullname
*/}}
{{- define "loki.querierFullname" -}}
{{ include "loki.fullname" . }}-querier
{{- end }}

{{/*
querier common labels
*/}}
{{- define "loki.querierLabels" -}}
{{ include "loki.labels" . }}
app.kubernetes.io/component: querier
{{- end }}

{{/*
querier selector labels
*/}}
{{- define "loki.querierSelectorLabels" -}}
{{ include "loki.selectorLabels" . }}
app.kubernetes.io/component: querier
{{- end }}

{{/*
querier priority class name
*/}}
{{- define "loki.querierPriorityClassName" -}}
{{- $pcn := coalesce .Values.global.priorityClassName .Values.querier.priorityClassName -}}
{{- if $pcn }}
priorityClassName: {{ $pcn }}
{{- end }}
{{- end }}

{{/*
querier readiness probe
*/}}
{{- define "loki.querier.readinessProbe" }}
{{- with .Values.querier.readinessProbe | default .Values.loki.readinessProbe }}
readinessProbe:
  {{- toYaml . | nindent 2 }}
{{- end }}
{{- end }}

{{/*
querier liveness probe
*/}}
{{- define "loki.querier.livenessProbe" }}
{{- with .Values.querier.livenessProbe | default .Values.loki.livenessProbe }}
livenessProbe:
  {{- toYaml . | nindent 2 }}
{{- end }}
{{- end }}
