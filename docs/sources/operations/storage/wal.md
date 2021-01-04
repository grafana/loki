---
title: Write Ahead Log
---

# Write Ahead Log (WAL)


Ingesters store all their data in memory. If there is a crash, there can be data loss. The WAL helps fill this gap in reliability.
This section will use Kubernetes as a reference.

To use the WAL, there are some changes that needs to be made.

## Changes to deployment

1. Since ingesters need to have the same persistent volume across restarts/rollout, all the ingesters should be run on [statefulset](https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/) with fixed volumes.

2. Following flags needs to be set
    * `--ingester.wal-enabled` to `true` which enables writing to WAL during ingestion.
    * `--ingester.wal-dir` to the directory where the WAL data should be stores and/or recovered from. Note that this should be on the mounted volume.
    * `--ingester.checkpoint-duration` to the interval at which checkpoints should be created.
    * `--ingester.recover-from-wal` to `true` to recover data from an existing WAL. The data is recovered even if WAL is disabled and this is set to `true`. The WAL dir needs to be set for this.
        * If you are going to enable WAL, it is advisable to always set this to `true`.

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

Hence before actually scaling down in Kubernetes, port forward those ingesters and hit the [`/ingester/shutdown`](../../api#post-ingestershutdown) endpoint. This will flush the chunks and shut down the ingesters (while also removing itself from the ring).

After hitting the endpoint for `ingester-2 ingester-3`, scale down the ingesters to 2.

## Additional notes

### Kubernetes hacking

Statefulsets are significantly more cumbersome to work with/upgrade/etc. Much of this stems from immutable fields on the specification. For example, if one wants to start using the WAL with single store Loki and wants separate volume mounts for the WAL and the boltdb-shipper, you may see immutability errors when attempting updates the Kubernetes statefulsets.

In this case, try `kubectl -n <namespace> delete sts ingester --cascade=false`. This will leave the pods alive but delete the statefulset. Then you may recreate the (updated) statefulset and one-by-one start deleting the `ingester-0` through `ingester-n` pods _in that order_, allowing the statefulset to spin up new pods to replace them.

### Non-Kubernetes or baremetal deployments

* When the ingester restarts for any reason (upgrade, crash, etc), it should be able to attach to the same volume in order to recover back the WAL and tokens.
* 2 ingesters should not be working with the same volume/directory for the WAL.
* A Rollout should bring down an ingester completely and then start the new ingester, not the other way around.
