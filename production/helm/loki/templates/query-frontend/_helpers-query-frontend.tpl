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
app.kubernetes.io/instance: loki
app.kubernetes.io/name: loki
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
