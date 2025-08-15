{{/*
table-manager fullname
*/}}
{{- define "loki.tableManagerFullname" -}}
{{ include "loki.fullname" . }}-table-manager
{{- end }}

{{/*
table-manager common labels
*/}}
{{- define "loki.tableManagerLabels" -}}
{{ include "loki.labels" . }}
app.kubernetes.io/component: table-manager
{{- end }}

{{/*
table-manager selector labels
*/}}
{{- define "loki.tableManagerSelectorLabels" -}}
{{ include "loki.selectorLabels" . }}
app.kubernetes.io/component: table-manager
{{- end }}

{{/*
table-manager priority class name
*/}}
{{- define "loki.tableManagerPriorityClassName" -}}
{{- $pcn := coalesce .Values.global.priorityClassName .Values.tableManager.priorityClassName -}}
{{- if $pcn }}
priorityClassName: {{ $pcn }}
{{- end }}
{{- end }}

{{/*
table-manager readiness probe
*/}}
{{- define "loki.tableManager.readinessProbe" }}
{{- with .Values.tableManager.readinessProbe | default .Values.loki.readinessProbe }}
readinessProbe:
  {{- toYaml . | nindent 2 }}
{{- end }}
{{- end }}

{{/*
table-manager liveness probe
*/}}
{{- define "loki.tableManager.livenessProbe" }}
{{- with .Values.tableManager.livenessProbe | default .Values.loki.livenessProbe }}
livenessProbe:
  {{- toYaml . | nindent 2 }}
{{- end }}
{{- end }}
