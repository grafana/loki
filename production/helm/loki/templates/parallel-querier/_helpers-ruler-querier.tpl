{{/*
parallel querier fullname
*/}}
{{- define "loki.parallelQuerierFullname" -}}
{{ include "loki.fullname" . }}-parallel-querier
{{- end }}

{{/*
parallel querier common labels
*/}}
{{- define "loki.parallelQuerierLabels" -}}
{{ include "loki.labels" . }}
app.kubernetes.io/component:parallel-querier
{{- end }}

{{/*
parallel querier selector labels
*/}}
{{- define "loki.parallelQuerierSelectorLabels" -}}
{{ include "loki.selectorLabels" . }}
app.kubernetes.io/component: parallel-querier
{{- end }}

{{/*
parallel querier priority class name
*/}}
{{- define "loki.parallelQuerierPriorityClassName" -}}
{{- $pcn := coalesce .Values.global.priorityClassName .Values.parallelQuerier.priorityClassName -}}
{{- if $pcn }}
priorityClassName: {{ $pcn }}
{{- end }}
{{- end }}
