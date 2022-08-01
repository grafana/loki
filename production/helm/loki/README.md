# loki-simple-scalable

![Version: 1.8.4](https://img.shields.io/badge/Version-1.8.4-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: 2.6.1](https://img.shields.io/badge/AppVersion-2.6.1-informational?style=flat-square)

Helm chart for Grafana Loki in simple, scalable mode

## Source Code

* <https://github.com/grafana/loki>
* <https://grafana.com/oss/loki/>
* <https://grafana.com/docs/loki/latest/>

## Requirements

| Repository | Name | Version |
|------------|------|---------|
| https://grafana.github.io/helm-charts | grafana-agent-operator(grafana-agent-operator) | 0.2.2 |
| https://helm.min.io/ | minio(minio) | 8.0.9 |

## Chart Repo

Add the following repo to use the chart:

```sh
helm repo add grafana https://grafana.github.io/helm-charts
```

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| enterprise.adminApi | object | `{"enabled":true}` | If enabled, the correct admin_client storage will be configured. If disabled while running enterprise, make sure auth is set to `type: trust`, or that `auth_enabled` is set to `false`. |
| enterprise.config | string | `"{{- if .Values.enterprise.adminApi.enabled }}\n{{- if or .Values.minio.enabled (eq .Values.loki.storage.type \"s3\") (eq .Values.loki.storage.type \"gcs\") }}\nadmin_client:\n  storage:\n    s3:\n      bucket_name: {{ .Values.loki.storage.bucketNames.admin }}\n{{- end }}\n{{- end }}\nauth:\n  type: {{ .Values.enterprise.adminApi.enabled | ternary \"enterprise\" \"trust\" }}\nauth_enabled: {{ .Values.loki.auth_enabled }}\ncluster_name: {{ .Release.Name }}\nlicense:\n  path: /etc/loki/license/license.jwt\n"` |  |
| enterprise.enabled | bool | `false` |  |
| enterprise.externalLicenseName | string | `nil` | Name of external licesne secret to use |
| enterprise.image.pullPolicy | string | `"IfNotPresent"` | Docker image pull policy |
| enterprise.image.registry | string | `"docker.io"` | The Docker registry |
| enterprise.image.repository | string | `"grafana/enterprise-logs"` | Docker image repository |
| enterprise.image.tag | string | `"v1.4.0"` | Overrides the image tag whose default is the chart's appVersion |
| enterprise.license | object | `{"contents":"NOTAVALIDLICENSE"}` | Grafana Enterprise Logs license In order to use Grafana Enterprise Logs features, you will need to provide the contents of your Grafana Enterprise Logs license, either by providing the contents of the license.jwt, or the name Kubernetes Secret that contains your license.jwt. To set the license contents, use the flag `--set-file 'license.contents=./license.jwt'` |
| enterprise.nginxConfig.file | string | `"worker_processes  5;  ## Default: 1\nerror_log  /dev/stderr;\npid        /tmp/nginx.pid;\nworker_rlimit_nofile 8192;\n\nevents {\n  worker_connections  4096;  ## Default: 1024\n}\n\nhttp {\n  client_body_temp_path /tmp/client_temp;\n  proxy_temp_path       /tmp/proxy_temp_path;\n  fastcgi_temp_path     /tmp/fastcgi_temp;\n  uwsgi_temp_path       /tmp/uwsgi_temp;\n  scgi_temp_path        /tmp/scgi_temp;\n\n  proxy_http_version    1.1;\n\n  default_type application/octet-stream;\n  log_format   {{ .Values.gateway.nginxConfig.logFormat }}\n\n  {{- if .Values.gateway.verboseLogging }}\n  access_log   /dev/stderr  main;\n  {{- else }}\n\n  map $status $loggable {\n    ~^[23]  0;\n    default 1;\n  }\n  access_log   /dev/stderr  main  if=$loggable;\n  {{- end }}\n\n  sendfile     on;\n  tcp_nopush   on;\n  resolver {{ .Values.global.dnsService }}.{{ .Values.global.dnsNamespace }}.svc.{{ .Values.global.clusterDomain }};\n\n  {{- with .Values.gateway.nginxConfig.httpSnippet }}\n  {{ . | nindent 2 }}\n  {{- end }}\n\n  server {\n    listen             8080;\n\n    {{- if .Values.gateway.basicAuth.enabled }}\n    auth_basic           \"Loki\";\n    auth_basic_user_file /etc/nginx/secrets/.htpasswd;\n    {{- end }}\n\n    location = / {\n      return 200 'OK';\n      auth_basic off;\n    }\n\n    location = /api/prom/push {\n      proxy_pass       http://{{ include \"loki.writeFullname\" . }}.{{ .Release.Namespace }}.svc.{{ .Values.global.clusterDomain }}:3100$request_uri;\n    }\n\n    location = /api/prom/tail {\n      proxy_pass       http://{{ include \"loki.readFullname\" . }}.{{ .Release.Namespace }}.svc.{{ .Values.global.clusterDomain }}:3100$request_uri;\n      proxy_set_header Upgrade $http_upgrade;\n      proxy_set_header Connection \"upgrade\";\n    }\n\n    location ~ /api/prom/.* {\n      proxy_pass       http://{{ include \"loki.readFullname\" . }}.{{ .Release.Namespace }}.svc.{{ .Values.global.clusterDomain }}:3100$request_uri;\n    }\n\n    location ~ /prometheus/api/v1/alerts.* {\n      proxy_pass       http://{{ include \"loki.readFullname\" . }}.{{ .Release.Namespace }}.svc.{{ .Values.global.clusterDomain }}:3100$request_uri;\n    }\n\n    location ~ /prometheus/api/v1/rules.* {\n      proxy_pass       http://{{ include \"loki.readFullname\" . }}.{{ .Release.Namespace }}.svc.{{ .Values.global.clusterDomain }}:3100$request_uri;\n    }\n\n    location = /loki/api/v1/push {\n      proxy_pass       http://{{ include \"loki.writeFullname\" . }}.{{ .Release.Namespace }}.svc.{{ .Values.global.clusterDomain }}:3100$request_uri;\n    }\n\n    location = /loki/api/v1/tail {\n      proxy_pass       http://{{ include \"loki.readFullname\" . }}.{{ .Release.Namespace }}.svc.{{ .Values.global.clusterDomain }}:3100$request_uri;\n      proxy_set_header Upgrade $http_upgrade;\n      proxy_set_header Connection \"upgrade\";\n    }\n\n    location ~ /loki/api/.* {\n      proxy_pass       http://{{ include \"loki.readFullname\" . }}.{{ .Release.Namespace }}.svc.{{ .Values.global.clusterDomain }}:3100$request_uri;\n    }\n\n    location ~ /admin/api/.* {\n      proxy_pass       http://{{ include \"loki.writeFullname\" . }}.{{ .Release.Namespace }}.svc.{{ .Values.global.clusterDomain }}:3100$request_uri;\n    }\n\n    location ~ /compactor/.* {\n      proxy_pass       http://{{ include \"loki.readFullname\" . }}.{{ .Release.Namespace }}.svc.{{ .Values.global.clusterDomain }}:3100$request_uri;\n    }\n\n    location ~ /distributor/.* {\n      proxy_pass       http://{{ include \"loki.writeFullname\" . }}.{{ .Release.Namespace }}.svc.{{ .Values.global.clusterDomain }}:3100$request_uri;\n    }\n\n    location ~ /ring {\n      proxy_pass       http://{{ include \"loki.writeFullname\" . }}.{{ .Release.Namespace }}.svc.{{ .Values.global.clusterDomain }}:3100$request_uri;\n    }\n\n    location ~ /ingester/.* {\n      proxy_pass       http://{{ include \"loki.writeFullname\" . }}.{{ .Release.Namespace }}.svc.{{ .Values.global.clusterDomain }}:3100$request_uri;\n    }\n\n    location ~ /ruler/.* {\n      proxy_pass       http://{{ include \"loki.readFullname\" . }}.{{ .Release.Namespace }}.svc.{{ .Values.global.clusterDomain }}:3100$request_uri;\n    }\n\n    location ~ /scheduler/.* {\n      proxy_pass       http://{{ include \"loki.readFullname\" . }}.{{ .Release.Namespace }}.svc.{{ .Values.global.clusterDomain }}:3100$request_uri;\n    }\n\n    {{- with .Values.gateway.nginxConfig.serverSnippet }}\n    {{ . | nindent 4 }}\n    {{- end }}\n  }\n}\n"` |  |
| enterprise.tokengen | object | `{"adminTokenSecret":"gel-admin-token","annotations":{},"enabled":true,"env":[],"extraArgs":[],"extraVolumeMounts":[],"extraVolumes":[],"labels":{},"securityContext":{"fsGroup":10001,"runAsGroup":10001,"runAsNonRoot":true,"runAsUser":10001}}` | Configuration for `tokengen` target |
| enterprise.tokengen.adminTokenSecret | string | `"gel-admin-token"` | Name of the secret to store the admin token in |
| enterprise.tokengen.annotations | object | `{}` | Additional annotations for the `tokengen` Job |
| enterprise.tokengen.enabled | bool | `true` | Whether the job should be part of the deployment |
| enterprise.tokengen.env | list | `[]` | Additional Kubernetes environment |
| enterprise.tokengen.extraArgs | list | `[]` | Additional CLI arguments for the `tokengen` target |
| enterprise.tokengen.extraVolumeMounts | list | `[]` | Additional volume mounts for Pods |
| enterprise.tokengen.extraVolumes | list | `[]` | Additional volumes for Pods |
| enterprise.tokengen.labels | object | `{}` | Additional labels for the `tokengen` Job |
| enterprise.tokengen.securityContext | object | `{"fsGroup":10001,"runAsGroup":10001,"runAsNonRoot":true,"runAsUser":10001}` | Run containers as user `enterprise-logs(uid=10001)` |
| enterprise.useExternalLicense | bool | `false` | Set to true when providing an external license |
| enterprise.version | string | `"v1.5.0"` |  |
| fullnameOverride | string | `nil` | Overrides the chart's computed fullname |
| gateway.affinity | string | Hard node and soft zone anti-affinity | Affinity for gateway pods. Passed through `tpl` and, thus, to be configured as string |
| gateway.autoscaling.enabled | bool | `false` | Enable autoscaling for the gateway |
| gateway.autoscaling.maxReplicas | int | `3` | Maximum autoscaling replicas for the gateway |
| gateway.autoscaling.minReplicas | int | `1` | Minimum autoscaling replicas for the gateway |
| gateway.autoscaling.targetCPUUtilizationPercentage | int | `60` | Target CPU utilisation percentage for the gateway |
| gateway.autoscaling.targetMemoryUtilizationPercentage | string | `nil` | Target memory utilisation percentage for the gateway |
| gateway.basicAuth.enabled | bool | `false` | Enables basic authentication for the gateway |
| gateway.basicAuth.existingSecret | string | `nil` | Existing basic auth secret to use. Must contain '.htpasswd' |
| gateway.basicAuth.htpasswd | string | `"{{ htpasswd (required \"'gateway.basicAuth.username' is required\" .Values.gateway.basicAuth.username) (required \"'gateway.basicAuth.password' is required\" .Values.gateway.basicAuth.password) }}"` | Uses the specified username and password to compute a htpasswd using Sprig's `htpasswd` function. The value is templated using `tpl`. Override this to use a custom htpasswd, e.g. in case the default causes high CPU load. |
| gateway.basicAuth.password | string | `nil` | The basic auth password for the gateway |
| gateway.basicAuth.username | string | `nil` | The basic auth username for the gateway |
| gateway.containerSecurityContext | object | `{"allowPrivilegeEscalation":false,"capabilities":{"drop":["ALL"]},"readOnlyRootFilesystem":true}` | The SecurityContext for gateway containers |
| gateway.deploymentStrategy | object | `{"type":"RollingUpdate"}` | ref: https://kubernetes.io/docs/concepts/workloads/controllers/deployment/#strategy |
| gateway.enabled | bool | `true` | Specifies whether the gateway should be enabled |
| gateway.extraArgs | list | `[]` | Additional CLI args for the gateway |
| gateway.extraEnv | list | `[]` | Environment variables to add to the gateway pods |
| gateway.extraEnvFrom | list | `[]` | Environment variables from secrets or configmaps to add to the gateway pods |
| gateway.extraVolumeMounts | list | `[]` | Volume mounts to add to the gateway pods |
| gateway.extraVolumes | list | `[]` | Volumes to add to the gateway pods |
| gateway.image.pullPolicy | string | `"IfNotPresent"` | The gateway image pull policy |
| gateway.image.registry | string | `"docker.io"` | The Docker registry for the gateway image |
| gateway.image.repository | string | `"nginxinc/nginx-unprivileged"` | The gateway image repository |
| gateway.image.tag | string | `"1.19-alpine"` | The gateway image tag |
| gateway.ingress.annotations | object | `{}` | Annotations for the gateway ingress |
| gateway.ingress.enabled | bool | `false` | Specifies whether an ingress for the gateway should be created |
| gateway.ingress.hosts | list | `[{"host":"gateway.loki.example.com","paths":[{"path":"/"}]}]` | Hosts configuration for the gateway ingress |
| gateway.ingress.tls | list | `[{"hosts":["gateway.loki.example.com"],"secretName":"loki-gateway-tls"}]` | TLS configuration for the gateway ingress |
| gateway.nginxConfig.file | string | See values.yaml | Config file contents for Nginx. Passed through the `tpl` function to allow templating |
| gateway.nginxConfig.httpSnippet | string | `""` | Allows appending custom configuration to the http block |
| gateway.nginxConfig.logFormat | string | `"main '$remote_addr - $remote_user [$time_local]  $status '\n        '\"$request\" $body_bytes_sent \"$http_referer\" '\n        '\"$http_user_agent\" \"$http_x_forwarded_for\"';"` | NGINX log format |
| gateway.nginxConfig.serverSnippet | string | `""` | Allows appending custom configuration to the server block |
| gateway.nodeSelector | object | `{}` | Node selector for gateway pods |
| gateway.podAnnotations | object | `{}` | Annotations for gateway pods |
| gateway.podSecurityContext | object | `{"fsGroup":101,"runAsGroup":101,"runAsNonRoot":true,"runAsUser":101}` | The SecurityContext for gateway containers |
| gateway.priorityClassName | string | `nil` | The name of the PriorityClass for gateway pods |
| gateway.readinessProbe.httpGet.path | string | `"/"` |  |
| gateway.readinessProbe.httpGet.port | string | `"http"` |  |
| gateway.readinessProbe.initialDelaySeconds | int | `15` |  |
| gateway.readinessProbe.timeoutSeconds | int | `1` |  |
| gateway.replicas | int | `1` | Number of replicas for the gateway |
| gateway.resources | object | `{}` | Resource requests and limits for the gateway |
| gateway.service.annotations | object | `{}` | Annotations for the gateway service |
| gateway.service.clusterIP | string | `nil` | ClusterIP of the gateway service |
| gateway.service.labels | object | `{}` | Labels for gateway service |
| gateway.service.loadBalancerIP | string | `nil` | Load balancer IPO address if service type is LoadBalancer |
| gateway.service.nodePort | int | `nil` | Node port if service type is NodePort |
| gateway.service.port | int | `80` | Port of the gateway service |
| gateway.service.type | string | `"ClusterIP"` | Type of the gateway service |
| gateway.terminationGracePeriodSeconds | int | `30` | Grace period to allow the gateway to shutdown before it is killed |
| gateway.tolerations | list | `[]` | Tolerations for gateway pods |
| gateway.verboseLogging | bool | `true` | Enable logging of 2xx and 3xx HTTP requests |
| global.clusterDomain | string | `"cluster.local"` | configures cluster domain ("cluster.local" by default) |
| global.dnsNamespace | string | `"kube-system"` | configures DNS service namespace |
| global.dnsService | string | `"kube-dns"` | configures DNS service name |
| global.image.registry | string | `nil` | Overrides the Docker registry globally for all images |
| global.priorityClassName | string | `nil` | Overrides the priorityClassName for all pods |
| imagePullSecrets | list | `[]` | Image pull secrets for Docker images |
| loki.auth_enabled | bool | `true` |  |
| loki.commonConfig | object | `{"path_prefix":"/var/loki","replication_factor":3}` | Check https://grafana.com/docs/loki/latest/configuration/#common_config for more info on how to provide a common configuration |
| loki.config | string | See values.yaml | Config file contents for Loki |
| loki.containerSecurityContext | object | `{"allowPrivilegeEscalation":false,"capabilities":{"drop":["ALL"]},"readOnlyRootFilesystem":true}` | The SecurityContext for Loki containers |
| loki.existingSecretForConfig | string | `""` | Specify an existing secret containing loki configuration. If non-empty, overrides `loki.config` |
| loki.image.pullPolicy | string | `"IfNotPresent"` | Docker image pull policy |
| loki.image.registry | string | `"docker.io"` | The Docker registry |
| loki.image.repository | string | `"grafana/loki"` | Docker image repository |
| loki.image.tag | string | `nil` | Overrides the image tag whose default is the chart's appVersion |
| loki.memcached | object | `{"chunk_cache":{"batch_size":256,"enabled":false,"host":"","parallelism":10,"service":"memcached-client"},"results_cache":{"default_validity":"12h","enabled":false,"host":"","service":"memcached-client","timeout":"500ms"}}` | Configure memcached as an external cache for chunk and results cache. Disabled by default must enable and specify a host for each cache you would like to use. |
| loki.podAnnotations | object | `{}` | Common annotations for all pods |
| loki.podSecurityContext | object | `{"fsGroup":10001,"runAsGroup":10001,"runAsNonRoot":true,"runAsUser":10001}` | The SecurityContext for Loki pods |
| loki.query_scheduler | object | `{}` | Additional query scheduler config |
| loki.readinessProbe.httpGet.path | string | `"/ready"` |  |
| loki.readinessProbe.httpGet.port | string | `"http-metrics"` |  |
| loki.readinessProbe.initialDelaySeconds | int | `30` |  |
| loki.readinessProbe.timeoutSeconds | int | `1` |  |
| loki.revisionHistoryLimit | int | `10` | The number of old ReplicaSets to retain to allow rollback |
| loki.schemaConfig | object | `{}` | Check https://grafana.com/docs/loki/latest/configuration/#schema_config for more info on how to configure schemas |
| loki.storage.bucketNames.admin | string | `"admin"` |  |
| loki.storage.bucketNames.chunks | string | `"chunks"` |  |
| loki.storage.bucketNames.ruler | string | `"ruler"` |  |
| loki.storage.gcs.chunkBufferSize | int | `0` |  |
| loki.storage.gcs.enableHttp2 | bool | `true` |  |
| loki.storage.gcs.requestTimeout | string | `"0s"` |  |
| loki.storage.local.chunks_directory | string | `"/var/loki/chunks"` |  |
| loki.storage.local.rules_directory | string | `"/var/loki/rules"` |  |
| loki.storage.s3.accessKeyId | string | `nil` |  |
| loki.storage.s3.endpoint | string | `nil` |  |
| loki.storage.s3.insecure | bool | `false` |  |
| loki.storage.s3.region | string | `nil` |  |
| loki.storage.s3.s3 | string | `nil` |  |
| loki.storage.s3.s3ForcePathStyle | bool | `false` |  |
| loki.storage.s3.secretAccessKey | string | `nil` |  |
| loki.storage.type | string | `"s3"` |  |
| loki.storage_config | object | `{"hedging":{"at":"250ms","max_per_second":20,"up_to":3}}` | Additional storage config |
| loki.structuredConfig | object | `{}` | Structured loki configuration, takes precedence over `loki.config`, `loki.schemaConfig`, `loki.storageConfig` |
| minio | object | `{"accessKey":"enterprise-logs","buckets":[{"name":"chunks","policy":"none","purge":false},{"name":"ruler","policy":"none","purge":false},{"name":"admin","policy":"none","purge":false}],"enabled":false,"persistence":{"size":"5Gi"},"resources":{"requests":{"cpu":"100m","memory":"128Mi"}},"secretKey":"supersecret"}` | ----------------------------------- |
| monitoring.alerts.annotations | object | `{}` | Additional annotations for the alerts PrometheusRule resource |
| monitoring.alerts.enabled | bool | `true` | If enabled, create PrometheusRule resource with Loki alerting rules |
| monitoring.alerts.labels | object | `{}` | Additional labels for the alerts PrometheusRule resource |
| monitoring.alerts.namespace | string | `nil` | Alternative namespace to create alerting rules PrometheusRule resource in |
| monitoring.dashboards.annotations | object | `{}` | Additional annotations for the dashboards ConfigMap |
| monitoring.dashboards.enabled | bool | `true` | If enabled, create configmap with dashboards for monitoring Loki |
| monitoring.dashboards.labels | object | `{}` | Additional labels for the dashboards ConfigMap |
| monitoring.dashboards.namespace | string | `nil` | Alternative namespace to create dashboards ConfigMap in |
| monitoring.rules.additionalGroups | list | `[]` | Additional groups to add to the rules file |
| monitoring.rules.alerting | bool | `true` | Include alerting rules |
| monitoring.rules.annotations | object | `{}` | Additional annotations for the rules PrometheusRule resource |
| monitoring.rules.enabled | bool | `true` | If enabled, create PrometheusRule resource with Loki recording rules |
| monitoring.rules.labels | object | `{}` | Additional labels for the rules PrometheusRule resource |
| monitoring.rules.namespace | string | `nil` | Alternative namespace to create recording rules PrometheusRule resource in |
| monitoring.selfMonitoring.enabled | bool | `true` |  |
| monitoring.selfMonitoring.grafanaAgent.annotations | object | `{}` | Grafana Agent annotations |
| monitoring.selfMonitoring.grafanaAgent.enableConfigReadAPI | bool | `false` | Enable the config read api on port 8080 of the agent |
| monitoring.selfMonitoring.grafanaAgent.installOperator | bool | `true` | Controls whether to install the Grafana Agent Operator and its CRDs. Note that helm will not install CRDs if this flag is enabled during an upgrade. In that case install the CRDs manually from https://github.com/grafana/agent/tree/main/production/operator/crds |
| monitoring.selfMonitoring.grafanaAgent.labels | object | `{}` | Additional Grafana Agent labels |
| monitoring.selfMonitoring.grafanaAgent.namespace | string | `nil` | Alternative namespace for Grafana Agent resources |
| monitoring.selfMonitoring.logsInstance.annotations | object | `{}` | LogsInstance annotations |
| monitoring.selfMonitoring.logsInstance.labels | object | `{}` | Additional LogsInstance labels |
| monitoring.selfMonitoring.logsInstance.namespace | string | `nil` | Alternative namespace for LogsInstance resources |
| monitoring.selfMonitoring.podLogs.annotations | object | `{}` | PodLogs annotations |
| monitoring.selfMonitoring.podLogs.labels | object | `{}` | Additional PodLogs labels |
| monitoring.selfMonitoring.podLogs.namespace | string | `nil` | Alternative namespace for PodLogs resources |
| monitoring.selfMonitoring.podLogs.relabelings | list | `[]` | PodLogs relabel configs to apply to samples before scraping https://github.com/prometheus-operator/prometheus-operator/blob/master/Documentation/api.md#relabelconfig |
| monitoring.serviceMonitor.annotations | object | `{}` | ServiceMonitor annotations |
| monitoring.serviceMonitor.enabled | bool | `true` | If enabled, ServiceMonitor resources for Prometheus Operator are created |
| monitoring.serviceMonitor.interval | string | `nil` | ServiceMonitor scrape interval |
| monitoring.serviceMonitor.labels | object | `{}` | Additional ServiceMonitor labels |
| monitoring.serviceMonitor.namespace | string | `nil` | Alternative namespace for ServiceMonitor resources |
| monitoring.serviceMonitor.namespaceSelector | object | `{}` | Namespace selector for ServiceMonitor resources |
| monitoring.serviceMonitor.relabelings | list | `[]` | ServiceMonitor relabel configs to apply to samples before scraping https://github.com/prometheus-operator/prometheus-operator/blob/master/Documentation/api.md#relabelconfig |
| monitoring.serviceMonitor.scheme | string | `"http"` | ServiceMonitor will use http by default, but you can pick https as well |
| monitoring.serviceMonitor.scrapeTimeout | string | `nil` | ServiceMonitor scrape timeout in Go duration format (e.g. 15s) |
| monitoring.serviceMonitor.tlsConfig | string | `nil` | ServiceMonitor will use these tlsConfig settings to make the health check requests |
| nameOverride | string | `nil` | Overrides the chart's name |
| networkPolicy.alertmanager.namespaceSelector | object | `{}` | Specifies the namespace the alertmanager is running in |
| networkPolicy.alertmanager.podSelector | object | `{}` | Specifies the alertmanager Pods. As this is cross-namespace communication, you also need the namespaceSelector. |
| networkPolicy.alertmanager.port | int | `9093` | Specify the alertmanager port used for alerting |
| networkPolicy.discovery.namespaceSelector | object | `{}` | Specifies the namespace the discovery Pods are running in |
| networkPolicy.discovery.podSelector | object | `{}` | Specifies the Pods labels used for discovery. As this is cross-namespace communication, you also need the namespaceSelector. |
| networkPolicy.discovery.port | int | `nil` | Specify the port used for discovery |
| networkPolicy.enabled | bool | `false` | Specifies whether Network Policies should be created |
| networkPolicy.externalStorage.cidrs | list | `[]` | Specifies specific network CIDRs you want to limit access to |
| networkPolicy.externalStorage.ports | list | `[]` | Specify the port used for external storage, e.g. AWS S3 |
| networkPolicy.ingress.namespaceSelector | object | `{}` | Specifies the namespaces which are allowed to access the http port |
| networkPolicy.ingress.podSelector | object | `{}` | Specifies the Pods which are allowed to access the http port. As this is cross-namespace communication, you also need the namespaceSelector. |
| networkPolicy.metrics.cidrs | list | `[]` | Specifies specific network CIDRs which are allowed to access the metrics port. In case you use namespaceSelector, you also have to specify your kubelet networks here. The metrics ports are also used for probes. |
| networkPolicy.metrics.namespaceSelector | object | `{}` | Specifies the namespaces which are allowed to access the metrics port |
| networkPolicy.metrics.podSelector | object | `{}` | Specifies the Pods which are allowed to access the metrics port. As this is cross-namespace communication, you also need the namespaceSelector. |
| rbac.pspEnabled | bool | `false` | If pspEnabled true, a PodSecurityPolicy is created for K8s that use psp. |
| rbac.sccEnabled | bool | `false` | For OpenShift set pspEnabled to 'false' and sccEnabled to 'true' to use the SecurityContextConstraints. |
| read.affinity | string | Hard node and soft zone anti-affinity | Affinity for read pods. Passed through `tpl` and, thus, to be configured as string |
| read.autoscaling.enabled | bool | `false` | Enable autoscaling for the read, this is only used if `queryIndex.enabled: true` |
| read.autoscaling.maxReplicas | int | `3` | Maximum autoscaling replicas for the read |
| read.autoscaling.minReplicas | int | `1` | Minimum autoscaling replicas for the read |
| read.autoscaling.targetCPUUtilizationPercentage | int | `60` | Target CPU utilisation percentage for the read |
| read.autoscaling.targetMemoryUtilizationPercentage | string | `nil` | Target memory utilisation percentage for the read |
| read.extraArgs | list | `[]` | Additional CLI args for the read |
| read.extraEnv | list | `[]` | Environment variables to add to the read pods |
| read.extraEnvFrom | list | `[]` | Environment variables from secrets or configmaps to add to the read pods |
| read.extraVolumeMounts | list | `[]` | Volume mounts to add to the read pods |
| read.extraVolumes | list | `[]` | Volumes to add to the read pods |
| read.image.registry | string | `nil` | The Docker registry for the read image. Overrides `loki.image.registry` |
| read.image.repository | string | `nil` | Docker image repository for the read image. Overrides `loki.image.repository` |
| read.image.tag | string | `nil` | Docker image tag for the read image. Overrides `loki.image.tag` |
| read.nodeSelector | object | `{}` | Node selector for read pods |
| read.persistence.size | string | `"10Gi"` | Size of persistent disk |
| read.persistence.storageClass | string | `nil` | Storage class to be used. If defined, storageClassName: <storageClass>. If set to "-", storageClassName: "", which disables dynamic provisioning. If empty or set to null, no storageClassName spec is set, choosing the default provisioner (gp2 on AWS, standard on GKE, AWS, and OpenStack). |
| read.podAnnotations | object | `{}` | Annotations for read pods |
| read.priorityClassName | string | `nil` | The name of the PriorityClass for read pods |
| read.replicas | int | `3` | Number of replicas for the read |
| read.resources | object | `{}` | Resource requests and limits for the read |
| read.selectorLabels | object | `{}` | Additional selecto labels for each `read` pod |
| read.serviceLabels | object | `{}` | Labels for read service |
| read.terminationGracePeriodSeconds | int | `30` | Grace period to allow the read to shutdown before it is killed |
| read.tolerations | list | `[]` | Tolerations for read pods |
| serviceAccount.annotations | object | `{}` | Annotations for the service account |
| serviceAccount.automountServiceAccountToken | bool | `true` | Set this toggle to false to opt out of automounting API credentials for the service account |
| serviceAccount.create | bool | `true` | Specifies whether a ServiceAccount should be created |
| serviceAccount.imagePullSecrets | list | `[]` | Image pull secrets for the service account |
| serviceAccount.name | string | `nil` | The name of the ServiceAccount to use. If not set and create is true, a name is generated using the fullname template |
| write.affinity | string | Hard node and soft zone anti-affinity | Affinity for write pods. Passed through `tpl` and, thus, to be configured as string |
| write.extraArgs | list | `[]` | Additional CLI args for the write |
| write.extraEnv | list | `[]` | Environment variables to add to the write pods |
| write.extraEnvFrom | list | `[]` | Environment variables from secrets or configmaps to add to the write pods |
| write.extraVolumeMounts | list | `[]` | Volume mounts to add to the write pods |
| write.extraVolumes | list | `[]` | Volumes to add to the write pods |
| write.image.registry | string | `nil` | The Docker registry for the write image. Overrides `loki.image.registry` |
| write.image.repository | string | `nil` | Docker image repository for the write image. Overrides `loki.image.repository` |
| write.image.tag | string | `nil` | Docker image tag for the write image. Overrides `loki.image.tag` |
| write.nodeSelector | object | `{}` | Node selector for write pods |
| write.persistence.size | string | `"10Gi"` | Size of persistent disk |
| write.persistence.storageClass | string | `nil` | Storage class to be used. If defined, storageClassName: <storageClass>. If set to "-", storageClassName: "", which disables dynamic provisioning. If empty or set to null, no storageClassName spec is set, choosing the default provisioner (gp2 on AWS, standard on GKE, AWS, and OpenStack). |
| write.podAnnotations | object | `{}` | Annotations for write pods |
| write.priorityClassName | string | `nil` | The name of the PriorityClass for write pods |
| write.replicas | int | `3` | Number of replicas for the write |
| write.resources | object | `{}` | Resource requests and limits for the write |
| write.selectorLabels | object | `{}` | Additional selector labels for each `write` pod |
| write.serviceLabels | object | `{}` | Labels for ingestor service |
| write.terminationGracePeriodSeconds | int | `300` | Grace period to allow the write to shutdown before it is killed. Especially for the ingestor, this must be increased. It must be long enough so writes can be gracefully shutdown flushing/transferring all data and to successfully leave the member ring on shutdown. |
| write.tolerations | list | `[]` | Tolerations for write pods |

## Components

The chart supports the components shown in the following table.
Ingester, distributor, querier, and query-frontend are always installed.
The other components are optional.

| Component | Optional | Enabled by default |
| --- | --- | --- |
| gateway |  ✅ |  ✅ |
| write |  ❎ | n/a |
| read |  ❎ | n/a |

## Configuration

This chart configures Loki in simple, scalable mode.
It has been tested to work with [boltdb-shipper](https://grafana.com/docs/loki/latest/operations/storage/boltdb-shipper/)
and [memberlist](https://grafana.com/docs/loki/latest/configuration/#memberlist_config) while other storage and discovery options should work as well.
However, the chart does not support setting up Consul or Etcd for discovery,
and it is not intended to support these going forward.
They would have to be set up separately.
Instead, memberlist can be used which does not require a separate key/value store.
The chart creates a headless service for the memberlist which read and write nodes are part of.

You can find some working examples of this Helm Chart within the [docs/examples](/docs/examples) directory of this repo.

----

**NOTE:**
In its default configuration, the chart uses `boltdb-shipper` and `filesystem` as storage.
The reason for this is that the chart can be validated and installed in a CI pipeline.
However, this setup is not fully functional.
Querying will not be possible (or limited to the write nodes' in-memory caches) because that would otherwise require shared storage between read and write nodes
which the chart does not support and would require a volume that supports `ReadWriteMany` access mode anyways.
The recommendation is to use object storage, such as S3, GCS, MinIO, etc., or one of the other options documented at https://grafana.com/docs/loki/latest/storage/.
In order to do this, please override the `loki.config` value with a valid object storage configuration. This means overriding
the `common.storage` section, as well as the `object_store` in your `schema_config`. Please note that because of the way
helm deep merges values, you will need to explicitly `null` out the default `filesystem` configuration.

For example, to use MinIO (deployed separately) as your backend, provide the following values when installing this chart:

```yaml
loki:
  config:
    common:
      storage:
        filesystem: null
        s3:
          endpoint: minio.minio.svc.cluster.local:9000
          insecure: true
          bucketnames: loki-data
          access_key_id: loki
          secret_access_key: supersecret
          s3forcepathstyle: true
    schema_config:
      configs:
        - from: "2020-09-07"
          store: boltdb-shipper
          object_store: s3
          schema: v11
          index:
            period: 24h
            prefix: loki_index_
```

Alternatively, in order to quickly test Loki using the filestore, the [single binary chart](https://github.com/grafana/helm-charts/tree/main/charts/loki) can be used.

----

### Directory and File Locations

* Volumes are mounted to `/var/loki`. The various directories Loki needs should be configured as subdirectories (e. g. `/var/loki/index`, `/var/loki/cache`). Loki will create the directories automatically.
  * Persistence is require to use this chart, and PVCs will be created and mapped to `/var/loki`
* The config file is mounted to `/etc/loki/config/config.yaml` and passed as CLI arg.

### Example configuration using memberlist, boltdb-shipper, and S3 for storage

If using the default config values, queries will not work. At a minimum you need to provide:
  * `loki.config.common.storage` (with `filesystem` set to `null`, and valid cloud storage provided)
  * `loki.config.schema_config.configs` (with at least one config that has an `object_store` that matches the the `loki.config.common.storage` config)
values, you must provide an entire `loki.config` value.

`loki.config` is passed through the `tpl` function, meaning it is also possible to e.g. externalize S3 bucket names:

```yaml
loki:
  config:
    common:
      storage:
        s3:
          s3: s3://eu-central-1
          bucketnames: {{ .values.bucketnames }}
```

```console
helm upgrade loki --install -f values.yaml --set bucketnames=my-loki-bucket
```

## Gateway

By default and inspired by Grafana's [Tanka setup](https://github.com/grafana/loki/tree/master/production/ksonnet/loki), the chart installs the gateway component which is an NGINX that exposes Loki's API
and automatically proxies requests to the correct Loki components (read or write).
The gateway must be enabled if an Ingress is required, since the Ingress exposes the gateway only.
If the gateway is enabled, Grafana and log shipping agents, such as Promtail, should be configured to use the gateway.
If NetworkPolicies are enabled, they are more restrictive if the gateway is enabled.

## Metrics

Loki exposes Prometheus metrics.
The chart can create ServiceMonitor objects for all Loki components.

```yaml
serviceMonitor:
  enabled: true
```

Furthermore, it is possible to add Prometheus rules:

```yaml
prometheusRule:
  enabled: true
  groups:
    - name: loki-rules
      rules:
        - record: job:loki_request_duration_seconds_bucket:sum_rate
          expr: sum(rate(loki_request_duration_seconds_bucket[1m])) by (le, job)
        - record: job_route:loki_request_duration_seconds_bucket:sum_rate
          expr: sum(rate(loki_request_duration_seconds_bucket[1m])) by (le, job, route)
        - record: node_namespace_pod_container:container_cpu_usage_seconds_total:sum_rate
          expr: sum(rate(container_cpu_usage_seconds_total[1m])) by (node, namespace, pod, container)
```

## Caching

By default, this chart configures in-memory caching. If that caching does not work for your deployment, take a look at the [distributed chart](https://github.com/grafana/helm-charts/tree/main/charts/loki-distributed) for how to setup memcache.

## Monitoring

**This feature is currently a work in process. Many of the dashboards are only partially functional. We are actively working on improving them**

This chart includes a set of dashboards and custom resources that enable monitoring of the deployed Loki cluster. The dashboards are deployed via a set of config maps that can be mounted to a Grafana instance. The dashboards expect a certain set of labels, which will be correct if you also use the provided `ServiceMonitor`, `GrafanaAgent`, `LogsInstance`, and `PodLogs` custom resources

### Dashboards

This chart includes dashboards for monitoring Loki. These are a work in progress, and require the scrape configs defined in the `monitoring.serviceMonitor` and `monitoring.selfMonitoring` sections described below. The dashboards are deployed via a config map which can be mounted on a Grafana instance. To deploy the dashboards, set `monitoring.dashboards.enabled = true`. If your Grafana instance is in a different namespace than this Loki cluster, you may want to set `monitoring.dashboards.namespace` to the namespace of the Grafana instance.

### Service Monitor

The `ServiceMonitor` resource works with either the Prometheus Operator or the Grafana Agent Operator, and defines how Loki's metrics should be scraped. The resource can be deployed by setting `serviceMonitor.enable = true`. Scraping this Loki cluster using the scrape config defined in the `SerivceMonitor` resource is required for the included dashboards to work.

### Self Monitoring

Self monitoring can be enabled by setting `selfMonitoring.enable = true`. This will deploy a `GrafanaAgent`, `LogsInstance`, and `PodLogs` resource which will instruct the Grafana Agent Operator (installed seperately) on how to scrape this Loki cluster's logs and send them back to itself. Scraping this Loki cluster using the scrape config defined in the `PodLogs` resource is required for the included dashboards to work.
