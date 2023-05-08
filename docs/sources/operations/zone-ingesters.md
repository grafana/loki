 ---
title: Zone Aware Ingesters
description: Migration guide and additional details
weight: 10
 ---

# Zone Aware Ingesters
Loki's zone aware ingesters are used by Grafana Labs in order to allow for easier rollouts of large Loki deployments. You can think of them as three logical zones, however with some extra k8s config you could deploy them in separate zones.

By default, an incomming log stream's logs are replicated to 3 random (but the same 3, except in the case of some replica scaling) ingesters. This means that if one ingester is restarted, no data is lost, but two can mean data is lost and also impacts the systems ability to ingest logs because of an unhealthy ring status.

With zone awareness enabled, an incomming log line will be replicated to one ingester in each zone. This means that we're no only concerned if ingesters in multiple zones restart at the same time. We can now rollout, or lose, an entire zone at once and not impact the system. This allows deployments with a large number of ingesters to be deployed too much more quickly.

We also make use of [rollout-operator](https://github.com/grafana/rollout-operator) to manage rollouts to the 3 statefulsets gracefully. The rollout-operator looks for labels on statefulsets to know which statefulsets are part of a certain rollout group, and coordinate rollouts of pods only from a single statefulset in the group at a time. See the README in the rollout-operator repo. for a more in depth explanation.

## Migration
Migrating from a single ingester statefulset to 3 zone aware ingester statefulsets. The migration follows a few general steps, regardless of deployment method.
0. configure your existing ingesters to be part of a zone, for example `zone-default`, this will allow us to later exclude them from the write path while still allowing for graceful shutdown
1. Prep for the increase in active streams (due to the way streams are split between ingesters) by increasing the # of active streams allowed for your tenants
2. add and scale up your new zone aware ingester statefulsets such that each has 1/3 of the total # of replicas you want to run
3. enable zone awareness on the write path by setting `distributor.zone-awareness-enabled` to true for distributors and rulers
4. wait some time to ensure that the new zone aware ingesters have data for the time period they are queried for (`query_ingesters_within`)
5. enable zone awareness on the read path by setting `distributor.zone-awareness-enabled` to true for queriers
6. configure distributors and rulers to exclude ingesters in the `zone-default` so those ingesters no longer receive write traffic via `distributor.excluded-zones`
7. use the shutdown endpoint to flush data from the default ingesters, then scale down and remove the associated statefulset
8. clean up


### jsonnet
The following are steps to live migrate (no downtime) an existing Loki deployment from a single ingester statefulset to 3 zone aware ingester statefulsets. 

These instructions assume you are using the zone aware ingester jsonnet deployment code from this repo, see [here](https://github.com/grafana/loki/blob/main/production/ksonnet/loki/multi-zone.libsonnet). If you are not using jsonnet please see relevant annotations in some steps that describe how to perform that step manually.

1. Configure the zone for the existing “ingester” StatefulSet as zone-default by setting multi_zone_default_ingester_zone: true, this allows us to later filter out that zone from the write path.
2. Configure ingester-pdb with maxUnavailable=0 and deploy 3x zone-aware StatefulSets with 0 replicas by setting
```
 _config+:: {
    multi_zone_ingester_enabled: true,
    multi_zone_ingester_migration_enabled: true,
    multi_zone_ingester_replicas: 0,
    // These last two lines are necessary now that we enable zone aware ingester by default
    // so that newly created cells will not be migrated later on. If you miss them you will
    // break writes in the cell.
    multi_zone_ingester_replication_write_path_enabled: false,
    multi_zone_ingester_replication_read_path_enabled: false,
  },
```

If you're not using jsonnet the new ingester statefulsets should have a label with `rollout-group: ingester`, annotation `rollout-max-unavailable: x` (put a placeholder value in, later you should set the value of this to be some portion of the statefulsets total replicas, for example in jsonnet we template this so that each statefulset runs 1/3 of the total replicas and the max unavailable is 1/3 of each statefulsets replicas), and set the update strategy to `OnDelete`.
2.  Diff ingester and ingester-zone-a StatefulSets and make sure all config matches 
```
kubectl get statefulset -n loki-dev-008 ingester -o yaml > ingester.yaml
kubectl get statefulset -n loki-dev-008 ingester-zone-a -o yaml > ingester-zone-a.yaml
diff ingester.yaml ingester-zone-a.yaml
```
expected diffs are things like: creation time and revision #, the zone, fields used by rollout operator, # of replicas, anything related to kustomize/flux, and PVC for the wal since the containers don't exist yet
3. Temporarily double max series limits for users that are using more than 50% of their current limit, the queries are as follows (add label selectors as appropriate):
```
sum by (tenant)(sum (loki_ingester_memory_streams) by (cluster, namespace, tenant) / on (namespace) group_left max by(namespace) (loki_distributor_replication_factor))
>
on (tenant) (
max by (tenant) (label_replace(loki_overrides{limit_name="max_global_streams_per_user"} / 2.5, "tenant", "$1", "user", "(.+)"))
)
```

```
(sum (loki_ingester_memory_streams) by (cluster, namespace, tenant) / on (namespace) group_left max by(namespace) (loki_distributor_replication_factor)
) / ignoring(tenant) group_left max by (cluster, namespace)(loki_overrides_defaults{limit_name="max_global_streams_per_user"}) > 0.4)
unless on (tenant) (
(label_replace(loki_overrides{limit_name="max_global_streams_per_user"},"tenant", "$1", "user", "(.+)")))
```
4. Scale up zone-aware StatefulSets until they have ⅓ of replicas each. In small cells you can do this all at once, in larger cells it might be safer to do it in chunks. The config value you need to change is `multi_zone_ingester_replicas: 6`, the value will be split across the three statefulsets. So in this case each statefulset would run 2 replicas.
   
   If you're not using jsonnet this is the step where you would also set the annotation `rollout-max-unavailable` to some value that is less than or equal to the # of replicas each statefulset is running.
5. enable zone awareness on the write path via `multi_zone_ingester_replication_write_path_enabled: true`, this causes distributors and rulers to reshuffle series to distributors in each zone, be sure to check that all the distributors and rulers have restarted properly.
   
   If you're not using jsonnet enable zone awareness on the write path by setting `distributor.zone-awareness-enabled` to true for distributors and rulers.
6. Wait `query_ingesters_within` configured hours, by default this is 3h. This ensures that no data will be missing if we query a new ingester. However, because we cut chunks at least every 30m due to `chunk_idle_period` we can likely reduce this amount of time.
7. Check that rule evaluations are still correct on the migration, look for increases in the rate for metrics with names with the following suffixes:
```
   rule_evaluations_total
   rule_evaluation_failures_total
   rule_group_iterations_missed_total
```

8. Enable zone-aware replication on the read path `multi_zone_ingester_replication_read_path_enabled: true` or if you're not using jsonnet set `distributor.zone-awareness-enabled` to true for queriers.
9. Check that queries are still executing correctly, for example look at `loki_logql_querystats_latency_seconds_count` to see that you don't have a big increase in latency or error count for a specific query type
10. Configure distributor / ruler to exclude ingesters in the “zone-default” so those ingesters no longer receive write traffic `multi_zone_ingester_exclude_default: true` or if you're not using jsonnet set `distributor.excluded-zones` on distributors and rulers

It's a good idea to check rules evaluations again at this point, and also that the zone aware ingester statefulset is now receiving all the write traffic, you can compare `sum(loki_ingester_memory_streams{cluster="<cluster>",job=~"(<namespace>)/ingester"})` to `sum(loki_ingester_memory_streams{cluster="<cluster>",job=~"(<namespace>)/ingester-zone.*"})` 
11. if you're using an automated reconcilliation/deployment system like flux, disable it now (for example using flux ignore), if possible for just the default ingester statefulset
12. Shutdown flush the default ingesters, unregistering them from the ring, you can do this by port-forwarding each ingester pod and using the endpoint: `"http://url:PORT/ingester/shutdown?flush=true&delete_ring_tokens=true&terminate=false"`
13. manually scale down the default ingester statefulset to 0 replicas, we do this via `tk apply` but you could do it via modifying the yaml
14. merge a PR to your central config repo to keep the statefulset 0'd, and then remove the flux ignore
15. clean up any remaining temporary config from the migration, for example `multi_zone_ingester_migration_enabled: true` is no longer needed
16. ensure that all the old default ingester PVC/PV are removed