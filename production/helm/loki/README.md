# loki

![Version: 6.39.0](https://img.shields.io/badge/Version-6.39.0-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: 3.5.3](https://img.shields.io/badge/AppVersion-3.5.3-informational?style=flat-square)

Helm chart for Grafana Loki and Grafana Enterprise Logs supporting monolithic, simple scalable, and microservices modes.

## Source Code

* <https://github.com/grafana/loki>
* <https://grafana.com/oss/loki/>
* <https://grafana.com/docs/loki/latest/>

## Requirements

| Repository | Name | Version |
|------------|------|---------|
| https://charts.min.io/ | minio(minio) | 5.4.0 |
| https://grafana.github.io/helm-charts | grafana-agent-operator(grafana-agent-operator) | 0.5.1 |
| https://grafana.github.io/helm-charts | rollout_operator(rollout-operator) | 0.32.0 |

Find more information in the Loki Helm Chart [documentation](https://grafana.com/docs/loki/latest/setup/install/helm/).

## Contributing

Please see our [Helm Contributing Guidelines](./CONTRIBUTING.md) for detailed information about contributing to the Loki Helm Chart.

## Releases

Normally, contributors need _not_ bump the Chart version. A new version of the Chart will follow this cadence:
- Automatic weekly releases
- Releases that coincide with Loki/GEL releases
- Manual releases when necessary (ie. to address a CVE or critical bug)
