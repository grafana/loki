{{/*
parallel-query-scheduler fullname
*/}}
{{- define "loki.parallelQuerySchedulerFullname" -}}
{{ include "loki.fullname" . }}-parallel-query-scheduler
{{- end }}

{{/*
parallel-query-scheduler common labels
*/}}
{{- define "loki.parallelQuerySchedulerLabels" -}}
{{ include "loki.labels" . }}
app.kubernetes.io/component: parallel-query-scheduler
{{- end }}

{{/*
query-scheduler selector labels
*/}}
{{- define "loki.parallelQuerySchedulerSelectorLabels" -}}
{{ include "loki.selectorLabels" . }}
app.kubernetes.io/component: parallel-query-scheduler
{{- end }}

{{/*
parallel-query-scheduler image
*/}}
{{- define "loki.parallelQuerySchedulerImage" -}}
{{- $dict := dict "loki" .Values.loki.image "service" .Values.parallelQueryScheduler.image "global" .Values.global.image "defaultVersion" .Chart.AppVersion -}}
{{- include "loki.lokiImage" $dict -}}
{{- end }}

{{/*
parallel-query-scheduler priority class name
*/}}
{{- define "loki.parallelQuerySchedulerPriorityClassName" -}}
{{- $pcn := coalesce .Values.global.priorityClassName .Values.parallelQueryScheduler.priorityClassName -}}
{{- if $pcn }}
priorityClassName: {{ $pcn }}
{{- end }}
{{- end }}
