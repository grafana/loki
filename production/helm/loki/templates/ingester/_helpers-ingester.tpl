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
{{- with .Values.ingester.readinessProbe | default .Values.loki.readinessProbe }}
readinessProbe:
  {{- toYaml . | nindent 2 }}
{{- end }}
{{- end }}

{{/*
ingester liveness probe
*/}}
{{- define "loki.ingester.livenessProbe" }}
{{- with .Values.ingester.livenessProbe | default .Values.loki.livenessProbe }}
livenessProbe:
  {{- toYaml . | nindent 2 }}
{{- end }}
{{- end }}

{{/*
ingester startup probe
*/}}
{{- define "loki.ingester.startupProbe" }}
{{- with .Values.ingester.startupProbe | default .Values.loki.startupProbe }}
startupProbe:
  {{- toYaml . | nindent 2 }}
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
Returns true if only zone-a should get an HPA when zone-aware replication is enabled.
Defaults to rollout_operator.enabled when scaleOnlyZoneA is not explicitly set.
*/}}
{{- define "loki.ingester.autoscaling.scaleOnlyZoneA" -}}
{{- if kindIs "bool" .Values.ingester.autoscaling.scaleOnlyZoneA -}}
{{- .Values.ingester.autoscaling.scaleOnlyZoneA -}}
{{- else -}}
{{- .Values.rollout_operator.enabled -}}
{{- end -}}
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
