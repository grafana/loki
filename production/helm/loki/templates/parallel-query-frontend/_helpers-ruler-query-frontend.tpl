{{/*
parallel-query-frontend fullname
*/}}
{{- define "loki.parallelQueryFrontendFullname" -}}
{{ include "loki.fullname" . }}-parallel-query-frontend
{{- end }}

{{/*
parallel-query-frontend common labels
*/}}
{{- define "loki.parallelQueryFrontendLabels" -}}
{{ include "loki.labels" . }}
app.kubernetes.io/component: parallel-query-frontend
{{- end }}

{{/*
parallel-query-frontend selector labels
*/}}
{{- define "loki.parallelQueryFrontendSelectorLabels" -}}
{{ include "loki.selectorLabels" . }}
app.kubernetes.io/component: parallel-query-frontend
{{- end }}

{{/*
parallel-query-frontend priority class name
*/}}
{{- define "loki.parallelQueryFrontendPriorityClassName" -}}
{{- $pcn := coalesce .Values.global.priorityClassName .Values.parallelQueryFrontend.priorityClassName -}}
{{- if $pcn }}
priorityClassName: {{ $pcn }}
{{- end }}
{{- end }}
