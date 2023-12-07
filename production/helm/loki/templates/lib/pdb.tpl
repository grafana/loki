{{/*
Loki common PodDisruptionBudget definition
Params:
  ctx = . context
  component = name of the component
*/}}
{{- define "loki.lib.podDisruptionBudget" -}}
{{- $componentSection := include "loki.componentSectionFromName" . | fromYaml }}
{{ with ($componentSection).podDisruptionBudget }}
apiVersion: {{ include "loki.podDisruptionBudget.apiVersion" $.ctx }}
kind: PodDisruptionBudget
metadata:
  name: {{ include "loki.resourceName" (dict "ctx" $.ctx "component" $.component) }}
  labels:
    {{- include "loki.labels" (dict "ctx" $.ctx "component" $.component) | nindent 4 }}
  namespace: {{ $.ctx.Release.Namespace | quote }}
spec:
  selector:
    matchLabels:
      {{- include "loki.selectorLabels" (dict "ctx" $.ctx "component" $.component) | nindent 6 }}
{{ toYaml . | indent 2 }}
{{- end -}}
{{- end -}}
