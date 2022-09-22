{{/*
Docker image name for loki-canary-test
*/}}
{{- define "loki-canary-test.image" -}}
{{- $dict := dict "service" .Values.monitoring.selfMonitoring.lokiCanary.test.image "global" .Values.global.image "defaultVersion" "latest" -}}
{{- include "loki.baseImage" $dict -}}
{{- end -}}
