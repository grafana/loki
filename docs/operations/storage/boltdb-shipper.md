# Loki with BoltDB Shipper

:warning: BoltDB Shipper is still an experimental feature. It is not recommended to be used in production environments.

BoltDB Shipper lets you run Loki without any dependency on NoSQL stores for storing index.
It locally stores the index in BoltDB files instead and keeps shipping those files to a shared object store i.e the same object store which is being used for storing chunks.
It also keeps syncing BoltDB files from shared object store to a configured local directory for getting index entries created by other services of same Loki cluster.
This helps run Loki with one less dependency and also saves costs in storage since object stores are likely to be much cheaper compared to cost of a hosted NoSQL store or running a self hosted instance of Cassandra.

## Example Configuration

Example configuration with GCS:

```yaml
schema_config:
  configs:
    - from: 2018-04-15
      store: boltdb-shipper
      object_store: gcs
      schema: v11
      index:
        prefix: loki_index_
        period: 168h

storage_config:
  gcs:
    bucket_name: GCS_BUCKET_NAME

  boltdb_shipper:
    active_index_directory: /loki/index
    shared_store: gcs
    cache_location: /loki/boltdb-cache
``` 

This would run Loki with BoltDB Shipper storing BoltDB files locally at `/loki/index` and chunks at configured `GCS_BUCKET_NAME`.
It would also keep shipping BoltDB files periodically to same configured bucket.
It would also keep downloading BoltDB files from shared bucket uploaded by other ingesters to `/loki/boltdb-cache` folder locally.

## Operational Details

Loki can be configured to run as just a single vertically scaled instance or as a cluster of horizontally scaled single binary(running all Loki services) instances or in micro-services mode running just one of the services in each instance.
When it comes to reads and writes, Ingesters are the ones which writes the index and chunks to stores and Queriers are the ones which reads index and chunks from the store for serving requests.

Before we get into more details, it is important to understand how Loki manages index in stores. Loki shards index as per configured period which defaults to 7 days i.e when it comes to table based stores like Bigtable/Cassandra/DynamoDB there would be separate table per week containing index for that week.
In case of BoltDB files there is no concept of tables so it creates a BoltDB file per week. Files/Tables created per week are identified by a configured `prefix_` + `<period-number-since-epoch>`.
Here `<period-number-since-epoch>` in case of default config would be week number since epoch.
For example, if you have prefix set to `loki_index_` and a write requests comes in on 20th April 2020, it would be stored in table/file named `loki_index_2624` because it has been `2623` weeks since epoch and we are in `2624`th week.
Since sharding of index creates multiple files when using BoltDB, BoltDB Shipper would create a folder per week and add files for that week in that folder and names those files after ingesters which created them.

To show how BoltDB files in shared object store would look like, let us consider 2 ingesters named `ingester-0` and `ingester-1` running in a Loki cluster and
they both having shipped files for week `2623` and `2624` with prefix `loki_index_`, here is how the files would look like:

```
└── index
    ├── loki_index_2623
    │   ├── ingester-0
    │   └── ingester-1
    └── loki_index_2624
        ├── ingester-0
        └── ingester-1
```
*NOTE: We also add a timestamp to names of the files to randomize the names to avoid overwriting files when running Ingesters with same name and not have a persistent storage. Timestamps not shown here for simplification*

Let us talk about more in depth about how both Ingesters and Queriers work when running them with BoltDB Shipper.

### Ingesters

Ingesters keep writing the index to BoltDB files in `active_index_directory` and BoltDB Shipper keeps looking for new and updated files in that directory every 15 Minutes to upload them to the shared object store.
When running Loki in clustered mode there could be multiple ingesters serving write requests hence each of them generating BoltDB files locally.

*NOTE: To avoid any loss of index when Ingester crashes it is recommended to run Ingesters as statefulset(when using k8s) with a persistent storage for storing index files.*

Another important detail to note is when chunks are flushed they are available for reads in object store instantly while index is not since we only upload them every 15 Minutes with BoltDB shipper.
To avoid missing logs from queries which happen to be indexed in BoltDB files which are not shipped yet, while serving queries for in-memory logs, Ingesters would also do a store query for `now()` - (`max_chunk_age` + `30 Min`) to `<end-time-from-query-request>`.

### Queriers

Queriers lazily loads BoltDB files from shared object store to configured `cache_location`.
When a querier receives a read request, query range from request is resolved to period numbers and all the files for those period numbers are downloaded to `cache_location` if not already.
Once we have downloaded files for a period we keep looking for updates in shared object store and download them every 15 Minutes by default.
Frequency for checking updates can be configured with `resync_interval` config.

To avoid keeping downloaded index files forever there is a ttl for them which defaults to 24 hours, which means if index files for a period are not used for 24 hours they would be removed from cache location.
ttl can be configured using `cache_ttl` config.



