# loki

![Version: 6.25.0](https://img.shields.io/badge/Version-6.25.0-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: 3.3.2](https://img.shields.io/badge/AppVersion-3.3.2-informational?style=flat-square)

Helm chart for Grafana Loki and Grafana Enterprise Logs supporting both simple, scalable and distributed modes.

## Source Code

* <https://github.com/grafana/loki>
* <https://grafana.com/oss/loki/>
* <https://grafana.com/docs/loki/latest/>

## Requirements

| Repository | Name | Version |
|------------|------|---------|
| https://charts.min.io/ | minio(minio) | 5.3.0 |
| https://grafana.github.io/helm-charts | grafana-agent-operator(grafana-agent-operator) | 0.5.0 |
| https://grafana.github.io/helm-charts | rollout_operator(rollout-operator) | 0.22.0 |

Find more information in the Loki Helm Chart [documentation](https://grafana.com/docs/loki/next/installation/helm).

## Contributing and releasing

If you made any changes to the [Chart.yaml](https://github.com/grafana/loki/blob/main/production/helm/loki/Chart.yaml) or [values.yaml](https://github.com/grafana/loki/blob/main/production/helm/loki/values.yaml) run `make helm-docs` from the root of the repository to update the documentation and commit the changed files.

Futhermore, please add an entry to the [CHANGELOG.md](./CHANGELOG.md) file about what you changed.  This file has a header that looks like this:

```
[//]: # (<AUTOMATED_UPDATES_LOCATOR> : do not remove this line. This locator is used by the CI pipeline to automatically create a changelog entry for each new Loki release. Add other chart versions and respective changelog entries bellow this line.)
````

Place your changes as a bulleted list below this header. The helm chart is automatically released once a week, at which point the `CHANGELOG.md` file will be updated to reflect the release of all changes between this header the the header of the previous version as the changes for that weeks release. For example, if the weekly release will be `1.21.0`, and the `CHANGELOG.md` file has the following entries:

```
[//]: # (<AUTOMATED_UPDATES_LOCATOR> : do not remove this line. This locator is used by the CI pipeline to automatically create a changelog entry for each new Loki release. Add other chart versions and respective changelog entries bellow this line.)

- [CHANGE] Changed the thing
- [FEATURE] Cool new feature

## 1.20.0

- [BUGFIX] Fixed the bug
```

Then the weekly release will create a `CHANGELOG.md` with the following content:
```
[//]: # (<AUTOMATED_UPDATES_LOCATOR> : do not remove this line. This locator is used by the CI pipeline to automatically create a changelog entry for each new Loki release. Add other chart versions and respective changelog entries bellow this line.)

## 1.21.0

- [CHANGE] Changed the thing
- [FEATURE] Cool new feature

## 1.20.0

- [BUGFIX] Fixed the bug
```

#### Versioning

Normally contributors need _not_ bump the version nor update the [CHANGELOG.md](https://github.com/grafana/loki/blob/main/production/helm/loki/CHANGELOG.md). A new version of the Chart will follow this cadence:
- Automatic weekly releases
- Releases that coincide with Loki/GEL releases
- Manual releases when necessary (ie. to address a CVE or critical bug)
