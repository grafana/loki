{{/*
distributor fullname
*/}}
{{- define "loki.distributorFullname" -}}
{{ include "loki.fullname" . }}-distributor
{{- end }}

{{/*
distributor common labels
*/}}
{{- define "loki.distributorLabels" -}}
{{ include "loki.labels" . }}
app.kubernetes.io/component: distributor
{{- end }}

{{/*
distributor selector labels
*/}}
{{- define "loki.distributorSelectorLabels" -}}
{{ include "loki.selectorLabels" . }}
app.kubernetes.io/component: distributor
{{- end }}

{{/*
distributor priority class name
*/}}
{{- define "loki.distributorPriorityClassName" -}}
{{- $pcn := coalesce .Values.global.priorityClassName .Values.distributor.priorityClassName -}}
{{- if $pcn }}
priorityClassName: {{ $pcn }}
{{- end }}
{{- end }}
