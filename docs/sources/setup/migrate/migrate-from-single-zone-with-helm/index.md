---
title: "Migrate from single zone to zone-aware replication in Loki Helm chart version 5.41.0"
menuTitle: "Migrate to zone-aware ingesters(write) with helm"
description: "Learn how to migrate ingesters(write) from having a single availability zone to full zone-aware replication using the Grafana Loki Helm chart"
weight: 800
keywords:
  - migrate
  - ssd
  - scalable
  - simple
  - zone-aware
  - helm
---

# Migrate from single zone to zone-aware replication in Loki Helm chart version 5.41.0

> **Note:** This document was tested with Loki versions 2.9.2 and `loki` Helm chart version 5.41.0. It might work with more recent versions of Loki, but it is recommended to verify that no changes to default configs and helm chart values have been made.

The `loki` Helm chart version 5.41.0 allows for enabling zone-aware replication for the write component. This is not enabled by default in version 5.41.0 due to being a breaking change for existing installations and requires a migration.

This document explains how to migrate the write component from single zone to [zone-aware replication]({{< relref "../operations/zone-ingesters" >}}) with Helm.

**Before you begin:**

We recommend having a Grafana instance available to monitor the existing cluster, to make sure there is no data loss during the migration process.

## Migration
The following are steps to live migrate (no downtime) an existing Loki deployment from a single write StatefulSet to 3 zone aware write StatefulSets.

These instructions assume you are using the SSD `loki` helm chart deployment.

1. Temporarily double max series limits
   
   Explanation: while the new write StatefulSets are being added, some series will start to be written to new write pods, however the series will also exist on old write pods, thus the series will count twice towards limits. Not updating the limits might lead to writes to be refused due to limits violation.

  The `limits_config.max_global_streams_per_user` Loki configuration parameter has a non-zero default value of 5000. Double the default or your value by setting:

  ```yaml
  loki:
    limits_config:
      max_global_streams_per_user: 10000 # <-- or your value doubled
  ```

1. Start the migration by using the following helm values:
  ```yaml
  rollout_operator:
    enabled: true

  write:
    zoneAwareReplication:
      enabled: true
      maxUnavailable: <N>
      migration:
        enabled: true
      topologyKey: 'kubernetes.io/hostname'
      zones:
        - name: <ZONE-A>
          nodeSelector:
            topology.kubernetes.io/zone: <ZONE-A>
        - name: <ZONE-B>
          nodeSelector:
            topology.kubernetes.io/zone: <ZONE-B>
        - name: <ZONE-C>
          nodeSelector:
            topology.kubernetes.io/zone: <ZONE-C>

  ```
  > **Note**: replace `<N>` with 1/3 of the current replicas.
  > **Note**: replace `<ZONE-[A-C]>` with your real zones.

  1. Allow the new changes to be rolled out.

1. Scale up the new write StatefulSets to match the old write StatefulSet.
  ```yaml
  write:
    zoneAwareReplication:
      migration:
        replicas: <N>
  ```
  > **Note**: replace `<N>` with the number of replicas in the old write StatefulSet - `<N>` will be divided by 3, so if `<N>` is set to 3 then each new StatefulSet replica will be set to 1.

1. Enable zone-awareness on the write path.
  ```yaml
  write:
    zoneAwareReplication:
      migration:
        writePath: true
  ```
  1. Check that all the write pods have restarted properly.

1. Wait `query_ingesters_within` configured hours, by default this is 3h. This ensures that no data will be missing if we query a new write pods.

1. Check that rule evaluations are still correct on the migration, look for increases in the rate for metrics with names with the following suffixes:

  ```
  rule_evaluations_total
  rule_evaluation_failures_total
  rule_group_iterations_missed_total
  ```

1. Enable zone-awareness on the read path.
  ```yaml
  write:
    zoneAwareReplication:
      migration:
        readPath: true
  ```
  1. Check that queries are still executing correctly, for example look at `loki_logql_querystats_latency_seconds_count` to see that you don’t have a big increase in latency or error count for a specific query type.

1. Exclude the non zone-aware write pods from the write path.
  ```yaml
  write:
    zoneAwareReplication:
      migration:
        excludeDefaultZone: true
  ```
  It's a good idea to check rules evaluations again at this point, and also that the zone aware write StatefulSets is now receiving all the write traffic, you can compare `sum(loki_ingester_memory_streams{cluster="<cluster>",job=~"(<namespace>)/loki-write"})` to `sum(loki_ingester_memory_streams{cluster="<cluster>",job=~"(<namespace>)/loki-write-zone.*"})`

1. Scale down the non zone-aware write StatefulSet to 0.
  ```yaml
  write:
    zoneAwareReplication:
      migration:
        scaleDownDefaultZone: true
  ```

1. Wait until all non zone-aware write pods are terminated.

1. Remove all values for used for migration, causing defaults to be used, which removes the old write StatefulSet.
  ```yaml
  write:
    zoneAwareReplication:
      migration:
        # removed from value overrides. 
  ```

1. Wait atleast the `chunk_idle_period` configured hours/minutes, by default this is 30m.

1. Undo the doubling of series limits done in the first step.