# Changelog

All notable changes to this library will be documented in this file.

Entries should be ordered as follows:

- [CHANGE]
- [FEATURE]
- [ENHANCEMENT]
- [BUGFIX]

Entries should include a reference to the pull request that introduced the change.

## 3.0.4

- [CHANGE] Default minio replicas to 1 node with 2 drives. The old config used the default, which was 16 nodes with 1 drive each.
- [BUGFIX] Minio subchart values `accessKey` and `secretKey` were removed in the new chart and replaced with `rootUser` and `rootPassword`.
- [Cahnge] The tokengen job no longer creates a `grafana-token`, as the base64 encoding was not working in a Grafana Enterprise GEL plugin installation.

## 3.0.0

- [CHANGE] Loki helm chart was moved to this location in the Loki repo. The chart now supports both
[single binary](https://github.com/grafana/helm-charts/tree/main/charts/loki) and [simple scalable](https://github.com/grafana/helm-charts/tree/main/charts/loki-simple-scalable) deployment modes. For changes prior to version 3.0.0, please
look in the respective deprectated [single binary](https://github.com/grafana/helm-charts/tree/main/charts/loki) and [simple scalable](https://github.com/grafana/helm-charts/blob/main/charts/loki-simple-scalable/CHANGELOG.md) charts.
