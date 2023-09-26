# Loki Migrate Tool

**WARNING: THIS TOOL IS NOT WELL TESTED, ALWAYS MAKE BACKUPS AND TEST ON LESS IMPORTANT DATA FIRST!**

This is sort of a bare minimum code hooked directly into the store interfaces within Loki.

Two stores are created, a source store and destination (abbreviated dest) store.

Chunks are queried from the source store and written to the dest store, new index entries are created in the dest store as well.

You should be able to:

* Migrate between clusters
* Change tenant ID during migration
* Migrate data between schemas

All data is read and re-written (even when migrating within the same cluster). There are really no optimizations in this code for performance and there are much faster ways to move data depending on what you want to change.

This is simple and because it uses the storage interfaces, should be complete and should stay working, but it's not optimized to be fast.

There is however some parallelism built in and there are a few flags to tune this, `migrate -help` for more info

This does not remove or modify any source data, it only reads existing source data and writes to the destination.

## Usage

Build with

```
make migrate
```

or

```
make migrate-image
```

The docker image currently runs and doesn't do anything, it's intended you exec into the container and run commands manually.


### Examples

Migrate between clusters

```
migrate -source.config.file=/etc/loki-us-west1/config/config.yaml -dest.config.file=/etc/loki-us-central1/config/config.yaml -source.tenant=2289 -dest.tenant=2289 -from=2020-06-16T14:00:00-00:00 -to=2020-07-01T00:00:00-00:00
```

Migrate tenant ID within a cluster

```
migrate -source.config.file=/etc/loki-us-west1/config/config.yaml -dest.config.file=/etc/loki-us-west1/config/config.yaml -source.tenant=fake -dest.tenant=1 -from=2020-06-16T14:00:00-00:00 -to=2020-07-01T00:00:00-00:00
```

### Stopping and restarting

It's ok to process the same data multiple times, chunks are uniquely addressable, they will just replace each other.

For boltdb-shipper you will end up with multiple index files which contain duplicate entries,
Loki will handle this without issue and the compactor will reduce the number of files if there are more than 3
(TODO we should make a compactor mode which forces cleanup to a single file)

You can use the output of the processed sync ranges to help in restarting from a point of already processed data,
however be aware that because of parallel processing, you need to find the last finished time for *ALL* the threads
to determine where processing finished, because of the parallel dispatching of sync ranges the order of messages
will not always be sorted chronologically.

Also be aware of special considerations for a boltdb-shipper destination outlined below.

### batchLen, shardBy, and parallel flags

The defaults here are probably ok for normal sized computers.

If sending data from something like a Raspberry Pi, you probably want to run something like `-batchLen=10 -parallel=4` or risk running out of memory.

The transfer works by breaking up the time range into `shardBy` windows, a window is called a sync range, 
each sync range is then dispatched to one of up to `parallel` worker threads.

For each sync range, the source index is queried for all the chunks in the sync range, then the list of chunks is processed `batchLen` at a time from the source, 
re-encoded if necessary (such as changing tenant ID), and send to the destination store. You need enough memory to handle having `batchlen` chunks in memory
times the number of `parallel` threads.

If you have a really huge amount of chunks, many tens or hundreds of thousands per day, you might want to decrease `shardBy` to a smaller window. 
If you don't have many chunks and `shardBy` is too small you will process the same chunks from multiple sync ranges.

`parallel` can likely be increased up to a point until you saturate your CPU or exhaust memory.

If you have a lot of really tiny chunks it may make sense to increase `batchLen`, but I'm not sure how much changing this affects the performance. 

There is not an exact science to tuning these params yet, 
the output window gives information on throughput and you can play around with values to maximize throughput for your data.


### boltdb-shipper

When the destination index type is boltdb shipper, it's important to make sure index files are uploaded. 
This happens automatically in the background with some timers built into the boltdb-shipper code.
It also happens explicitly when all the sync ranges have been processed and the store is shutdown.

However, if the process crashes while processing, there may be index files which were not uploaded.

If restarting after a crash, it's best to overlap the start time with previously processed sync ranges. 
Exactly how much to overlap is hard to say, you could look for the most recently uploaded index file in 
the destination store which is the number of days since the unix epoch, and convert it to seconds to see what day it is.

e.g. index_18262: 18262 * (60 * 60 * 24) = 1577836800 which is `Wednesday, January 1, 2020 12:00:00 AM`
