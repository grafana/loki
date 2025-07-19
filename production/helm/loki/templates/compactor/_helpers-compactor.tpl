{{/*
compactor fullname
*/}}
{{- define "loki.compactorFullname" -}}
{{ include "loki.fullname" . }}-compactor
{{- end }}

{{/*
compactor common labels
*/}}
{{- define "loki.compactorLabels" -}}
{{ include "loki.labels" . }}
app.kubernetes.io/component: compactor
{{- end }}

{{/*
compactor selector labels
*/}}
{{- define "loki.compactorSelectorLabels" -}}
{{ include "loki.selectorLabels" . }}
app.kubernetes.io/component: compactor
{{- end }}

{{/*
compactor image
*/}}
{{- define "loki.compactorImage" -}}
{{- $dict := dict "loki" .Values.loki.image "service" .Values.compactor.image "global" .Values.global.image "defaultVersion" .Chart.AppVersion -}}
{{- include "loki.lokiImage" $dict -}}
{{- end }}

{{/*
compactor readiness probe
*/}}
{{- define "loki.compactor.readinessProbe" }}
{{- with .Values.compactor.readinessProbe | default .Values.loki.readinessProbe }}
readinessProbe:
  {{- toYaml . | nindent 2 }}
{{- end }}
{{- end }}

{{/*
compactor liveness probe
*/}}
{{- define "loki.compactor.livenessProbe" }}
{{- with .Values.compactor.livenessProbe | default .Values.loki.livenessProbe }}
livenessProbe:
  {{- toYaml . | nindent 2 }}
{{- end }}
{{- end }}

{{/*
compactor priority class name
*/}}
{{- define "loki.compactorPriorityClassName" }}
{{- $pcn := coalesce .Values.global.priorityClassName .Values.compactor.priorityClassName -}}
{{- if $pcn }}
priorityClassName: {{ $pcn }}
{{- end }}
{{- end }}

{{/*
Create the name of the compactor service account
*/}}
{{- define "loki.compactorServiceAccountName" -}}
{{- if .Values.compactor.serviceAccount.create -}}
    {{ default (print (include "loki.serviceAccountName" .) "-compactor") .Values.compactor.serviceAccount.name }}
{{- else -}}
    {{ default (include "loki.serviceAccountName" .) .Values.compactor.serviceAccount.name }}
{{- end -}}
{{- end -}}
