{{/*
memcached StatefulSet
Params:
  ctx = . context
  memcacheConfig = cache config
  valuesSection = name of the section in values.yaml
  component = name of the component
valuesSection and component are specified separately because helm prefers camelcase for naming convention and k8s components are named with snake case.
*/}}
{{- define "loki.memcached.statefulSet" -}}
{{ with $.memcacheConfig }}
{{- if and .enabled ($.ctx.Values.memcached.enabled) -}}
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: {{ include "loki.resourceName" (dict "ctx" $.ctx "component" $.component "suffix" .suffix) }}
  labels:
    {{- include "loki.labels" $.ctx | nindent 4 }}
    app.kubernetes.io/component: "memcached-{{ $.component }}{{ include "loki.memcached.suffix" .suffix }}"
    name: "memcached-{{ $.component }}{{ include "loki.memcached.suffix" .suffix }}"
  annotations:
    {{- toYaml .annotations | nindent 4 }}
  namespace: {{ include "loki.namespace" $.ctx | quote }}
spec:
  podManagementPolicy: {{ .podManagementPolicy }}
  replicas: {{ .replicas }}
  selector:
    matchLabels:
      {{- include "loki.selectorLabels" $.ctx | nindent 6 }}
      app.kubernetes.io/component: "memcached-{{ $.component }}{{ include "loki.memcached.suffix" .suffix }}"
      name: "memcached-{{ $.component }}{{ include "loki.memcached.suffix" .suffix }}"
  updateStrategy:
    {{- toYaml .statefulStrategy | nindent 4 }}
  serviceName: {{ template "loki.fullname" $.ctx }}-{{ $.component }}{{ include "loki.memcached.suffix" .suffix }}
  template:
    metadata:
      labels:
        {{- include "loki.selectorLabels" $.ctx | nindent 8 }}
        app.kubernetes.io/component: "memcached-{{ $.component }}{{ include "loki.memcached.suffix" .suffix }}"
        name: "memcached-{{ $.component }}{{ include "loki.memcached.suffix" .suffix }}"
        {{- with $.ctx.Values.loki.podLabels }}
        {{- toYaml . | nindent 8 }}
        {{- end }}
        {{- with .podLabels }}
        {{- toYaml . | nindent 8 }}
        {{- end }}
      annotations:
        {{- with $.ctx.Values.global.podAnnotations }}
        {{- toYaml . | nindent 8 }}
        {{- end }}
        {{- with .podAnnotations }}
        {{- toYaml . | nindent 8 }}
        {{- end }}
    spec:
      serviceAccountName: {{ template "loki.serviceAccountName" $.ctx }}
      {{- if .priorityClassName }}
      priorityClassName: {{ .priorityClassName }}
      {{- end }}
      {{- if and (semverCompare ">=1.33-0" (include "loki.kubeVersion" $.ctx)) (kindIs "bool" .hostUsers) }}
      hostUsers: {{ .hostUsers }}
      {{- end }}
      securityContext:
        {{- toYaml $.ctx.Values.memcached.podSecurityContext | nindent 8 }}
      {{- with .dnsConfig | default $.ctx.Values.loki.dnsConfig }}
      dnsConfig:
        {{- toYaml . | nindent 8 }}
      {{- end }}
      initContainers:
        {{- toYaml .initContainers | nindent 8 }}
      nodeSelector:
        {{- toYaml .nodeSelector | nindent 8 }}
      affinity:
        {{- toYaml .affinity | nindent 8 }}
      topologySpreadConstraints:
        {{- toYaml .topologySpreadConstraints | nindent 8 }}
      tolerations:
        {{- toYaml .tolerations | nindent 8 }}
      terminationGracePeriodSeconds: {{ .terminationGracePeriodSeconds }}
      {{- with $.ctx.Values.imagePullSecrets }}
      imagePullSecrets:
        {{- toYaml . | nindent 8 }}
      {{- end }}
      {{- if .extraVolumes }}
      volumes:
        {{- toYaml .extraVolumes | nindent 8 }}
      {{- end }}
      containers:
        {{- if .extraContainers }}
        {{ toYaml .extraContainers | nindent 8 }}
        {{- end }}
        - name: memcached
          {{- $dict := dict "service" $.ctx.Values.memcached.image "global" $.ctx.Values.global }}
          image: {{ include "loki.baseImage" $dict }}
          imagePullPolicy: {{ $.ctx.Values.memcached.image.pullPolicy }}
          resources:
          {{- if .resources }}
            {{- toYaml .resources | nindent 12 }}
          {{- else }}
          {{- /* Calculate requested memory as round(allocatedMemory * 1.2). But with integer built-in operators. */}}
          {{- $requestMemory := div (add (mul .allocatedMemory 12) 5) 10 }}
            limits:
              memory: {{ $requestMemory }}Mi
            requests:
              cpu: 500m
              memory: {{ $requestMemory }}Mi
          {{- end }}
          ports:
            - containerPort: {{ .port }}
              name: client
          {{- /* Calculate storage size as round(.persistence.storageSize * 0.9). But with integer built-in operators. */}}
          {{- $persistenceSize := (div (mul (trimSuffix "Gi" .persistence.storageSize | trimSuffix "G") 9) 10 ) }}
          args:
            - -m {{ .allocatedMemory }}
            - --extended=modern,track_sizes{{ if .persistence.enabled }},ext_path={{ .persistence.mountPath }}/file:{{ $persistenceSize }}G,ext_wbuf_size=16{{ end }}{{ with .extraExtendedOptions }},{{ . }}{{ end }}
            - -I {{ .maxItemMemory }}m
            - -c {{ .connectionLimit }}
            - -v
            - -u {{ .port }}
            {{- range $key, $value := .extraArgs }}
            - "-{{ $key }}{{ if $value }} {{ $value }}{{ end }}"
            {{- end }}
          env:
            {{- with $.ctx.Values.global.extraEnv }}
              {{ toYaml . | nindent 12 }}
            {{- end }}
          envFrom:
            {{- with $.ctx.Values.global.extraEnvFrom }}
              {{- toYaml . | nindent 12 }}
            {{- end }}
          securityContext:
            {{- toYaml $.ctx.Values.memcached.containerSecurityContext | nindent 12 }}
          {{- if or .persistence.enabled .extraVolumeMounts }}
          volumeMounts:
          {{- if .persistence.enabled }}
            - name: data
              mountPath: {{ .persistence.mountPath }}
          {{- end }}
          {{- if .extraVolumeMounts }}
            {{- toYaml .extraVolumeMounts | nindent 12 }}
          {{- end }}
          {{- end }}
          {{- with $.ctx.Values.memcached.readinessProbe }}
          readinessProbe:
            {{- toYaml . | nindent 12 }}
          {{- end }}
          {{- with $.ctx.Values.memcached.livenessProbe }}
          livenessProbe:
            {{- toYaml . | nindent 12 }}
          {{- end }}
          {{- with $.ctx.Values.memcached.startupProbe }}
          startupProbe:
            {{- toYaml . | nindent 12 }}
          {{- end }}

      {{- if $.ctx.Values.memcachedExporter.enabled }}
        - name: exporter
          {{- $dict := dict "service" $.ctx.Values.memcachedExporter.image "global" $.ctx.Values.global }}
          image: {{ include "loki.baseImage" $dict }}
          imagePullPolicy: {{ $.ctx.Values.memcachedExporter.image.pullPolicy }}
          ports:
            - containerPort: 9150
              name: http-metrics
          args:
            - "--memcached.address=localhost:{{ .port }}"
            - "--web.listen-address=0.0.0.0:9150"
            {{- range $key, $value := $.ctx.Values.memcachedExporter.extraArgs }}
            - "--{{ $key }}{{ if $value }}={{ $value }}{{ end }}"
            {{- end }}
          resources:
            {{- toYaml $.ctx.Values.memcachedExporter.resources | nindent 12 }}
          securityContext:
            {{- toYaml $.ctx.Values.memcachedExporter.containerSecurityContext | nindent 12 }}
          {{- with $.ctx.Values.memcachedExporter.readinessProbe }}
          readinessProbe:
            {{- toYaml . | nindent 12 }}
          {{- end }}
          {{- with $.ctx.Values.memcachedExporter.livenessProbe }}
          livenessProbe:
            {{- toYaml . | nindent 12 }}
          {{- end }}
          {{- with $.ctx.Values.memcachedExporter.startupProbe }}
          startupProbe:
            {{- toYaml . | nindent 12 }}
          {{- end }}
          {{- if .extraVolumeMounts }}
          volumeMounts:
            {{- toYaml .extraVolumeMounts | nindent 12 }}
          {{- end }}
      {{- end }}
  {{- if .persistence.enabled }}
  volumeClaimTemplates:
    - apiVersion: v1
      kind: PersistentVolumeClaim
      metadata:
        name: data
        {{- with .persistence.labels }}
        labels:
          {{- toYaml . | nindent 10 }}
        {{- end }}
      spec:
        accessModes: [ "ReadWriteOnce" ]
        {{- with .persistence.storageClass }}
        storageClassName: {{ if (eq "-" .) }}""{{ else }}{{ . }}{{ end }}
        {{- end }}
        {{- with .persistence.volumeAttributesClassName }}
        volumeAttributesClassName: {{ . }}
        {{- end }}
        resources:
          requests:
            storage: {{ .persistence.storageSize | quote }}
  {{- end }}
{{- end -}}
{{- end -}}
{{- end -}}
