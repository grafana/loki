{{/*
canary fullname
*/}}
{{- define "loki-canary.fullname" -}}
{{ include "loki.name" . }}-canary
{{- end }}

{{/*
canary common labels
*/}}
{{- define "loki-canary.labels" -}}
{{ include "loki.labels" . }}
app.kubernetes.io/component: canary
{{- end }}

{{/*
canary selector labels
*/}}
{{- define "loki-canary.selectorLabels" -}}
{{ include "loki.selectorLabels" . }}
app.kubernetes.io/component: canary
{{- end }}

{{/*
Docker image name for loki-canary
*/}}
{{- define "loki-canary.image" -}}
{{- $dict := dict "service" .Values.monitoring.selfMonitoring.lokiCanary.image "global" .Values.global.image "defaultVersion" "latest" -}}
{{- include "loki.baseImage" $dict -}}
{{- end -}}

{{/*
canry priority class name
*/}}
{{- define "loki-canary.priorityClassName" -}}
{{- $pcn := coalesce .Values.global.priorityClassName .Values.read.priorityClassName -}}
{{- if $pcn }}
priorityClassName: {{ $pcn }}
{{- end }}
{{- end }}
