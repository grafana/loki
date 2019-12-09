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

{{/*
Create the app name of loki clients. Defaults to the same logic as "loki.fullname", and default client expects "promtail".
*/}}
{{- define "client.name" -}}
{{- if .Values.client.name -}}
{{- .Values.client.name -}}
{{- else if .Values.client.fullnameOverride -}}
{{- .Values.client.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- $name := default "promtail" .Values.client.nameOverride -}}
{{- if contains $name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{/*
Get configuration as a string.
.Values.config is undefined by default, meaning it is only ever defined by the user.
If .Values.config is unspecified, we simply template and return .Values.configDefaults as a YAML string.
Otherwise, we check the type of .Values.config, if it's a string, we template it and return it, otherwise we assume it is a map and merge it with the .Values.configDefaults, convert it from the merged map into a YAML string, and return that string templated.
*/}}
{{- define "loki.config" -}}
{{- if .Values.config }}
{{- if kindIs "string" .Values.config }}
{{ tpl .Values.config . }}
{{- else }}
{{- $srcCopy := deepCopy .Values.configDefaults -}}
{{- $destCopy := deepCopy .Values.config -}}
{{- $newDict := mergeOverwrite $destCopy $srcCopy -}}
{{ tpl (toYaml $newDict) . }}
{{- end -}}{{/* end if kindIs string */}}
{{- else }}{{/* else is when .Values.config is not truthy */}}
{{ tpl (toYaml .Values.configDefaults) . }}
{{- end -}}{{/* end if .Values.config */}}
{{- end -}}{{/* end define */}}



