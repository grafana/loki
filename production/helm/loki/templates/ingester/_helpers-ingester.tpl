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

{{/*
ingester readiness probe
*/}}
{{- define "loki.ingester.readinessProbe" }}
{{- if .Values.ingester.readinessProbe }}
readinessProbe:
  {{- toYaml .Values.ingester.readinessProbe | nindent 2 }}
{{- else if .Values.loki.readinessProbe }}
readinessProbe:
  {{- toYaml .Values.loki.readinessProbe | nindent 2 }}
{{- end }}
{{- end }}

{{/*
ingester liveness probe
*/}}
{{- define "loki.ingester.livenessProbe" }}
{{- if .Values.ingester.livenessProbe }}
livenessProbe:
  {{- toYaml .Values.ingester.livenessProbe | nindent 2 }}
{{- else if .Values.loki.livenessProbe }}
livenessProbe:
  {{- toYaml .Values.loki.livenessProbe | nindent 2 }}
{{- end }}
{{- end }}

{{/*
ingester startup probe
*/}}
{{- define "loki.ingester.startupProbe" }}
{{- if .Values.ingester.startupProbe }}
startupProbe:
  {{- toYaml .Values.ingester.startupProbe | nindent 2 }}
{{- else if .Values.loki.startupProbe }}
startupProbe:
  {{- toYaml .Values.loki.startupProbe | nindent 2 }}
{{- end }}
{{- end }}

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

{{/*
Return rollout-group prefix if it is set
*/}}
{{- define "loki.prefixRolloutGroup" -}}
{{- if .Values.ingester.rolloutGroupPrefix -}}
{{- .Values.ingester.rolloutGroupPrefix -}}-
{{- end -}}
{{- end -}}

{{/*
Return ingester name prefix if required
*/}}
{{- define "loki.prefixIngesterName" -}}
{{- if .Values.ingester.addIngesterNamePrefix -}}
loki-
{{- end -}}
{{- end -}}
