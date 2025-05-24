{{/*
bloom planner fullname
*/}}
{{- define "loki.bloomPlannerFullname" -}}
{{ include "loki.fullname" . }}-bloom-planner
{{- end }}

{{/*
bloom planner common labels
*/}}
{{- define "loki.bloomPlannerLabels" -}}
{{ include "loki.labels" . }}
app.kubernetes.io/component: bloom-planner
{{- end }}

{{/*
bloom planner selector labels
*/}}
{{- define "loki.bloomPlannerSelectorLabels" -}}
{{ include "loki.selectorLabels" . }}
app.kubernetes.io/component: bloom-planner
{{- end }}

{{/*
bloom planner livenessProbe
*/}}
{{- define "loki.bloomPlanner.livenessProbe" }}
{{- if .Values.bloomPlanner.livenessProbe }}
livenessProbe:
  {{- toYaml .Values.bloomPlanner.livenessProbe | nindent 2 }}
{{- else if .Values.loki.livenessProbe }}
livenessProbe:
  {{- toYaml .Values.loki.livenessProbe | nindent 2 }}
{{- end }}
{{- end }}

{{/*
bloom planner readinessProbe
*/}}
{{- define "loki.bloomPlanner.readinessProbe" }}
{{- if .Values.bloomPlanner.readinessProbe }}
readinessProbe:
  {{- toYaml .Values.bloomPlanner.readinessProbe | nindent 2 }}
{{- else if .Values.loki.readinessProbe }}
readinessProbe:
  {{- toYaml .Values.loki.readinessProbe | nindent 2 }}
{{- end }}
{{- end }}

{{/*
bloom planner startupProbe
*/}}
{{- define "loki.bloomPlanner.startupProbe" }}
{{- if .Values.bloomPlanner.startupProbe }}
startupProbe:
  {{- toYaml .Values.bloomPlanner.startupProbe | nindent 2 }}
{{- else if .Values.loki.startupProbe }}
startupProbe:
  {{- toYaml .Values.loki.startupProbe | nindent 2 }}
{{- end }}
{{- end }}

{{/*
bloom planner priority class name
*/}}
{{- define "loki.bloomPlannerPriorityClassName" }}
{{- $pcn := coalesce .Values.global.priorityClassName .Values.bloomPlanner.priorityClassName -}}
{{- if $pcn }}
priorityClassName: {{ $pcn }}
{{- end }}
{{- end }}

{{/*
Create the name of the bloom planner service account
*/}}
{{- define "loki.bloomPlannerServiceAccountName" -}}
{{- if .Values.bloomPlanner.serviceAccount.create -}}
    {{ default (print (include "loki.serviceAccountName" .) "-bloom-planner") .Values.bloomPlanner.serviceAccount.name }}
{{- else -}}
    {{ default (include "loki.serviceAccountName" .) .Values.bloomPlanner.serviceAccount.name }}
{{- end -}}
{{- end -}}
