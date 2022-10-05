# loki

![Version: 3.2.1](https://img.shields.io/badge/Version-3.2.1-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: 2.6.1](https://img.shields.io/badge/AppVersion-2.6.1-informational?style=flat-square)

Helm chart for Grafana Loki in simple, scalable mode

## Source Code

* <https://github.com/grafana/loki>
* <https://grafana.com/oss/loki/>
* <https://grafana.com/docs/loki/latest/>

## Requirements

| Repository | Name | Version |
|------------|------|---------|
| https://charts.min.io/ | minio(minio) | 4.0.12 |
| https://grafana.github.io/helm-charts | grafana-agent-operator(grafana-agent-operator) | 0.2.3 |

## Chart Repo

Add the following repo to use the chart:

```sh
helm repo add grafana https://grafana.github.io/helm-charts
```

## Upgrading from v2.x

v3.x represents a major milestone for this chart, showing a committment by the Loki team to provide a better supported, scalable helm chart.
In addition to moving the source code for this helm chart into the Loki repo itself, it also combines what were previously two separate charts,
[`grafana/loki`](https://github.com/grafana/helm-charts/tree/main/charts/loki) and [`grafana/loki-simple-scalable`](https://github.com/grafana/helm-charts/tree/main/charts/loki-simple-scalable) into one chart. This chart will automatically assume the "Single Binary" mode previously deployed by the `grafana/loki` chart if you are using a filesystem backend, and will assume the "Scalable" mode previoulsy deployed by the `grafana/loki-simple-scalable` chart if you are using an object storage backend.

As a result of this major change, upgrades from the charts this replaces might be difficult. We are attempting to support the 3 most common upgrade paths.

  1. Upgrade from `grafana/loki` using local `filesystem` storage
  1. Upgrade from `grafana/loki-simple-scalable` using a cloud based object storage such as S3 or GCS, or an api compatible equivilent like MinIO.

### Upgrading from `grafana/loki`

The default installation of `grafana/loki` is a single instance backed by `filesystem` storage that is not highly available. As a result, this upgrade method will involve downtime. The upgrade will involve deleting the previously deployed loki stateful set, the running the `helm upgrade` which will create the new one with the same name, which should attach to the existing PVC or ephemeral storage, thus preserving your data. Will still highly recommend backing up all data before conducting the upgrade.

To upgrade, you will need at least the following in your `values.yaml`:

```yaml
loki:
  commonConfig:
    replication_factor: 1
  storage:
    type: 'filesystem'
```

You will need to 1. Update the grafana helm repo, 2. delete the exsiting stateful set, and 3. upgrade making sure to have the values above included in your `values.yaml`. If you installed `grafana/loki` as `loki` in namespace `loki`, the commands would be:

```console
helm repo update grafana
kubectl -n loki delete statefulsets.apps loki
helm upgrade loki grafana/loki \
  --values values.yaml \
  --namespace loki
```

You will need to manually delete the existing stateful set for the above command to work.

### Upgrading from `grafana/loki-simple-scalable`

As this chart is largely based off the `grafana/loki-simple-scalable` chart, you should be able to use your existing `values.yaml` file and just upgrade to the new chart name. For example, if you installed the `grafana/loki-simple-scalable` chart as `loki` in the namespace `loki`, your upgrade would be:

```console
helm repo update grafana
helm upgrade loki grafana/loki \
  --values values.yaml \
  --namespace loki
```

## Configuration

By default, this chart configures Loki to run `read` and `write` targets in a scalable, highly available architecture (3 replicas of each) designed to work with object storage. If this chart is configured to run with a filesystem backend, it will assume you only want a single instance of Loki deployed
in "Single Binary" mode (see "Upgrading from `filesystem` storage"). This chart does not support a scalable single binary mode, with multiple single binary instances communicating with shared object storage. For that we recommend using a single read and write instance with shared object storage if replication is not desired, or the default configuration of 3 read and 3 write instances if replication is desired.

You can find some working examples of this Helm Chart within the [docs/examples](/docs/examples) directory of this repo.

## Gateway

By default and inspired by Grafana's [Tanka setup](https://github.com/grafana/loki/tree/master/production/ksonnet/loki), the chart
installs the gateway component which is an NGINX that exposes Loki's API and automatically proxies requests to the correct
Loki components (read or write, or single instance in the case of filesystem storage).
The gateway must be enabled if an Ingress is required, since the Ingress exposes the gateway only.
If the gateway is enabled, Grafana and log shipping agents, such as Promtail, should be configured to use the gateway.
If NetworkPolicies are enabled, they are more restrictive if the gateway is enabled.

## Caching

By default, this chart configures in-memory caching. If that caching does not work for your deployment, take a look at the [distributed chart](https://github.com/grafana/helm-charts/tree/main/charts/loki-distributed) for how to setup memcache.

## Monitoring

**This feature is currently a work in process. Many of the dashboards are only partially functional. We are actively working on improving them**

This chart includes a set of dashboards and custom resources that enable monitoring of the deployed Loki cluster. The dashboards are deployed via a set of config maps that can be mounted to a Grafana instance. The dashboards expect a certain set of labels, which will be correct if you also use the provided `ServiceMonitor`, `GrafanaAgent`, `LogsInstance`, and `PodLogs` custom resources

### Dashboards

This chart includes dashboards for monitoring Loki. These are a work in progress, and require the scrape configs defined in the `monitoring.serviceMonitor` and `monitoring.selfMonitoring` sections described below. The dashboards are deployed via a config map which can be mounted on a Grafana instance. To deploy the dashboards, set `monitoring.dashboards.enabled = true`. If your Grafana instance is in a different namespace than this Loki cluster, you may want to set `monitoring.dashboards.namespace` to the namespace of the Grafana instance.

### Service Monitor

The `ServiceMonitor` resource works with either the Prometheus Operator or the Grafana Agent Operator, and defines how Loki's metrics should be scraped. The resource can be deployed by setting:

```yaml
serviceMonitor:
  enabled: true
```

Scraping this Loki cluster using the scrape config defined in the `SerivceMonitor` resource is required for the included dashboards to work.

### Self Monitoring

Self monitoring can be enabled by setting `selfMonitoring.enable = true`. This will deploy a `GrafanaAgent`, `LogsInstance`, and `PodLogs` resource which will instruct the Grafana Agent Operator (installed seperately) on how to scrape this Loki cluster's logs and send them back to itself. Scraping this Loki cluster using the scrape config defined in the `PodLogs` resource is required for the included dashboards to work.

#### Rules and Alerts

If self monitoring is enabled, a default set of rules and alerts will be deployed. It is possible to add additional Prometheus rules
as well:

```yaml
monitoring:
  rules:
    additionalGroups:
      - name: loki-rules
        rules:
          - record: job:loki_request_duration_seconds_bucket:sum_rate
            expr: sum(rate(loki_request_duration_seconds_bucket[1m])) by (le, job)
          - record: job_route:loki_request_duration_seconds_bucket:sum_rate
            expr: sum(rate(loki_request_duration_seconds_bucket[1m])) by (le, job, route)
          - record: node_namespace_pod_container:container_cpu_usage_seconds_total:sum_rate
            expr: sum(rate(container_cpu_usage_seconds_total[1m])) by (node, namespace, pod, container)
```

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| enterprise.adminApi | object | `{"enabled":true}` | If enabled, the correct admin_client storage will be configured. If disabled while running enterprise, make sure auth is set to `type: trust`, or that `auth_enabled` is set to `false`. |
| enterprise.adminTokenSecret | string | `nil` | Alternative name for admin token secret, needed by tokengen and provisioner jobs |
| enterprise.canarySecret | string | `nil` | Alternative name of the secret to store token for the canary |
| enterprise.cluster_name | string | `nil` | Optional name of the GEL cluster, otherwise will use .Release.Name The cluster name must match what is in your GEL license |
| enterprise.config | string | `"{{- if .Values.enterprise.adminApi.enabled }}\n{{- if or .Values.minio.enabled (eq .Values.loki.storage.type \"s3\") (eq .Values.loki.storage.type \"gcs\") }}\nadmin_client:\n  storage:\n    s3:\n      bucket_name: {{ .Values.loki.storage.bucketNames.admin }}\n{{- end }}\n{{- end }}\nauth:\n  type: {{ .Values.enterprise.adminApi.enabled | ternary \"enterprise\" \"trust\" }}\nauth_enabled: {{ .Values.loki.auth_enabled }}\ncluster_name: {{ include \"loki.clusterName\" . }}\nlicense:\n  path: /etc/loki/license/license.jwt\n"` |  |
| enterprise.enabled | bool | `false` |  |
| enterprise.externalLicenseName | string | `nil` | Name of external licesne secret to use |
| enterprise.image.pullPolicy | string | `"IfNotPresent"` | Docker image pull policy |
| enterprise.image.registry | string | `"docker.io"` | The Docker registry |
| enterprise.image.repository | string | `"grafana/enterprise-logs"` | Docker image repository |
| enterprise.image.tag | string | `"v1.4.0"` | Overrides the image tag whose default is the chart's appVersion |
| enterprise.license | object | `{"contents":"NOTAVALIDLICENSE"}` | Grafana Enterprise Logs license In order to use Grafana Enterprise Logs features, you will need to provide the contents of your Grafana Enterprise Logs license, either by providing the contents of the license.jwt, or the name Kubernetes Secret that contains your license.jwt. To set the license contents, use the flag `--set-file 'license.contents=./license.jwt'` |
| enterprise.nginxConfig.file | string | `"worker_processes  5;  ## Default: 1\nerror_log  /dev/stderr;\npid        /tmp/nginx.pid;\nworker_rlimit_nofile 8192;\n\nevents {\n  worker_connections  4096;  ## Default: 1024\n}\n\nhttp {\n  client_body_temp_path /tmp/client_temp;\n  proxy_temp_path       /tmp/proxy_temp_path;\n  fastcgi_temp_path     /tmp/fastcgi_temp;\n  uwsgi_temp_path       /tmp/uwsgi_temp;\n  scgi_temp_path        /tmp/scgi_temp;\n\n  proxy_http_version    1.1;\n\n  default_type application/octet-stream;\n  log_format   {{ .Values.gateway.nginxConfig.logFormat }}\n\n  {{- if .Values.gateway.verboseLogging }}\n  access_log   /dev/stderr  main;\n  {{- else }}\n\n  map $status $loggable {\n    ~^[23]  0;\n    default 1;\n  }\n  access_log   /dev/stderr  main  if=$loggable;\n  {{- end }}\n\n  sendfile     on;\n  tcp_nopush   on;\n  resolver {{ .Values.global.dnsService }}.{{ .Values.global.dnsNamespace }}.svc.{{ .Values.global.clusterDomain }};\n\n  {{- with .Values.gateway.nginxConfig.httpSnippet }}\n  {{ . | nindent 2 }}\n  {{- end }}\n\n  server {\n    listen             8080;\n\n    {{- if .Values.gateway.basicAuth.enabled }}\n    auth_basic           \"Loki\";\n    auth_basic_user_file /etc/nginx/secrets/.htpasswd;\n    {{- end }}\n\n    location = / {\n      return 200 'OK';\n      auth_basic off;\n    }\n\n    location = /api/prom/push {\n      proxy_pass       http://{{ include \"loki.writeFullname\" . }}.{{ .Release.Namespace }}.svc.{{ .Values.global.clusterDomain }}:3100$request_uri;\n    }\n\n    location = /api/prom/tail {\n      proxy_pass       http://{{ include \"loki.readFullname\" . }}.{{ .Release.Namespace }}.svc.{{ .Values.global.clusterDomain }}:3100$request_uri;\n      proxy_set_header Upgrade $http_upgrade;\n      proxy_set_header Connection \"upgrade\";\n    }\n\n    location ~ /api/prom/.* {\n      proxy_pass       http://{{ include \"loki.readFullname\" . }}.{{ .Release.Namespace }}.svc.{{ .Values.global.clusterDomain }}:3100$request_uri;\n    }\n\n    location ~ /prometheus/api/v1/alerts.* {\n      proxy_pass       http://{{ include \"loki.readFullname\" . }}.{{ .Release.Namespace }}.svc.{{ .Values.global.clusterDomain }}:3100$request_uri;\n    }\n\n    location ~ /prometheus/api/v1/rules.* {\n      proxy_pass       http://{{ include \"loki.readFullname\" . }}.{{ .Release.Namespace }}.svc.{{ .Values.global.clusterDomain }}:3100$request_uri;\n    }\n\n    location = /loki/api/v1/push {\n      proxy_pass       http://{{ include \"loki.writeFullname\" . }}.{{ .Release.Namespace }}.svc.{{ .Values.global.clusterDomain }}:3100$request_uri;\n    }\n\n    location = /loki/api/v1/tail {\n      proxy_pass       http://{{ include \"loki.readFullname\" . }}.{{ .Release.Namespace }}.svc.{{ .Values.global.clusterDomain }}:3100$request_uri;\n      proxy_set_header Upgrade $http_upgrade;\n      proxy_set_header Connection \"upgrade\";\n    }\n\n    location ~ /loki/api/.* {\n      proxy_pass       http://{{ include \"loki.readFullname\" . }}.{{ .Release.Namespace }}.svc.{{ .Values.global.clusterDomain }}:3100$request_uri;\n    }\n\n    location ~ /admin/api/.* {\n      proxy_pass       http://{{ include \"loki.writeFullname\" . }}.{{ .Release.Namespace }}.svc.{{ .Values.global.clusterDomain }}:3100$request_uri;\n    }\n\n    location ~ /compactor/.* {\n      proxy_pass       http://{{ include \"loki.readFullname\" . }}.{{ .Release.Namespace }}.svc.{{ .Values.global.clusterDomain }}:3100$request_uri;\n    }\n\n    location ~ /distributor/.* {\n      proxy_pass       http://{{ include \"loki.writeFullname\" . }}.{{ .Release.Namespace }}.svc.{{ .Values.global.clusterDomain }}:3100$request_uri;\n    }\n\n    location ~ /ring {\n      proxy_pass       http://{{ include \"loki.writeFullname\" . }}.{{ .Release.Namespace }}.svc.{{ .Values.global.clusterDomain }}:3100$request_uri;\n    }\n\n    location ~ /ingester/.* {\n      proxy_pass       http://{{ include \"loki.writeFullname\" . }}.{{ .Release.Namespace }}.svc.{{ .Values.global.clusterDomain }}:3100$request_uri;\n    }\n\n    location ~ /ruler/.* {\n      proxy_pass       http://{{ include \"loki.readFullname\" . }}.{{ .Release.Namespace }}.svc.{{ .Values.global.clusterDomain }}:3100$request_uri;\n    }\n\n    location ~ /scheduler/.* {\n      proxy_pass       http://{{ include \"loki.readFullname\" . }}.{{ .Release.Namespace }}.svc.{{ .Values.global.clusterDomain }}:3100$request_uri;\n    }\n\n    {{- with .Values.gateway.nginxConfig.serverSnippet }}\n    {{ . | nindent 4 }}\n    {{- end }}\n  }\n}\n"` |  |
| enterprise.provisioner | object | `{"annotations":{},"enabled":true,"env":[],"image":{"pullPolicy":"IfNotPresent","registry":"docker.io","repository":"grafana/enterprise-logs-provisioner","tag":null},"labels":{},"priorityClassName":null,"provisionedSecretPrefix":"{{ include \"loki.name\" . }}-provisioned","securityContext":{"fsGroup":10001,"runAsGroup":10001,"runAsNonRoot":true,"runAsUser":10001},"tenants":[]}` | Configuration for `provisioner` target |
| enterprise.provisioner.annotations | object | `{}` | Additional annotations for the `provisioner` Job |
| enterprise.provisioner.enabled | bool | `true` | Whether the job should be part of the deployment |
| enterprise.provisioner.env | list | `[]` | Additional Kubernetes environment |
| enterprise.provisioner.image | object | `{"pullPolicy":"IfNotPresent","registry":"docker.io","repository":"grafana/enterprise-logs-provisioner","tag":null}` | Provisioner image to Utilize |
| enterprise.provisioner.image.pullPolicy | string | `"IfNotPresent"` | Docker image pull policy |
| enterprise.provisioner.image.registry | string | `"docker.io"` | The Docker registry |
| enterprise.provisioner.image.repository | string | `"grafana/enterprise-logs-provisioner"` | Docker image repository |
| enterprise.provisioner.image.tag | string | `nil` | Overrides the image tag whose default is the chart's appVersion |
| enterprise.provisioner.labels | object | `{}` | Additional labels for the `provisioner` Job |
| enterprise.provisioner.priorityClassName | string | `nil` | The name of the PriorityClass for provisioner Job |
| enterprise.provisioner.provisionedSecretPrefix | string | `"{{ include \"loki.name\" . }}-provisioned"` | Name of the secret to store provisioned tokens in |
| enterprise.provisioner.securityContext | object | `{"fsGroup":10001,"runAsGroup":10001,"runAsNonRoot":true,"runAsUser":10001}` | Run containers as user `enterprise-logs(uid=10001)` |
| enterprise.provisioner.tenants | list | `[]` | Tenants to be created. Each tenant will get a read and write policy and associated token. |
| enterprise.tokengen | object | `{"annotations":{},"enabled":true,"env":[],"extraArgs":[],"extraVolumeMounts":[],"extraVolumes":[],"labels":{},"securityContext":{"fsGroup":10001,"runAsGroup":10001,"runAsNonRoot":true,"runAsUser":10001},"tolerations":[]}` | Configuration for `tokengen` target |
| enterprise.tokengen.annotations | object | `{}` | Additional annotations for the `tokengen` Job |
| enterprise.tokengen.enabled | bool | `true` | Whether the job should be part of the deployment |
| enterprise.tokengen.env | list | `[]` | Additional Kubernetes environment |
| enterprise.tokengen.extraArgs | list | `[]` | Additional CLI arguments for the `tokengen` target |
| enterprise.tokengen.extraVolumeMounts | list | `[]` | Additional volume mounts for Pods |
| enterprise.tokengen.extraVolumes | list | `[]` | Additional volumes for Pods |
| enterprise.tokengen.labels | object | `{}` | Additional labels for the `tokengen` Job |
| enterprise.tokengen.securityContext | object | `{"fsGroup":10001,"runAsGroup":10001,"runAsNonRoot":true,"runAsUser":10001}` | Run containers as user `enterprise-logs(uid=10001)` |
| enterprise.tokengen.tolerations | list | `[]` | Tolerations for tokengen Job |
| enterprise.useExternalLicense | bool | `false` | Set to true when providing an external license |
| enterprise.version | string | `"v1.5.2"` |  |
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
| ingress.annotations | object | `{}` |  |
| ingress.enabled | bool | `false` |  |
| ingress.hosts[0] | string | `"loki.example.com"` |  |
| ingress.paths.read[0] | string | `"/api/prom/tail"` |  |
| ingress.paths.read[1] | string | `"/loki/api/v1/tail"` |  |
| ingress.paths.read[2] | string | `"/loki/api"` |  |
| ingress.paths.read[3] | string | `"/api/prom/rules"` |  |
| ingress.paths.read[4] | string | `"/loki/api/v1/rules"` |  |
| ingress.paths.read[5] | string | `"/prometheus/api/v1/rules"` |  |
| ingress.paths.read[6] | string | `"/prometheus/api/v1/alerts"` |  |
| ingress.paths.write[0] | string | `"/api/prom/push"` |  |
| ingress.paths.write[1] | string | `"/loki/api/v1/push"` |  |
| kubectlImage.pullPolicy | string | `"IfNotPresent"` | Docker image pull policy |
| kubectlImage.registry | string | `"docker.io"` | The Docker registry |
| kubectlImage.repository | string | `"bitnami/kubectl"` | Docker image repository |
| kubectlImage.tag | string | `nil` | Overrides the image tag whose default is the chart's appVersion |
| loki.auth_enabled | bool | `true` |  |
| loki.commonConfig | object | `{"path_prefix":"/var/loki","replication_factor":3}` | Check https://grafana.com/docs/loki/latest/configuration/#common_config for more info on how to provide a common configuration |
| loki.compactor | object | `{}` | Optional compactor configuration |
| loki.config | string | See values.yaml | Config file contents for Loki |
| loki.containerSecurityContext | object | `{"allowPrivilegeEscalation":false,"capabilities":{"drop":["ALL"]},"readOnlyRootFilesystem":true}` | The SecurityContext for Loki containers |
| loki.existingSecretForConfig | string | `""` | Specify an existing secret containing loki configuration. If non-empty, overrides `loki.config` |
| loki.image.pullPolicy | string | `"IfNotPresent"` | Docker image pull policy |
| loki.image.registry | string | `"docker.io"` | The Docker registry |
| loki.image.repository | string | `"grafana/loki"` | Docker image repository |
| loki.image.tag | string | `nil` | Overrides the image tag whose default is the chart's appVersion |
| loki.limits_config | object | `{"enforce_metric_name":false,"max_cache_freshness_per_query":"10m","reject_old_samples":true,"reject_old_samples_max_age":"168h","split_queries_by_interval":"15m"}` | Limits config |
| loki.memcached | object | `{"chunk_cache":{"batch_size":256,"enabled":false,"host":"","parallelism":10,"service":"memcached-client"},"results_cache":{"default_validity":"12h","enabled":false,"host":"","service":"memcached-client","timeout":"500ms"}}` | Configure memcached as an external cache for chunk and results cache. Disabled by default must enable and specify a host for each cache you would like to use. |
| loki.podAnnotations | object | `{}` | Common annotations for all pods |
| loki.podSecurityContext | object | `{"fsGroup":10001,"runAsGroup":10001,"runAsNonRoot":true,"runAsUser":10001}` | The SecurityContext for Loki pods |
| loki.query_scheduler | object | `{}` | Additional query scheduler config |
| loki.readinessProbe.httpGet.path | string | `"/ready"` |  |
| loki.readinessProbe.httpGet.port | string | `"http-metrics"` |  |
| loki.readinessProbe.initialDelaySeconds | int | `30` |  |
| loki.readinessProbe.timeoutSeconds | int | `1` |  |
| loki.revisionHistoryLimit | int | `10` | The number of old ReplicaSets to retain to allow rollback |
| loki.rulerConfig | object | `{}` | Check https://grafana.com/docs/loki/latest/configuration/#ruler for more info on configuring ruler |
| loki.schemaConfig | object | `{}` | Check https://grafana.com/docs/loki/latest/configuration/#schema_config for more info on how to configure schemas |
| loki.server | object | `{"grpc_listen_port":9095,"http_listen_port":3100}` | Check https://grafana.com/docs/loki/latest/configuration/#server for more info on the server configuration. |
| loki.storage | object | `{"bucketNames":{"admin":"admin","chunks":"chunks","ruler":"ruler"},"filesystem":{"chunks_directory":"/var/loki/chunks","rules_directory":"/var/loki/rules"},"gcs":{"chunkBufferSize":0,"enableHttp2":true,"requestTimeout":"0s"},"s3":{"accessKeyId":null,"endpoint":null,"http_config":{},"insecure":false,"region":null,"s3":null,"s3ForcePathStyle":false,"secretAccessKey":null},"type":"s3"}` | Storage config. Providing this will automatically populate all necessary storage configs in the templated config. |
| loki.storage_config | object | `{"hedging":{"at":"250ms","max_per_second":20,"up_to":3}}` | Additional storage config |
| loki.structuredConfig | object | `{}` | Structured loki configuration, takes precedence over `loki.config`, `loki.schemaConfig`, `loki.storageConfig` |
| minio | object | `{"buckets":[{"name":"chunks","policy":"none","purge":false},{"name":"ruler","policy":"none","purge":false},{"name":"admin","policy":"none","purge":false}],"drivesPerNode":2,"enabled":false,"persistence":{"size":"5Gi"},"replicas":1,"resources":{"requests":{"cpu":"100m","memory":"128Mi"}},"rootPassword":"supersecret","rootUser":"enterprise-logs"}` | ----------------------------------- |
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
| monitoring.selfMonitoring.lokiCanary.annotations | object | `{}` | Additional annotations for the `loki-canary` Daemonset |
| monitoring.selfMonitoring.lokiCanary.enabled | bool | `true` |  |
| monitoring.selfMonitoring.lokiCanary.extraArgs | list | `[]` | Additional CLI arguments for the `loki-canary' command |
| monitoring.selfMonitoring.lokiCanary.extraEnv | list | `[]` | Environment variables to add to the canary pods |
| monitoring.selfMonitoring.lokiCanary.extraEnvFrom | list | `[]` | Environment variables from secrets or configmaps to add to the canary pods |
| monitoring.selfMonitoring.lokiCanary.image | object | `{"pullPolicy":"IfNotPresent","registry":"docker.io","repository":"grafana/loki-canary","tag":null}` | Image to use for loki canary |
| monitoring.selfMonitoring.lokiCanary.image.pullPolicy | string | `"IfNotPresent"` | Docker image pull policy |
| monitoring.selfMonitoring.lokiCanary.image.registry | string | `"docker.io"` | The Docker registry |
| monitoring.selfMonitoring.lokiCanary.image.repository | string | `"grafana/loki-canary"` | Docker image repository |
| monitoring.selfMonitoring.lokiCanary.image.tag | string | `nil` | Overrides the image tag whose default is the chart's appVersion |
| monitoring.selfMonitoring.lokiCanary.nodeSelector | object | `{}` | Node selector for canary pods |
| monitoring.selfMonitoring.lokiCanary.resources | object | `{}` | Resource requests and limits for the canary |
| monitoring.selfMonitoring.lokiCanary.tolerations | list | `[]` | Tolerations for canary pods |
| monitoring.selfMonitoring.podLogs.annotations | object | `{}` | PodLogs annotations |
| monitoring.selfMonitoring.podLogs.labels | object | `{}` | Additional PodLogs labels |
| monitoring.selfMonitoring.podLogs.namespace | string | `nil` | Alternative namespace for PodLogs resources |
| monitoring.selfMonitoring.podLogs.relabelings | list | `[]` | PodLogs relabel configs to apply to samples before scraping https://github.com/prometheus-operator/prometheus-operator/blob/master/Documentation/api.md#relabelconfig |
| monitoring.selfMonitoring.tenant | string | `"self-monitoring"` | Tenant to use for self monitoring |
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
| singleBinary.affinity | string | Hard node and soft zone anti-affinity | Affinity for single binary pods. Passed through `tpl` and, thus, to be configured as string |
| singleBinary.autoscaling.enabled | bool | `false` | Enable autoscaling, this is only used if `queryIndex.enabled: true` |
| singleBinary.autoscaling.maxReplicas | int | `3` | Maximum autoscaling replicas for the single binary |
| singleBinary.autoscaling.minReplicas | int | `1` | Minimum autoscaling replicas for the single binary |
| singleBinary.autoscaling.targetCPUUtilizationPercentage | int | `60` | Target CPU utilisation percentage for the single binary |
| singleBinary.autoscaling.targetMemoryUtilizationPercentage | string | `nil` | Target memory utilisation percentage for the single binary |
| singleBinary.extraArgs | list | `[]` | Labels for single binary service |
| singleBinary.extraEnv | list | `[]` | Environment variables to add to the single binary pods |
| singleBinary.extraEnvFrom | list | `[]` | Environment variables from secrets or configmaps to add to the single binary pods |
| singleBinary.extraVolumeMounts | list | `[]` | Volume mounts to add to the single binary pods |
| singleBinary.extraVolumes | list | `[]` | Volumes to add to the single binary pods |
| singleBinary.image.registry | string | `nil` | The Docker registry for the single binary image. Overrides `loki.image.registry` |
| singleBinary.image.repository | string | `nil` | Docker image repository for the single binary image. Overrides `loki.image.repository` |
| singleBinary.image.tag | string | `nil` | Docker image tag for the single binary image. Overrides `loki.image.tag` |
| singleBinary.nodeSelector | object | `{}` | Node selector for single binary pods |
| singleBinary.persistence.size | string | `"10Gi"` | Size of persistent disk |
| singleBinary.persistence.storageClass | string | `nil` | Storage class to be used. If defined, storageClassName: <storageClass>. If set to "-", storageClassName: "", which disables dynamic provisioning. If empty or set to null, no storageClassName spec is set, choosing the default provisioner (gp2 on AWS, standard on GKE, AWS, and OpenStack). |
| singleBinary.podAnnotations | object | `{}` | Annotations for single binary pods |
| singleBinary.priorityClassName | string | `nil` | The name of the PriorityClass for single binary pods |
| singleBinary.replicas | int | `1` | Number of replicas for the single binary |
| singleBinary.resources | object | `{}` | Resource requests and limits for the single binary |
| singleBinary.selectorLabels | object | `{}` | Additional selecto labels for each `single binary` pod |
| singleBinary.terminationGracePeriodSeconds | int | `30` | Grace period to allow the single binary to shutdown before it is killed |
| singleBinary.tolerations | list | `[]` | Tolerations for single binary pods |
| tracing.jaegerAgentHost | string | `""` |  |
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
