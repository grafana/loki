{{/*
pattern ingester fullname
*/}}
{{- define "loki.patternIngesterFullname" -}}
{{ include "loki.fullname" . }}-pattern-ingester
{{- end }}

{{/*
pattern ingester common labels
*/}}
{{- define "loki.patternIngesterLabels" -}}
{{ include "loki.labels" . }}
app.kubernetes.io/component: pattern-ingester
{{- end }}

{{/*
pattern ingester selector labels
*/}}
{{- define "loki.patternIngesterSelectorLabels" -}}
{{ include "loki.selectorLabels" . }}
app.kubernetes.io/component: pattern-ingester
{{- end }}

{{/*
pattern ingester readinessProbe
*/}}
{{- define "loki.patternIngester.readinessProbe" -}}
{{- with .Values.patternIngester.readinessProbe }}
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
pattern ingester priority class name
*/}}
{{- define "loki.patternIngesterPriorityClassName" }}
{{- $pcn := coalesce .Values.global.priorityClassName .Values.patternIngester.priorityClassName -}}
{{- if $pcn }}
priorityClassName: {{ $pcn }}
{{- end }}
{{- end }}

{{/*
Create the name of the pattern ingester service account
*/}}
{{- define "loki.patternIngesterServiceAccountName" -}}
{{- if .Values.patternIngester.serviceAccount.create -}}
    {{ default (print (include "loki.serviceAccountName" .) "-pattern-ingester") .Values.patternIngester.serviceAccount.name }}
{{- else -}}
    {{ default (include "loki.serviceAccountName" .) .Values.patternIngester.serviceAccount.name }}
{{- end -}}
{{- end -}}
