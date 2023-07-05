# Changelog

All notable changes to this library will be documented in this file.

Entries should be ordered as follows:

- [CHANGE]
- [FEATURE]
- [ENHANCEMENT]
- [BUGFIX]

Entries should include a reference to the pull request that introduced the change.

[//]: # (<AUTOMATED_UPDATES_LOCATOR> : do not remove this line. This locator is used by the CI pipeline to automatically create a changelog entry for each new Loki release. Add other chart versions and respective changelog entries bellow this line.)

## 5.8.9

- [BUGFIX] Fix loki/logs dashboard: allow querying multiple log level at once

## 5.8.8

- [ENHANCEMENT] Add loki.storage.azure.endpointSuffix to support Azure private endpoint

## 5.8.7

- [BUGFIX] Remove persistentVolumeClaimRetentionPolicy from single-binary StatefulSet when persistence is disabled

## 5.8.6

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
