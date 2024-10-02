{{/*
ruler-query-scheduler fullname
*/}}
{{- define "loki.rulerQuerySchedulerFullname" -}}
{{ include "loki.fullname" . }}-ruler-query-scheduler
{{- end }}

{{/*
ruler-query-scheduler common labels
*/}}
{{- define "loki.rulerQuerySchedulerLabels" -}}
{{ include "loki.labels" . }}
app.kubernetes.io/component: ruler-query-scheduler
{{- end }}

{{/*
query-scheduler selector labels
*/}}
{{- define "loki.rulerQuerySchedulerSelectorLabels" -}}
{{ include "loki.selectorLabels" . }}
app.kubernetes.io/component: ruler-query-scheduler
{{- end }}

{{/*
ruler-query-scheduler image
*/}}
{{- define "loki.rulerQuerySchedulerImage" -}}
{{- $dict := dict "loki" .Values.loki.image "service" .Values.rulerQueryScheduler.image "global" .Values.global.image "defaultVersion" .Chart.AppVersion -}}
{{- include "loki.lokiImage" $dict -}}
{{- end }}

{{/*
ruler-query-scheduler priority class name
*/}}
{{- define "loki.rulerQuerySchedulerPriorityClassName" -}}
{{- $pcn := coalesce .Values.global.priorityClassName .Values.rulerQueryScheduler.priorityClassName -}}
{{- if $pcn }}
priorityClassName: {{ $pcn }}
{{- end }}
{{- end }}
