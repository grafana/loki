# Changelog

All notable changes to this library will be documented in this file.

Entries should be ordered as follows:

- [CHANGE]
- [FEATURE]
- [ENHANCEMENT]
- [BUGFIX]

Entries should include a reference to the pull request that introduced the change.

[//]: # (<AUTOMATED_UPDATES_LOCATOR> : do not remove this line. This locator is used by the CI pipeline to automatically create a changelog entry for each new Loki release. Add other chart versions and respective changelog entries bellow this line.)

## 6.25.0

- [BUGFIX] Removed minio-mc init container from admin-api.
- [BUGFIX] Fixed admin-api and gateway deployment container args.
- [FEATURE] Added support for Overrides Exporter

## 6.24.1

- [ENHANCEMENT] Fix Inconsistency between sidecar.securityContext and loki.containerSecurityContext

## 6.24.0

- [BUGFIX] Add conditional to include ruler config only if `ruler.enabled=true`
- [BUGFIX] Disables the Helm test pod when `test.enabled=false`.
- [BUGFIX] Fix `enterprise.image.tag` to `3.3.0`
- [ENHANCEMENT] Bump Loki version to 3.3.2

## 6.23.0

- [CHANGE] Changed version of Grafana Loki to 3.3.1
- [CHANGE] Changed version of Minio helm chart to 5.3.0 (#14834)
- [BUGFIX] Add default wal dir to ruler config ([#14920](https://github.com/grafana/loki/pull/14920))
- [FIX] Fix statefulset templates to not show diffs in ArgoCD

## 6.22.0

## 6.21.0

## 6.20.0

- [CHANGE] Changed version of Grafana Loki to 3.3.0

## 6.19.0-weekly.227

- [ENHANCEMENT] Expose Topology Spread Constraints in Helm chart templates and default values.

## 6.19.0

## 6.18.0

- [CHANGE] Added automated weekly releases, which created this release.

## 6.17.1

- [BUGFIX] Added missing `loki.storage.azure.chunkDelimiter` parameter to Helm chart.

## 6.17.0

- [CHANGE] Changed version of Grafana Loki to 3.2.0

## 6.16.0

- [ENHANCEMENT] Allow setting nodeSelector, tolerations and affinity to enterprise components (tokengen and provisioner).

## 6.15.0

- [ENHANCEMENT] Allow setting annotations for memberlist and query-scheduler-discovery services
- [ENHANCEMENT] Allow to customize `client_max_body_size` when using Loki Gateway. #12924

## 6.14.1

- [BUGFIX] Fixed Memcached persistence options.

## 6.14.0

- [FEATURE] Add additional service annotations for components in distributed mode
- [FIX] Rename loki/templates/query-frontend/poddisruptionbudget-query-frontend.yaml to fix spelling mistake.

## 6.13.0

- [CHANGE] Correctly wrap ClusterRoleBinding around `rbac/namespaced` conditional.
- [FIX] Do not create bloom planner, bloom builder, bloom gateway Deployment/Statefulset if their replica count is 0.
- [FIX] Configure (ephemeral) storage for bloom builder working directory
- [ENHANCEMENT] Automatically configure bloom planner address for bloom builders and bloom gateway addresses for bloom gateway clients.

## 6.12.0

- [ENHANCEMENT] Replace Bloom Compactor component with Bloom Planner and Bloom Builder. These are the new components to build bloom blocks.

## 6.11.0

- [FEATURE] Add support for configuring persistence for memcached.

## 6.10.2

- [CHANGE] Bumped version of `nginxinc/nginx-unprivileged` to 1.27-alpine; this remediates several CVE

## 6.10.1

- [CHANGE] Bumped version of `kiwigrid/k8s-sidecar` to 1.27.5; this remediates several CVE

## 6.10.0

- [CHANGE] Changed version of Grafana Enterprise Logs to 3.1.1
- [CHANGE] Changed version of Grafana Loki to 3.1.1
- [ENHANCEMENT] Added ability to disable AWS S3 dualstack endpoint usage.

## 6.9.0

- [BUGFIX] Fixed how we set imagePullSecrets for the memcached and provisioner.

## 6.8.0

- [BUGFIX] Fixed how we set imagePullSecrets for the admin-api and enterprise-gateway

## 6.7.4

- [ENHANCEMENT] Allow configuring the SSE section under AWS S3 storage config.

## 6.7.3

- [BUGFIX] Removed Helm test binary

## 6.7.2

- [BUGFIX] Fix imagePullSecrets for statefulset-results-cache

## 6.7.1

- [CHANGE] Changed version of Loki to 3.1.0

## 6.7.0

- [CHANGE] Changed version of Grafana Enterprise Logs to 3.1.0

## 6.6.6

- [BUGFIX] Fix HPA ingester typo

## 6.6.5

- [BUGFIX] Fix querier address in SingleBinary mode

## 6.6.4

- [BUGFIX] Fix extraObjects

## 6.6.3

- [BUGFIX] Fix indentation of `query_range` Helm chart values

## 6.6.2

- [BUGFIX] Fix query-frontend (headless) and ruler http-metrics targetPort

## 6.6.1

- [BUGFIX] Fix query scheduler http-metrics targetPort

## 6.6.0

- [ENHANCEMENT] Allow setting PVC annotations for all volume claim templates in simple scalable and single binary mode

## 6.5.2

- [BUGFIX] Fixed Ingress routing for all deployment modes.

## 6.5.0

- [CHANGE] Changed version of Grafana Enterprise Logs to v3.0.1

## 6.4.2

- [BUGFIX] Fixed helm helper functions to include missing `loki.hpa.apiVersion`  #12716

## 6.4.1

- [BUGFIX] Fixes read & backend replicas settings

## 6.4.0

- [ENHANCEMENT] Allow extraObject items as multiline strings, allowing for templating field names

## 6.3.4

- [BUGFIX] Add missing OTLP endpoint to nginx config

## 6.3.3

- [ENHANCEMENT] make the singlebinary set 0 the replicas number of backend, write,read.

## 6.3.2

- [BUGFIX] Missing password for Loki-Canary when loki.auth_enabled is true

## 6.3.1

- [BUGFIX] Fixed Typo in Ingester templates for zoneAwareReplication

## 6.3.0

- [CHANGE] Changed version of Grafana Enterprise Logs to v3.0.0

## 6.2.5

- [BUGFIX] Add missing toleration blocks to bloom components.

## 6.2.4

- [ENHANCEMENT] Activate the volume endpoint by default.

## 6.2.3

- [ENHANCEMENT] Allow minio address to be overridden.
- [CHANGE] `query-scheduler-discovery` service will now be prefixed by query scheduler full name.
- [BUGFIX] Fix `helm-tests` Go source which was missing a param following #12245.

## 6.2.2

- [FEATURE] Add support for enabling pattern ingester config via `loki.pattern_ingester.enabled`.

## 6.2.1

- [BUGFIX] Removed duplicate bucketNames from documentation and fixed key name `deploymentMode`

## 6.2.0

- [FEATURE] Add a headless service to the bloom gateway component.

## 6.1.0

- [CHANGE] Only default bucket names in helm when using minio.

## 6.0.0

- [FEATURE] added a new `Distributed` mode of deployment.
- [CHANGE] the lokiCanary section was moved from under monitoring to be under the root of the file.
- [CHANGE] the definitions for topologySpreadConstraints and podAffinity were converted from string templates to objects. Also removed the soft constraint on zone.
- [CHANGE] the externalConfigSecretName was replaced with more generic configs

## 5.47.2

- [ENHANCEMENT] Allow for additional pipeline stages to be configured on the `selfMonitoring` `Podlogs` resource.

## 5.47.1

- [BUGFIX] Increase default value of backend minReplicas to 3

## 5.47.0

- [CHANGE] Changed version of Loki to 2.9.6

## 5.46.0

- [CHANGE] Changed version of Loki to 2.9.5

## 5.45.0

- [CHANGE] Add extraContainers parameter for the read pod

## 5.44.4

- [ENHANCEMENT] Use http_listen_port for `compactorAddress`.

## 5.44.3

- [BUGFIX] Fix template error: `<.Values.loki.server.http_listen_port>: can't evaluate field Values in type interface {}`

## 5.44.2

- [BUGFIX] Fix usage of `http_listen_port` and `grpc_listen_port` field in template.

## 5.44.1

- [BUGFIX] Fix `compactorAddress` field: add protocol and port.

## 5.44.0

- [FEATURE] Modified helm template to use parameters http_listen_port and grpc_listen_port instead of hardcoded values.

## 5.43.7

- [BUGFIX] allow to configure http_config for ruler

## 5.43.6

- [ENHANCEMENT] Add `ciliumnetworkpolicy` with egress to world for table-manager if enabled.

## 5.43.5

- [BUGFIX] Add `---` before the `backend-kubeapiserver-egress` ciliumnetworkpolicy to prevent the `backend-world-egress` one from being dumped if both are enabled.

## 5.43.4

- [ENHANCEMENT] Add `ciliumnetworkpolicies` with egress to world for write, read and backend.

## 5.43.3

- [ENHANCEMENT] Added missing default values to support ServerSideApply

## 5.43.2

- [BUGFIX] Added `alibabacloud` to `isUsingObjectStorage` check.

## 5.43.1

- [BUGFIX] Fix `toPorts` fields in the `ciliumnetworkpolicy` template

## 5.43.0

- [ENHANCEMENT] Allow the definition of resources for GrafanaAgent pods

## 5.42.3

- [BUGFIX] Added condition for `egress-discovery` networkPolicies and ciliumNetworkPolicies.

## 5.42.2

- [BUGFIX] Remove trailing tab character in statefulset templates

## 5.42.1

- [BUGFIX] Added missing annotations to loki-read StatefulSet.

## 5.42.0

- [CHANGE] Changed versions of Loki v2.9.4 and GEL v1.8.6
- [ENHANCEMENT] Bumped "grafana-agent-operator" depenency chart version to it's latest version

## 5.41.8

- [BUGFIX] Fix gateway: add possibility to disable listening on ipv6 to prevent crash on ipv4-only system.

## 5.41.7

- [FEATURE] Add support to disable specific alert rules

## 5.41.6

- [BUGFIX] Added missing namespace to query-scheduler-discovery service when deploying loki in a specific namespace.

## 5.41.5

- [BUGFIX] Added "swift" type object storage to resolve Loki HELM Chart error.

## 5.41.4

- [CHANGE] Use `/ingester/shutdown?terminate=false` for write `preStop` hook

## 5.41.3

- [FEATURE] Add support for defining an s3 backoff config.

## 5.41.2

- [FEATURE] Add ciliumnetworkpolicies.

## 5.41.1

- [FEATURE] Allow topology spread constraints for Loki read deployment component.

## 5.41.0

- [CHANGE] Changed version of Loki to 2.9.3

## 5.40.1

- [BUGFIX] Remove ruler enabled condition in networkpolicies.

## 5.40.0

- [CHANGE] Add extraContainers parameter for the write pod

## 5.39.0

- [FEATURE] Add support for adding OpenStack swift container credentials via helm chart

## 5.38.0

- [CHANGE] Changed MinIO Helm Chart version to 4.0.15

## 5.37.0

- [FEATURE] Add support for enabling tracing.

## 5.36.2

- [BUGFIX] Add support to run dnsmasq

## 5.36.1

- [FEATURE] Allow topology spread constraints for Loki

## 5.36.0

- [CHANGE] Changed version of Loki to 2.9.2

## 5.35.0

- [FEATURE] Add support for configuring distributor.

## 5.34.0

- [BUGFIX] Fix missing annotations when using Loki in single binary mode.

## 5.33.0

- [CHANGE] Changed version of Grafana Enterprise Logs to v1.8.4

## 5.32.0

- [CHANGE] Grafana dashboards are no longer created solely in scalable mode and with external cloud storage enabled.

## 5.31.0

- [CHANGE] Changed version of Loki to 2.9.2

## 5.30.0

- [CHANGE] Changed version of Grafana Enterprise Logs to v1.8.3

## 5.29.0

- [ENHANCEMENT] Allow specifying `apiVersion` for Loki's PodLog CRD.

## 5.28.0

- [BUGFIX] Fix GrafanaAgent tolerations scope

## 5.27.0

- [CHANGE] Bump `nginxinc/nginx-unpriviledged` image version to remediate [CVE-2023-4863](https://github.com/advisories/GHSA-j7hp-h8jx-5ppr)

## 5.26.0

- [ENHANCEMENT] Allow emptyDir data volumes for backend and write (via `X.persistence.volumeClaimsEnabled: false`)

## 5.25.0

- [BUGFIX] Add complete object schema to single-binary volumeClaimTemplate to avoid synchronization issues

## 5.24.0

- [ENHANCEMENT] #10613 Allow tolerations for GrafanaAgent pods

## 5.23.1

- [BUGFIX] Add missing namespaces to some components

## 5.23.0

- [ENHANCEMENT] Add loki.storage.azure.connectionString to support Azure connection string

## 5.22.2

- [BUGFIX] Fix sidecar configuration for Backend

## 5.22.1

- ENHANCEMENT #10452 Improve gitops compatibility

## 5.22.0

- [CHANGE] Changed version of Loki to 2.9.1

## 5.21.0

- [CHANGE] Changed version of Grafana Enterprise Logs to v1.8.1

## 5.20.0

- [CHANGE] Changed version of Grafana Enterprise Logs to v1.8.0

## 5.19.0

- [FEATURE] Add optional sidecard to load rules from ConfigMaps and Secrets.

## 5.18.1

- [ENHANCEMENT] #8627 Add service labels and annotations for all services.
- [CHANGE] #8627 Move read, write and table manager labels from #component.serviceLabels to #component.service.labels to improve consistency.

## 5.18.0

- [CHANGE] Changed version of Loki to 2.9.0

## 5.17.0

- [CHANGE] Changed version of Loki to 2.9.0

## 5.16.1

- [BUGFIX] Increase default minReplicas to 2 and maxReplicas to 6

## 5.16.0

- [ENHANCEMENT] Add dnsConfig to values

## 5.15.0

- [ENHANCEMENT] Add rbac.pspAnnotations to define PSP annotations

## 5.14.1

- [BUGFIX] Use the correct name of the service inside the ingress.

## 5.14.0

- [ENHANCEMENT] Make table_manager configuration toggle.

## 5.13.0

- [ENHANCEMENT] Use "loki.clusterLabel" template for PodLogs cluster label

## 5.12.0

- [ENHANCEMENT] Use tpl function in ingress and gateway-ingress for hosts

## 5.11.0

- [CHANGE] Changed version of Loki to 2.8.4

## 5.10.0

- [CHANGE] Changed version of Grafana Enterprise Logs to v1.7.3

## 5.9.2

- [ENHANCEMENT] Add custom labels value for loki ingress

## 5.9.1

- [BUGFIX] Fix loki helm chart helper function for loki.host to explicitly include gateway port

## 5.9.0

- [CHANGE] Changed version of Loki to 2.8.3

## 5.8.11

- [BUGFIX] Fix gateway: Add `/config` proxy_pass to nginx configuration

## 5.8.10

- [ENHANCEMENT] Canary labelname can now be configured via monitoring.lokiCanary.labelname

## 5.8.9

- [BUGFIX] Fix loki/logs dashboard: allow querying multiple log level at once

## 5.8.8

- [ENHANCEMENT] Add loki.storage.azure.endpointSuffix to support Azure private endpoint

## 5.8.7

- [BUGFIX] Remove persistentVolumeClaimRetentionPolicy from single-binary StatefulSet when persistence is disabled

## 5.8.6

- [ENHANCEMENT] Add serviceMonitor.metricRelabelings to support metric relabelings

## 5.8.4

- [ENHANCEMENT] Add loki.lokiCanary.updateStrategy configuration

## 5.8.3

- [ENHANCEMENT] Add priorityClassName for Grafana Agent and Loki Canary

## 5.8.2

- [BUGFIX] Reference the correct configmap name for table manager

## 5.8.1

- [BUGFIX] Fix config as a secret mount for single binary statefulset

## 5.8.0

- [ENHANCEMENT] Add loki.memberlistConfig to fully control the memberlist configuration

## 5.7.1

- [FEATURE] Add support for additional labels on loki-canary pods

## 5.6.4

- [FEATURE] Make table manager retention options configurable in values

## 5.6.3

- [BUGFIX] Fix configmap checksum in read statefulset template

## 5.6.2

- [BUGFIX] Fix configmap checksum in table manager deployment template

## 5.6.1

- [BUGFIX] Fix HPA for single binary deployment

## 5.6.0

- [ENHANCEMENT] Add `gateway.ingress.labels` to values and ingress-gateway in helm chart.

## 5.5.12

- [BUGFIX] Fix checksum annotation for config in single binary

## 5.5.11

- [BUGFIX] Add missing metrics section in backend hpa template

## 5.5.10

- [CHANGE] Make the gateway listen on IPv6 as well as IPv4

## 5.5.9

- [FEATURE] Add `loki.configStorageType` & `loki.externalConfigSecretName` values to chart and templates.

## 5.5.8

- [CHANGE] Add support for annotations on all Deployments and StatefulSets

## 5.5.7

- [BUGFIX] Fix breaking helm upgrade by changing sts podManagementPolicy from Parallel to OrderedReady which fails since that field cannot be modified on sts.

## 5.5.6

- [FEATURE] Add hpa templates for read, write and backend.

## 5.5.5

- [BUGFIX] Quote tenantId value in logsInstance

## 5.5.4

- [CHANGE] Add extraVolumeClaimTemplates for StatefulSet of the write component.

## 5.5.3

- [BUGFIX] Fix issue in distribution of queries to available read pods by using k8s service for discovering query-scheduler replicas

## 5.5.2

- [BUGFIX] Use $.Release.Namespace consistently
- [CHANGE] Add clusterLabelOverride for alert label overrides.
- [BUGFIX] Use $.Release.Namespace consistently

## 5.5.1

- [FEATURE] Added ability to reference images by digest

## 5.5.0

- [CHANGE] Changed version of Grafana Enterprise Logs to v1.7.2

## 5.4.0

- [CHANGE] Changed version of Loki to 2.8.2

- [CHANGE] Change default GEL and Loki versions to 1.7.1 and 2.8.1 respectively
- [BUGFIX] Fix dns port in network-policy

## 4.10.0

- [CHANGE] Changed version of Grafana Enterprise Logs to v1.6.3

- [BUGFIX] Add projected volume type to psp

## 4.9.0

- [CHANGE] Changed version of Loki to 2.7.5

- [BUGFIX] Fix role/PSP mapping

- [BUGFIX] Fix service/ingress mapping

## 4.8.0

- [CHANGE] Changed version of Grafana Enterprise Logs to v1.6.2

## 4.7

- [CHANGE] **BREAKING** Rename `gel-license.jwt` property of secret `gel-secrets` to `license.jwt` on enterprise-logs chart.

## 4.6.2

- [BUGFIX] Fix tokengen and provisioner secrets creation on enterprise-logs chart.

## 4.6.1

- [FEATURE] Add `gateway.nginxConfig.customReadUrl`, `gateway.nginxConfig.customWriteUrl` and `gateway.nginxConfig.customBackendUrl` to override read/write/backend paths.
- [BUGFIX] Remove unreleased setting `useFederatedToken` from Azure configuration block.

## 4.6

- [Change] Bump Loki version to 2.7.3. Revert to 2 target simple scalable mode as default until third target ships in minor release.

## 4.5.1

- [BUGFIX] Fix rendering of namespace in provisioner job.
- [ENHANCEMENT] Allow to configure `publishNotReadyAddresses` on memberlist service.
- [BUGFIX] Correctly set `compactor_address` for 3 target scalable configuration.

## 4.5

- [ENHANCEMENT] Single binary mode is now possible for more than 1 replica, with a gateway and object storage backend.

## 4.4.2

- [CHANGE] Bump Loki version to 2.7.2 and GEL version to 1.6.1

## 4.4.1

- [BUGFIX] Fix a few problems with the included dashboards and allow the rules to be created in a different namespace (which may be necessary based on how your Prometheus Operator is deployed).

## 4.1.1

- [FEATURE] Added `loki.runtimeConfig` helm values to provide a reloadable runtime configuration.

## 4.1

- [BUGFIX] Fix bug in provisioner job that caused the self-monitoring tenant secret to be created with an empty token.

## 4.0

- [FEATURE] Added `enterprise.adminToken.additionalNamespaces` which are a list of additional namespaces to create secrets containing the GEL admin token in. This is especially useful if your Grafana instance is in another namespace.
- [CHANGE] **BREAKING** Remove `enterprise.nginxConfig.file`. Both enterprise and gateway configurations now share the same nginx config, use `gateway.nginxConfig.file` for both. Admin routes will 404 on OSS deployments.
- [CHANGE] **BREAKING** Default simple deployment mode to new, 3 target configuration (read, write, and backend). This new configuration allows the `read` target to be run as a deployment and auto-scaled. To go back to the legacy, 2 target configuration, set `read.legacyReadTraget` to `true`.
- [CHANGE] **BREAKING** Change how tenants are defined
- [CHANGE] **BREKAING** Remove `enterprise.adminTokenSecret`. This is now defined under `enterprise.adminToken.secret`.
- [CHANGE] **BREKAING** Rename and change format of `enterprise.provisioner.tenants`. Property has been renamed to `enterprise.provisioner.additionalTenants`, and is now an array of objects rather than string. Each object must contain a `name` and a `secretNamespace` field, where `name` is the name of the tenant and `secretNamespace` is the namespace to create the secret with the tenant's read and write token.
- [CHANGE] **BREAKING** Change the structure of `monitoring.selfMonitoring.tenant` from a string to an object. The new object must have a `name` and a `secretNamespace` field, where `name` is the name of the self-monitoring tenant and `secretNamespace` is the namespace to create an additional secret with the tenant's token. A secret will still also be created in the release namespace as it's needed by the Loki canary.
- [CHANGE] **BREAKING** Remove ability to create self-monitoring resources in different namespaces (with the exception of dashboard configmaps).

## 3.10.0

- [CHANGE] Deprecate `enterprise.nginxConfig.file`. Both enterprise and gateway configurations now share the same nginx config. Admin routes will 404 on OSS deployments. Will be removed in version 4 of the chart, please use `gateway.nginxConfig.file` for both OSS and Enterprise gateways.
- [FEATURE] Added new simple deployment target `backend`. Running 3 targets for simple deployment will soon be the default in Loki. This new target allows the `read` target to be run as a deployment and auto-scaled.

## 3.9.0

- [BUGFIX] Fix race condition between minio create bucket job and enterprise tokengen job

## 3.8.2

- [BUGFIX] Fix autoscaling/v2 template
- [FEATURE] Added `extraObjects` helm values to extra manifests.

## 3.8.1

- [ENHANCEMENT] Add the ability to specify container lifecycle

## 3.8.0

- [BUGFIX] Added `helm-weight` annotations to the tokengen and provisioner jobs to make sure tokengen always runs before provisioner

## 3.7.0

**BREAKING**: Configuration values for Loki Canary moved from `monitoring.selfMonitoring.lokiCanary` to `monitoring.lokiCanary`

- [ENHANCEMENT] Decouple the Loki Canary from the self-monitoring setup, which adds an unnecessary dependency on the Grafana Agent Operator.

## 3.6.1

- [BUGFIX] Fix regression that produced empty PrometheusRule alerts resource

## 3.6.0

- [CHANGE] Bump Loki version to 2.7.0 and GEL version to 1.6.0

## 3.5.0

- [FEATURE] Add support for azure blob storage

## 3.4.3

- [ENHANCEMENT] Allow to change Loki `-target` argument
- [ENHANCEMENT] Add toggle for persistence disk in single-binary mode

## 3.4.2

- [BUGFIX] Fix read-only /tmp in single-binary mode

## 3.4.1

- [BUGFIX] Remove extra `/` in image name if `registry` or `repository` is empty

## 3.4.0

- [ENHANCEMENT] Allow to add some selector for Loki persistent volume

## 3.3.3

- [BUGFIX] Add missing label `prometheus.io/service-monitor: "false"` to single-binary headless service

## 3.3.2

- [BUGFIX] Fixed indentation in single-binary pdb template

## 3.3.1

- [BUGFIX] Fix invalid ruler config when filesystem storage is being used
- [BUGFIX] Fix ingress template to work with both deployment types (scalable and single binary)

## 3.3.0

- [CHANGE] Remove ServiceMonitor and PrometheusRule CRD

## 3.2.2

- [CHANGE] Add envFrom section to the tokengen job

## 3.2.1

- [BUGFIX] Fixed k8s selectors in k8s Service for single-binary mode.

## 3.2.0

- [CHANGE] Bump Grafana Enterprise Logs version to v1.5.2

## 3.1.0

- [FEATURE] Loki canary and GEL token provisioner added. The GEL token provisioner will provision a tenant and token to be used by the self-monitoring features (including the canary), as well as any additional tenants specified. A k8s secret will be created with a read and write token for each additional tenant specified.

## 3.0.4

- [CHANGE] Default minio replicas to 1 node with 2 drives. The old config used the default, which was 16 nodes with 1 drive each.
- [BUGFIX] Minio subchart values `accessKey` and `secretKey` were removed in the new chart and replaced with `rootUser` and `rootPassword`.
- [CHANGE] The tokengen job no longer creates a `grafana-token`, as the base64 encoding was not working in a Grafana Enterprise GEL plugin installation.

## 3.0.0

- [CHANGE] Loki helm chart was moved to this location in the Loki repo. The chart now supports both
[single binary](https://github.com/grafana/helm-charts/tree/main/charts/loki) and [simple scalable](https://github.com/grafana/helm-charts/tree/main/charts/loki-simple-scalable) deployment modes. For changes prior to version 3.0.0, please
look in the respective deprectated [single binary](https://github.com/grafana/helm-charts/tree/main/charts/loki) and [simple scalable](https://github.com/grafana/helm-charts/blob/main/charts/loki-simple-scalable/CHANGELOG.md) charts.
