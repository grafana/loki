{{/*
overrides-exporter fullname
*/}}
{{- define "loki.overridesExporterFullname" -}}
{{ include "loki.fullname" . }}-overrides-exporter
{{- end }}

{{/*
overrides-exporter common labels
*/}}
{{- define "loki.overridesExporterLabels" -}}
{{ include "loki.labels" . }}
app.kubernetes.io/component: overrides-exporter
{{- end }}

{{/*
overrides-exporter selector labels
*/}}
{{- define "loki.overridesExporterSelectorLabels" -}}
{{ include "loki.selectorLabels" . }}
app.kubernetes.io/component: overrides-exporter
{{- end }}

{{/*
overrides-exporter priority class name
*/}}
{{- define "loki.overridesExporterPriorityClassName" -}}
{{- $pcn := coalesce .Values.global.priorityClassName .Values.overridesExporter.priorityClassName -}}
{{- if $pcn }}
priorityClassName: {{ $pcn }}
{{- end }}
{{- end }}
