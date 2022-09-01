{{/*
dashboards name
*/}}
{{- define "loki.dashboardsName" -}}
{{ include "loki.name" . }}-dashboards
{{- end }}
