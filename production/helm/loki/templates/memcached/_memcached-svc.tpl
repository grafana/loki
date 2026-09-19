{{/*
memcached Service
Params:
  ctx = . context
  valuesSection = name of the section in values.yaml
  memcacheConfig = cache config
  component = name of the component
valuesSection and component are specified separately because helm prefers camelcase for naming convetion and k8s components are named with snake case.
*/}}
{{- define "loki.memcached.service" -}}
{{ with $.memcacheConfig }}
{{- if and .enabled ($.ctx.Values.memcached.enabled) -}}
apiVersion: v1
kind: Service
metadata:
  name: {{ include "loki.resourceName" (dict "ctx" $.ctx "component" $.component "suffix" .suffix) }}
  labels:
    {{- include "loki.labels" $.ctx | nindent 4 }}
    app.kubernetes.io/component: "memcached-{{ $.component }}{{ include "loki.memcached.suffix" .suffix }}"
    {{- with .service.labels }}
    {{- toYaml . | nindent 4 }}
    {{- end }}
  annotations:
    {{- toYaml .service.annotations | nindent 4 }}
  namespace: {{ include "loki.namespace" $.ctx | quote }}
spec:
  type: ClusterIP
  clusterIP: None
  ports:
    - name: memcached-client
      port: {{ .port }}
      targetPort: client
    {{ if $.ctx.Values.memcachedExporter.enabled -}}
    - name: http-metrics
      port: 9150
      targetPort: http-metrics
    {{ end }}
  selector:
    {{- include "loki.selectorLabels" $.ctx | nindent 4 }}
    app.kubernetes.io/component: "memcached-{{ $.component }}{{ include "loki.memcached.suffix" .suffix }}"
{{- end -}}
{{- end -}}
{{- end -}}
