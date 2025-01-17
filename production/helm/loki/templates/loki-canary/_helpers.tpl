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
{{- $dict := dict "service" .Values.lokiCanary.image "global" .Values.global.image "defaultVersion" .Chart.AppVersion -}}
{{- include "loki.baseImage" $dict -}}
{{- end -}}

{{/*
canary priority class name
*/}}
{{- define "loki-canary.priorityClassName" -}}
{{- $pcn := coalesce .Values.global.priorityClassName .Values.lokiCanary.priorityClassName .Values.read.priorityClassName -}}
{{- if $pcn }}
priorityClassName: {{ $pcn }}
{{- end }}
{{- end }}
