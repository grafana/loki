{{/*
legacy singleBinary labels
*/}}
{{- define "loki.legacy.singleBinaryLabels" -}}
chart: {{ template "loki.chart" . }}
heritage: {{ .Release.Service }}
{{- end }}

{{/*
legacy singleBinary selector labels
*/}}
{{- define "loki.legacy.singleBinarySelectorLabels" -}}
app: {{ template "loki.name" . }}
release: {{ .Release.Name }}
{{- end }}

{{/*
singleBinary common labels
*/}}
{{- define "loki.singleBinaryLabels" -}}
{{- if eq (include "loki.requiresMultiStageUpgrade" . ) "true" }}
{{ include "loki.legacy.singleBinaryLabels" . }}
{{ include "loki.legacy.singleBinarySelectorLabels" . }}
{{- end }}
{{ include "loki.labels" . }}
app.kubernetes.io/component: single-binary
{{- end }}


{{/* singleBinary selector labels */}}
{{- define "loki.singleBinarySelectorLabels" -}}
{{- if eq (include "loki.requiresMultiStageUpgrade" . ) "true" }}
{{ include "loki.legacy.singleBinarySelectorLabels" . }}
{{- else }}
{{ include "loki.selectorLabels" . }}
app.kubernetes.io/component: single-binary
{{- end }}
{{- end }}

{{/*
singleBinary priority class name
*/}}
{{- define "loki.singleBinaryPriorityClassName" -}}
{{- $pcn := coalesce .Values.global.priorityClassName .Values.singleBinary.priorityClassName -}}
{{- if $pcn }}
priorityClassName: {{ $pcn }}
{{- end }}
{{- end }}
