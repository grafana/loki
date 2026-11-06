---
title: Migrate from SSD to highly available monolithic deployment
menuTitle: Migrate from SSD to HA monolithic
description: Migration guide for migrating from a simple scalable deployment to a highly available monolithic deployment.
weight: 500
keywords:
  - migrate
  - ssd
  - ha-monolithic
---

# Migrate from SSD to highly available monolithic deployment

This guide provides instructions for migrating from a [simple scalable deployment (SSD)](https://grafana.com/docs/loki/<LOKI_VERSION>/get-started/deployment-modes/#simple-scalable) to a highly available (HA) monolithic deployment of Loki. Before starting the migration, make sure you have read the [planning](#planning-your-migration) section.

{{< admonition type="warning" >}}
Loki 4.0 removes Simple Scalable Deployment (SSD) mode. Migrate from SSD to a microservices or HA monolithic deployment before you upgrade to Loki 4.0. You can't run Loki 4.0 in SSD mode.
{{< /admonition >}}

{{< admonition type="note" >}}
This guide describes the migration independent of how you deploy Loki. The steps apply to Kubernetes (Helm, Tanka, or plain manifests), Docker Compose, and virtual machines. In this guide, an _instance_ is one running Loki process, for example a container, a Pod, or a process on a virtual machine.

The examples use Loki configuration and NGINX as the reverse proxy. Replace names, addresses, and commands with the equivalents of your environment.

The configuration in this guide is for Loki 4.0. The HA monolithic instances can run Loki 4.0 while the existing SSD instances still run the latest Loki 3.x release.
{{< /admonition >}}

## Planning your migration

Migrating from a simple scalable deployment to a HA monolithic deployment without sustained downtime is possible but requires careful planning. The following considerations should be taken into account:

1. **Data:** No changes are required to your underlying data storage. Both SSD and HA monolithic require object storage for chunks and indexes (TSDB Shipper). Cloud Service Providers usually have guarantees around data availability. Although data loss or corruption is unlikely, it is always recommended to back up your data before starting the migration process. If you are using a cloud provider you can take a snapshot/backup.
1. **Compute resources:** While SSD has 3 different components and each has their own shape in term of CPU and memory consumption, HA monolithic only has a single component that processes both writes, reads, and auxiliary operations. Therefore the shape of compute nodes running Loki instances differ from SSD. Make sure you have sufficient headroom to scale both horizontally and vertically if needed. Each instance also needs its own persistent volume for the write-ahead log (WAL). Instances must not share a volume.
1. **Configuration:** This guide only covers the parameters that need to be changed. Other parameters can remain the same. Loki 4.0 removes or renames several settings that SSD configurations commonly contain. [Stage 2](#stage-2-deploying-the-monolithic-component) lists them, and the [upgrade guide](https://grafana.com/docs/loki/<LOKI_VERSION>/setup/upgrade/) describes all breaking changes. The `deprecated-config-checker` tool described there finds removed settings in your configuration file.
1. **Failed requests:** A restart of an instance can fail individual requests that were in flight. Make sure that your clients retry failed requests. The procedures in this guide avoid sustained failures but not individual failed requests.
1. **Recent logs after the traffic switch:** Between routing traffic to the new instances and stopping the old `write` instances, queries don't return the most recent logs. [Stage 3](#stage-3-transitioning-to-monolithic-components) describes how to keep this period short.

### Does HA monolithic deployment fit my purpose?

| | HA Monolithic |
|---|---|
| Durability | ✅ |
| High availability | ✅ |
| Separation of execution paths | ❌ |
| Operational complexity | 🟧 medium |
| Scalability | 🟧 medium |

## Stage 1: Preparing existing deployment

Before beginning the actual migration you want to check your existing deployment. All changes in this stage are made to the configuration of the existing SSD instances and take effect after a restart.

1. Set `ingester.wal.flush_on_shutdown: true` to make sure that chunks are written and uploaded to object storage when ingesters shut down. In Stage 3, this setting makes the logs that are still in memory of the old ingesters available to the new instances.

1. Make sure that `ingester.lifecycler.unregister_on_shutdown` isn't set to `false`, so that after final shutdown ingesters leave the ring immediately. The default is `true`.

1. Set a memberlist cluster label, as described in the next section. **If your existing deployment already has the cluster label, you can skip this step.**

### Set a memberlist cluster label

The new instances run in a separate memberlist cluster with their own rings. A cluster label makes sure that the old and the new instances don't join the same cluster if they can reach each other, for example because they use the same network or the same DNS names to find peers.

Instances only talk to instances with the same label when verification is enabled. An instance with a label discards messages from instances without a label and vice versa.

To avoid longer downtime while restarting the instances with a new cluster label, roll out the label in three steps. Restart the instances one at a time and wait until each instance is ready before you restart the next one. Include the ingester settings from the previous section in the first step.


1. Disable verification of the label, without setting a label:

    ```yaml
    memberlist:
      cluster_label_verification_disabled: true
    ```

    With verification disabled, an instance accepts messages with and without a label. After this step every instance can receive messages with a label.

1. Set the label. Keep verification disabled:

    ```yaml
    memberlist:
      cluster_label: <your-unique-cluster-name>
      cluster_label_verification_disabled: true
    ```

1. Enable verification:

    ```yaml
    memberlist:
      cluster_label: <your-unique-cluster-name>
      cluster_label_verification_disabled: false
    ```

If you accept a short interruption, you can set the label and restart all instances at the same time.

## Stage 2: Deploying the monolithic component

In this stage, you deploy the new instances alongside the existing SSD instances. The new instances use the same object storage as the SSD instances. They don't receive any traffic yet.

Since there is only a single type of Loki instance in the HA monolithic deployment, all instances are configured equally, except for the compactor mode, and started with the `-target=all` target.

To achieve high availability you need **at least three Loki instances** and a replication factor (`common.replication_factor`) of 3. This ensures that you still have high availability - a quorum of two - during restarts where one instance is not available.

The following example shows the settings that differ from an SSD configuration. The sections after the example explain them.

```yaml
common:
  replication_factor: 3
  compactor_grpc_address: <main-instance-address>:9095
  ring:
    kvstore:
      store: memberlist

storage_config:
  use_thanos_objstore: true
  object_store:
    s3:
      # Same bucket and credentials as the existing deployment.
      endpoint: <endpoint>
      bucket_name: <bucket-name>

memberlist:
  join_members:
    - <address-of-instance-1>
    - <address-of-instance-2>
    - <address-of-instance-3>
  cluster_label: <your-new-unique-cluster-name>
  cluster_label_verification_disabled: false

ingester:
  lifecycler:
    unregister_on_shutdown: true
  wal:
    enabled: true
    dir: <wal-directory-on-the-persistent-volume>
    flush_on_shutdown: true

compactor:
  working_directory: <directory-on-the-persistent-volume>
  horizontal_scaling_mode: worker
  retention_enabled: true
  delete_request_store: s3

# Only if you use the ruler.
ruler_storage:
  backend: <backend>
```

### Configure the compactor

A HA monolithic deployment runs one main compactor and one or more workers. The main compactor must run on exactly one instance at any time.

1. Set `compactor.horizontal_scaling_mode` to `worker` in the configuration that all instances share. The default is `disabled`.
1. Override the mode on exactly one instance with `-compactor.horizontal-scaling-mode=main`. You can also use an environment variable, if you reference it in the configuration file and start Loki with `-config.expand-env=true`. Your deployment tool must be able to configure one instance differently from the others.
1. Set `common.compactor_grpc_address` to the gRPC address of the main instance. Worker instances use it to connect to the main compactor. The address must stay valid when the main instance restarts, for example a stable DNS name that resolves only to that instance.
1. Set `compactor.retention_enabled: true` and `compactor.delete_request_store` to the name of your object store, for example `s3`.

Worker instances only start when retention is enabled, because deletion is the only job that workers process. Without it, the instance exits at startup with `error="starting module compactor: ... no job runners registered"`. Enabling retention doesn't delete data on its own. `limits_config.retention_period` defaults to `0s`, which disables retention.

The main compactor waits for one `compactor.compaction_interval` (default `10m`) after it starts before it compacts. Refer to [Horizontal scaling of Compactor](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/storage/compactor-horizontal-scaling/) for details.

{{< admonition type="note" >}}
While both deployments run, the SSD `backend` instances and the new main instance each run a compactor. Keep the time that both deployments run as short as practical.
{{< /admonition >}}

### Configure storage

To achieve a transition without downtime, configure the storage of the HA monolithic instances the same as the existing deployment: same bucket, same prefix, and same `schema_config`.

Use the Thanos object client (`storage_config.use_thanos_objstore: true` and the `storage_config.object_store` configuration block), because the legacy object store clients are deprecated. Loki 4.0 uses the Thanos clients by default. If your configuration still contains a legacy storage block, such as `storage_config.aws`, and you don't set `use_thanos_objstore: false`, Loki fails to start with `when use_thanos_objstore is true, config must be specified in the object_store section`. Refer to [Migrate to Thanos storage clients](https://grafana.com/docs/loki/<LOKI_VERSION>/setup/migrate/migrate-storage-clients/) to convert your configuration. The Thanos clients read the data that the legacy clients wrote.

### Update removed and renamed settings

The following settings of a typical SSD configuration need to change.

| SSD setting | HA monolithic | Effect if you keep it |
|---|---|---|
| `-target=read`, `-target=write`, `-target=backend`, `-legacy-read-mode` | `-target=all` | Loki 4.0 fails to start |
| `common.compactor_address` | `common.compactor_grpc_address` | Worker instances fail to start without `compactor_grpc_address` |
| `table_manager` | Remove the block | Loki 4.0 fails to start with `field table_manager not found` |
| `ruler.storage` | `ruler_storage` | The ruler doesn't start and logs `Ruler storage is not configured; ruler will not be started` |
| `storage_config.aws` and the other legacy storage blocks | `storage_config.object_store` | Loki fails to start, unless `use_thanos_objstore` is `false` |

### Configure the ruler

If you use the ruler, make the rules available to all HA monolithic instances, either through object storage in `ruler_storage` or through the same rule files on every instance. Set `ruler.evaluation.query_frontend.address` to an address that resolves to all HA monolithic instances.

While both deployments run, both rulers evaluate your rules. Each ruler evaluates them against the data that its own deployment can query.

### Configure memberlist

Set `memberlist.join_members` to the addresses of the HA monolithic instances only. Use a cluster label that is different from the label of the existing deployment. This keeps the rings of both deployments separate.

### Verify the deployment

1. When all instances are started and running, open the ring page (`/ring`) of one instance. Verify that all instances are registered and in an `ACTIVE` state. The ring page of the existing SSD deployment still shows only the SSD instances.
1. Push a log line to one instance and query it from another instance. If multi-tenancy is enabled, use a test tenant.
1. Check the logs of all instances for errors.

## Stage 3: Transitioning to monolithic components

The final stage of the migration is routing the traffic to the new downstream instances in the reverse proxy or load balancer and shutting down the old SSD components.

### Route the traffic to the new instances

The reverse proxy or load balancer must do the following:

- Send all requests to all HA monolithic instances. Any instance can handle any request.
- Send requests to `/loki/api/v1/delete` only to the main compactor instance. This is only needed when deletes are enabled and you want to be able to access the API from outside. Other instances return `404` for this path.
- Forward the WebSocket upgrade for `/loki/api/v1/tail`. Without it, tailing logs fails with `400 Bad Request`.
- Track address changes of instances. An instance that restarts can get a new IP address. A proxy that resolves names only when it starts or reloads keeps sending requests to the old addresses, and all requests fail when every instance has restarted.

When you run Loki on Kubernetes, send the traffic to a Service with a cluster IP that selects all HA monolithic Pods, and to a second Service with a cluster IP that selects only the main instance. The addresses of these Services don't change when Pods restart.

The following NGINX configuration re-resolves the names every 5 seconds. The name `loki` resolves to all HA monolithic instances, and `loki-main` resolves to the main instance.

```nginx
http {
    # DNS server of your platform, for example 127.0.0.11 in Docker.
    resolver 127.0.0.11 valid=5s;

    server {
        listen 3100;

        proxy_connect_timeout 2s;

        location / {
            set $loki loki;
            proxy_pass http://$loki:3100;
        }

        location = /loki/api/v1/tail {
            set $loki loki;
            proxy_pass http://$loki:3100;
            proxy_http_version 1.1;
            proxy_set_header Upgrade $http_upgrade;
            proxy_set_header Connection "upgrade";
        }

        location ~ ^/loki/api/v1/delete {
            set $compactor loki-main;
            proxy_pass http://$compactor:3100;
        }
    }
}
```

The `resolver` directive of NGINX ignores the search domains of the host. Use fully qualified names, for example `loki.<namespace>.svc.cluster.local` on Kubernetes.

Reload the configuration without restarting the proxy, for example with `nginx -s reload`. After the reload, the new instances receive the traffic for both writes (push) and reads (queries).

### Stop the old write instances

Immediately after the reload, shut down the `write` instances of the old SSD. The new instances can't query the logs that are still in the memory of the old ingesters. Until the old instances flush these logs and the new instances load the uploaded index, queries don't return the most recent logs.

With `flush_on_shutdown: true`, the old ingesters flush their chunks to object storage and upload their index when they receive the termination signal. Make sure that your deployment tool waits until the shutdown completes. The time depends on the amount of data in memory. Set the termination grace period accordingly.

After the instances stop, the new instances load the uploaded index files with their next index sync (`storage_config.tsdb_shipper.resync_interval`, default `5m`). In a test with default settings, the flushed logs were queryable about 70 seconds after the old `write` instances stopped.

### Verify the migration

1. Query a time range that starts before the traffic switch and compare the number of log lines with a count you have from before the migration or from your log shipper.
1. Check the logs of the new instances for errors.
1. If you use the ruler, list your rules with the ruler API of one instance and verify that the rules evaluate without errors.

### Clean up

Terminate all old `read` and `backend` instances, and remove the old `write` instances. Keep the persistent volumes of the old `write` instances until you have verified that all logs are queryable. Then delete them.

## Operate the HA monolithic deployment

- Restart instances one at a time and wait until an instance is ready before you restart the next one. Individual in-flight requests can fail.
- When the main instance restarts, the compactor starts again with the startup delay of one `compactor.compaction_interval`.

## Troubleshooting

| Symptom | Cause | Solution |
|---|---|---|
| An instance exits with `no job runners registered` | `horizontal_scaling_mode` is `worker` and `retention_enabled` is `false` | Set `compactor.retention_enabled: true` and `compactor.delete_request_store` |
| Loki fails with `field table_manager not found` | Removed setting | Remove the `table_manager` block |
| Loki fails with `when use_thanos_objstore is true, config must be specified in the object_store section` | Legacy storage configuration with the 4.0 default | Convert the storage configuration or set `use_thanos_objstore: false` |
| The ruler doesn't evaluate rules and the log says `Ruler storage is not configured; ruler will not be started` | `ruler.storage` is ignored when `use_thanos_objstore` is `true` | Use `ruler_storage` |
| Existing SSD instances are unready or queries time out after Stage 1, and the log says `discarding stream with unacceptable label` | The label was set on all instances in one rolling restart | Finish the restart of all instances, or use the three-step rollout |
| The proxy returns `502 Bad Gateway` after instances restarted | The proxy still uses old instance addresses | Reload the proxy, and configure it to re-resolve names |
| Tailing logs returns `400 Bad Request` | The proxy doesn't forward the WebSocket upgrade | Add the upgrade headers for `/loki/api/v1/tail` |
| `failed loading deletes for user` with `unknown service grpc.Compactor` in the logs of worker instances | Worker instances don't serve delete requests | Refer to the warning in [Planning your migration](#does-ha-monolithic-deployment-fit-my-purpose) |
