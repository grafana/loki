# Installing Loki with Ksonnet

## Prerequisites

Make sure you have Ksonnet v0.8.0:

```
$ brew install https://raw.githubusercontent.com/ksonnet/homebrew-tap/82ef24cb7b454d1857db40e38671426c18cd8820/ks.rb
$ brew pin ks
$ ks version
ksonnet version: v0.8.0
jsonnet version: v0.9.5
client-go version: v1.6.8-beta.0+$Format:%h$
```

In your config repo, if you don't have a ksonnet application, make a new one
(copies the credentials from your current kubectl context):

```
$ ks init <application name>
$ cd <application name>
$ ks env add loki --namespace=loki
```

## Deploying

Grab the Loki module using jb:

```bash
$ go get -u github.com/jsonnet-bundler/jsonnet-bundler/cmd/jb
$ jb init
$ jb install github.com/grafana/loki/production/ksonnet/loki
```

Be sure to replace the username, password and the relevant htpasswd contents.
Making sure to set the value for username, password, and htpasswd properly,
replace the contents of `environments/loki/main.jsonnet` with:

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

Notice that `container_root_path` is your own data root for the Docker Daemon,
run `docker info | grep "Root Dir"` to get it.

Run `ks show loki` to see the manifests that will be deployed to the cluster and
finally run `ks apply loki` to deploy it.
