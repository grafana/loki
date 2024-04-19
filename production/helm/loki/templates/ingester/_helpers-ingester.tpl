{{/*
ingester fullname
*/}}
{{- define "loki.ingesterFullname" -}}
{{ include "loki.fullname" . }}-ingester
{{- end }}

{{/*
ingester common labels
*/}}
{{- define "loki.ingesterLabels" -}}
{{ include "loki.labels" . }}
app.kubernetes.io/component: ingester
{{- end }}

{{/*
ingester selector labels
*/}}
{{- define "loki.ingesterSelectorLabels" -}}
{{ include "loki.selectorLabels" . }}
app.kubernetes.io/component: ingester
{{- end }}

{{/*
ingester priority class name
*/}}
{{- define "loki.ingesterPriorityClassName" -}}
{{- $pcn := coalesce .Values.global.priorityClassName .Values.ingester.priorityClassName -}}
{{- if $pcn }}
priorityClassName: {{ $pcn }}
{{- end }}
{{- end }}

{{- define "loki.ingester.readinessProbe" -}}
{{- with .Values.ingester.readinessProbe }}  
readinessProbe:
  {{- toYaml . | nindent 2 }}
{{- else }}
{{- with .Values.loki.readinessProbe }}
readinessProbe:
  {{- toYaml . | nindent 2 }}
{{- end }}
{{- end }}
{{- end -}}

{{- define "loki.ingester.livenessProbe" -}}
{{- with .Values.ingester.livenessProbe }}
livenessProbe:
  {{- toYaml . | nindent 2 }}
{{- else }}
{{- with .Values.loki.livenessProbe }}
livenessProbe:
  {{- toYaml . | nindent 2 }}
{{- end }}
{{- end }}
{{- end -}}

{{/*
expects global context
*/}}
{{- define "loki.ingester.replicaCount" -}}
{{- ceil (divf .Values.ingester.replicas 3) -}}
{{- end -}}

{{/*
expects a dict
{
  "replicas": replicas in a zone,
  "ctx": global context
}
*/}}
{{- define "loki.ingester.maxUnavailable" -}}
{{- ceil (mulf .replicas (divf (int .ctx.Values.ingester.zoneAwareReplication.maxUnavailablePct) 100)) -}}
{{- end -}}