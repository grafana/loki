# Upgrading Loki

Every attempt is made to keep Loki backwards compatible, such that upgrades should be low risk and low friction.

Unfortunately Loki is software and software is hard and sometimes things are not as easy as we want them to be.

On this page we will document any upgrade issues/gotchas/considerations we are aware of.

## 1.5.0

Loki 1.5.0 vendors Cortex v1.0.0 (congratulations!), which has a [massive list of changes](https://cortexmetrics.io/docs/changelog/#1-0-0-2020-04-02).

While changes in the command line flags affect Loki as well, we usually recommend people to use configuration file instead.

Cortex has done lot of cleanup in the configuration files, and you are strongly urged to take a look at the [annotated diff for config file](https://cortexmetrics.io/docs/changelog/#config-file-breaking-changes) before upgrading to Loki 1.5.0.

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

