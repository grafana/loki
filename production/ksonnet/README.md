# Deploy Loki to Kubernetes

## Prerequisites

Make sure you have the ksonnet v0.8.0:

```
$ brew install https://raw.githubusercontent.com/ksonnet/homebrew-tap/82ef24cb7b454d1857db40e38671426c18cd8820/ks.rb
$ brew pin ks
$ ks version
ksonnet version: v0.8.0
jsonnet version: v0.9.5
client-go version: v1.6.8-beta.0+$Format:%h$
```

In your config repo, if you don't have a ksonnet application, make a new one (will copy credentials from current context):

```
$ ks init <application name>
$ cd <application name>
$ ks env add loki --namespace=loki
```

## Deploying Promtail to your cluster.

Grab the promtail module using jb:

```
$ go get -u github.com/jsonnet-bundler/jsonnet-bundler/cmd/jb
$ jb init
$ jb install github.com/grafana/loki/production/ksonnet/promtail
```

Replace the contents of `environments/loki/main.jsonnet` with:
```
local promtail = import 'promtail/promtail.libsonnet';


promtail + {
  _config+:: {
    namespace: 'loki',

    promtail_config: {
      scheme: 'https',
      hostname: 'logs-us-west1.grafana.net',
      username: 'user-id',
      password: 'password',
    },
  },
}
```

Then do `ks show loki` to see the manifests that'll be deployed to your cluster.
Apply them using `ks apply loki`.

## Deploying Loki to your cluster.

If you want to further also deploy the server to the cluster, then run the following to install the module:

```
jb install github.com/grafana/loki/production/ksonnet/loki
```

Be sure to replace the username, password and the relevant htpasswd contents.
Replace the contents of `environments/loki/main.jsonnet` with:

```
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
      password: 'password'
    },
    replication_factor: 3,
    consul_replicas: 1,
  },
}
```

Do `ks show loki` to see the manifests being deployed to the cluster.
Finally `ks apply loki` to deploy the server components to your cluster.
