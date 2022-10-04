{{/*
Docker image name for loki helm test
*/}}
{{- define "loki.helm-test-image" -}}
{{- $dict := dict "service" .Values.test.image "global" .Values.global.image "defaultVersion" "latest" -}}
{{- include "loki.baseImage" $dict -}}
{{- end -}}
