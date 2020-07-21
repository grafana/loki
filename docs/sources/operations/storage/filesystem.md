---
title: Filesystem
---
# Filesystem Object Store

The filesystem object store is the easiest to get started with Loki but there are some pros/cons to this approach.

Very simply it stores all the objects (chunks) in the specified directory:

```yaml
storage_config:
  filesystem:
    directory: /tmp/loki/
```

A folder is created for every tenant all the chunks for one tenant are stored in that directory.

If loki is run in single-tenant mode, all the chunks are put in a folder named `fake` which is the synthesized tenant name used for single tenant mode.

See [multi-tenancy](../../multi-tenancy/) for more information.

## Pros

Very simple, no additional software required to use Loki when paired with the BoltDB index store.

Great for low volume applications, proof of concepts, and just playing around with Loki.

## Cons

### Scaling

At some point there is a limit to how many chunks can be stored in a single directory, for example see [this issue](https://github.com/grafana/loki/issues/1502) which explains how a Loki user ran into a strange error with about **5.5 million chunk files** in their file store (and also a workaround for the problem).

However, if you keep your streams low (remember loki writes a chunk per stream) and use configs like `chunk_target_size` (around 1MB), `max_chunk_age` (increase beyond 1h), `chunk_idle_period` (increase to match `max_chunk_age`) can be tweaked to reduce the number of chunks flushed (although they will trade for more memory consumption).

It's still very possible to store terabytes of log data with the filestore, but realize there are limitations to how many files a filesystem will want to store in a single directory.

### Durability

The durability of the objects is at the mercy of the filesystem itself where other object stores like S3/GCS do a lot behind the scenes to offer extremely high durability to your data.

### High Availability

Running Loki clustered is not possible with the filesystem store unless the filesystem is shared in some fashion (NFS for example).  However using shared filesystems is likely going to be a bad experience with Loki just as it is for almost every other application.

## New AND VERY EXPERIMENTAL in 1.5.0: Horizontal scaling of the filesystem store

**WARNING** as the title suggests, this is very new and potentially buggy, and it is also very likely configs around this feature will change over time.

With that warning out of the way, the addition of the [boltdb-shipper](../boltdb-shipper/) index store has added capabilities making it possible to overcome many of the limitations listed above using the filesystem store, specifically running Loki with the filesystem store on separate machines but still operate as a cluster supporting replication, and write distribution via the hash ring.

As mentioned in the title, this is very alpha at this point but we would love for people to try this and help us flush out bugs.

Here is an example config to run with Loki:

Use this config on multiple computers (or containers), do not run it on the same computer as Loki uses the hostname as the ID in the ring.

Do not use a shared fileystem such as NFS for this, each machine should have its own filesystem

```yaml
auth_enabled: false # single tenant mode

server:
  http_listen_port: 3100

ingester:
  max_transfer_retries: 0 # Disable blocks transfers on ingesters shutdown or rollout.
  chunk_idle_period: 2h # Let chunks sit idle for at least 2h before flushing, this helps to reduce total chunks in store
  max_chunk_age: 2h  # Let chunks get at least 2h old before flushing due to age, this helps to reduce total chunks in store
  chunk_target_size: 1048576 # Target chunks of 1MB, this helps to reduce total chunks in store
  chunk_retain_period: 30s

  query_store_max_look_back_period: -1  # This will allow the ingesters to query the store for all data
  lifecycler:
    heartbeat_period: 5s
    interface_names:
      - eth0
    join_after: 30s
    num_tokens: 512
    ring:
      heartbeat_timeout: 1m
      kvstore:
        consul:
          consistent_reads: true
          host: localhost:8500
          http_client_timeout: 20s
        store: consul
      replication_factor: 1 # This can be increased and probably should if you are running multiple machines!

schema_config:
  configs:
    - from: 2018-04-15
      store: boltdb-shipper
      object_store: filesystem
      schema: v11
      index:
        prefix: index_
        period: 168h

storage_config:
  boltdb_shipper:
    shared_store: filesystem
    active_index_directory: /tmp/loki/index
    cache_location: /tmp/loki/boltdb-cache
  filesystem:
    directory: /tmp/loki/chunks

limits_config:
  enforce_metric_name: false
  reject_old_samples: true
  reject_old_samples_max_age: 168h

chunk_store_config:
  max_look_back_period: 0s  # No limit how far we can look back in the store

table_manager:
  retention_deletes_enabled: false
  retention_period: 0s  # No deletions, infinite retention
```

It does require Consul to be running for the ring (any of the ring stores will work: consul, etcd, memberlist, Consul is used in this example)

It is also required that Consul be available from each machine, this example only specifies `host: localhost:8500` you would likely need to change this to the correct hostname/ip and port of your consul server.

**The config needs to be the same on every Loki instance!**

The important piece of this config is `query_store_max_look_back_period: -1` this tells Loki to allow the ingesters to look in the store for all the data.

Traffic can be sent to any of the Loki servers, it can be round-robin load balanced if desired.

Each Loki instance will use Consul to properly route both read and write data to the correct Loki instance.

Scaling up is as easy as adding more loki instances and letting them talk to the same ring.

Scaling down is harder but possible.  You would need to shutdown a Loki server then take everything in:

```yaml
  filesystem:
    directory: /tmp/loki/chunks
```

And copy it to the same directory on another Loki server, there is currently no way to split the chunks between servers you must move them all.  We expect to provide more options here in the future.


