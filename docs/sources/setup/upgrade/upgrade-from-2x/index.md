---
title: Upgrade the Helm chart to 3.0
menuTitle: Upgrade the Helm chart to 3.0
description: Upgrade the Helm chart from 2.x to 3.0.
aliases:
  - ../installation/helm/upgrade
  - ../installation/helm/upgrade-from-2.x/ 
weight: 800
keywords:
  - upgrade
---

## Upgrading from v2.x

v3.x represents a major milestone for this chart, showing a committment by the Loki team to provide a better supported, scalable helm chart.
In addition to moving the source code for this helm chart into the Loki repo itself, it also combines what were previously two separate charts,
[`grafana/loki`](https://github.com/grafana/helm-charts/tree/main/charts/loki) and [`grafana/loki-simple-scalable`](https://github.com/grafana/helm-charts/tree/main/charts/loki-simple-scalable) into one chart. This chart will automatically assume the "Single Binary" mode previously deployed by the `grafana/loki` chart if you are using a filesystem backend, and will assume the "Scalable" mode previoulsy deployed by the `grafana/loki-simple-scalable` chart if you are using an object storage backend.

As a result of this major change, upgrades from the charts this replaces might be difficult. We are attempting to support the 3 most common upgrade paths.

  1. Upgrade from `grafana/loki` using local `filesystem` storage
  1. Upgrade from `grafana/loki-simple-scalable` using a cloud based object storage such as S3 or GCS, or an api compatible equivilent like MinIO.

### Upgrading from `grafana/loki`

The default installation of `grafana/loki` is a single instance backed by `filesystem` storage that is not highly available. As a result, this upgrade method will involve downtime. The upgrade will involve deleting the previously deployed loki stateful set, the running the `helm upgrade` which will create the new one with the same name, which should attach to the existing PVC or ephemeral storage, thus preserving you data. Will still highly recommend backing up all data before conducting the upgrade.

To upgrade, you will need at least the following in your `values.yaml`:

```yaml
loki:
  commonConfig:
    replication_factor: 1
  storage:
    type: 'filesystem'
```

You will need to 1. Update the grafana helm repo, 2. delete the exsiting stateful set, and 3. updgrade making sure to have the values above included in your `values.yaml`. If you installed `grafana/loki` as `loki` in namespace `loki`, the commands would be:

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
