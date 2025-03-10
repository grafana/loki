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
pattern ingester livenessProbe
*/}}
{{- define "loki.patternIngester.livenessProbe" }}
{{- if .Values.patternIngester.livenessProbe }}
livenessProbe:
  {{- toYaml .Values.patternIngester.livenessProbe | nindent 2 }}
{{- else if .Values.loki.livenessProbe }}
livenessProbe:
  {{- toYaml .Values.loki.livenessProbe | nindent 2 }}
{{- end }}
{{- end }}

{{/*
pattern ingester readinessProbe
*/}}
{{- define "loki.patternIngester.readinessProbe" }}
{{- if .Values.patternIngester.readinessProbe }}
readinessProbe:
  {{- toYaml .Values.patternIngester.readinessProbe | nindent 2 }}
{{- else if .Values.loki.readinessProbe }}
readinessProbe:
  {{- toYaml .Values.loki.readinessProbe | nindent 2 }}
{{- end }}
{{- end }}

{{/*
pattern ingester startupProbe
*/}}
{{- define "loki.patternIngester.startupProbe" }}
{{- if .Values.patternIngester.startupProbe }}
startupProbe:
  {{- toYaml .Values.patternIngester.startupProbe | nindent 2 }}
{{- else if .Values.loki.startupProbe }}
startupProbe:
  {{- toYaml .Values.loki.startupProbe | nindent 2 }}
{{- end }}
{{- end }}

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
