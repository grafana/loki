{{/*
singleBinary common labels
*/}}
{{- define "loki.singleBinaryLabels" -}}
{{ include "loki.labels" . }}
app.kubernetes.io/component: single-binary
{{- end }}


{{/* singleBinary selector labels */}}
{{- define "loki.singleBinarySelectorLabels" -}}
{{ include "loki.selectorLabels" . }}
app.kubernetes.io/component: single-binary
{{- end }}

{{/*
singleBinary priority class name
*/}}
{{- define "loki.singleBinaryPriorityClassName" -}}
{{- $pcn := coalesce .Values.global.priorityClassName .Values.singleBinary.priorityClassName -}}
{{- if $pcn }}
priorityClassName: {{ $pcn }}
{{- end }}
{{- end }}

{{/*
singleBinary readiness probe
*/}}
{{- define "loki.singleBinary.readinessProbe" }}
{{- with .Values.singleBinary.readinessProbe | default .Values.loki.readinessProbe }}
readinessProbe:
  {{- toYaml . | nindent 2 }}
{{- end }}
{{- end }}

{{/*
singleBinary liveness probe
*/}}
{{- define "loki.singleBinary.livenessProbe" }}
{{- with .Values.singleBinary.livenessProbe | default .Values.loki.livenessProbe }}
livenessProbe:
  {{- toYaml . | nindent 2 }}
{{- end }}
{{- end }}

{{/* singleBinary replicas calculation */}}
{{- define "loki.singleBinaryReplicas" -}}
{{- $replicas := 1 }}
{{- $usingObjectStorage := eq (include "loki.isUsingObjectStorage" .) "true" }}
{{- if and $usingObjectStorage (gt (int .Values.singleBinary.replicas) 1)}}
{{- $replicas = int .Values.singleBinary.replicas -}}
{{- end }}
{{- printf "%d" $replicas }}
{{- end }}
