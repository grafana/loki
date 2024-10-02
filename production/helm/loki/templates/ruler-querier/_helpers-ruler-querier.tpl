{{/*
ruler querier fullname
*/}}
{{- define "loki.rulerQuerierFullname" -}}
{{ include "loki.fullname" . }}-ruler-querier
{{- end }}

{{/*
ruler querier common labels
*/}}
{{- define "loki.rulerQuerierLabels" -}}
{{ include "loki.labels" . }}
app.kubernetes.io/component: ruler-querier
{{- end }}

{{/*
ruler querier selector labels
*/}}
{{- define "loki.rulerQuerierSelectorLabels" -}}
{{ include "loki.selectorLabels" . }}
app.kubernetes.io/component: ruler-querier
{{- end }}

{{/*
ruler querier priority class name
*/}}
{{- define "loki.rulerQuerierPriorityClassName" -}}
{{- $pcn := coalesce .Values.global.priorityClassName .Values.rulerQuerier.priorityClassName -}}
{{- if $pcn }}
priorityClassName: {{ $pcn }}
{{- end }}
{{- end }}
