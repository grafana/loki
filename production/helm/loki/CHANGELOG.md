# Changelog

All notable changes to this library will be documented in this file.

Entries should be ordered as follows:

- [CHANGE]
- [FEATURE]
- [ENHANCEMENT]
- [BUGFIX]

Entries should include a reference to the pull request that introduced the change.

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
