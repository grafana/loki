# Deploy Loki to Kubernetes

## Prerequisites

Make sure you have a recent version of [Tanka](https://github.com/grafana/tanka). Follow their [install instructions](https://tanka.dev/#getting-started) to do so. Make sure to install [jsonnet-bundler](https://github.com/jsonnet-bundler/jsonnet-bundler) as well.

```bash
# Verify it works
$ tk --version
tk version v0.5.0
```

In your config repo, if you don't yet have the directory structure of Tanka set up:

```bash
# create a directory (any name works)
$ mkdir config && cd config/
$ tk init
$ tk env add environments/loki --namespace=loki
$ tk env set environments/loki --server=https://${K8S_MASTER_ADDRESS}:6443
# Ksonnet kubernetes libraries
$ jb install github.com/ksonnet/ksonnet-lib/ksonnet.beta.3/k.libsonnet
$ jb install github.com/ksonnet/ksonnet-lib/ksonnet.beta.3/k8s.libsonnet
```

## Deploying Promtail to your cluster.

Grab the `promtail` module using jb:

```
$ jb install github.com/grafana/loki/production/ksonnet/promtail
```

Replace the contents of `environments/loki/main.jsonnet` with:
```jsonnet
local promtail = import 'promtail/promtail.libsonnet';

promtail + {
  _config+:: {
    namespace: 'loki',

    promtail_config+: {
      clients: [
        {
          scheme:: 'https',
          hostname:: 'logs-us-west1.grafana.net',
          username:: 'user-id',
          password:: 'password',
          external_labels: {},
        }
      ],
      container_root_path: '/var/lib/docker',
    },
  },
}

```
Notice that `container_root_path` is your own data root for docker daemon, use `docker info | grep "Root Dir"` to get it.

Now use `tk show environments/loki` to see the yaml, and `tk apply environments/loki` to apply it to the cluster.

## Deploying Loki to your cluster.

If you want to further also deploy the server to the cluster, then run the following to install the module:

```
$ jb install github.com/grafana/loki/production/ksonnet/loki
```
Be sure to replace the username, password and the relevant htpasswd contents.
Replace the contents of `environments/loki/main.jsonnet` with:

```jsonnet
local gateway = import 'loki/gateway.libsonnet';
local loki = import 'loki/loki.libsonnet';
local promtail = import 'promtail/promtail.libsonnet';

loki + promtail + gateway {
  _config+:: {
    namespace: 'loki',
    htpasswd_contents: 'loki:$apr1$H4yGiGNg$ssl5/NymaGFRUvxIV1Nyr.',


    promtail_config: {
      scheme: 'http',
      hostname: 'gateway.%(namespace)s.svc' % $._config,
      username: 'loki',
      password: 'password',
      container_root_path: '/var/lib/docker',
    },
    replication_factor: 3,
    consul_replicas: 1,
  },
}
```
Notice that `container_root_path` is your own data root for docker daemon, use `docker info | grep "Root Dir"` to get it.

Use `tk show environments/loki` to see the manifests being deployed to the cluster.
Finally `tk apply environments/loki` will deploy the server components to your cluster.
