# Loki Migrate Tool

**WARNING: THIS TOOL IS NOT WELL TESTED, ALWAYS MAKE BACKUPS AND TEST ON LESS IMPORTANT DATA FIRST!**

This is sort of a bare minimum code hooked directly into the store interfaces within Loki.

Two stores are created, a source store and destination (abbreviated dest) store.

Chunks are queried from the source store and written to the dest store, new index entries are created in the dest store as well.

This _should_ handle schema changes and different schemas on both the source and dest store.

You should be able to:

* Migrate between clusters
* Change tenant ID during migration
* Migrate data between schemas

All data is read and re-written (even when migrating within the same cluster). There are really no optimizations in this code for performance and there are much faster ways to move data depending on what you want to change.

This is simple and because it uses the storage interfaces, should be complete and should stay working, but it's not optimized to be fast.

There is however some parallelism built in and there are a few flags to tune this, `migrate -help` for more info

This does not remove any source data, it only reads existing source data and writes to the destination.

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
