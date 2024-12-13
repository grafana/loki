---
title: Filesystem object store
menuTitle: Filesystem object store
description: Describes the features and limitations of using a filesystem object store with Loki.
weight: 300
---
# Filesystem object store

The filesystem object store is the easiest to get started with Grafana Loki but there are some pros/cons to this approach.

Very simply it stores all the objects (chunks) in the specified directory:

```yaml
storage_config:
  filesystem:
    directory: /tmp/loki/
```

A folder is created for every tenant all the chunks for one tenant are stored in that directory.

If Loki is run in single-tenant mode, all the chunks are put in a folder named `fake` which is the synthesized tenant name used for single tenant mode.

See [multi-tenancy](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/multi-tenancy/) for more information.

## Pros

Very simple, no additional software required to use Loki when paired with the BoltDB index store.

Great for low volume applications, proof of concepts, and just playing around with Loki.

## Cons

The filesystem is not supported by Grafana Labs for production environments (for those customers who have purchased a support contract).

### Scaling

At some point there is a limit to how many chunks can be stored in a single directory, for example see [issue #1502](https://github.com/grafana/loki/issues/1502) which explains how a Loki user ran into a strange error with about **5.5 million chunk files** in their file store (and also a workaround for the problem).

However, if you keep your streams low (remember loki writes a chunk per stream) and use configs like `chunk_target_size` (around 1MB), `max_chunk_age` (increase beyond 1h), `chunk_idle_period` (increase to match `max_chunk_age`) can be tweaked to reduce the number of chunks flushed (although they will trade for more memory consumption).

It's still very possible to store terabytes of log data with the filestore, but realize there are limitations to how many files a filesystem will want to store in a single directory.

### Durability

The durability of the objects is at the mercy of the filesystem itself where other object stores like S3/GCS do a lot behind the scenes to offer extremely high durability to your data.

### High Availability

Running Loki clustered is not possible with the filesystem store unless the filesystem is shared in some fashion (NFS for example). However using shared filesystems is likely going to be a bad experience with Loki just as it is for almost every other application.
