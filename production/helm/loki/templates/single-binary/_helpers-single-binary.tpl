{{/*
singleBinary common labels
*/}}
{{- define "loki.singleBinaryLabels" -}}
{{ include "loki.labels" . }}
app.kubernetes.io/component: single-binary
{{- end }}


{{/* singleBinary selector labels */}}
{{- define "loki.singleBinarySelectorLabels" -}}
{{ include "loki.selectorLabels" . }}
app.kubernetes.io/component: single-binary
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
