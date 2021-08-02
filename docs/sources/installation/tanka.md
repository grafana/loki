---
title: Tanka
---
# Install Loki with Tanka

[Tanka](https://tanka.dev) is a reimplementation of
[Ksonnet](https://ksonnet.io) that Grafana Labs created after Ksonnet was
deprecated. Tanka is used by Grafana Labs to run Loki in production.

## Prerequisites

Install the latest version of Tanka (at least version v0.17.1) for the `tk env`
commands. Prebuilt binaries for Tanka can be found at the [Tanka releases
URL](https://github.com/grafana/tanka/releases).

In your config repo, if you don't have a Tanka application, create a folder and
call `tk init` inside of it. Then create an environment for Loki and provide the
URL for the Kubernetes API server to deploy to (e.g., `https://localhost:6443`):

```
mkdir <application name>
cd <application name>
tk init
tk env add environments/loki --namespace=loki --server=<Kubernetes API server>
```

## Deploying

Download and install the Loki and Promtail module using `jb` (with jb version >= v0.4.0):

```bash
go get -u github.com/jsonnet-bundler/jsonnet-bundler/cmd/jb
jb init  # not required if you already ran `tk init`
jb install github.com/grafana/loki/production/ksonnet/loki@main
jb install github.com/grafana/loki/production/ksonnet/promtail@main
```

Next, replace the contents of `environments/loki/main.jsonnet` with the YAML below, making sure to replace or modify the following:
1. Update the `username`, `password`, and the relevant `htpasswd` contents
1. Update the s3 or gcs variables and `boltdb_shipper_shared_store` variable based on your choice of object storage backend. See here for the full [storage_config](https://grafana.com/docs/loki/latest/configuration/#storage_config). 
1. Remove the object storage variables that are not relevant for your setup (e.g., remove the s3 variables if you're using gcs)
1. Update the `container_root_path` value so it reflects your own data root for the Docker Daemon. Run `docker info | grep "Root Dir"` to get the root path.
1. Update the `schema_config` section so that `object-store` has your choice of object storage backend (e.g., s3, gcs)
1. Update the `from` value in the schema config. The `from` date represents the first day from which the schema config is valid. For new Loki installs, we suggest setting `from` to 14 days before today's date (e.g. if today is Jan 14 2021, set `from` to Jan 1 2021). This recommendation is based on the fact that Loki will by default accept log lines from up to 14 days in the past (controlled by the value of the "reject_old_samples_max_age" knob). 

```jsonnet
local gateway = import 'loki/gateway.libsonnet';
local loki = import 'loki/loki.libsonnet';
local promtail = import 'promtail/promtail.libsonnet';

loki + promtail + gateway {
  _config+:: {
    namespace: 'loki',
    htpasswd_contents: 'loki:$apr1$H4yGiGNg$ssl5/NymaGFRUvxIV1Nyr.',

    // S3 variables remove if not using aws
    storage_backend: 's3,dynamodb',
    s3_access_key: 'key',
    s3_secret_access_key: 'secret access key',
    s3_address: 'url',
    s3_bucket_name: 'loki-test',
    dynamodb_region: 'region',

    // GCS variables remove if not using gcs
    storage_backend: 'bigtable,gcs',
    bigtable_instance: 'instance',
    bigtable_project: 'project',
    gcs_bucket_name: 'bucket',

    //Set this variable based on the object storage backend you're using (e.g., s3 or gcs)
    boltdb_shipper_shared_store: 'my-object-storage-backend-name',

    //Update the object_store and from fields
    loki+: {
      schema_config: {
        configs: [{
          from: 'YYYY-MM-DD',
          store: 'boltdb-shipper',
          object_store: 'my-object-storage-backend-name',
          schema: 'v11',
          index: {
            prefix: '%s_index_' % $._config.table_prefix,
            period: '%dh' % $._config.index_period_hours,
          },
        }],
      },
    },

    //Update the container_root_path if necessary
    promtail_config+: {
      clients: [{
        scheme:: 'http',
        hostname:: 'gateway.%(namespace)s.svc' % $._config,
        username:: 'loki',
        password:: 'password',
        container_root_path:: '/var/lib/docker',
      }],
    },

    replication_factor: 3,
    consul_replicas: 1,
  },
}
```

Run `tk show environments/loki` to see the manifests that will be deployed to
the cluster. Run `tk apply environments/loki` to deploy the manifests.