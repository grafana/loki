{{/*
bloom-builder fullname
*/}}
{{- define "loki.bloomBuilderFullname" -}}
{{ include "loki.fullname" . }}-bloom-builder
{{- end }}

{{/*
bloom-builder common labels
*/}}
{{- define "loki.bloomBuilderLabels" -}}
{{ include "loki.labels" . }}
app.kubernetes.io/component: bloom-builder
{{- end }}

{{/*
bloom-builder selector labels
*/}}
{{- define "loki.bloomBuilderSelectorLabels" -}}
{{ include "loki.selectorLabels" . }}
app.kubernetes.io/component: bloom-builder
{{- end }}

{{/*
bloom-builder priority class name
*/}}
{{- define "loki.bloomBuilderPriorityClassName" -}}
{{- $pcn := coalesce .Values.global.priorityClassName .Values.bloomBuilder.priorityClassName -}}
{{- if $pcn }}
priorityClassName: {{ $pcn }}
{{- end }}
{{- end }}

{{/*
bloomBuilder readiness probe
*/}}
{{- define "loki.bloomBuilder.readinessProbe" }}
{{- with .Values.bloomBuilder.readinessProbe | default .Values.loki.readinessProbe }}
readinessProbe:
  {{- toYaml . | nindent 2 }}
{{- end }}
{{- end }}

{{/*
bloomBuilder liveness probe
*/}}
{{- define "loki.bloomBuilder.livenessProbe" }}
{{- with .Values.bloomBuilder.livenessProbe | default .Values.loki.livenessProbe }}
livenessProbe:
  {{- toYaml . | nindent 2 }}
{{- end }}
{{- end }}
