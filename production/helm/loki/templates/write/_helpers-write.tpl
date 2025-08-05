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

{{/*
write readiness probe
*/}}
{{- define "loki.write.readinessProbe" }}
{{- with .Values.write.readinessProbe | default .Values.loki.readinessProbe }}
readinessProbe:
  {{- toYaml . | nindent 2 }}
{{- end }}
{{- end }}

{{/*
write liveness probe
*/}}
{{- define "loki.write.livenessProbe" }}
{{- with .Values.write.livenessProbe | default .Values.loki.livenessProbe }}
livenessProbe:
  {{- toYaml . | nindent 2 }}
{{- end }}
{{- end }}