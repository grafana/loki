# loki

![Version: 6.17.1](https://img.shields.io/badge/Version-6.17.1-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: 3.2.0](https://img.shields.io/badge/AppVersion-3.2.0-informational?style=flat-square)

Helm chart for Grafana Loki and Grafana Enterprise Logs supporting both simple, scalable and distributed modes.

## Source Code

* <https://github.com/grafana/loki>
* <https://grafana.com/oss/loki/>
* <https://grafana.com/docs/loki/latest/>

## Requirements

| Repository | Name | Version |
|------------|------|---------|
| https://charts.min.io/ | minio(minio) | 4.0.15 |
| https://grafana.github.io/helm-charts | grafana-agent-operator(grafana-agent-operator) | 0.3.15 |
| https://grafana.github.io/helm-charts | rollout_operator(rollout-operator) | 0.13.0 |

Find more information in the Loki Helm Chart [documentation](https://grafana.com/docs/loki/next/installation/helm).

## Contributing and releasing

If you made any changes to the [Chart.yaml](https://github.com/grafana/loki/blob/main/production/helm/loki/Chart.yaml) or [values.yaml](https://github.com/grafana/loki/blob/main/production/helm/loki/values.yaml) run `make helm-docs` from the root of the repository to update the documentation and commit the changed files.

#### Versioning

Normally contributors need _not_ bump the version nor update the [CHANGELOG.md](https://github.com/grafana/loki/blob/main/production/helm/loki/CHANGELOG.md). A new version of the Chart will follow this cadence:
- Automatic weekly releases
- Releases that coincide with Loki/GEL releases
- Manual releases when necessary (ie. to address a CVE or critical bug)
