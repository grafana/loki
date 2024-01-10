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
  {{- and (eq (include "loki.isUsingObjectStorage" . ) "true") (eq (int .Values.singleBinary.replicas) 0) }}
{{- end -}}

{{/*
Return if deployment mode is single binary
*/}}
{{- define "loki.deployment.isSingleBinary" -}}
  {{- $nonZeroReplicas := gt (int .Values.singleBinary.replicas) 0 }}
  {{- or (eq (include "loki.isUsingObjectStorage" . ) "false") ($nonZeroReplicas) }}
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
  {{- with .signatureVersion }}
  signature_version: {{ . }}
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
  {{- with .backoff_config}}
  backoff_config:
    {{- with .min_period }}
    min_period: {{ . }}
    {{- end}}
    {{- with .max_period }}
    max_period: {{ . }}
    {{- end}}
    {{- with .max_retries }}
    max_retries: {{ . }}
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

{{/*
Calculate the config from structured and unstructred text input
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
  secretName: {{ tpl .Values.loki.externalConfigSecretName . }}
{{- else if eq .Values.loki.configStorageType "ConfigMap" -}}
configMap:
  name: {{ tpl .Values.loki.externalConfigSecretName . }}
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
{{- printf "%s" (include "loki.singleBinaryFullname" .ctx) }}
{{- else }}
{{- printf "%s-%s" (include "loki.name" .ctx) .svcName }}
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

{{/* Determine if deployment is using object storage */}}
{{- define "loki.isUsingObjectStorage" -}}
{{- or (eq .Values.loki.storage.type "gcs") (eq .Values.loki.storage.type "s3") (eq .Values.loki.storage.type "azure") (eq .Values.loki.storage.type "swift") -}}
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

  client_max_body_size  4M;

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
    listen             8080;
    listen             [::]:8080;

    {{- if .Values.gateway.basicAuth.enabled }}
    auth_basic           "Loki";
    auth_basic_user_file /etc/nginx/secrets/.htpasswd;
    {{- end }}

    location = / {
      return 200 'OK';
      auth_basic off;
    }

    {{- $backendHost := include "loki.backendFullname" .}}
    {{- $readHost := include "loki.readFullname" .}}
    {{- $writeHost := include "loki.writeFullname" .}}

    {{- if .Values.read.legacyReadTarget }}
    {{- $backendHost = include "loki.readFullname" . }}
    {{- end }}

    {{- if gt (int .Values.singleBinary.replicas) 0 }}
    {{- $backendHost = include "loki.singleBinaryFullname" . }}
    {{- $readHost = include "loki.singleBinaryFullname" .}}
    {{- $writeHost = include "loki.singleBinaryFullname" .}}
    {{- end }}

    {{- $writeUrl    := printf "http://%s.%s.svc.%s:3100" $writeHost   .Release.Namespace .Values.global.clusterDomain }}
    {{- $readUrl     := printf "http://%s.%s.svc.%s:3100" $readHost    .Release.Namespace .Values.global.clusterDomain }}
    {{- $backendUrl  := printf "http://%s.%s.svc.%s:3100" $backendHost .Release.Namespace .Values.global.clusterDomain }}

    {{- if .Values.gateway.nginxConfig.customWriteUrl }}
    {{- $writeUrl  = .Values.gateway.nginxConfig.customWriteUrl }}
    {{- end }}
    {{- if .Values.gateway.nginxConfig.customReadUrl }}
    {{- $readUrl = .Values.gateway.nginxConfig.customReadUrl }}
    {{- end }}
    {{- if .Values.gateway.nginxConfig.customBackendUrl }}
    {{- $backendUrl = .Values.gateway.nginxConfig.customBackendUrl }}
    {{- end }}


    # Distributor
    location = /api/prom/push {
      proxy_pass       {{ $writeUrl }}$request_uri;
    }
    location = /loki/api/v1/push {
      proxy_pass       {{ $writeUrl }}$request_uri;
    }
    location = /distributor/ring {
      proxy_pass       {{ $writeUrl }}$request_uri;
    }

    # Ingester
    location = /flush {
      proxy_pass       {{ $writeUrl }}$request_uri;
    }
    location ^~ /ingester/ {
      proxy_pass       {{ $writeUrl }}$request_uri;
    }
    location = /ingester {
      internal;        # to suppress 301
    }

    # Ring
    location = /ring {
      proxy_pass       {{ $writeUrl }}$request_uri;
    }

    # MemberListKV
    location = /memberlist {
      proxy_pass       {{ $writeUrl }}$request_uri;
    }


    # Ruler
    location = /ruler/ring {
      proxy_pass       {{ $backendUrl }}$request_uri;
    }
    location = /api/prom/rules {
      proxy_pass       {{ $backendUrl }}$request_uri;
    }
    location ^~ /api/prom/rules/ {
      proxy_pass       {{ $backendUrl }}$request_uri;
    }
    location = /loki/api/v1/rules {
      proxy_pass       {{ $backendUrl }}$request_uri;
    }
    location ^~ /loki/api/v1/rules/ {
      proxy_pass       {{ $backendUrl }}$request_uri;
    }
    location = /prometheus/api/v1/alerts {
      proxy_pass       {{ $backendUrl }}$request_uri;
    }
    location = /prometheus/api/v1/rules {
      proxy_pass       {{ $backendUrl }}$request_uri;
    }

    # Compactor
    location = /compactor/ring {
      proxy_pass       {{ $backendUrl }}$request_uri;
    }
    location = /loki/api/v1/delete {
      proxy_pass       {{ $backendUrl }}$request_uri;
    }
    location = /loki/api/v1/cache/generation_numbers {
      proxy_pass       {{ $backendUrl }}$request_uri;
    }

    # IndexGateway
    location = /indexgateway/ring {
      proxy_pass       {{ $backendUrl }}$request_uri;
    }

    # QueryScheduler
    location = /scheduler/ring {
      proxy_pass       {{ $backendUrl }}$request_uri;
    }

    # Config
    location = /config {
      proxy_pass       {{ $backendUrl }}$request_uri;
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
      proxy_pass       {{ $readUrl }}$request_uri;
      proxy_set_header Upgrade $http_upgrade;
      proxy_set_header Connection "upgrade";
    }
    location = /loki/api/v1/tail {
      proxy_pass       {{ $readUrl }}$request_uri;
      proxy_set_header Upgrade $http_upgrade;
      proxy_set_header Connection "upgrade";
    }
    location ^~ /api/prom/ {
      proxy_pass       {{ $readUrl }}$request_uri;
    }
    location = /api/prom {
      internal;        # to suppress 301
    }
    location ^~ /loki/api/v1/ {
      proxy_pass       {{ $readUrl }}$request_uri;
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
{{- if semverCompare ">=1.13-0" .Capabilities.KubeVersion.Version -}}
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
{{- $compactorAddress := include "loki.backendFullname" . -}}
{{- if and $isSimpleScalable .Values.read.legacyReadTarget -}}
{{/* 2 target configuration */}}
{{- $compactorAddress = include "loki.readFullname" . -}}
{{- else if (not $isSimpleScalable) -}}
{{/* single binary */}}
{{- $compactorAddress = include "loki.singleBinaryFullname" . -}}
{{- end -}}
{{- printf "%s" $compactorAddress }}
{{- end }}

{{/* Determine query-scheduler address */}}
{{- define "loki.querySchedulerAddress" -}}
{{- $isSimpleScalable := eq (include "loki.deployment.isScalable" .) "true" -}}
{{- $schedulerAddress := ""}}
{{- if and $isSimpleScalable (not .Values.read.legacyReadTarget ) -}}
{{- $schedulerAddress = printf "query-scheduler-discovery.%s.svc.%s.:9095" .Release.Namespace .Values.global.clusterDomain -}}
{{- end -}}
{{- printf "%s" $schedulerAddress }}
{{- end }}
