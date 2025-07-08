{{/*
memcached StatefulSet
Params:
  ctx = . context
  memcacheConfig = cache config
  valuesSection = name of the section in values.yaml
  component = name of the component
valuesSection and component are specified separately because helm prefers camelcase for naming convetion and k8s components are named with snake case.
*/}}
{{- define "loki.memcached.pdb" -}}
{{ with $.memcacheConfig }}
{{- if .enabled -}}
apiVersion: {{ include "loki.pdb.apiVersion" $.ctx }}
kind: PodDisruptionBudget
metadata:
  name: {{ include "loki.resourceName" (dict "ctx" $.ctx "component" $.component "suffix" .suffix) }}
  namespace: {{ $.ctx.Release.Namespace }}
  labels:
    {{- include "loki.selectorLabels" $.ctx | nindent 4 }}
    app.kubernetes.io/component: "memcached-{{ $.component }}{{ include "loki.memcached.suffix" .suffix }}"
spec:
  selector:
    matchLabels:
      {{- include "loki.selectorLabels" $.ctx | nindent 6 }}
      app.kubernetes.io/component: "memcached-{{ $.component }}{{ include "loki.memcached.suffix" .suffix }}"
  maxUnavailable: 1
{{- end -}}
{{- end -}}
{{- end -}}
