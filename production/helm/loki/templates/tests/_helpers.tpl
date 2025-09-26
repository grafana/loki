{{/*
Docker image name for loki helm test
*/}}
{{- define "loki.helmTestImage" -}}
{{- $dict := dict "service" .Values.test.image "global" .Values.global.image "defaultVersion" .Chart.AppVersion -}}
{{- include "loki.baseImage" $dict -}}
{{- end -}}


{{/*
test common labels
*/}}
{{- define "loki.helmTestLabels" -}}
{{ include "loki.labels" . }}
app.kubernetes.io/component: helm-test
{{- end }}
