---
title: Write Ahead Log
---

# Write Ahead Log (WAL)

Ingesters temporarily store data in memory. In the event of a crash, there could be data loss. The WAL helps fill this gap in reliability.

The WAL in Loki records incoming data and stores it on the local file system in order to guarantee persistence of acknowledged data in the event of a process crash. Upon restart, Loki will "replay" all of the data in the log before registering itself as ready for subsequent writes. This allows Loki to maintain the performance & cost benefits of buffering data in memory _and_ durability benefits (it won't lose data once a write has been acknowledged).

This section will use Kubernetes as a reference deployment paradigm in the examples.

## Disclaimer & WAL nuances

The Write Ahead Log in Loki takes a few particular tradeoffs compared to other WALs you may be familiar with. The WAL aims to add additional durability guarantees, but _not at the expense of availability_. Particularly, there are two scenarios where the WAL sacrifices these guarantees.

1) Corruption/Deletion of the WAL prior to replaying it

In the event the WAL is corrupted/partially deleted, Loki will not be able to recover all of it's data. In this case, Loki will attempt to recover any data it can, but will not prevent Loki from starting.

Note: the Prometheus metric `loki_ingester_wal_corruptions_total` can be used to track and alert when this happens.

1) No space left on disk

In the event the underlying WAL disk is full, Loki will not fail incoming writes, but neither will it log them to the WAL. In this case, the persistence guarantees across process restarts will not hold.

Note: the Prometheus metric `loki_ingester_wal_disk_full_failures_total` can be used to track and alert when this happens.


### Backpressure

The WAL also includes a backpressure mechanism to allow a large WAL to be replayed within a smaller memory bound. This is helpful after bad scenarios (i.e. an outage) when a WAL has grown past the point it may be recovered in memory. In this case, the ingester will track the amount of data being replayed and once it's passed the `ingester.wal-replay-memory-ceiling` threshold, will flush to storage. When this happens, it's likely that Loki's attempt to deduplicate chunks via content addressable storage will suffer. We deemed this efficiency loss an acceptable tradeoff considering how it simplifies operation and that it should not occur during regular operation (rollouts, rescheduling) where the WAL can be replayed without triggering this threshold.

### Metrics

## Changes to deployment

1. Since ingesters need to have the same persistent volume across restarts/rollout, all the ingesters should be run on [statefulset](https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/) with fixed volumes.

2. Following flags needs to be set
    * `--ingester.wal-enabled` to `true` which enables writing to WAL during ingestion.
    * `--ingester.wal-dir` to the directory where the WAL data should be stored and/or recovered from. Note that this should be on the mounted volume.
    * `--ingester.checkpoint-duration` to the interval at which checkpoints should be created.
    * `--ingester.wal-replay-memory-ceiling` (default 4GB) may be set higher/lower depending on your resource settings. It handles memory pressure during WAL replays, allowing a WAL many times larger than available memory to be replayed. This is provided to minimize reconciliation time after very bad situations, i.e. an outage, and will likely not impact regular operations/rollouts _at all_. We suggest setting this to a high percentage (~75%) of available memory.

## Changes in lifecycle when WAL is enabled

1. Flushing of data to chunk store during rollouts or scale down is disabled. This is because during a rollout of statefulset there are no ingesters that are simultaneously leaving and joining, rather the same ingester is shut down and brought back again with updated config. Hence flushing is skipped and the data is recovered from the WAL.

## Disk space requirements

Based on tests in real world:

* Numbers from an ingester with 5000 series ingesting ~5mb/s.
* Checkpoint period was 5mins.
* disk utilization on a WAL-only disk was steady at ~10-15GB.

You should not target 100% disk utilisation.

## Migrating from stateless deployments

The ingester _deployment without WAL_ and _statefulset with WAL_ should be scaled down and up respectively in sync without transfer of data between them to ensure that any ingestion after migration is reliable immediately.

Let's take an example of 4 ingesters. The migration would look something like this:

1. Bring up one stateful ingester `ingester-0` and wait until it's ready (accepting read and write requests).
2. Scale down the old ingester deployment to 3 and wait until the leaving ingester flushes all the data to chunk store.
3. Once that ingester has disappeared from `kc get pods ...`, add another stateful ingester and wait until it's ready. Now you have `ingester-0` and `ingester-1`.
4. Repeat step 2 to reduce remove another ingester from old deployment.
5. Repeat step 3 to add another stateful ingester. Now you have `ingester-0 ingester-1 ingester-2`.
6. Repeat step 4 and 5, and now you will finally have `ingester-0 ingester-1 ingester-2 ingester-3`.

## How to scale up/down

### Scale up

Scaling up is same as what you would do without WAL or statefulsets. Nothing to change here.

### Scale down

When scaling down, we must ensure existing data on the leaving ingesters are flushed to storage instead of just the WAL. This is because we won't be replaying the WAL on an ingester that will no longer exist and we need to make sure the data is not orphaned.

Consider you have 4 ingesters `ingester-0 ingester-1 ingester-2 ingester-3` and you want to scale down to 2 ingesters, the ingesters which will be shutdown according to statefulset rules are `ingester-3` and then `ingester-2`.

Hence before actually scaling down in Kubernetes, port forward those ingesters and hit the [`/ingester/flush_shutdown`](../../api#post-ingesterflush_shutdown) endpoint. This will flush the chunks and remove itself from the ring, after which it will register as unready and may be deleted.

After hitting the endpoint for `ingester-2 ingester-3`, scale down the ingesters to 2.

## Additional notes

### Kubernetes hacking

Statefulsets are significantly more cumbersome to work with/upgrade/etc. Much of this stems from immutable fields on the specification. For example, if one wants to start using the WAL with single store Loki and wants separate volume mounts for the WAL and the boltdb-shipper, you may see immutability errors when attempting updates the Kubernetes statefulsets.

In this case, try `kubectl -n <namespace> delete sts ingester --cascade=false`. This will leave the pods alive but delete the statefulset. Then you may recreate the (updated) statefulset and one-by-one start deleting the `ingester-0` through `ingester-n` pods _in that order_, allowing the statefulset to spin up new pods to replace them.

### Non-Kubernetes or baremetal deployments

* When the ingester restarts for any reason (upgrade, crash, etc), it should be able to attach to the same volume in order to recover back the WAL and tokens.
* 2 ingesters should not be working with the same volume/directory for the WAL.
* A Rollout should bring down an ingester completely and then start the new ingester, not the other way around.
