---
title: Upgrade the Helm chart to 6.0
menuTitle: Upgrade the Helm chart to 6.0
description: Upgrade the Helm chart from 5.x to 6.0.
weight: 800
keywords:
  - upgrade
---

## Upgrading to v6.x

v6.x of this chart introduces distributed mode but also introduces breaking changes from v5x.

{{< admonition type="caution" >}}
If you have not yet [migrated to TSDB](https://grafana.com/docs/loki/<LOKI_VERSION>/setup/migrate/migrate-to-tsdb/), perform that migration before you upgrade your Loki Helm chart to v6.x.
{{< /admonition >}}

### Changes

#### BREAKING: `deploymentMode` setting

This only breaks you if you are running the chart in Single Binary mode, you will need to set

```yaml
deploymentMode: SingleBinary
```

#### BREAKING: `lokiCanary` section was moved

This section was moved from within the `monitoring` section to the root level of the values file.

#### BREAKING: `topologySpreadConstraints` and `podAffinity` converted to objects

Previously they were strings which were passed through `tpl` now they are normal objects which will be added to deployments.

Also we removed the soft constraint on zone.

#### BREAKING: `externalConfigSecretName` was removed and replaced

Instead you can now provide `configObjectName` which is used by Loki components for loading the config.

`generatedConfigObjectName` also can be used to control the name of the config object created by the chart.

This gives greater flexibility in using the chart to still generate a config object but allowing for another process to load and mutate this config into a new object which can be loaded by Loki and `configObjectName`

#### Monitoring

After some consideration of how this chart works with other charts provided by Grafana, we decided to deprecate the monitoring sections of this chart and take a new approach entirely to monitoring Loki, Mimir and Tempo with the [Meta Monitoring Chart](https://github.com/grafana/meta-monitoring-chart).

Reasons:

* There were conflicts with this chart and the Mimir chart both installing the Agent Operator.
* The Agent Operator is deprecated.
* The dependency on the Prometheus operator is not one we are able to support well.

The [Meta Monitoring Chart](https://github.com/grafana/meta-monitoring-chart) is an improvement over the previous approach because it allows for installing a clustered Grafana Agent which can send metrics, logs, and traces to Grafana Cloud, or letting you install a monitoring-only local installation of Loki, Mimir, Tempo, and Grafana.

The monitoring sections of this chart still exist but are disabled by default.

If you wish to continue using the self monitoring features you should use the following configuration, but please do note a future version of this chart will remove this capability completely:

```yaml
monitoring:
  enabled: true
  selfMonitoring:
    enabled: true
    grafanaAgent:
      installOperator: true
```

#### Memcached is included and enabled by default

Caching is crucial to the proper operation of Loki and Memcached is now included in this chart and enabled by default for the `chunksCache` and `resultsCache`.

If you are already running Memcached separately you can remove your existing installation and use the Memcached deployments built into this chart.

##### Single Binary

Memcached also deploys for the Single Binary, but this may not be desired in resource constrained environments.

You can disable it with the following configuration:

```yaml
chunksCache:
  enabled: false
resultsCache:
  enabled: false
```

With these caches disabled, Loki will return to defaults which enables an in-memory results and chunks cache, so you will still get some caching.

#### BREAKING: Zone-aware ingester StatefulSet serviceName fix (6.34.0+)

**Affected users**: Only deployments using zone-aware ingester replication (`ingester.zoneAwareReplication.enabled: true`)

In Helm chart version 6.34.0, [PR #18558](https://github.com/grafana/loki/pull/18558) fixed the `serviceName` field in zone-aware ingester StatefulSets to correctly reference headless services. However, since `serviceName` is an immutable field in Kubernetes StatefulSets, upgrading to 6.34.0 requires manual intervention.

**Required action before upgrading to 6.34.0**:

1. **Check if you're affected**:

   ```bash
   helm get values <RELEASE_NAME> | grep -A5 zoneAwareReplication
   ```

   If `enabled: true` appears, you need to follow these steps.

2. **Delete the StatefulSets** (data will be preserved):

   ```bash
   kubectl delete statefulset \
     <RELEASE_NAME>-ingester-zone-a \
     <RELEASE_NAME>-ingester-zone-b \
     <RELEASE_NAME>-ingester-zone-c \
     --cascade=orphan
   ```

3. **Proceed with the Helm upgrade**:

   ```bash
   helm upgrade <RELEASE_NAME> grafana/loki --version 6.34.0
   ```

**What happens**:

- PersistentVolumeClaims and data are preserved
- New StatefulSets will be created with correct service references
- Pods will restart and reattach to existing storage

**Why this change was necessary**:
The previous configuration caused ingester scaling operations to fail because the rollout-operator couldn't find the correct headless services for the `/ingester/prepare-downscale` endpoint.

#### BREAKING: Make access modes for persistence on all PVCs and StatefulSets editable (6.38.0+)

Version 6.38.0 of the Helm charts introduced the ability to edit the access modes for persistence on all PVCs and StatefulSets. This is a breaking change because it requires users to manually orphan StatefulSets before upgrading.

**Required action before upgrading to 6.38.0**:

1. **Delete the StatefulSets** (delete only the ones that exist in your deployment):

   ```bash
   # Core components (SimpleScalable mode)
   kubectl delete statefulset <RELEASE_NAME>-write --cascade=orphan
   kubectl delete statefulset <RELEASE_NAME>-backend --cascade=orphan
   
   # Single binary mode
   kubectl delete statefulset <RELEASE_NAME> --cascade=orphan
   
   # Distributed mode components
   kubectl delete statefulset <RELEASE_NAME>-ingester --cascade=orphan

   # Zone-aware ingester (if zoneAwareReplication.enabled is true)
   kubectl delete statefulset <RELEASE_NAME>-ingester-zone-a --cascade=orphan
   kubectl delete statefulset <RELEASE_NAME>-ingester-zone-b --cascade=orphan
   kubectl delete statefulset <RELEASE_NAME>-ingester-zone-c --cascade=orphan

   kubectl delete statefulset <RELEASE_NAME>-index-gateway --cascade=orphan
   kubectl delete statefulset <RELEASE_NAME>-compactor --cascade=orphan
   kubectl delete statefulset <RELEASE_NAME>-ruler --cascade=orphan
   kubectl delete statefulset <RELEASE_NAME>-pattern-ingester --cascade=orphan
   kubectl delete statefulset <RELEASE_NAME>-bloom-planner --cascade=orphan
   kubectl delete statefulset <RELEASE_NAME>-bloom-gateway --cascade=orphan
   ```

2. **Proceed with the Helm upgrade**:

   ```bash
   helm upgrade <RELEASE_NAME> grafana/loki --version 6.38.0
   ```

#### Distributed mode

This chart introduces the ability to run Loki in distributed, or [microservices mode](https://grafana.com/docs/loki/<LOKI_VERSION>/get-started/deployment-modes/#microservices-mode). For installation instructions, refer to [Install the microservices Helm chart](https://grafana.com/docs/loki/<LOKI_VERSION>/setup/install/helm/install-microservices/).
