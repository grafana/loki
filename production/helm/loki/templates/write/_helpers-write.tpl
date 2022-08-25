{{/*
write fullname
*/}}
{{- define "loki.writeFullname" -}}
{{ include "loki.name" . }}-write
{{- end }}

{{/*
write common labels
*/}}
{{- define "loki.writeLabels" -}}
{{ include "loki.labels" . }}
app.kubernetes.io/component: write
{{- end }}

{{/*
write selector labels
*/}}
{{- define "loki.writeSelectorLabels" -}}
{{ include "loki.selectorLabels" . }}
app.kubernetes.io/component: write
{{- end }}

{{/*
write priority class name
*/}}
{{- define "loki.writePriorityClassName" -}}
{{- $pcn := coalesce .Values.global.priorityClassName .Values.write.priorityClassName -}}
{{- if $pcn }}
priorityClassName: {{ $pcn }}
{{- end }}
{{- end }}
