{{/*
Enforce valid label value.
See https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/#syntax-and-character-set
*/}}
{{- define "loki.validLabelValue" -}}
{{- (regexReplaceAllLiteral "[^a-zA-Z0-9._-]" . "-") | trunc 63 | trimSuffix "-" | trimSuffix "_" | trimSuffix "." }}
{{- end }}

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
Resource name template
Params:
  ctx = . context
  component = component name (optional)
  rolloutZoneName = rollout zone name (optional)
*/}}
{{- define "loki.resourceName" -}}
{{- $resourceName := include "loki.fullname" .ctx -}}
{{- if .component -}}{{- $resourceName = printf "%s-%s" $resourceName .component -}}{{- end -}}
{{- if and (not .component) .rolloutZoneName -}}{{- printf "Component name cannot be empty if rolloutZoneName (%s) is set" .rolloutZoneName | fail -}}{{- end -}}
{{- if .rolloutZoneName -}}{{- $resourceName = printf "%s-%s" $resourceName .rolloutZoneName -}}{{- end -}}
{{- if gt (len $resourceName) 253 -}}{{- printf "Resource name (%s) exceeds kubernetes limit of 253 character. To fix: shorten release name if this will be a fresh install or shorten zone names (e.g. \"a\" instead of \"zone-a\") if using zone-awareness." $resourceName | fail -}}{{- end -}}
{{- $resourceName -}}
{{- end -}}

{{/*
Return if deployment mode is simple scalable
*/}}
{{- define "loki.deployment.isScalable" -}}
  {{- and (eq (include "loki.isUsingObjectStorage" . ) "true") (or (eq .Values.deploymentMode "SingleBinary<->SimpleScalable") (eq .Values.deploymentMode "SimpleScalable") (eq .Values.deploymentMode "SimpleScalable<->Distributed")) }}
{{- end -}}

{{/*
Return if deployment mode is single binary
*/}}
{{- define "loki.deployment.isSingleBinary" -}}
  {{- or (eq .Values.deploymentMode "SingleBinary") (eq .Values.deploymentMode "SingleBinary<->SimpleScalable") }}
{{- end -}}

{{/*
Return if deployment mode is distributed
*/}}
{{- define "loki.deployment.isDistributed" -}}
  {{- and (eq (include "loki.isUsingObjectStorage" . ) "true") (or (eq .Values.deploymentMode "Distributed") (eq .Values.deploymentMode "SimpleScalable<->Distributed")) }}
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

{{/*
Cluster label for rules and alerts.
*/}}
{{- define "loki.clusterLabel" -}}
{{- if .Values.clusterLabelOverride }}
{{- .Values.clusterLabelOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
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
{{- if or (.Chart.AppVersion) (.Values.loki.image.tag) }}
app.kubernetes.io/version: {{ include "loki.validLabelValue" (.Values.loki.image.tag | default .Chart.AppVersion) | quote }}
{{- end }}
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
{{- $ref := ternary (printf ":%s" (.service.tag | default .defaultVersion | toString)) (printf "@%s" .service.digest) (empty .service.digest) -}}
{{- if and $registry $repository -}}
  {{- printf "%s/%s%s" $registry $repository $ref -}}
{{- else -}}
  {{- printf "%s%s%s" $registry $repository $ref -}}
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
{{- if .Values.loki.storage.use_thanos_objstore -}}
object_store:
  {{- include "loki.thanosStorageConfig" (dict "ctx" . "bucketName" .Values.loki.storage.bucketNames.chunks) | nindent 2 }}
{{- else }}
{{- if .Values.minio.enabled -}}
s3:
  endpoint: {{ include "loki.minio" $ }}
  bucketnames: chunks
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
  {{- with .signatureVersion }}
  signature_version: {{ . }}
  {{- end }}
  s3forcepathstyle: {{ .s3ForcePathStyle }}
  insecure: {{ .insecure }}
  {{- with .disable_dualstack }}
  disable_dualstack: {{ . }}
  {{- end }}
  {{- with .http_config}}
  http_config:
{{ toYaml . | indent 4 }}
  {{- end }}
  {{- with .backoff_config}}
  backoff_config:
{{ toYaml . | indent 4 }}
  {{- end }}
  {{- with .sse }}
  sse:
{{ toYaml . | indent 4 }}
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
  {{- with .connectionString }}
  connection_string: {{ . }}
  {{- end }}
  container_name: {{ $.Values.loki.storage.bucketNames.chunks }}
  use_managed_identity: {{ .useManagedIdentity }}
  use_federated_token: {{ .useFederatedToken }}
  {{- with .userAssignedId }}
  user_assigned_id: {{ . }}
  {{- end }}
  {{- with .requestTimeout }}
  request_timeout: {{ . }}
  {{- end }}
  {{- with .endpointSuffix }}
  endpoint_suffix: {{ . }}
  {{- end }}
  {{- with .chunkDelimiter }}
  chunk_delimiter: {{ . }}
  {{- end }}
{{- end -}}
{{- else if eq .Values.loki.storage.type "alibabacloud" -}}
{{- with .Values.loki.storage.alibabacloud }}
alibabacloud:
  bucket: {{ $.Values.loki.storage.bucketNames.chunks }}
  endpoint: {{ .endpoint }}
  access_key_id: {{ .accessKeyId }}
  secret_access_key: {{ .secretAccessKey }}
{{- end -}}
{{- else if eq .Values.loki.storage.type "swift" -}}
{{- with .Values.loki.storage.swift }}
swift:
{{ toYaml . | indent 2 }}
{{- end -}}
{{- else -}}
{{- with .Values.loki.storage.filesystem }}
filesystem:
  chunks_directory: {{ .chunks_directory }}
  rules_directory: {{ .rules_directory }}
{{- end -}}
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
  bucketnames: ruler
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
  {{- with .http_config }}
  http_config: {{ toYaml . | nindent 6 }}
  {{- end }}
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
type: "azure"
azure:
  account_name: {{ .accountName }}
  {{- with .accountKey }}
  account_key: {{ . }}
  {{- end }}
  {{- with .connectionString }}
  connection_string: {{ . }}
  {{- end }}
  container_name: {{ $.Values.loki.storage.bucketNames.ruler }}
  use_managed_identity: {{ .useManagedIdentity }}
  use_federated_token: {{ .useFederatedToken }}
  {{- with .userAssignedId }}
  user_assigned_id: {{ . }}
  {{- end }}
  {{- with .requestTimeout }}
  request_timeout: {{ . }}
  {{- end }}
  {{- with .endpointSuffix }}
  endpoint_suffix: {{ . }}
  {{- end }}
{{- end -}}
{{- else if eq .Values.loki.storage.type "swift" -}}
{{- with .Values.loki.storage.swift }}
swift:
  {{- with .auth_version }}
  auth_version: {{ . }}
  {{- end }}
  auth_url: {{ .auth_url }}
  {{- with .internal }}
  internal: {{ . }}
  {{- end }}
  username: {{ .username }}
  user_domain_name: {{ .user_domain_name }}
  {{- with .user_domain_id }}
  user_domain_id: {{ . }}
  {{- end }}
  {{- with .user_id }}
  user_id: {{ . }}
  {{- end }}
  password: {{ .password }}
  {{- with .domain_id }}
  domain_id: {{ . }}
  {{- end }}
  domain_name: {{ .domain_name }}
  project_id: {{ .project_id }}
  project_name: {{ .project_name }}
  project_domain_id: {{ .project_domain_id }}
  project_domain_name: {{ .project_domain_name }}
  region_name: {{ .region_name }}
  container_name: {{ .container_name }}
  max_retries: {{ .max_retries | default 3 }}
  connect_timeout: {{ .connect_timeout | default "10s" }}
  request_timeout: {{ .request_timeout | default "5s" }}
{{- end -}}
{{- else }}
type: "local"
{{- end -}}
{{- end -}}

{{/* Loki ruler config */}}
{{- define "loki.rulerConfig" }}
ruler:
  storage:
    {{- include "loki.rulerStorageConfig" . | nindent 4}}
{{- if (not (empty .Values.loki.rulerConfig)) }}
{{- toYaml .Values.loki.rulerConfig | nindent 2}}
{{- end }}
{{- end }}

{{/* Ruler Thanos Storage Config */}}
{{- define "loki.rulerThanosStorageConfig" -}}
{{- if and .Values.loki.storage.use_thanos_objstore .Values.ruler.enabled}}
  backend: {{ .Values.loki.storage.object_store.type }}
  {{- include "loki.thanosStorageConfig" (dict "ctx" . "bucketName" .Values.loki.storage.bucketNames.ruler) | nindent 2 }}
{{- end }}
{{- end }}

{{/* Enterprise Logs Admin API storage config */}}
{{- define "enterprise-logs.adminAPIStorageConfig" }}
storage:
  {{- if .Values.loki.storage.use_thanos_objstore }}
  backend: {{ .Values.loki.storage.object_store.type }}
    {{- include "loki.thanosStorageConfig" (dict "ctx" . "bucketName" .Values.loki.storage.bucketNames.admin) | nindent 2 }}
  {{- else if .Values.minio.enabled }}
  backend: "s3"
  s3:
    bucket_name: admin
  {{- else if eq .Values.loki.storage.type "s3" -}}
  {{- with .Values.loki.storage.s3 }}
  backend: "s3"
  s3:
    bucket_name: {{ $.Values.loki.storage.bucketNames.admin }}
  {{- end -}}
  {{- else if eq .Values.loki.storage.type "gcs" -}}
  {{- with .Values.loki.storage.gcs }}
  backend: "gcs"
  gcs:
    bucket_name: {{ $.Values.loki.storage.bucketNames.admin }}
  {{- end -}}
  {{- else if eq .Values.loki.storage.type "azure" -}}
  {{- with .Values.loki.storage.azure }}
  backend: "azure"
  azure:
    account_name: {{ .accountName }}
    {{- with .accountKey }}
    account_key: {{ . }}
    {{- end }}
    {{- with .connectionString }}
    connection_string: {{ . }}
    {{- end }}
    container_name: {{ $.Values.loki.storage.bucketNames.admin }}
    {{- with .endpointSuffix }}
    endpoint_suffix: {{ . }}
    {{- end }}
  {{- end -}}
  {{- else if eq .Values.loki.storage.type "swift" -}}
  {{- with .Values.loki.storage.swift }}
  backend: "swift"
  swift:
    {{- with .auth_version }}
    auth_version: {{ . }}
    {{- end }}
    auth_url: {{ .auth_url }}
    {{- with .internal }}
    internal: {{ . }}
    {{- end }}
    username: {{ .username }}
    user_domain_name: {{ .user_domain_name }}
    {{- with .user_domain_id }}
    user_domain_id: {{ . }}
    {{- end }}
    {{- with .user_id }}
    user_id: {{ . }}
    {{- end }}
    password: {{ .password }}
    {{- with .domain_id }}
    domain_id: {{ . }}
    {{- end }}
    domain_name: {{ .domain_name }}
    project_id: {{ .project_id }}
    project_name: {{ .project_name }}
    project_domain_id: {{ .project_domain_id }}
    project_domain_name: {{ .project_domain_name }}
    region_name: {{ .region_name }}
    container_name: {{ .container_name }}
    max_retries: {{ .max_retries | default 3 }}
    connect_timeout: {{ .connect_timeout | default "10s" }}
    request_timeout: {{ .request_timeout | default "5s" }}
  {{- end -}}
  {{- else }}
  backend: "filesystem"
  filesystem:
    dir: {{ .Values.loki.storage.filesystem.admin_api_directory }}
  {{- end -}}
{{- end }}

{{/*
Calculate the config from structured and unstructured text input
*/}}
{{- define "loki.calculatedConfig" -}}
{{ tpl (mergeOverwrite (tpl .Values.loki.config . | fromYaml) .Values.loki.structuredConfig | toYaml) . }}
{{- end }}

{{/*
The volume to mount for loki configuration
*/}}
{{- define "loki.configVolume" -}}
{{- if eq .Values.loki.configStorageType "Secret" -}}
secret:
  secretName: {{ tpl .Values.loki.configObjectName . }}
{{- else -}}
configMap:
  name: {{ tpl .Values.loki.configObjectName . }}
  items:
    - key: "config.yaml"
      path: "config.yaml"
{{- end -}}
{{- end -}}

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

{{/* Allow KubeVersion to be overridden. */}}
{{- define "loki.kubeVersion" -}}
  {{- default .Capabilities.KubeVersion.Version .Values.kubeVersionOverride -}}
{{- end -}}

{{/*
Return the appropriate apiVersion for ingress.
*/}}
{{- define "loki.ingress.apiVersion" -}}
  {{- if and (.Capabilities.APIVersions.Has "networking.k8s.io/v1") (semverCompare ">= 1.19-0" (include "loki.kubeVersion" .)) -}}
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
  {{- or (eq (include "loki.ingress.isStable" .) "true") (and (eq (include "loki.ingress.apiVersion" .) "networking.k8s.io/v1beta1") (semverCompare ">= 1.18-0" (include "loki.kubeVersion" .))) -}}
{{- end -}}

{{/*
Return if ingress supports pathType.
*/}}
{{- define "loki.ingress.supportsPathType" -}}
  {{- or (eq (include "loki.ingress.isStable" .) "true") (and (eq (include "loki.ingress.apiVersion" .) "networking.k8s.io/v1beta1") (semverCompare ">= 1.18-0" (include "loki.kubeVersion" .))) -}}
{{- end -}}

{{/*
Generate list of ingress service paths based on deployment type
*/}}
{{- define "loki.ingress.servicePaths" -}}
{{- if (eq (include "loki.deployment.isSingleBinary" .) "true") -}}
{{- include "loki.ingress.singleBinaryServicePaths" . }}
{{- else if (eq (include "loki.deployment.isDistributed" .) "true") -}}
{{- include "loki.ingress.distributedServicePaths" . }}
{{- else if and (eq (include "loki.deployment.isScalable" .) "true") (not .Values.read.legacyReadTarget ) -}}
{{- include "loki.ingress.scalableServicePaths" . }}
{{- else -}}
{{- include "loki.ingress.legacyScalableServicePaths" . }}
{{- end -}}
{{- end -}}

{{/*
Ingress service paths for distributed deployment
*/}}
{{- define "loki.ingress.distributedServicePaths" -}}
{{- $distributorServiceName := include "loki.distributorFullname" . }}
{{- include "loki.ingress.servicePath" (dict "ctx" . "serviceName" $distributorServiceName "paths" .Values.ingress.paths.distributor )}}
{{- $queryFrontendServiceName := include "loki.queryFrontendFullname" . }}
{{- include "loki.ingress.servicePath" (dict "ctx" . "serviceName" $queryFrontendServiceName "paths" .Values.ingress.paths.queryFrontend )}}
{{- $rulerServiceName := include "loki.rulerFullname" . }}
{{- include "loki.ingress.servicePath" (dict "ctx" . "serviceName" $rulerServiceName "paths" .Values.ingress.paths.ruler)}}
{{- end -}}

{{/*
Ingress service paths for legacy simple scalable deployment when backend components were part of read component.
*/}}
{{- define "loki.ingress.scalableServicePaths" -}}
{{- $readServiceName := include "loki.readFullname" . }}
{{- include "loki.ingress.servicePath" (dict "ctx" . "serviceName" $readServiceName "paths" .Values.ingress.paths.queryFrontend )}}
{{- $writeServiceName := include "loki.writeFullname" . }}
{{- include "loki.ingress.servicePath" (dict "ctx" . "serviceName" $writeServiceName "paths" .Values.ingress.paths.distributor )}}
{{- $backendServiceName := include "loki.backendFullname" . }}
{{- include "loki.ingress.servicePath" (dict "ctx" . "serviceName" $backendServiceName "paths" .Values.ingress.paths.ruler )}}
{{- end -}}

{{/*
Ingress service paths for legacy simple scalable deployment
*/}}
{{- define "loki.ingress.legacyScalableServicePaths" -}}
{{- $readServiceName := include "loki.readFullname" . }}
{{- include "loki.ingress.servicePath" (dict "ctx" . "serviceName" $readServiceName "paths" .Values.ingress.paths.queryFrontend )}}
{{- include "loki.ingress.servicePath" (dict "ctx" . "serviceName" $readServiceName "paths" .Values.ingress.paths.ruler )}}
{{- $writeServiceName := include "loki.writeFullname" . }}
{{- include "loki.ingress.servicePath" (dict "ctx" . "serviceName" $writeServiceName "paths" .Values.ingress.paths.distributor )}}
{{- end -}}

{{/*
Ingress service paths for single binary deployment
*/}}
{{- define "loki.ingress.singleBinaryServicePaths" -}}
{{- $serviceName := include "loki.singleBinaryFullname" . }}
{{- include "loki.ingress.servicePath" (dict "ctx" . "serviceName" $serviceName "paths" .Values.ingress.paths.distributor )}}
{{- include "loki.ingress.servicePath" (dict "ctx" . "serviceName" $serviceName "paths" .Values.ingress.paths.queryFrontend )}}
{{- include "loki.ingress.servicePath" (dict "ctx" . "serviceName" $serviceName "paths" .Values.ingress.paths.ruler )}}
{{- end -}}

{{/*
Ingress service path helper function
Params:
  ctx = . context
  serviceName = fully qualified k8s service name
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
    service:
      name: {{ $.serviceName }}
      port:
        number: {{ $.ctx.Values.loki.server.http_listen_port }}
    {{- else }}
    serviceName: {{ $.serviceName }}
    servicePort: {{ $.ctx.Values.loki.server.http_listen_port }}
    {{- end -}}
{{- end -}}
{{- end -}}

{{/*
Create the service endpoint including port for MinIO.
*/}}
{{- define "loki.minio" -}}
{{- if .Values.minio.enabled -}}
{{- .Values.minio.address | default (printf "%s-%s.%s.svc:%s" .Release.Name "minio" .Release.Namespace (.Values.minio.service.port | toString)) -}}
{{- end -}}
{{- end -}}

{{/* Determine if deployment is using object storage */}}
{{- define "loki.isUsingObjectStorage" -}}
{{- or (eq .Values.loki.storage.type "gcs") (eq .Values.loki.storage.type "s3") (eq .Values.loki.storage.type "azure") (eq .Values.loki.storage.type "swift") (eq .Values.loki.storage.type "alibabacloud") -}}
{{- end -}}

{{/* Configure the correct name for the memberlist service */}}
{{- define "loki.memberlist" -}}
{{ include "loki.name" . }}-memberlist
{{- end -}}

{{/* Determine the public host for the Loki cluster */}}
{{- define "loki.host" -}}
{{- $isSingleBinary := eq (include "loki.deployment.isSingleBinary" .) "true" -}}
{{- $url := printf "%s.%s.svc.%s.:%s" (include "loki.gatewayFullname" .) .Release.Namespace .Values.global.clusterDomain (.Values.gateway.service.port | toString)  }}
{{- if and $isSingleBinary (not .Values.gateway.enabled)  }}
  {{- $url = printf "%s.%s.svc.%s.:%s" (include "loki.singleBinaryFullname" .) .Release.Namespace .Values.global.clusterDomain (.Values.loki.server.http_listen_port | toString) }}
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
{{- .Values.enterprise.adminToken.secret | default (printf "%s-admin-token" (include "loki.name" . )) -}}
{{- end -}}

{{/* Prefix for provisioned secrets created for each provisioned tenant */}}
{{- define "enterprise-logs.provisionedSecretPrefix" }}
{{- .Values.enterprise.provisioner.provisionedSecretPrefix | default (printf "%s-provisioned" (include "loki.name" . )) -}}
{{- end -}}

{{/* Name of kubernetes secret to persist canary credentials in */}}
{{- define "enterprise-logs.selfMonitoringTenantSecret" }}
{{- .Values.enterprise.canarySecret | default (printf "%s-%s" (include "enterprise-logs.provisionedSecretPrefix" . ) .Values.monitoring.selfMonitoring.tenant.name) -}}
{{- end -}}

{{/* Snippet for the nginx file used by gateway */}}
{{- define "loki.nginxFile" }}
worker_processes  5;  ## Default: 1
error_log  /dev/stderr;
pid        /tmp/nginx.pid;
worker_rlimit_nofile 8192;

events {
  worker_connections  4096;  ## Default: 1024
}

http {
  client_body_temp_path /tmp/client_temp;
  proxy_temp_path       /tmp/proxy_temp_path;
  fastcgi_temp_path     /tmp/fastcgi_temp;
  uwsgi_temp_path       /tmp/uwsgi_temp;
  scgi_temp_path        /tmp/scgi_temp;

  client_max_body_size  {{ .Values.gateway.nginxConfig.clientMaxBodySize }};

  proxy_read_timeout    600; ## 10 minutes
  proxy_send_timeout    600;
  proxy_connect_timeout 600;

  proxy_http_version    1.1;

  default_type application/octet-stream;
  log_format   {{ .Values.gateway.nginxConfig.logFormat }}

  {{- if .Values.gateway.verboseLogging }}
  access_log   /dev/stderr  main;
  {{- else }}

  map $status $loggable {
    ~^[23]  0;
    default 1;
  }
  access_log   /dev/stderr  main  if=$loggable;
  {{- end }}

  sendfile     on;
  tcp_nopush   on;
  {{- if .Values.gateway.nginxConfig.resolver }}
  resolver {{ .Values.gateway.nginxConfig.resolver }};
  {{- else }}
  resolver {{ .Values.global.dnsService }}.{{ .Values.global.dnsNamespace }}.svc.{{ .Values.global.clusterDomain }}.;
  {{- end }}

  {{- with .Values.gateway.nginxConfig.httpSnippet }}
  {{- tpl . $ | nindent 2 }}
  {{- end }}

  server {
    {{- if (.Values.gateway.nginxConfig.ssl) }}
    listen             8080 ssl;
    {{- if .Values.gateway.nginxConfig.enableIPv6 }}
    listen             [::]:8080 ssl;
    {{- end }}
    {{- else }}
    listen             8080;
    {{- if .Values.gateway.nginxConfig.enableIPv6 }}
    listen             [::]:8080;
    {{- end }}
    {{- end }}

    {{- if .Values.gateway.basicAuth.enabled }}
    auth_basic           "Loki";
    auth_basic_user_file /etc/nginx/secrets/.htpasswd;
    {{- end }}

    location = / {
      return 200 'OK';
      auth_basic off;
    }

    ########################################################
    # Configure backend targets

    {{- $backendHost := include "loki.backendFullname" .}}
    {{- $readHost := include "loki.readFullname" .}}
    {{- $writeHost := include "loki.writeFullname" .}}

    {{- if .Values.read.legacyReadTarget }}
    {{- $backendHost = include "loki.readFullname" . }}
    {{- end }}

    {{- $httpSchema := .Values.gateway.nginxConfig.schema }}

    {{- $writeUrl    := printf "%s://%s.%s.svc.%s:%s" $httpSchema $writeHost   .Release.Namespace .Values.global.clusterDomain (.Values.loki.server.http_listen_port | toString) }}
    {{- $readUrl     := printf "%s://%s.%s.svc.%s:%s" $httpSchema $readHost    .Release.Namespace .Values.global.clusterDomain (.Values.loki.server.http_listen_port | toString) }}
    {{- $backendUrl  := printf "%s://%s.%s.svc.%s:%s" $httpSchema $backendHost .Release.Namespace .Values.global.clusterDomain (.Values.loki.server.http_listen_port | toString) }}

    {{- if .Values.gateway.nginxConfig.customWriteUrl }}
    {{- $writeUrl  = .Values.gateway.nginxConfig.customWriteUrl }}
    {{- end }}
    {{- if .Values.gateway.nginxConfig.customReadUrl }}
    {{- $readUrl = .Values.gateway.nginxConfig.customReadUrl }}
    {{- end }}
    {{- if .Values.gateway.nginxConfig.customBackendUrl }}
    {{- $backendUrl = .Values.gateway.nginxConfig.customBackendUrl }}
    {{- end }}

    {{- $singleBinaryHost := include "loki.singleBinaryFullname" . }}
    {{- $singleBinaryUrl  := printf "%s://%s.%s.svc.%s:%s" $httpSchema $singleBinaryHost .Release.Namespace .Values.global.clusterDomain (.Values.loki.server.http_listen_port | toString) }}

    {{- $distributorHost := include "loki.distributorFullname" .}}
    {{- $ingesterHost := include "loki.ingesterFullname" .}}
    {{- $queryFrontendHost := include "loki.queryFrontendFullname" .}}
    {{- $indexGatewayHost := include "loki.indexGatewayFullname" .}}
    {{- $rulerHost := include "loki.rulerFullname" .}}
    {{- $compactorHost := include "loki.compactorFullname" .}}
    {{- $schedulerHost := include "loki.querySchedulerFullname" .}}


    {{- $distributorUrl := printf "%s://%s.%s.svc.%s:%s" $httpSchema $distributorHost .Release.Namespace .Values.global.clusterDomain (.Values.loki.server.http_listen_port | toString) -}}
    {{- $ingesterUrl := printf "%s://%s.%s.svc.%s:%s" $httpSchema $ingesterHost .Release.Namespace .Values.global.clusterDomain (.Values.loki.server.http_listen_port | toString) }}
    {{- $queryFrontendUrl := printf "%s://%s.%s.svc.%s:%s" $httpSchema $queryFrontendHost .Release.Namespace .Values.global.clusterDomain (.Values.loki.server.http_listen_port | toString) }}
    {{- $indexGatewayUrl := printf "%s://%s.%s.svc.%s:%s" $httpSchema $indexGatewayHost .Release.Namespace .Values.global.clusterDomain (.Values.loki.server.http_listen_port | toString) }}
    {{- $rulerUrl := printf "%s://%s.%s.svc.%s:%s" $httpSchema $rulerHost .Release.Namespace .Values.global.clusterDomain (.Values.loki.server.http_listen_port | toString) }}
    {{- $compactorUrl := printf "%s://%s.%s.svc.%s:%s" $httpSchema $compactorHost .Release.Namespace .Values.global.clusterDomain (.Values.loki.server.http_listen_port | toString) }}
    {{- $schedulerUrl := printf "%s://%s.%s.svc.%s:%s" $httpSchema $schedulerHost .Release.Namespace .Values.global.clusterDomain (.Values.loki.server.http_listen_port | toString) }}

    {{- if eq (include "loki.deployment.isSingleBinary" .) "true"}}
    {{- $distributorUrl = $singleBinaryUrl }}
    {{- $ingesterUrl = $singleBinaryUrl }}
    {{- $queryFrontendUrl = $singleBinaryUrl }}
    {{- $indexGatewayUrl = $singleBinaryUrl }}
    {{- $rulerUrl = $singleBinaryUrl }}
    {{- $compactorUrl = $singleBinaryUrl }}
    {{- $schedulerUrl = $singleBinaryUrl }}
    {{- else if eq (include "loki.deployment.isScalable" .) "true"}}
    {{- $distributorUrl = $writeUrl }}
    {{- $ingesterUrl = $writeUrl }}
    {{- $queryFrontendUrl = $readUrl }}
    {{- $indexGatewayUrl = $backendUrl }}
    {{- $rulerUrl = $backendUrl }}
    {{- $compactorUrl = $backendUrl }}
    {{- $schedulerUrl = $backendUrl }}
    {{- end -}}

    {{- if .Values.loki.ui.gateway.enabled }}
    location ^~ /ui {
      proxy_pass       {{ $distributorUrl }}$request_uri;
    }
    {{- end }}

    # Distributor
    location = /api/prom/push {
      proxy_pass       {{ $distributorUrl }}$request_uri;
    }
    location = /loki/api/v1/push {
      proxy_pass       {{ $distributorUrl }}$request_uri;
    }
    location = /distributor/ring {
      proxy_pass       {{ $distributorUrl }}$request_uri;
    }
    location = /otlp/v1/logs {
      proxy_pass       {{ $distributorUrl }}$request_uri;
    }

    # Ingester
    location = /flush {
      proxy_pass       {{ $ingesterUrl }}$request_uri;
    }
    location ^~ /ingester/ {
      proxy_pass       {{ $ingesterUrl }}$request_uri;
    }
    location = /ingester {
      internal;        # to suppress 301
    }

    # Ring
    location = /ring {
      proxy_pass       {{ $ingesterUrl }}$request_uri;
    }

    # MemberListKV
    location = /memberlist {
      proxy_pass       {{ $ingesterUrl }}$request_uri;
    }

    # Ruler
    location = /ruler/ring {
      proxy_pass       {{ $rulerUrl }}$request_uri;
    }
    location = /api/prom/rules {
      proxy_pass       {{ $rulerUrl }}$request_uri;
    }
    location ^~ /api/prom/rules/ {
      proxy_pass       {{ $rulerUrl }}$request_uri;
    }
    location = /loki/api/v1/rules {
      proxy_pass       {{ $rulerUrl }}$request_uri;
    }
    location ^~ /loki/api/v1/rules/ {
      proxy_pass       {{ $rulerUrl }}$request_uri;
    }
    location = /prometheus/api/v1/alerts {
      proxy_pass       {{ $rulerUrl }}$request_uri;
    }
    location = /prometheus/api/v1/rules {
      proxy_pass       {{ $rulerUrl }}$request_uri;
    }

    # Compactor
    location = /compactor/ring {
      proxy_pass       {{ $compactorUrl }}$request_uri;
    }
    location = /loki/api/v1/delete {
      proxy_pass       {{ $compactorUrl }}$request_uri;
    }
    location = /loki/api/v1/cache/generation_numbers {
      proxy_pass       {{ $compactorUrl }}$request_uri;
    }

    # IndexGateway
    location = /indexgateway/ring {
      proxy_pass       {{ $indexGatewayUrl }}$request_uri;
    }

    # QueryScheduler
    location = /scheduler/ring {
      proxy_pass       {{ $schedulerUrl }}$request_uri;
    }

    # Config
    location = /config {
      proxy_pass       {{ $ingesterUrl }}$request_uri;
    }

    {{- if and .Values.enterprise.enabled .Values.enterprise.adminApi.enabled }}
    # Admin API
    location ^~ /admin/api/ {
      proxy_pass       {{ $backendUrl }}$request_uri;
    }
    location = /admin/api {
      internal;        # to suppress 301
    }
    {{- end }}


    # QueryFrontend, Querier
    location = /api/prom/tail {
      proxy_pass       {{ $queryFrontendUrl }}$request_uri;
      proxy_set_header Upgrade $http_upgrade;
      proxy_set_header Connection "upgrade";
    }
    location = /loki/api/v1/tail {
      proxy_pass       {{ $queryFrontendUrl }}$request_uri;
      proxy_set_header Upgrade $http_upgrade;
      proxy_set_header Connection "upgrade";
    }
    location ^~ /api/prom/ {
      proxy_pass       {{ $queryFrontendUrl }}$request_uri;
    }
    location = /api/prom {
      internal;        # to suppress 301
    }
    # if the X-Query-Tags header is empty, set a noop= without a value as empty values are not logged
    set $query_tags $http_x_query_tags;
    if ($query_tags !~* '') {
      set $query_tags "noop=";
    }
    location ^~ /loki/api/v1/ {
      # pass custom headers set by Grafana as X-Query-Tags which are logged as key/value pairs in metrics.go log messages
      proxy_set_header X-Query-Tags "${query_tags},user=${http_x_grafana_user},dashboard_id=${http_x_dashboard_uid},dashboard_title=${http_x_dashboard_title},panel_id=${http_x_panel_id},panel_title=${http_x_panel_title},source_rule_uid=${http_x_rule_uid},rule_name=${http_x_rule_name},rule_folder=${http_x_rule_folder},rule_version=${http_x_rule_version},rule_source=${http_x_rule_source},rule_type=${http_x_rule_type}";
      proxy_pass       {{ $queryFrontendUrl }}$request_uri;
    }
    location = /loki/api/v1 {
      internal;        # to suppress 301
    }

    {{- with .Values.gateway.nginxConfig.serverSnippet }}
    {{ . | nindent 4 }}
    {{- end }}
  }
}
{{- end }}

{{/* Configure enableServiceLinks in pod */}}
{{- define "loki.enableServiceLinks" -}}
{{- if semverCompare ">=1.13-0" (include "loki.kubeVersion" .) -}}
{{- if or (.Values.loki.enableServiceLinks) (ne .Values.loki.enableServiceLinks false) -}}
enableServiceLinks: true
{{- else -}}
enableServiceLinks: false
{{- end -}}
{{- end -}}
{{- end -}}

{{/* Determine compactor address based on target configuration */}}
{{- define "loki.compactorAddress" -}}
{{- $isSimpleScalable := eq (include "loki.deployment.isScalable" .) "true" -}}
{{- $isDistributed := eq (include "loki.deployment.isDistributed" .) "true" -}}
{{- $isSingleBinary := eq (include "loki.deployment.isSingleBinary" .) "true" -}}
{{- $compactorAddress := include "loki.backendFullname" . -}}
{{- if and $isSimpleScalable .Values.read.legacyReadTarget -}}
{{/* 2 target configuration */}}
{{- $compactorAddress = include "loki.readFullname" . -}}
{{- else if $isSingleBinary -}}
{{/* single binary */}}
{{- $compactorAddress = include "loki.singleBinaryFullname" . -}}
{{/* distributed */}}
{{- else if $isDistributed -}}
{{- $compactorAddress = include "loki.compactorFullname" . -}}
{{- end -}}
{{- printf "http://%s:%s" $compactorAddress (.Values.loki.server.http_listen_port | toString) }}
{{- end }}

{{/* Determine query-scheduler address */}}
{{- define "loki.querySchedulerAddress" -}}
{{- $schedulerAddress := ""}}
{{- $isDistributed := eq (include "loki.deployment.isDistributed" .) "true" -}}
{{- if $isDistributed -}}
{{- $schedulerAddress = printf "%s.%s.svc.%s:%s" (include "loki.querySchedulerFullname" .) .Release.Namespace .Values.global.clusterDomain (.Values.loki.server.grpc_listen_port | toString) -}}
{{- end -}}
{{- printf "%s" $schedulerAddress }}
{{- end }}

{{/* Determine querier address */}}
{{- define "loki.querierAddress" -}}
{{- $querierAddress := "" }}
{{- $isDistributed := eq (include "loki.deployment.isDistributed" .) "true" -}}
{{- if $isDistributed -}}
{{- $querierHost := include "loki.querierFullname" .}}
{{- $querierUrl := printf "http://%s.%s.svc.%s:3100" $querierHost .Release.Namespace .Values.global.clusterDomain }}
{{- $querierAddress = $querierUrl }}
{{- end -}}
{{- printf "%s" $querierAddress }}
{{- end }}

{{/* Determine index-gateway address */}}
{{- define "loki.indexGatewayAddress" -}}
{{- $idxGatewayAddress := ""}}
{{- $isDistributed := eq (include "loki.deployment.isDistributed" .) "true" -}}
{{- $isScalable := eq (include "loki.deployment.isScalable" .) "true" -}}
{{- if $isDistributed -}}
{{- $idxGatewayAddress = printf "dns+%s-headless.%s.svc.%s:%s" (include "loki.indexGatewayFullname" .) .Release.Namespace .Values.global.clusterDomain (.Values.loki.server.grpc_listen_port | toString) -}}
{{- end -}}
{{- if $isScalable -}}
{{- $idxGatewayAddress = printf "dns+%s-headless.%s.svc.%s:%s" (include "loki.backendFullname" .) .Release.Namespace .Values.global.clusterDomain (.Values.loki.server.grpc_listen_port | toString) -}}
{{- end -}}
{{- printf "%s" $idxGatewayAddress }}
{{- end }}

{{/* Determine bloom-planner address */}}
{{- define "loki.bloomPlannerAddress" -}}
{{- $bloomPlannerAddress := ""}}
{{- $isDistributed := eq (include "loki.deployment.isDistributed" .) "true" -}}
{{- $isScalable := eq (include "loki.deployment.isScalable" .) "true" -}}
{{- if $isDistributed -}}
{{- $bloomPlannerAddress = printf "%s-headless.%s.svc.%s:%s" (include "loki.bloomPlannerFullname" .) .Release.Namespace .Values.global.clusterDomain (.Values.loki.server.grpc_listen_port | toString) -}}
{{- end -}}
{{- if $isScalable -}}
{{- $bloomPlannerAddress = printf "%s-headless.%s.svc.%s:%s" (include "loki.backendFullname" .) .Release.Namespace .Values.global.clusterDomain (.Values.loki.server.grpc_listen_port | toString) -}}
{{- end -}}
{{- printf "%s" $bloomPlannerAddress}}
{{- end }}

{{/* Determine bloom-gateway address */}}
{{- define "loki.bloomGatewayAddresses" -}}
{{- $bloomGatewayAddresses := ""}}
{{- $isDistributed := eq (include "loki.deployment.isDistributed" .) "true" -}}
{{- $isScalable := eq (include "loki.deployment.isScalable" .) "true" -}}
{{- if $isDistributed -}}
{{- $bloomGatewayAddresses = printf "dnssrvnoa+_grpc._tcp.%s-headless.%s.svc.%s" (include "loki.bloomGatewayFullname" .) .Release.Namespace .Values.global.clusterDomain -}}
{{- end -}}
{{- if $isScalable -}}
{{- $bloomGatewayAddresses = printf "dnssrvnoa+_grpc._tcp.%s-headless.%s.svc.%s" (include "loki.backendFullname" .) .Release.Namespace .Values.global.clusterDomain -}}
{{- end -}}
{{- printf "%s" $bloomGatewayAddresses}}
{{- end }}

{{- define "loki.config.checksum" -}}
checksum/config: {{ include "loki.configMapOrSecretContentHash" (dict "ctx" . "name" "/config.yaml") }}
{{- end -}}

{{/*
Return the appropriate apiVersion for PodDisruptionBudget.
*/}}
{{- define "loki.pdb.apiVersion" -}}
  {{- if and (.Capabilities.APIVersions.Has "policy/v1") (semverCompare ">=1.21-0" (include "loki.kubeVersion" .)) -}}
    {{- print "policy/v1" -}}
  {{- else -}}
    {{- print "policy/v1beta1" -}}
  {{- end -}}
{{- end -}}

{{/*
Return the object store type for use with the test schema.
*/}}
{{- define "loki.testSchemaObjectStore" -}}
  {{- if .Values.minio.enabled -}}
    s3
  {{- else -}}
    filesystem
  {{- end -}}
{{- end -}}

{{/*
Return the appropriate apiVersion for HorizontalPodAutoscaler.
*/}}
{{- define "loki.hpa.apiVersion" -}}
  {{- if and (.Capabilities.APIVersions.Has "autoscaling/v2") (semverCompare ">= 1.19-0" (include "loki.kubeVersion" .)) -}}
      {{- print "autoscaling/v2" -}}
  {{- else if .Capabilities.APIVersions.Has "autoscaling/v2beta2" -}}
    {{- print "autoscaling/v2beta2" -}}
  {{- else -}}
    {{- print "autoscaling/v2beta1" -}}
  {{- end -}}
{{- end -}}

{{/*
compute a ConfigMap or Secret checksum only based on its .data content.
This function needs to be called with a context object containing the following keys:
- ctx: the current Helm context (what '.' is at the call site)
- name: the file name of the ConfigMap or Secret
*/}}
{{- define "loki.configMapOrSecretContentHash" -}}
{{ get (include (print .ctx.Template.BasePath .name) .ctx | fromYaml) "data" | toYaml | sha256sum }}
{{- end }}

{{/* Thanos object storage configuration helper to build
the thanos_storage_config model*/}}
{{- define "loki.thanosStorageConfig" -}}
{{- $bucketName := .bucketName }}
{{- with .ctx.Values.loki.storage.object_store }}
{{- if eq .type "s3" }}
s3:
  {{- with .s3 }}
  bucket_name: {{ $bucketName }}
  endpoint: {{ .endpoint }}
  access_key_id: {{ .access_key_id }}
  secret_access_key: {{ .secret_access_key }}
  region: {{ .region }}
  insecure: {{ .insecure }}
  http:
    {{ toYaml .http | nindent 4 }}
  sse:
    {{ toYaml .sse | nindent 4 }}
  {{- end }}
{{- else if eq .type "gcs" }}
gcs:
  {{- with .gcs }}
  bucket_name: {{ $bucketName }}
  service_account: {{ .service_account }}
  {{- end }}
{{- else if eq .type "azure" }}
azure:
  {{- with .azure }}
  container_name: {{ $bucketName }}
  account_name: {{ .account_name }}
  account_key: {{ .account_key }}
  {{- end }}
{{- end }}
storage_prefix: {{ .storage_prefix }}
{{- end }}
{{- end }}
