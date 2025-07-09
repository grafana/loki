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
