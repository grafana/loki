{{/*
bloom compactor fullname
*/}}
{{- define "loki.bloomCompactorFullname" -}}
{{ include "loki.fullname" . }}-bloom-compactor
{{- end }}

{{/*
bloom compactor common labels
*/}}
{{- define "loki.bloomCompactorLabels" -}}
{{ include "loki.labels" . }}
app.kubernetes.io/component: bloom-compactor
{{- end }}

{{/*
bloom compactor selector labels
*/}}
{{- define "loki.bloomCompactorSelectorLabels" -}}
{{ include "loki.selectorLabels" . }}
app.kubernetes.io/component: bloom-compactor
{{- end }}

{{/*
bloom compactor readinessProbe
*/}}
{{- define "loki.bloomCompactor.readinessProbe" -}}
{{- with .Values.bloomCompactor.readinessProbe }}
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
bloom compactor priority class name
*/}}
{{- define "loki.bloomCompactorPriorityClassName" }}
{{- $pcn := coalesce .Values.global.priorityClassName .Values.bloomCompactor.priorityClassName -}}
{{- if $pcn }}
priorityClassName: {{ $pcn }}
{{- end }}
{{- end }}

{{/*
Create the name of the bloom compactor service account
*/}}
{{- define "loki.bloomCompactorServiceAccountName" -}}
{{- if .Values.bloomCompactor.serviceAccount.create -}}
    {{ default (print (include "loki.serviceAccountName" .) "-bloom-compactor") .Values.bloomCompactor.serviceAccount.name }}
{{- else -}}
    {{ default (include "loki.serviceAccountName" .) .Values.bloomCompactor.serviceAccount.name }}
{{- end -}}
{{- end -}}
