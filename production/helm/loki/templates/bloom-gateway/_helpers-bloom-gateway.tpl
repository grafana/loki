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
bloom gateway readinessProbe
*/}}
{{- define "loki.bloomGateway.readinessProbe" -}}
{{- with .Values.bloomGateway.readinessProbe }}
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
