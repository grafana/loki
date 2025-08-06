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
read readiness probe
*/}}
{{- define "loki.read.readinessProbe" }}
{{- with .Values.read.readinessProbe | default .Values.loki.readinessProbe }}
readinessProbe:
  {{- toYaml . | nindent 2 }}
{{- end }}
{{- end }}

{{/*
read liveness probe
*/}}
{{- define "loki.read.livenessProbe" }}
{{- with .Values.read.livenessProbe | default .Values.loki.livenessProbe }}
livenessProbe:
  {{- toYaml . | nindent 2 }}
{{- end }}
{{- end }}
