# Upgrading Loki

Every attempt is made to keep Loki backwards compatible, such that upgrades should be low risk and low friction.

Unfortunately Loki is software and software is hard and sometimes things are not as easy as we want them to be.

On this page we will document any upgrade issues/gotchas/considerations we are aware of.


## Master / Unreleased

Configuration document has been re-orderd a bit and for all the config, corresponding `CLI` flag is
provided. 

S3 config now supports exapnded config. Example can be found here [s3_expanded_config](../configuration/examples.md#s3-expanded-config)

### New Ingester GRPC API special rollout procedure in microservices mode

A new ingester GRPC API has been added allowing to speed up metric queries, to ensure a rollout without query errors make sure you upgrade all ingesters first.
Once this is done you can then proceed with the rest of the deployment, this is to ensure that queriers won't look for an API not yet available.

If you roll out everything at once, queriers with this new code will attempt to query ingesters which may not have the new method on the API and queries will fail.

This will only affect reads(queries) and not writes and only for the duration of the rollout.

### Breaking CLI flags changes

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
```

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

This would mount a docker volume named `loki-data` to the `/temp/loki` folder which is where Loki will persist the `index` and `chunks` folder in 1.4.0

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

Notice the change in the `target=/loki` for 1.5.0 to the new data directory location specified in the [included Loki config file](../../cmd/loki/loki-docker-config.yaml).

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

The underlying backoff library used in promtail had a config change which wasn't originally noted in the release notes:

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

Loki 1.4.0 vendors Cortex v0.7.0-rc.0 which contains [several breaking config changes](https://github.com/cortexproject/cortex/blob/v0.7.0-rc.0/CHANGELOG.md).

One such config change which will affect Loki users:

In the [cache_config](../configuration/README.md#cache_config):

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

* Running without using a shared ring (inmemory): No action required
* Running with a shared ring and upgrading from v1.3.0 -> v1.4.0: No action required
* Running with a shared ring and upgrading from any version less than v1.3.0 (e.g. v1.2.0) -> v1.4.0: **ACTION REQUIRED**

There are two options for upgrade if you are not on version 1.3.0 and are using a shared ring:

* Upgrade first to v1.3.0 **BEFORE** upgrading to v1.4.0

OR

**Note:** If you are running a single binary you only need to add this flag to your single binary command.

1. Add the following configuration to your ingesters command: `-ingester.normalise-tokens=true`
1. Restart your ingesters with this config
1. Proceed with upgrading to v1.4.0
1. Remove the config option (only do this after everything is running v1.4.0)

**Note:** It's also possible to enable this flag via config file, see the [docs](https://github.com/grafana/loki/tree/v1.3.0/docs/configuration#lifecycler_config)

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
