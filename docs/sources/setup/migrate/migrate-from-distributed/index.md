---
title: Migrate from `loki-distributed` Helm chart
menuTitle: Migrate from `loki-distributed`
description: Migration guide for moving from `loki-distributed` to `loki`
aliases:
  - ../../installation/helm/migrate-from-distributed
weight: 600
keywords:
  - migrate
  - loki-distributed
  - distributed
---

# Migrate from `loki-distributed` Helm chart

This guide will walk you through migrating to the `loki` Helm Chart, v3.0 or higher, from the `loki-distributed` Helm Chart (v0.63.2 at time of writing). The process consists of deploying the new `loki` Helm Chart alongside the existing `loki-distributed` installation. By joining the new cluster to the existing cluster's ring, you will create one large cluster. This will allow you to manually bring down the `loki-distributed` components in a safe way to avoid any data loss.

**Before you begin:**

We recommend having a Grafana instance available to monitor both the existing and new clusters, to make sure there is no data loss during the migration process. The `loki` chart ships with self-monitoring features, including dashboards. These are useful for monitoring the health of the new cluster as it spins up.

Start by updating your existing Grafana Agent or Promtail config (whatever is scraping logs from your environment) to _exclude_ the new deployment. The new `loki` chart ships with its own self-monitoring mechanisms, and we want to make sure it's not scraped twice, which would produce duplicate logs. The best way to do this is via a relabel config that will drop logs from the new deployment, for example something like:

```yaml
- source_labels:
    - "__meta_kubernetes_pod_label_app_kubernetes_io_component"
  regex: "(canary|read|write)"
  action: "drop"
```

This leverages the fact that the new deployment adds a `app.kubernetes.io/component` label of either `read` for the Read pods, `write` for the Write pods, and `canary` for the Loki Canary pods. These annotations are not present in the `loki-distributed` deployment, so this should only match logs from the new deployment.

**To Migrate from `loki-distributed` to `loki`**

1. Deploy new Loki Cluster

   Next you will deploy the new `loki` chart in the same namespace as your existing `loki-distributed` installation. Make sure to use the same buckets and storage configuration as your existing installation. You will need to set some special `migrate` values as well:

   ```yaml
   migrate:
     fromDistributed:
       enabled: true
       memberlistService: loki-loki-distributed-memberlist
   ```

   The value of `memberlistService` must be the kubernetes service created for the purpose of Memberlist DNS in the `loki-distributed` deployment. It should match the value of `memberlist.join_members` in the config of the `loki-distributed` deployment. This is what will make the new cluster join the existing clusters ring. It is important to join all existing rings, if you are using different memberlist DNS for different rings, you must manually set the value of each applicable `join_members` configuration for each ring. If using the same memberlist DNS for all rings, as is the default in the `loki-distributed` chart, setting `migrate.memberlistService` should be enough.

   Once the new cluster is up, add the appropriate data source in Grafana for the new cluster. Check that the following queries return results:

   - Confirm new and old logs are in the new deployment. Using the new deployment's Loki data source in Grafana, look for:
     - Logs with a job that is unique to your existing Promtail or Grafana Agent, the one we adjusted above to exclude logs from the new deployment which is not yet pushing logs to the new deployment. If you can query those via the new deployment in shows we have not lost historical logs.
     - Logs with the label `job="loki/loki-read"`. The read component does not exist in `loki-distributed`, so this show the new Loki cluster's self monitoring is working correctly.
   - Confirm new logs are in the old deployment. Using the old deployment's Loki data source in Grafana, look for:
     - Logs with the label `job="loki/loki-read"`. Since you have excluded logs from the new deployment from going to the `loki-distributed` deployment, if you can query them through the `loki-distributed` Loki data source that show the ingesters have joined the same ring, and are queryable from the `loki-distributed` queriers.

1. Convert all Clients to Push Logs to New `loki` Deployment

   Assuming everything is working as expected, you can now modify the `clients` section of your Grafana Agent or Promtail configuration to push logs to the new deployment. After this change is made, the `loki-distributed` cluster will no longer recieve new logs and can be carefully scaled down.

   Once this has deployed, query the new `loki` cluster's Loki data source for new logs to make sure they're still being ingested.

1. Tear Down the Old Loki Canary

   If you had previously deployed the canary via the `loki-canary` Helm Chart, you can now tear it down. The new chart ships the canary by default and is automatically configured to scrape it.

1. Update Flush Config On `loki-distributed` Deployment

   You are almost ready to start scaling down the old `loki-distributed` cluster. Before you start, however, it is important to make sure the `loki-distributed` ingesters are configured to flush on shutdown. Since these ingesters will not be coming back, there will be no opportunity for them to replay their WALs, so they need to flush all in-memory logs before shutting down to prevent data loss.

   The easiest way to do this is via the `extraArgs` argument in the `ingester` section of the `loki-distributed` chart. You may also want to set the ingester's log level to `debug` to see messages about the flushing process.

   ````yaml
   ingester:
     replicas: 3
     extraArgs:
       - '-ingester.flush-on-shutdown=true'
       - '-log.level=debug'
       ```

   Deploy this change, and make sure all ingesters restart and are running the latest configuration.

   ````

1. Scale Down Ingesters One at a Time

   It is now time to start scaling down `loki-distributed`. Scale down the ingester StatefulSet or Deployment (depending on how your `loki-distributed` chart is deployed) 1 replica at a time. If `debug` logs were enabled, you can monitor the logs of each ingester as it's terminating to make sure the flushing process was successful. Once the ingester pod is fully terminated, continue decrementing by another 1 replica. Continue until there are 0 instances of the ingester running.

   You can use the following command to edit the ingester StatefulSet in order to change the number of replicas (making sure to replace _\<NAMESPACE\>_ with the correct namespace):

   ```bash
   kubectl -n <NAMESPACE> edit statefulsets.apps loki-distributed-ingester
   ```

1. Remove `loki-distributed` cluster

   Double check that logs are still coming in to the new cluster. If something is wrong, it will be much easier to quickly scale back up `loki-distributed` ingesters before tearing down the whole cluster so you can investigate. If everything looks good, you can tear down `loki-distributed` using `helm uninstall`. For example:

   ```bash
   helm uninstall -n loki loki-distributed
   ```

   Now edit the new `loki` cluster to remove the `migrate` options you added when first installing. Remove all of the following from your `values.yaml`:

   ```yaml
   migrate:
     fromDistributed:
       enabled: true
       memberlistService: loki-loki-distributed-memberlist
   ```

   To apply the new configuration (assuming you installed the new chart as `loki` in the _loki_ namespace):

   ```bash
   helm upgrade -n loki loki grafana/loki --values values.yaml
   ```

   The `migrate.fromDistributed.memberlistService` was added as an _additional_ memberlist join member, so once this new config is pushed, the components should roll without interruption.

1. Check the Dashboards

   Now that the migration is finished, you can check the dashboards included with the new `loki` chart to make sure everything is working as expected. You can also check the loki canary metrics to make sure nothing was lost during the migration. Assuming everything was in the `loki` namespace, the following query, if run over a time period that starts before the migration, and ends after it was complete, should clearly illustrate when metrics started coming from the new canary, and if and when there was data detected by either during the process:

   ```logql
   (
     sum(increase(loki_canary_missing_entries_total{namespace="loki"}[$__range])) by (job)
     /
     sum(increase(loki_canary_entries_total{namespace="loki"}[$__range])) by (job)
   )*100
   ```
