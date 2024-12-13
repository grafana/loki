{{/*
memcached StatefulSet
Params:
  ctx = . context
  valuesSection = name of the section in values.yaml
  component = name of the component
valuesSection and component are specified separately because helm prefers camelcase for naming convetion and k8s components are named with snake case.
*/}}
{{- define "loki.memcached.statefulSet" -}}
{{ with (index $.ctx.Values $.valuesSection) }}
{{- if .enabled -}}
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: {{ include "loki.resourceName" (dict "ctx" $.ctx "component" $.component) }}
  labels:
    {{- include "loki.labels" $.ctx | nindent 4 }}
    app.kubernetes.io/component: "memcached-{{ $.component }}"
    name: "memcached-{{ $.component }}"
  annotations:
    {{- toYaml .annotations | nindent 4 }}
  namespace: {{ $.ctx.Release.Namespace | quote }}
spec:
  podManagementPolicy: {{ .podManagementPolicy }}
  replicas: {{ .replicas }}
  selector:
    matchLabels:
      {{- include "loki.selectorLabels" $.ctx | nindent 6 }}
      app.kubernetes.io/component: "memcached-{{ $.component }}"
      name: "memcached-{{ $.component }}"
  updateStrategy:
    {{- toYaml .statefulStrategy | nindent 4 }}
  serviceName: {{ template "loki.fullname" $.ctx }}-{{ $.component }}

  template:
    metadata:
      labels:
        {{- include "loki.selectorLabels" $.ctx | nindent 8 }}
        app.kubernetes.io/component: "memcached-{{ $.component }}"
        name: "memcached-{{ $.component }}"
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
      securityContext:
        {{- toYaml $.ctx.Values.memcached.podSecurityContext | nindent 8 }}
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
          {{- with $.ctx.Values.memcached.image }}
          image: {{ .repository }}:{{ .tag }}
          imagePullPolicy: {{ .pullPolicy }}
          {{- end }}
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

      {{- if $.ctx.Values.memcachedExporter.enabled }}
        - name: exporter
          {{- with $.ctx.Values.memcachedExporter.image }}
          image: {{ .repository}}:{{ .tag }}
          imagePullPolicy: {{ .pullPolicy }}
          {{- end }}
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
      spec:
        accessModes: [ "ReadWriteOnce" ]
        {{- with .persistence.storageClass }}
        storageClassName: {{ if (eq "-" .) }}""{{ else }}{{ . }}{{ end }}
        {{- end }}
        resources:
          requests:
            storage: {{ .persistence.storageSize | quote }}
  {{- end }}
{{- end -}}
{{- end -}}
{{- end -}}

