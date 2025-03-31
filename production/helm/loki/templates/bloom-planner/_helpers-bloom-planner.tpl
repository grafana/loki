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
bloom planner readinessProbe
*/}}
{{- define "loki.bloomPlanner.readinessProbe" -}}
{{- with .Values.bloomPlanner.readinessProbe }}
readinessProbe:
  {{- toYaml . | nindent 2 }}
{{- else }}
{{- with .Values.loki.readinessProbe }}
readinessProbe:
  {{- toYaml . | nindent 2 }}
{{- end }}
{{- end }}
{{- end -}}

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
