{{/* vim: set filetype=mustache: */}}
{{/*
Expand the name of the chart.
*/}}
{{- define "loki.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "loki.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- $name := default .Chart.Name .Values.nameOverride -}}
{{- if contains $name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "loki.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Create the name of the service account
*/}}
{{- define "loki.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
    {{ default (include "loki.fullname" .) .Values.serviceAccount.name }}
{{- else -}}
    {{ default "default" .Values.serviceAccount.name }}
{{- end -}}
{{- end -}}

{{- define "configSecret" }}
auth_enabled: {{ .Values.config.auth_enabled }}

server:
  http_listen_port: {{ .Values.port }}

limits_config:
  enforce_metric_name: false

ingester:
  lifecycler:
    ring:
      store: {{ .Values.config.ingester.lifecycler.ring.store }}
      replication_factor: {{ .Values.config.ingester.lifecycler.ring.replication_factor }}
  chunk_idle_period: 15m

{{- if .Values.config.schema_configs }}
schema_config:
  configs:
{{- range .Values.config.schema_configs }}
  - from: {{ .from }}
    store: {{ .store }}
    object_store: {{ .object_store }}
    schema: {{ .schema }}
    index:
      prefix: {{ .index.prefix }}
      period: {{ .index.period }}
{{- end -}}
{{- end -}}

{{- with .Values.config.storage_config }}
storage_config:
{{ toYaml . | indent 2 }}
{{- end }}

{{- end}}
