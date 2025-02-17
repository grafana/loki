{{/*
ruler fullname
*/}}
{{- define "loki.rulerFullname" -}}
{{ include "loki.fullname" . }}-ruler
{{- end }}

{{/*
ruler common labels
*/}}
{{- define "loki.rulerLabels" -}}
{{ include "loki.labels" . }}
app.kubernetes.io/component: ruler
{{- end }}

{{/*
ruler selector labels
*/}}
{{- define "loki.rulerSelectorLabels" -}}
{{ include "loki.selectorLabels" . }}
app.kubernetes.io/component: ruler
{{- end }}

{{/*
ruler image
*/}}
{{- define "loki.rulerImage" -}}
{{- $dict := dict "loki" .Values.loki.image "service" .Values.ruler.image "global" .Values.global.image "defaultVersion" .Chart.AppVersion -}}
{{- include "loki.lokiImage" $dict -}}
{{- end }}

{{/*
format rules dir
*/}}
{{- define "loki.rulerRulesDirName" -}}
rules-{{ . | replace "_" "-" | trimSuffix "-" | lower }}
{{- end }}

{{/*
ruler priority class name
*/}}
{{- define "loki.rulerPriorityClassName" -}}
{{- $pcn := coalesce .Values.global.priorityClassName .Values.ruler.priorityClassName -}}
{{- if $pcn }}
priorityClassName: {{ $pcn }}
{{- end }}
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "loki.rulerServiceAccountName" -}}
{{- if .Values.ruler.serviceAccount.create -}}
    {{ .Values.ruler.serviceAccount.name | default printf "%s-ruler" (include "loki.serviceAccountName" .) }}
{{- else -}}
    {{ default (include "loki.serviceAccountName" .) }}
{{- end -}}
{{- end -}}
