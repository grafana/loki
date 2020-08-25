---
title: Tanka
---
# Install Loki with Tanka

[Tanka](https://tanka.dev) is a reimplementation of
[Ksonnet](https://ksonnet.io) that Grafana Labs created after Ksonnet was
deprecated. Tanka is used by Grafana Labs to run Loki in production.

## Prerequisites

Install the latest version of Tanka (at least version v0.5.0) for the `tk env`
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

Download and install the Loki and Promtail module using `jb`:

```bash
go get -u github.com/jsonnet-bundler/jsonnet-bundler/cmd/jb
jb init  # not required if you already ran `tk init`
jb install github.com/grafana/loki/production/ksonnet/loki
jb install github.com/grafana/loki/production/ksonnet/promtail
```

> **Note:** As of 2020-08-13, we use some features that are not yet generally available. This step will be unnecessary in future Tanka releases. For now, install this library, which we'll use as an override:

```bash
jb install github.com/jsonnet-libs/k8s-alpha/1.14
```

Next, override the `lib/k.libsonnet` with the following

```jsonnet
(import 'github.com/jsonnet-libs/k8s-alpha/1.14/main.libsonnet')
+ (import 'github.com/jsonnet-libs/k8s-alpha/1.14/extensions/kausal-shim.libsonnet')
```

Be sure to replace the username, password, and the relevant `htpasswd` contents.
Making sure to set the value for username, password, and `htpasswd` properly,
replace the contents of `environments/loki/main.jsonnet` with:

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

Notice that `container_root_path` is your own data root for the Docker Daemon.
Run `docker info | grep "Root Dir"` to get the root path.

Run `tk show environments/loki` to see the manifests that will be deployed to
the cluster. Run `tk apply environments/loki` to deploy the manifests.
