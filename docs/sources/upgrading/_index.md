---
title: Upgrading
weight: 250
---

# Upgrading Loki

Every attempt is made to keep Loki backwards compatible, such that upgrades should be low risk and low friction.

Unfortunately Loki is software and software is hard and sometimes we are forced to make decisions between ease of use and ease of maintenance.

If we have any expectation of difficulty upgrading we will document it here.

As more versions are released it becomes more likely unexpected problems arise moving between multiple versions at once. 
If possible try to stay current and do sequential updates. If you want to skip versions, try it in a development environment before attempting to upgrade production.


## Master / Unreleased

-_add changes here which are unreleased_

## 2.3.0

### Loki

#### Query restriction introduced for queries which do not have at least one equality matcher

PR [3216](https://github.com/grafana/loki/pull/3216) **sandeepsukhani**: check for stream selectors to have at least one equality matcher

This change now rejects any query which does not contain at least one equality matcher, an example may better illustrate:

`{namespace=~".*"}`

This query will now be rejected, however there are several ways to modify it for it to succeed:

Add at least one equals label matcher:

`{cluster="us-east-1",namespace=~".*"}`

Use `.+` instead of `.*`

`{namespace=~".+"}`

This difference may seem subtle but if we break it down `.` matches any character, `*` matches zero or more of the preceding character and `+` matches one or more of the preceding character. The `.*` case will match empty values where `.+` will not, this is the important difference. `{namespace=""}` is an invalid request (unless you add another equals label matcher like the example above)

The reasoning for this change has to do with how index lookups work in Loki, if you don't have at least one equality matcher Loki has to perform a complete index table scan which is an expensive and slow operation.


## 2.2.0

### Loki

**Be sure to upgrade to 2.0 or 2.1 BEFORE upgrading to 2.2**

In Loki 2.2 we changed the internal version of our chunk format from v2 to v3, this is a transparent change and is only relevant if you every try to _downgrade_ a Loki installation. We incorporated the code to read v3 chunks in 2.0.1 and 2.1, as well as 2.2 and any future releases.

**If you upgrade to 2.2+ any chunks created can only be read by 2.0.1, 2.1 and 2.2+**

This makes it important to first upgrade to 2.0, 2.0.1, or 2.1 **before** upgrading to 2.2 so that if you need to rollback for any reason you can do so easily.

**Note:** 2.0 and 2.0.1 are identical in every aspect except 2.0.1 contains the code necessary to read the v3 chunk format. Therefor if you are on 2.0 and ugrade to 2.2, if you want to rollback, you must rollback to 2.0.1.

### Loki Config

**Read this if you use the query-frontend and have `sharded_queries_enabled: true`**

We discovered query scheduling related to sharded queries over long time ranges could lead to unfair work scheduling by one single query in the per tenant work queue. 

The `max_query_parallelism` setting is designed to limit how many split and sharded units of 'work' for a single query are allowed to be put into the per tenant work queue at one time. The previous behavior would split the query by time using the `split_queries_by_interval` and compare this value to `max_query_parallelism` when filling the queue, however with sharding enabled, every split was then sharded into 16 additional units of work after the `max_query_parallelism` limit was applied.

In 2.2 we changed this behavior to apply the `max_query_parallelism` after splitting _and_ sharding a query resulting a more fair and expected queue scheduling per query.

**What this means** Loki will be putting much less work into the work queue per query if you are using the query frontend and have sharding_queries_enabled (which you should).  **You may need to increase your `max_query_parallelism` setting if you are noticing slower query performance** In practice, you may not see a difference unless you were running a cluster with a LOT of queriers or queriers with a very high `parallelism` frontend_worker setting.

You could consider multiplying your current `max_query_parallelism` setting by 16 to obtain the previous behavior, though in practice we suspect few people would really want it this high unless you have a significant querier worker pool.

**Also be aware to make sure `max_outstanding_per_tenant` is always greater than `max_query_parallelism` or large queries will automatically fail with a 429 back to the user.**



### Promtail 

For 2.0 we eliminated the long deprecated `entry_parser` configuration in Promtail configs, however in doing so we introduced a very confusing and erroneous default behavior:

If you did not specify a `pipeline_stages` entry you would be provided with a default which included the `docker` pipeline stage.  This can lead to some very confusing results.

In [3404](https://github.com/grafana/loki/pull/3404), we corrected this behavior

**If you are using docker, and any of your `scrape_configs` are missing a `pipeline_stages` definition**, you should add the following to obtain the correct behaviour:

```yaml
pipeline_stages:
  - docker: {}
```

## 2.1.0

The upgrade from 2.0.0 to 2.1.0 should be fairly smooth, please be aware of these two things:

### Helm charts have moved!

Helm charts are now located at: https://github.com/grafana/helm-charts/

The helm repo URL is now: https://grafana.github.io/helm-charts

### Fluent Bit plugin renamed

Fluent bit officially supports Loki as an output plugin now! WoooHOOO!

However this created a naming conflict with our existing output plugin (the new native output uses the name `loki`) so we have renamed our plugin.

In time our plan is to deprecate and eliminate our output plugin in favor of the native Loki support. However until then you can continue using the plugin with the following change:

Old:

```
[Output]
    Name loki
```

New:

```
[Output]
    Name grafana-loki
```

## 2.0.0

This is a major Loki release and there are some very important upgrade considerations.
For the most part, there are very few impactful changes and for most this will be a seamless upgrade.

2.0.0 Upgrade Topics:

* [IMPORTANT If you are using a docker image, read this!](#important-if-you-are-using-a-docker-image-read-this)
* [IMPORTANT boltdb-shipper upgrade considerations](#important-boltdb-shipper-upgrade-considerations)
* [IMPORTANT results_cachemax_freshness removed from yaml config](#important-results_cachemax_freshness-removed-from-yaml-config)
* [Promtail removed entry_parser config](#promtail-config-removed)
* [If you would like to use the new single store index and v11 schema](#upgrading-schema-to-use-boltdb-shipper-andor-v11-schema)

### **IMPORTANT If you are using a docker image, read this!**

(This includes, Helm, Tanka, docker-compose etc.)

The default config file in the docker image, as well as the default helm values.yaml and jsonnet for Tanka all specify a schema definition to make things easier to get started.

>**If you have not specified your own config file with your own schema definition (or you do not have a custom schema definition in your values.yaml), upgrading to 2.0 will break things!** 

In 2.0 the defaults are now v11 schema and the `boltdb-shipper` index type.


If you are using an index type of `aws`, `bigtable`, or `cassandra` this means you have already defined a custom schema and there is _nothing_ further you need to do regarding the schema.
You can consider however adding a new schema entry to use the new `boltdb-shipper` type if you want to move away from these separate index stores and instead use just one object store.

#### What to do

The minimum action required is to create a config which specifies the schema to match what the previous defaults were.

(Keep in mind this will only tell Loki to use the old schema default, if you would like to upgrade to v11 and/or move to the single store boltdb-shipper, [see below](#upgrading-schema-to-use-boltdb-shipper-andor-v11-schema))

There are three places we have hard coded the schema definition:

##### Helm

Helm has shipped with the same internal schema in the values.yaml file for a very long time.

If you are providing your own values.yaml file then there is no _required_ action because you already have a schema definition.

**If you are not providing your own values.yaml file, you will need to make one!**

We suggest using the included [values.yaml file from the 1.6.0 tag](https://raw.githubusercontent.com/grafana/loki/v1.6.0/production/helm/loki/values.yaml)

This matches what the default values.yaml file had prior to 2.0 and is necessary for Loki to work post 2.0 

As mentioned above, you should also consider looking at moving to the v11 schema and boltdb-shipper [see below](#upgrading-schema-to-use-boltdb-shipper-andor-v11-schema) for more information.

##### Tanka

This likely only affects a small portion of tanka users because the default schema config for Loki was forcing `GCS` and `bigtable`.

**If your `main.jsonnet` (or somewhere in your manually created jsonnet) does not have a schema config section then you will need to add one like this!**

```jsonnet
{
  _config+:: {
    using_boltdb_shipper: false,
    loki+: {
      schema_config+: {
        configs: [{
          from: '2018-04-15',
          store: 'bigtable',
          object_store: 'gcs',
          schema: 'v11',
          index: {
            prefix: '%s_index_' % $._config.table_prefix,
            period: '168h',        
          },
        }],
      },    
    },
  }
}
```

>**NOTE** If you had set `index_period_hours` to a value other than 168h (the previous default) you must update this in the above config `period:` to match what you chose.

>**NOTE** We have changed the default index store to `boltdb-shipper` it's important to add `using_boltdb_shipper: false,` until you are ready to change (if you want to change)

Changing the jsonnet config to use the `boltdb-shipper` type is the same as [below](#upgrading-schema-to-use-boltdb-shipper-andor-v11-schema) where you need to add a new schema section.

**HOWEVER** Be aware when you change `using_boltdb_shipper: true` the deployment type for the ingesters and queriers will change to statefulsets! Statefulsets are required for the ingester and querier using boltdb-shipper. 

##### Docker (e.g. docker-compose)

For docker related cases you will have to mount a Loki config file separate from what's shipped inside the container

I would recommend taking the previous default file from the [1.6.0 tag on github](https://raw.githubusercontent.com/grafana/loki/v1.6.0/cmd/loki/loki-docker-config.yaml)

How you get this mounted and in use by Loki might vary based on how you are using the image, but this is a common example:

```shell
docker run -d --name=loki --mount type=bind,source="path to loki-config.yaml",target=/etc/loki/local-config.yaml
```

The Loki docker image is expecting to find the config file at `/etc/loki/local-config.yaml`


### IMPORTANT: boltdb-shipper upgrade considerations.

Significant changes have taken place between 1.6.0 and 2.0.0 for boltdb-shipper index type, if you are already running this index and are upgrading some extra caution is warranted.

Please strongly consider taking a complete backup of the `index` directory in your object store, this location might be slightly different depending on what store you use.
It should be a folder named index with a bunch of folders inside with names like `index_18561`,`index_18560`...

The chunks directory should not need any special backups.

If you have an environment to test this in please do so before upgrading against critical data.

There are 2 significant changes warranting the backup of this data because they will make rolling back impossible:
* A compactor is included which will take existing index files and compact them to one per day and remove non compacted files
* All index files are now gzipped before uploading

The second part is important because 1.6.0 does not understand how to read the gzipped files, so any new files uploaded or any files compacted become unreadable to 1.6.0 or earlier.

_THIS BEING SAID_ we are not expecting problems, our testing so far has not uncovered any problems, but some extra precaution might save data loss in unforeseen circumstances!

Please report any problems via GitHub issues or reach us on the #loki slack channel.

**Note if are using boltdb-shipper and were running with high availability and separate filesystems**

This was a poorly documented and even more experimental mode we toyed with using boltdb-shipper. For now we removed the documentation and also any kind of support for this mode.

To use boltdb-shipper in 2.0 you need a shared storage (S3, GCS, etc), the mode of running with separate filesystem stores in HA using a ring is not officially supported.

We didn't do anything explicitly to limit this functionality however we have not had any time to actually test this which is why we removed the docs and are listing it as not supported.

#### If running in microservices, deploy ingesters before queriers

Ingesters now expose a new RPC method that queriers use when the index type is `boltdb-shipper`.
Queriers generally roll out faster than ingesters, so if new queriers query older ingesters using the new RPC, the queries would fail.
To avoid any query downtime during the upgrade, rollout ingesters before queriers.

#### If running the compactor, ensure it has delete permissions for the object storage.

The compactor is an optional but suggested component that combines and deduplicates the boltdb-shipper index files. When compacting index files, the compactor writes a new file and deletes unoptimized files. Ensure that the compactor has appropriate permissions for deleting files, for example, s3:DeleteObject permission for AWS S3.

### IMPORTANT: `results_cache.max_freshness` removed from YAML config

The `max_freshness` config from `results_cache` has been removed in favour of another flag called `max_cache_freshness_per_query` in `limits_config` which has the same effect.
If you happen to have `results_cache.max_freshness` set please use `limits_config.max_cache_freshness_per_query` YAML config instead.

### Promtail config removed

The long deprecated `entry_parser` config in Promtail has been removed, use [pipeline_stages]({{< relref "../clients/promtail/configuration/#pipeline_stages" >}}) instead. 

### Upgrading schema to use boltdb-shipper and/or v11 schema

If you would also like to take advantage of the new Single Store (boltdb-shipper) index, as well as the v11 schema if you aren't already using it.

You can do this by adding a new schema entry.

Here is an example:

```yaml
schema_config:
  configs:
    - from: 2018-04-15           ①
      store: boltdb              ①④
      object_store: filesystem   ①④
      schema: v11                ②
      index:
        prefix: index_           ①
        period: 168h             ①
    - from: 2020-10-24           ③
      store: boltdb-shipper
      object_store: filesystem   ④
      schema: v11
      index:
        prefix: index_
        period: 24h              ⑤
```
① Make sure all of these match your current schema config  
② Make sure this matches your previous schema version, Helm for example is likely v9  
③ Make sure this is a date in the **FUTURE** keep in mind Loki only knows UTC so make sure it's a future UTC date  
④ Make sure this matches your existing config (e.g. maybe you were using gcs for your object_store)  
⑤ 24h is required for boltdb-shipper  
 
There are more examples on the [Storage description page]({{< relref "../storage/_index.md#examples" >}}) including the information you need to setup the `storage` section for boltdb-shipper.


## 1.6.0

### Important: Ksonnet port changed and removed NET_BIND_SERVICE capability from Docker image

In 1.5.0 we changed the Loki user to not run as root which created problems binding to port 80.
To address this we updated the docker image to add the NET_BIND_SERVICE capability to the loki process
which allowed Loki to bind to port 80 as a non root user, so long as the underlying system allowed that 
linux capability.

This has proved to be a problem for many reasons and in PR [2294](https://github.com/grafana/loki/pull/2294/files)
the capability was removed.

It is now no longer possible for the Loki to be started with a port less than 1024 with the published docker image.

The default for Helm has always been port 3100, and Helm users should be unaffect unless they changed the default.

**Ksonnet users however should closely check their configuration, in PR 2294 the loki port was changed from 80 to 3100**


### IMPORTANT: If you run Loki in microservices mode, special rollout instructions

A new ingester GRPC API has been added allowing to speed up metric queries, to ensure a rollout without query errors **make sure you upgrade all ingesters first.**
Once this is done you can then proceed with the rest of the deployment, this is to ensure that queriers won't look for an API not yet available.

If you roll out everything at once, queriers with this new code will attempt to query ingesters which may not have the new method on the API and queries will fail.

This will only affect reads(queries) and not writes and only for the duration of the rollout.

### IMPORTANT: Scrape config changes to both Helm and Ksonnet will affect labels created by Promtail

PR [2091](https://github.com/grafana/loki/pull/2091) Makes several changes to the Promtail scrape config:

````
This is triggered by https://github.com/grafana/jsonnet-libs/pull/261

The above PR changes the instance label to be actually unique within
a scrape config. It also adds a pod and a container target label
so that metrics can easily be joined with metrics from cAdvisor, KSM,
and the Kubelet.

This commit adds the same to the Loki scrape config. It also removes
the container_name label. It is the same as the container label
and was already added to Loki previously. However, the
container_name label is deprecated and has disappeared in K8s 1.16,
so that it will soon become useless for direct joining.
````

TL;DR

The following label have been changed in both the Helm and Ksonnet Promtail scrape configs:

`instance` -> `pod`  
`container_name` -> `container`


### Experimental boltdb-shipper changes

PR [2166](https://github.com/grafana/loki/pull/2166) now forces the index to have a period of exactly `24h`:

Loki will fail to start with an error if the active schema or upcoming schema are not set to a period of `24h`

You can add a new schema config like this:

```yaml
schema_config:
  configs:
    - from: 2020-01-01      <----- This is your current entry, date will be different
      store: boltdb-shipper
      object_store: aws
      schema: v11
      index:
        prefix: index_
        period: 168h
    - from: [INSERT FUTURE DATE HERE]   <----- Add another entry, set a future date
      store: boltdb-shipper
      object_store: aws
      schema: v11
      index:
        prefix: index_
        period: 24h   <--- This must be 24h
```
If you are not on `schema: v11` this would be a good opportunity to make that change _in the new schema config_ also.

**NOTE** If the current time in your timezone is after midnight UTC already, set the date one additional day forward.

There was also a significant overhaul to how boltdb-shipper internals, this should not be visible to a user but as this 
feature is experimental and under development bug are possible!

The most noticeable change if you look in the storage, Loki no longer updates an existing file and instead creates a
new index file every 15mins, this is an important move to make sure objects in the object store are immutable and
will simplify future operations like compaction and deletion.

### Breaking CLI flags changes

The following CLI flags where changed to improve consistency, they are not expected to be widely used

```diff
- querier.query_timeout
+ querier.query-timeout

- distributor.extra-query-delay
+ querier.extra-query-delay

- max-chunk-batch-size
+ store.max-chunk-batch-size

- ingester.concurrent-flushed
+ ingester.concurrent-flushes
```

### Loki Canary metric name changes

When adding some new features to the canary we realized the existing metrics were not compliant with standards for counter names, the following metrics have been renamed:

```nohighlight
loki_canary_total_entries               ->      loki_canary_entries_total
loki_canary_out_of_order_entries        ->      loki_canary_out_of_order_entries_total
loki_canary_websocket_missing_entries   ->      loki_canary_websocket_missing_entries_total
loki_canary_missing_entries             ->      loki_canary_missing_entries_total
loki_canary_unexpected_entries          ->      loki_canary_unexpected_entries_total
loki_canary_duplicate_entries           ->      loki_canary_duplicate_entries_total
loki_canary_ws_reconnects               ->      loki_canary_ws_reconnects_total
loki_canary_response_latency            ->      loki_canary_response_latency_seconds
```

### Ksonnet Changes

In `production/ksonnet/loki/config.libsonnet` the variable `storage_backend` used to have a default value of `'bigtable,gcs'`.
This has been changed to providing no default and will error if not supplied in your environment jsonnet, 
here is an example of what you should add to have the same behavior as the default (namespace and cluster should already be defined):

```jsonnet
_config+:: {
    namespace: 'loki-dev',
    cluster: 'us-central1',
    storage_backend: 'gcs,bigtable',
```

Defaulting to `gcs,bigtable` was confusing for anyone using ksonnet with other storage backends as it would manifest itself with obscure bigtable errors.

## 1.5.0

Note: The required upgrade path outlined for version 1.4.0 below is still true for moving to 1.5.0 from any release older than 1.4.0 (e.g. 1.3.0->1.5.0 needs to also look at the 1.4.0 upgrade requirements).

### Breaking config changes!

Loki 1.5.0 vendors Cortex v1.0.0 (congratulations!), which has a [massive list of changes](https://cortexmetrics.io/docs/changelog/#1-0-0-2020-04-02).

While changes in the command line flags affect Loki as well, we usually recommend people to use configuration file instead.

Cortex has done lot of cleanup in the configuration files, and you are strongly urged to take a look at the [annotated diff for config file](https://cortexmetrics.io/docs/changelog/#config-file-breaking-changes) before upgrading to Loki 1.5.0.

Following fields were removed from YAML configuration completely: `claim_on_rollout` (always true), `normalise_tokens` (always true).

#### Test Your Config

To see if your config needs to change, one way to quickly test is to download a 1.5.0 (or newer) binary from the [release page](https://github.com/grafana/loki/releases/tag/v1.5.0)

Then run the binary providing your config file `./loki-linux-amd64 -config.file=myconfig.yaml`

If there are configs which are no longer valid you will see errors immediately:

```shell
./loki-linux-amd64 -config.file=loki-local-config.yaml
failed parsing config: loki-local-config.yaml: yaml: unmarshal errors:
  line 35: field dynamodbconfig not found in type aws.StorageConfig
```

Referencing the [list of diffs](https://cortexmetrics.io/docs/changelog/#config-file-breaking-changes) I can see this config changed:

```diff
-  dynamodbconfig:
+  dynamodb:
```

Also several other AWS related configs changed and would need to udpate those as well.


### Loki Docker Image User and File Location Changes

To improve security concerns, in 1.5.0 the Docker container no longer runs the loki process as `root` and instead the process runs as user `loki` with UID `10001` and GID `10001`

This may affect people in a couple ways:

#### Loki Port

If you are running Loki with a config that opens a port number above 1024 (which is the default, 3100 for HTTP and 9095 for GRPC) everything should work fine in regards to ports.

If you are running Loki with a config that opens a port number less than 1024 Linux normally requires root permissions to do this, HOWEVER in the Docker container we run `setcap cap_net_bind_service=+ep /usr/bin/loki`

This capability lets the loki process bind to a port less than 1024 when run as a non root user.

Not every environment will allow this capability however, it's possible to restrict this capability in linux.  If this restriction is in place, you will be forced to run Loki with a config that has HTTP and GRPC ports above 1024.

#### Filesystem

**Please note the location Loki is looking for files with the provided config in the docker image has changed**

In 1.4.0 and earlier the included config file in the docker container was using directories:

```
/tmp/loki/index
/tmp/loki/chunks
```

In 1.5.0 this has changed:

```
/loki/index
/loki/chunks
```

This will mostly affect anyone using docker-compose or docker to run Loki and are specifying a volume to persist storage.

**There are two concerns to track here, one is the correct ownership of the files and the other is making sure your mounts updated to the new location.**

One possible upgrade path would look like this:

If I were running Loki with this command `docker run -d --name=loki --mount source=loki-data,target=/tmp/loki -p 3100:3100 grafana/loki:1.4.0`

This would mount a docker volume named `loki-data` to the `/tmp/loki` folder which is where Loki will persist the `index` and `chunks` folder in 1.4.0

To move to 1.5.0 I can do the following (please note that your container names and paths and volumes etc may be different):

```
docker stop loki
docker rm loki
docker run --rm --name="loki-perm" -it --mount source=loki-data,target=/mnt ubuntu /bin/bash
cd /mnt
chown -R 10001:10001 ./*
exit
docker run -d --name=loki --mount source=loki-data,target=/loki -p 3100:3100 grafana/loki:1.5.0
```

Notice the change in the `target=/loki` for 1.5.0 to the new data directory location specified in the [included Loki config file](https://github.com/grafana/loki/tree/master/cmd/loki/loki-docker-config.yaml).

The intermediate step of using an ubuntu image to change the ownership of the Loki files to the new user might not be necessary if you can easily access these files to run the `chown` command directly.
That is if you have access to `/var/lib/docker/volumes` or if you mounted to a different local filesystem directory, you can change the ownership directly without using a container.


### Loki Duration Configs

If you get an error like:

```nohighlight
 ./loki-linux-amd64-1.5.0 -log.level=debug -config.file=/etc/loki/config.yml
failed parsing config: /etc/loki/config.yml: not a valid duration string: "0"
```

This is because of some underlying changes that no longer allow durations without a unit.

Unfortunately the yaml parser doesn't give a line number but it's likely to be one of these two:

```yaml
chunk_store_config:
  max_look_back_period: 0s # DURATION VALUES MUST HAVE A UNIT EVEN IF THEY ARE ZERO

table_manager:
  retention_deletes_enabled: false
  retention_period: 0s # DURATION VALUES MUST HAVE A UNIT EVEN IF THEY ARE ZERO
```

### Promtail Config Changes

The underlying backoff library used in Promtail had a config change which wasn't originally noted in the release notes:

If you get this error:

```nohighlight
Unable to parse config: /etc/promtail/promtail.yaml: yaml: unmarshal errors:
  line 3: field maxbackoff not found in type util.BackoffConfig
  line 4: field maxretries not found in type util.BackoffConfig
  line 5: field minbackoff not found in type util.BackoffConfig
```

The new values are:

```yaml
min_period:
max_period:
max_retries:
```

## 1.4.0

Loki 1.4.0 vendors Cortex v0.7.0-rc.0 which contains [several breaking config changes](https://github.com/cortexproject/cortex/blob/v0.7.0-rc.0/CHANGELOG).

One such config change which will affect Loki users:

In the [cache_config](../../configuration#cache_config):

`defaul_validity` has changed to `default_validity`

Also in the unlikely case you were configuring your schema via arguments and not a config file, this is no longer supported.  This is not something we had ever provided as an option via docs and is unlikely anyone is doing, but worth mentioning.

The other config changes should not be relevant to Loki.

### Required Upgrade Path

The newly vendored version of Cortex removes code related to de-normalized tokens in the ring. What you need to know is this:

*Note:* A "shared ring" as mentioned below refers to using *consul* or *etcd* for values in the following config:

```yaml
kvstore:
  # The backend storage to use for the ring. Supported values are
  # consul, etcd, inmemory
  store: <string>
```

- Running without using a shared ring (inmemory): No action required
- Running with a shared ring and upgrading from v1.3.0 -> v1.4.0: No action required
- Running with a shared ring and upgrading from any version less than v1.3.0 (e.g. v1.2.0) -> v1.4.0: **ACTION REQUIRED**

There are two options for upgrade if you are not on version 1.3.0 and are using a shared ring:

- Upgrade first to v1.3.0 **BEFORE** upgrading to v1.4.0

OR

**Note:** If you are running a single binary you only need to add this flag to your single binary command.

1. Add the following configuration to your ingesters command: `-ingester.normalise-tokens=true`
1. Restart your ingesters with this config
1. Proceed with upgrading to v1.4.0
1. Remove the config option (only do this after everything is running v1.4.0)

**Note:** It's also possible to enable this flag via config file, see the [`lifecycler_config`](https://github.com/grafana/loki/tree/v1.3.0/docs/configuration#lifecycler_config) configuration option.

If using the Helm Loki chart:

```yaml
extraArgs:
  ingester.normalise-tokens: true
```

If using the Helm Loki-Stack chart:

```yaml
loki:
  extraArgs:
    ingester.normalise-tokens: true
```

#### What will go wrong

If you attempt to add a v1.4.0 ingester to a ring created by Loki v1.2.0 or older which does not have the commandline argument `-ingester.normalise-tokens=true` (or configured via [config file](https://github.com/grafana/loki/tree/v1.3.0/docs/configuration#lifecycler_config)), the v1.4.0 ingester will remove all the entries in the ring for all the other ingesters as it cannot "see" them.

This will result in distributors failing to write and a general ingestion failure for the system.

If this happens to you, you will want to rollback your deployment immediately. You need to remove the v1.4.0 ingester from the ring ASAP, this should allow the existing ingesters to re-insert their tokens.  You will also want to remove any v1.4.0 distributors as they will not understand the old ring either and will fail to send traffic.
