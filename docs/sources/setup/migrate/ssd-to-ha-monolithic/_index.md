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
Simple Scalable Deployment (SSD) mode is being deprecated and will be removed with the Loki 4.0 release. You should plan to migrate from SSD to microservices or HA monolithic deployment. You will not be able to run Loki 4.0 in SSD mode.
{{< /admonition >}}

{{< admonition type="note" >}}
This guide assumes a Docker compose setup with NGINX as gateway. However, the migration process can be mirrored for other deployment methods (Helm, Tanka, etc.) as well.
{{< /admonition >}}

## Planning your migration

Migrating from a simple scalable deployment to a HA monolithic deployment with zero downtime is possible but requires careful planning. The following considerations should be taken into account:

1. **Data:** No changes are required to your underlying data storage. Both SSD and HA monolithic require object storage for chunks and indexes (TSDB Shipper). Cloud Service Providers usually have guarantees around data availability. Although data loss or corruption is unlikely, it is always recommended to back up your data before starting the migration process. If you are using a cloud provider you can take a snapshot/backup.
1. **Compute Resources:** While SSD has 3 different components and each has their own shape in term of CPU and memory consumption, HA monolithic only has a single component that processes both writes, reads, and auxiliary operations. Therefore the shape of compute nodes running Loki instances differ from SSD. Make sure you have sufficient headroom to scale both horizontally and vertically if needed.
1. **Configuration:** We do not account for all configuration parameters in this guide. We only cover the parameters that need to be changed. Other parameters can remain the same.

### Does HA monolithic deployment fit my purpose?

| | HA Monolithic |
|---|---|
| Durability | ✅ |
| High availability | ✅ |
| Separation of execution paths | ❌ |
| Operational complexity | 🟧 medium|
| Scalability | 🟧 medium |

## Stage 1: Preparing existing deployment

Before beginning the actual migration you want to check your existing deployment.

1. Set `ingester.wal.flush_on_shutdown: true` to make sure that chunks are written and uploaded to object storage when ingesters shut down.

1. Set `memberlist.cluster_label: <your-unique-cluster-name>` and `memberlist.cluster_label_verification_disabled: false` in your existing deployment.

1. Set `ingester.lifecycler.unregister_on_shutdown: true` so that after final shutdown ingesters leave the ring immediately.

1. Restart your existing deployment to ensure configuration changes take effect.

## Stage 2: Deploying the monolithic component

In this stage, we will deploy the new component alongside the existing SSD components.
Since there is only a single type of Loki node in the HA monolithic deployment, all instances can be configured equally and started with the `-target=all` target.

To achive high availability you need **at least three Loki instances** and a replication factor (`common.replication_factor`) of 3. This ensures that you still have high availability - a quorum of two - during restarts where one instance is not available.

However, because you must run only a single main compactor (`-compactor.horizontal-scaling-mode=main`) at any given time, you must configure one Loki instance as `main`. This is done by overriding the default value `worker` with `main` in the configuration of that node using an environment variable or via the CLI argument. The `common.compactor_grpc_address` setting needs to point to this dedicated compactor node, which in our case is `loki-1:9095`.

To achieve a zero-downtime transition you need to configure the HA monolithic storage equal to the existing deployment. We strongly recommend using the Thanos object client (`storage_config.use_thanos_objstore: true` and the `storage_config.object_store` configuration block), because the legacy object store clients are deprecated.

Once all Loki instances are started and running you can check the ring page (`/ring`) for one of the instances to verify that all instances are registered and in an `ACTIVE` state.

## Stage 3: Transitioning to monolithic components

The final stage of the migration is routing the traffic to the new downstream instances in the reverse proxy / load balancer and shutting down the old SSD components.

When using NGINX as reverse proxy you can use the `upstream` and `proxy_pass` directives to route the traffic.

```nginx 
http {
    ...
    upstream loki {
        server loki-1:3100;
        ...
        server loki-n:3100;
    }
    upstream compactor {
        server loki-1:3100;
    }
    server {
        listen 3100;
        ...
        location / {
            proxy_pass http://loki;
            ...
        }
        location ~ ^/loki/api/v1/delete {
            proxy_pass http://compactor;
            ...
        }
    }
}
```

The dedicated `location` for the compactor is needed to route delete requests correctly to the main compactor instance. This is only needed when deletes are enabled and you want to be able to access the API from outside.

Once the reverse proxy configuration has been updated and the service is restarted, the new components will receive the traffic for both writes (push) and reads (queries).

However, you also need to shut down (or at least flush) the `write` components of the old simple scalable deployment in order to query the data that was still held in memory.

Finally, all old `write`, `read`, and `backend` SSD targets can be terminated and cleaned up.
