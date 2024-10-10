{{/*
ruler-query-frontend fullname
*/}}
{{- define "loki.rulerQueryFrontendFullname" -}}
{{ include "loki.fullname" . }}-ruler-query-frontend
{{- end }}

{{/*
ruler-query-frontend common labels
*/}}
{{- define "loki.rulerQueryFrontendLabels" -}}
{{ include "loki.labels" . }}
app.kubernetes.io/component: ruler-query-frontend
{{- end }}

{{/*
ruler-query-frontend selector labels
*/}}
{{- define "loki.rulerQueryFrontendSelectorLabels" -}}
{{ include "loki.selectorLabels" . }}
app.kubernetes.io/component: ruler-query-frontend
{{- end }}

{{/*
ruler-query-frontend priority class name
*/}}
{{- define "loki.rulerQueryFrontendPriorityClassName" -}}
{{- $pcn := coalesce .Values.global.priorityClassName .Values.rulerQueryFrontend.priorityClassName -}}
{{- if $pcn }}
priorityClassName: {{ $pcn }}
{{- end }}
{{- end }}
