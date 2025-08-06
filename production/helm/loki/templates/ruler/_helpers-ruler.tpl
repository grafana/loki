{{/*
ruler fullname
*/}}
{{- define "loki.rulerFullname" -}}
{{ include "loki.fullname" . }}-ruler
{{- end }}

{{/*
ruler common labels
*/}}
{{- define "loki.rulerLabels" -}}
{{ include "loki.labels" . }}
app.kubernetes.io/component: ruler
{{- end }}

{{/*
ruler selector labels
*/}}
{{- define "loki.rulerSelectorLabels" -}}
{{ include "loki.selectorLabels" . }}
app.kubernetes.io/component: ruler
{{- end }}

{{/*
ruler image
*/}}
{{- define "loki.rulerImage" -}}
{{- $dict := dict "loki" .Values.loki.image "service" .Values.ruler.image "global" .Values.global.image "defaultVersion" .Chart.AppVersion -}}
{{- include "loki.lokiImage" $dict -}}
{{- end }}

{{/*
format rules dir
*/}}
{{- define "loki.rulerRulesDirName" -}}
rules-{{ . | replace "_" "-" | trimSuffix "-" | lower }}
{{- end }}

{{/*
ruler priority class name
*/}}
{{- define "loki.rulerPriorityClassName" -}}
{{- $pcn := coalesce .Values.global.priorityClassName .Values.ruler.priorityClassName -}}
{{- if $pcn }}
priorityClassName: {{ $pcn }}
{{- end }}
{{- end }}

{{/*
ruler readiness probe
*/}}
{{- define "loki.ruler.readinessProbe" }}
{{- with .Values.ruler.readinessProbe | default .Values.loki.readinessProbe }}
readinessProbe:
  {{- toYaml . | nindent 2 }}
{{- end }}
{{- end }}

{{/*
ruler liveness probe
*/}}
{{- define "loki.ruler.livenessProbe" }}
{{- with .Values.ruler.livenessProbe | default .Values.loki.livenessProbe }}
livenessProbe:
  {{- toYaml . | nindent 2 }}
{{- end }}
{{- end }}
