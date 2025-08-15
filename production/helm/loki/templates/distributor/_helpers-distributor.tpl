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

{{/*
distributor readiness probe
*/}}
{{- define "loki.distributor.readinessProbe" }}
{{- with .Values.distributor.readinessProbe | default .Values.loki.readinessProbe }}
readinessProbe:
  {{- toYaml . | nindent 2 }}
{{- end }}
{{- end }}

{{/*
distributor liveness probe
*/}}
{{- define "loki.distributor.livenessProbe" }}
{{- with .Values.distributor.livenessProbe | default .Values.loki.livenessProbe }}
livenessProbe:
  {{- toYaml . | nindent 2 }}
{{- end }}
{{- end }}
