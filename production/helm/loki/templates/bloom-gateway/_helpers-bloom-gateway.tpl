{{/*
bloom gateway fullname
*/}}
{{- define "loki.bloomGatewayFullname" -}}
{{ include "loki.fullname" . }}-bloom-gateway
{{- end }}

{{/*
bloom gateway common labels
*/}}
{{- define "loki.bloomGatewayLabels" -}}
{{ include "loki.labels" . }}
app.kubernetes.io/component: bloom-gateway
{{- end }}

{{/*
bloom gateway selector labels
*/}}
{{- define "loki.bloomGatewaySelectorLabels" -}}
{{ include "loki.selectorLabels" . }}
app.kubernetes.io/component: bloom-gateway
{{- end }}

{{/*
bloom gateway livenessProbe
*/}}
{{- define "loki.bloomGateway.livenessProbe" }}
{{- if .Values.bloomGateway.livenessProbe }}
livenessProbe:
  {{- toYaml .Values.bloomGateway.livenessProbe | nindent 2 }}
{{- else if .Values.loki.livenessProbe }}
livenessProbe:
  {{- toYaml .Values.loki.livenessProbe | nindent 2 }}
{{- end }}
{{- end }}

{{/*
bloom gateway readinessProbe
*/}}
{{- define "loki.bloomGateway.readinessProbe" }}
{{- if .Values.bloomGateway.readinessProbe }}
readinessProbe:
  {{- toYaml .Values.bloomGateway.readinessProbe | nindent 2 }}
{{- else if .Values.loki.readinessProbe }}
readinessProbe:
  {{- toYaml .Values.loki.readinessProbe | nindent 2 }}
{{- end }}
{{- end }}

{{/*
bloom gateway startupProbe
*/}}
{{- define "loki.bloomGateway.startupProbe" }}
{{- if .Values.bloomGateway.startupProbe }}
startupProbe:
  {{- toYaml .Values.bloomGateway.startupProbe | nindent 2 }}
{{- else if .Values.loki.startupProbe }}
startupProbe:
  {{- toYaml .Values.loki.startupProbe | nindent 2 }}
{{- end }}
{{- end }}

{{/*
bloom gateway priority class name
*/}}
{{- define "loki.bloomGatewayPriorityClassName" }}
{{- $pcn := coalesce .Values.global.priorityClassName .Values.bloomGateway.priorityClassName -}}
{{- if $pcn }}
priorityClassName: {{ $pcn }}
{{- end }}
{{- end }}

{{/*
Create the name of the bloom gateway service account
*/}}
{{- define "loki.bloomGatewayServiceAccountName" -}}
{{- if .Values.bloomGateway.serviceAccount.create -}}
    {{ default (print (include "loki.serviceAccountName" .) "-bloom-gateway") .Values.bloomGateway.serviceAccount.name }}
{{- else -}}
    {{ default (include "loki.serviceAccountName" .) .Values.bloomGateway.serviceAccount.name }}
{{- end -}}
{{- end -}}
