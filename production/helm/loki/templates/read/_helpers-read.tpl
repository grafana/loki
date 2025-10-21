{{/*
read fullname
*/}}
{{- define "loki.readFullname" -}}
{{ include "loki.name" . }}-read
{{- end }}

{{/*
read common labels
*/}}
{{- define "loki.readLabels" -}}
{{ include "loki.labels" . }}
app.kubernetes.io/component: read
{{- end }}

{{/*
read selector labels
*/}}
{{- define "loki.readSelectorLabels" -}}
{{ include "loki.selectorLabels" . }}
app.kubernetes.io/component: read
{{- end }}

{{/*
read priority class name
*/}}
{{- define "loki.readPriorityClassName" -}}
{{- $pcn := coalesce .Values.global.priorityClassName .Values.read.priorityClassName -}}
{{- if $pcn }}
priorityClassName: {{ $pcn }}
{{- end }}
{{- end }}

{{/*
read target
*/}}
{{- define "loki.readTarget" -}}
{{- .Values.read.targetModule -}}{{- if .Values.loki.ui.enabled -}},ui{{- end -}}
{{- end -}}
