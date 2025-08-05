{{/*
query-scheduler fullname
*/}}
{{- define "loki.querySchedulerFullname" -}}
{{ include "loki.fullname" . }}-query-scheduler
{{- end }}

{{/*
query-scheduler common labels
*/}}
{{- define "loki.querySchedulerLabels" -}}
{{ include "loki.labels" . }}
app.kubernetes.io/component: query-scheduler
{{- end }}

{{/*
query-scheduler selector labels
*/}}
{{- define "loki.querySchedulerSelectorLabels" -}}
{{ include "loki.selectorLabels" . }}
app.kubernetes.io/component: query-scheduler
{{- end }}

{{/*
query-scheduler image
*/}}
{{- define "loki.querySchedulerImage" -}}
{{- $dict := dict "loki" .Values.loki.image "service" .Values.queryScheduler.image "global" .Values.global.image "defaultVersion" .Chart.AppVersion -}}
{{- include "loki.lokiImage" $dict -}}
{{- end }}

{{/*
query-scheduler priority class name
*/}}
{{- define "loki.querySchedulerPriorityClassName" -}}
{{- $pcn := coalesce .Values.global.priorityClassName .Values.queryScheduler.priorityClassName -}}
{{- if $pcn }}
priorityClassName: {{ $pcn }}
{{- end }}
{{- end }}

{{/*
queryScheduler readiness probe
*/}}
{{- define "loki.queryScheduler.readinessProbe" }}
{{- with .Values.queryScheduler.readinessProbe | default .Values.loki.readinessProbe }}
readinessProbe:
  {{- toYaml . | nindent 2 }}
{{- end }}
{{- end }}

{{/*
queryScheduler liveness probe
*/}}
{{- define "loki.queryScheduler.livenessProbe" }}
{{- with .Values.queryScheduler.livenessProbe | default .Values.loki.livenessProbe }}
livenessProbe:
  {{- toYaml . | nindent 2 }}
{{- end }}
{{- end }}
