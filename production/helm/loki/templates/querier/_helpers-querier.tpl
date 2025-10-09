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
querier target
*/}}
{{- define "loki.querierTarget" -}}
querier{{- if .Values.loki.ui.enabled -}},ui{{- end -}}
{{- end -}}
