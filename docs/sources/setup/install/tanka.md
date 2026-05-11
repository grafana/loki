---
title: Tanka 
menuTitle: Install using Tanka
description: Describes how to install Loki using Tanka.
aliases: 
  - ../../installation/tanka/
weight: 300
---
# Tanka

[Tanka](https://tanka.dev) is a reimplementation of
[Ksonnet](https://ksonnet.io) that Grafana Labs created after Ksonnet was
deprecated. Tanka is used by Grafana Labs to run Grafana Loki in production.

The Tanka installation runs the Loki cluster in microservices mode.

{{< admonition type="note" >}}
Grafana Loki does not come with any included authentication layer. You must run an authenticating reverse proxy in front of your services to prevent unauthorized access to Loki (for example, nginx). Refer to [Manage authentication](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/authentication/) for a list of open-source reverse proxies you can use.
{{< /admonition >}}

## Prerequisites

Install the latest version of Tanka (version v0.31.0 or a more recent version) for the `tk env`
commands. Prebuilt binaries for Tanka can be found at the [Tanka releases
URL](https://github.com/grafana/tanka/releases).

In your config repo, if you don't have a Tanka application, create a folder and
call `tk init` inside of it. Then create an environment for Loki and provide the
URL for the Kubernetes API server to deploy to (e.g., `https://localhost:6443`):

```bash
mkdir <application name>
cd <application name>
tk init
tk env add environments/loki --namespace=loki --server=<Kubernetes API server>
```

Install `jsonnet-bundler` (`jb`), find instructions for your platform in Tanka's [installation docs](https://tanka.dev/install#jsonnet-bundler).

## Deploying

Download and install the Loki module using `jb` (version v0.6.0 or a more recent version):

```bash
jb init  # not required if you already ran `tk init`
jb install github.com/grafana/loki/production/ksonnet/loki@main
```

## Install a log-shipping agent

The ksonnet Promtail module has been removed and is no longer available. Use [Grafana Alloy](https://grafana.com/docs/alloy/latest/set-up/) to ship logs to Loki.

For installation and migration guidance, refer to:

- [Install Grafana Alloy](https://grafana.com/docs/alloy/latest/set-up/install/)
- [Install Alloy on Kubernetes](https://grafana.com/docs/alloy/latest/set-up/install/kubernetes/)
- [Migrate from Promtail to Alloy](https://grafana.com/docs/alloy/latest/set-up/migrate/from-promtail/)

Revise the YAML contents of `environments/loki/main.jsonnet`, updating these variables:

- Update the `username`, `password`, and the relevant `htpasswd` variable values.
- Update object storage variables for one backend (`s3`, `gcs`, or `azure`) and remove variables for backends that are not part of your setup. In this module, `storage_backend` must be a single value. Refer to [storage_config](https://grafana.com/docs/loki/<LOKI_VERSION>/configuration/#storage_config) for configuration details.
- Update the `from` value in the Loki `schema_config` section to no more than 14 days prior to the current date. The `from` date represents the first day for which the `schema_config` section is valid. For example, if today is `2021-01-15`, set `from` to `2021-01-01`. This recommendation is based on the Loki default acceptance of log lines up to 14 days in the past. The `reject_old_samples_max_age` configuration variable controls the acceptance range. Use TSDB (`store: tsdb`, `schema: v13`) for new installs.

```jsonnet
local gateway = import 'loki/gateway.libsonnet';
local loki = import 'loki/loki.libsonnet';

loki + gateway {
  _config+:: {
    namespace: 'loki',
    htpasswd_contents: 'loki:$apr1$H4yGiGNg$ssl5/NymaGFRUvxIV1Nyr.',

    // Use TSDB shipper
    using_tsdb_shipper: true

    // Pick one object storage backend: 's3', 'gcs', or 'azure'
    storage_backend: 's3',

    // S3 variables (used when storage_backend = 's3')
    s3_access_key: 'key',
    s3_secret_access_key: 'secret access key',
    s3_address: 'url',
    s3_bucket_name: 'loki-chunks',
    s3_bucket_region: 'us-east-1',
    s3_path_style: false,

    // GCS variables (used when storage_backend = 'gcs')
    // gcs_bucket_name: 'loki-chunks',

    // Azure variables (used when storage_backend = 'azure')
    // azure_container_name: 'loki-chunks',
    // azure_account_name: 'account',
    // azure_account_key: 'key',

    // TSDB index store configuration
    loki+: {
      schema_config: {
        configs: [{
          from: 'YYYY-MM-DD',
          store: 'tsdb',
          object_store: $._config.storage_backend,
          schema: 'v13',
          index: {
            prefix: '%s_index_' % $._config.table_prefix,
            period: '%dh' % $._config.index_period_hours,
          },
        }],
      },
    },

    replication_factor: 3,
    memberlist_ring_enabled: true,
  },
}
```

Run `tk show environments/loki` to see the manifests that will be deployed to
the cluster. Run `tk apply environments/loki` to deploy the manifests.
To delete the environment from cluster, run `tk delete environments/loki`.
