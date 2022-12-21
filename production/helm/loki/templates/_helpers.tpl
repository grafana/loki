{{/*
Expand the name of the chart.
*/}}
{{- define "loki.name" -}}
{{- $default := ternary "enterprise-logs" "loki" .Values.enterprise.enabled }}
{{- coalesce .Values.nameOverride $default | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
singleBinary fullname
*/}}
{{- define "loki.singleBinaryFullname" -}}
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
Return if deployment mode is simple scalable
*/}}
{{- define "loki.deployment.isScalable" -}}
  {{- eq (include "loki.isUsingObjectStorage" . ) "true" }}
{{- end -}}

{{/*
Return if deployment mode is single binary
*/}}
{{- define "loki.deployment.isSingleBinary" -}}
  {{- eq (include "loki.isUsingObjectStorage" . ) "false" }}
{{- end -}}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "loki.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := include "loki.name" . }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{/* Create a default storage config that uses filesystem storage
This is required for CI, but Loki will not be queryable with this default
applied, thus it is encouraged that users override this.
*/}}
{{- define "loki.storageConfig" -}}
{{- if .Values.loki.storageConfig -}}
{{- .Values.loki.storageConfig | toYaml | nindent 4 -}}
{{- else }}
{{- .Values.loki.defaultStorageConfig | toYaml | nindent 4 }}
{{- end}}
{{- end}}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "loki.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "loki.labels" -}}
helm.sh/chart: {{ include "loki.chart" . }}
{{ include "loki.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "loki.selectorLabels" -}}
app.kubernetes.io/name: {{ include "loki.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "loki.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
    {{ default (include "loki.name" .) .Values.serviceAccount.name }}
{{- else -}}
    {{ default "default" .Values.serviceAccount.name }}
{{- end -}}
{{- end -}}

{{/*
Base template for building docker image reference
*/}}
{{- define "loki.baseImage" }}
{{- $registry := .global.registry | default .service.registry | default "" -}}
{{- $repository := .service.repository | default "" -}}
{{- $tag := .service.tag | default .defaultVersion | toString -}}
{{- if and $registry $repository -}}
  {{- printf "%s/%s:%s" $registry $repository $tag -}}
{{- else -}}
  {{- printf "%s%s:%s" $registry $repository $tag -}}
{{- end -}}
{{- end -}}

{{/*
Docker image name for Loki
*/}}
{{- define "loki.lokiImage" -}}
{{- $dict := dict "service" .Values.loki.image "global" .Values.global.image "defaultVersion" .Chart.AppVersion -}}
{{- include "loki.baseImage" $dict -}}
{{- end -}}

{{/*
Docker image name for enterprise logs
*/}}
{{- define "loki.enterpriseImage" -}}
{{- $dict := dict "service" .Values.enterprise.image "global" .Values.global.image "defaultVersion" .Values.enterprise.version -}}
{{- include "loki.baseImage" $dict -}}
{{- end -}}

{{/*
Docker image name
*/}}
{{- define "loki.image" -}}
{{- if .Values.enterprise.enabled -}}{{- include "loki.enterpriseImage" . -}}{{- else -}}{{- include "loki.lokiImage" . -}}{{- end -}}
{{- end -}}

{{/*
Docker image name for kubectl container
*/}}
{{- define "loki.kubectlImage" -}}
{{- $dict := dict "service" .Values.kubectlImage "global" .Values.global.image "defaultVersion" "latest" -}}
{{- include "loki.baseImage" $dict -}}
{{- end -}}

{{/*
Generated storage config for loki common config
*/}}
{{- define "loki.commonStorageConfig" -}}
{{- if .Values.minio.enabled -}}
s3:
  endpoint: {{ include "loki.minio" $ }}
  bucketnames: {{ $.Values.loki.storage.bucketNames.chunks }}
  secret_access_key: {{ $.Values.minio.rootPassword }}
  access_key_id: {{ $.Values.minio.rootUser }}
  s3forcepathstyle: true
  insecure: true
{{- else if eq .Values.loki.storage.type "s3" -}}
{{- with .Values.loki.storage.s3 }}
s3:
  {{- with .s3 }}
  s3: {{ . }}
  {{- end }}
  {{- with .endpoint }}
  endpoint: {{ . }}
  {{- end }}
  {{- with .region }}
  region: {{ . }}
  {{- end}}
  bucketnames: {{ $.Values.loki.storage.bucketNames.chunks }}
  {{- with .secretAccessKey }}
  secret_access_key: {{ . }}
  {{- end }}
  {{- with .accessKeyId }}
  access_key_id: {{ . }}
  {{- end }}
  s3forcepathstyle: {{ .s3ForcePathStyle }}
  insecure: {{ .insecure }}
  {{- with .http_config}}
  http_config:
    {{- with .idle_conn_timeout }}
    idle_conn_timeout: {{ . }}
    {{- end}}
    {{- with .response_header_timeout }}
    response_header_timeout: {{ . }}
    {{- end}}
    {{- with .insecure_skip_verify }}
    insecure_skip_verify: {{ . }}
    {{- end}}
    {{- with .ca_file}}
    ca_file: {{ . }}
    {{- end}}
  {{- end }}
{{- end -}}
{{- else if eq .Values.loki.storage.type "gcs" -}}
{{- with .Values.loki.storage.gcs }}
gcs:
  bucket_name: {{ $.Values.loki.storage.bucketNames.chunks }}
  chunk_buffer_size: {{ .chunkBufferSize }}
  request_timeout: {{ .requestTimeout }}
  enable_http2: {{ .enableHttp2 }}
{{- end -}}
{{- else if eq .Values.loki.storage.type "azure" -}}
{{- with .Values.loki.storage.azure }}
azure:
  account_name: {{ .accountName }}
  {{- with .accountKey }}
  account_key: {{ . }}
  {{- end }}
  container_name: {{ $.Values.loki.storage.bucketNames.chunks }}
  use_managed_identity: {{ .useManagedIdentity }}
  {{- with .userAssignedId }}
  user_assigned_id: {{ . }}
  {{- end }}
  {{- with .requestTimeout }}
  request_timeout: {{ . }}
  {{- end }}
{{- end -}}
{{- else -}}
{{- with .Values.loki.storage.filesystem }}
filesystem:
  chunks_directory: {{ .chunks_directory }}
  rules_directory: {{ .rules_directory }}
{{- end -}}
{{- end -}}
{{- end -}}

{{/*
Storage config for ruler
*/}}
{{- define "loki.rulerStorageConfig" -}}
{{- if .Values.minio.enabled -}}
type: "s3"
s3:
  bucketnames: {{ $.Values.loki.storage.bucketNames.ruler }}
{{- else if eq .Values.loki.storage.type "s3" -}}
{{- with .Values.loki.storage.s3 }}
type: "s3"
s3:
  {{- with .s3 }}
  s3: {{ . }}
  {{- end }}
  {{- with .endpoint }}
  endpoint: {{ . }}
  {{- end }}
  {{- with .region }}
  region: {{ . }}
  {{- end}}
  bucketnames: {{ $.Values.loki.storage.bucketNames.ruler }}
  {{- with .secretAccessKey }}
  secret_access_key: {{ . }}
  {{- end }}
  {{- with .accessKeyId }}
  access_key_id: {{ . }}
  {{- end }}
  s3forcepathstyle: {{ .s3ForcePathStyle }}
  insecure: {{ .insecure }}
{{- end -}}
{{- else if eq .Values.loki.storage.type "gcs" -}}
{{- with .Values.loki.storage.gcs }}
type: "gcs"
gcs:
  bucket_name: {{ $.Values.loki.storage.bucketNames.ruler }}
  chunk_buffer_size: {{ .chunkBufferSize }}
  request_timeout: {{ .requestTimeout }}
  enable_http2: {{ .enableHttp2 }}
{{- end -}}
{{- else if eq .Values.loki.storage.type "azure" -}}
{{- with .Values.loki.storage.azure }}
azure:
  account_name: {{ .accountName }}
  {{- with .accountKey }}
  account_key: {{ . }}
  {{- end }}
  container_name: {{ $.Values.loki.storage.bucketNames.ruler }}
  use_managed_identity: {{ .useManagedIdentity }}
  {{- with .userAssignedId }}
  user_assigned_id: {{ . }}
  {{- end }}
  {{- with .requestTimeout }}
  request_timeout: {{ . }}
  {{- end }}
{{- end -}}
{{- end -}}
{{- end -}}

{{/* Predicate function to determin if custom ruler config should be included */}}
{{- define "loki.shouldIncludeRulerConfig" }}
{{- or (not (empty .Values.loki.rulerConfig)) (.Values.minio.enabled) (eq .Values.loki.storage.type "s3") (eq .Values.loki.storage.type "gcs") }}
{{- end }}

{{/* Loki ruler config */}}
{{- define "loki.rulerConfig" }}
{{- if eq (include "loki.shouldIncludeRulerConfig" .) "true" }}
ruler:
{{- if (not (empty .Values.loki.rulerConfig)) }}
{{- toYaml .Values.loki.rulerConfig | nindent 2}}
{{- else }}
  storage:
  {{- include "loki.rulerStorageConfig" . | nindent 4}}
{{- end }}
{{- end }}
{{- end }}

{{/*
Memcached Docker image
*/}}
{{- define "loki.memcachedImage" -}}
{{- $dict := dict "service" .Values.memcached.image "global" .Values.global.image -}}
{{- include "loki.image" $dict -}}
{{- end }}

{{/*
Memcached Exporter Docker image
*/}}
{{- define "loki.memcachedExporterImage" -}}
{{- $dict := dict "service" .Values.memcachedExporter.image "global" .Values.global.image -}}
{{- include "loki.image" $dict -}}
{{- end }}

{{/*
Return the appropriate apiVersion for ingress.
*/}}
{{- define "loki.ingress.apiVersion" -}}
  {{- if and (.Capabilities.APIVersions.Has "networking.k8s.io/v1") (semverCompare ">= 1.19-0" .Capabilities.KubeVersion.Version) -}}
      {{- print "networking.k8s.io/v1" -}}
  {{- else if .Capabilities.APIVersions.Has "networking.k8s.io/v1beta1" -}}
    {{- print "networking.k8s.io/v1beta1" -}}
  {{- else -}}
    {{- print "extensions/v1beta1" -}}
  {{- end -}}
{{- end -}}

{{/*
Return if ingress is stable.
*/}}
{{- define "loki.ingress.isStable" -}}
  {{- eq (include "loki.ingress.apiVersion" .) "networking.k8s.io/v1" -}}
{{- end -}}

{{/*
Return if ingress supports ingressClassName.
*/}}
{{- define "loki.ingress.supportsIngressClassName" -}}
  {{- or (eq (include "loki.ingress.isStable" .) "true") (and (eq (include "loki.ingress.apiVersion" .) "networking.k8s.io/v1beta1") (semverCompare ">= 1.18-0" .Capabilities.KubeVersion.Version)) -}}
{{- end -}}

{{/*
Return if ingress supports pathType.
*/}}
{{- define "loki.ingress.supportsPathType" -}}
  {{- or (eq (include "loki.ingress.isStable" .) "true") (and (eq (include "loki.ingress.apiVersion" .) "networking.k8s.io/v1beta1") (semverCompare ">= 1.18-0" .Capabilities.KubeVersion.Version)) -}}
{{- end -}}

{{/*
Generate list of ingress service paths based on deployment type
*/}}
{{- define "loki.ingress.servicePaths" -}}
{{- if (eq (include "loki.deployment.isScalable" .) "true") -}}
{{- include "loki.ingress.scalableServicePaths" . }}
{{- else -}}
{{- include "loki.ingress.singleBinaryServicePaths" . }}
{{- end -}}
{{- end -}}

{{/*
Ingress service paths for scalable deployment
*/}}
{{- define "loki.ingress.scalableServicePaths" -}}
{{- include "loki.ingress.servicePath" (dict "ctx" . "svcName" "read" "paths" .Values.ingress.paths.read )}}
{{- include "loki.ingress.servicePath" (dict "ctx" . "svcName" "write" "paths" .Values.ingress.paths.write )}}
{{- end -}}

{{/*
Ingress service paths for single binary deployment
*/}}
{{- define "loki.ingress.singleBinaryServicePaths" -}}
{{- include "loki.ingress.servicePath" (dict "ctx" . "svcName" "singleBinary" "paths" .Values.ingress.paths.singleBinary )}}
{{- end -}}

{{/*
Ingress service path helper function
Params:
  ctx = . context
  svcName = service name without the "loki.fullname" part (ie. read, write)
  paths = list of url paths to allow ingress for
*/}}
{{- define "loki.ingress.servicePath" -}}
{{- $ingressApiIsStable := eq (include "loki.ingress.isStable" .ctx) "true" -}}
{{- $ingressSupportsPathType := eq (include "loki.ingress.supportsPathType" .ctx) "true" -}}
{{- range .paths }}
- path: {{ . }}
  {{- if $ingressSupportsPathType }}
  pathType: Prefix
  {{- end }}
  backend:
    {{- if $ingressApiIsStable }}
    {{- $serviceName := include "loki.ingress.serviceName" (dict "ctx" $.ctx "svcName" $.svcName) }}
    service:
      name: {{ $serviceName }}
      port:
        number: 3100
    {{- else }}
    serviceName: {{ $serviceName }}
    servicePort: 3100
{{- end -}}
{{- end -}}
{{- end -}}

{{/*
Ingress service name helper function
Params:
  ctx = . context
  svcName = service name without the "loki.fullname" part (ie. read, write)
*/}}
{{- define "loki.ingress.serviceName" -}}
{{- if (eq .svcName "singleBinary") }}
{{- printf "%s" (include "loki.fullname" .ctx) }}
{{- else }}
{{- printf "%s-%s" (include "loki.fullname" .ctx) .svcName }}
{{- end -}}
{{- end -}}

{{/*
Create the service endpoint including port for MinIO.
*/}}
{{- define "loki.minio" -}}
{{- if .Values.minio.enabled -}}
{{- printf "%s-%s.%s.svc:%s" .Release.Name "minio" .Release.Namespace (.Values.minio.service.port | toString) -}}
{{- end -}}
{{- end -}}

{{/* Return the appropriate apiVersion for PodDisruptionBudget. */}}
{{- define "loki.podDisruptionBudget.apiVersion" -}}
  {{- if and (.Capabilities.APIVersions.Has "policy/v1") (semverCompare ">= 1.21-0" .Capabilities.KubeVersion.Version) -}}
    {{- print "policy/v1" -}}
  {{- else -}}
    {{- print "policy/v1beta1" -}}
  {{- end -}}
{{- end -}}

{{/* Determine if deployment is using object storage */}}
{{- define "loki.isUsingObjectStorage" -}}
{{- or (eq .Values.loki.storage.type "gcs") (eq .Values.loki.storage.type "s3") (eq .Values.loki.storage.type "azure") -}}
{{- end -}}

{{/* Configure the correct name for the memberlist service */}}
{{- define "loki.memberlist" -}}
{{ include "loki.name" . }}-memberlist
{{- end -}}

{{/* Determine the public host for the Loki cluster */}}
{{- define "loki.host" -}}
{{- $isSingleBinary := eq (include "loki.deployment.isSingleBinary" .) "true" -}}
{{- $url := printf "%s.%s.svc.%s." (include "loki.gatewayFullname" .) .Release.Namespace .Values.global.clusterDomain }}
{{- if and $isSingleBinary (not .Values.gateway.enabled)  }}
  {{- $url = printf "%s.%s.svc.%s.:3100" (include "loki.singleBinaryFullname" .) .Release.Namespace .Values.global.clusterDomain }}
{{- end }}
{{- printf "%s" $url -}}
{{- end -}}

{{/* Determine the public endpoint for the Loki cluster */}}
{{- define "loki.address" -}}
{{- printf "http://%s" (include "loki.host" . ) -}}
{{- end -}}

{{/* Name of the cluster */}}
{{- define "loki.clusterName" -}}
{{- $name := .Values.enterprise.cluster_name | default .Release.Name }}
{{- printf "%s" $name -}}
{{- end -}}

{{/* Name of kubernetes secret to persist GEL admin token to */}}
{{- define "enterprise-logs.adminTokenSecret" }}
{{- .Values.enterprise.adminTokenSecret | default (printf "%s-admin-token" (include "loki.name" . )) -}}
{{- end -}}

{{/* Name of kubernetes secret to persist canary credentials in */}}
{{- define "enterprise-logs.canarySecret" }}
{{- .Values.enterprise.canarySecret | default (printf "%s-canary-secret" (include "loki.name" . )) -}}
{{- end -}}
