# Installing Loki with Tanka

[Tanka](https://tanka.dev) is a reimplementation of
[Ksonnet](https://ksonnet.io) that Grafana Labs created after Ksonnet was
deprecated. Tanka is used by Grafana Labs to run Loki in production.

## Prerequisites

Grab the latest version of Tanka (at least version v0.5.0) for the `tk env`
commands. Prebuilt binaries for Tanka can be found at the [Tanka releases
URL](https://github.com/grafana/tanka/releases).

In your config repo, if you don't have a Tanka application, create a folder and
call `tk init` inside of it. Then create an environment for Loki and provide the
URL for the Kubernetes API server to deploy to (e.g., `https://localhost:6443`):

```
$ mkdir <application name>
$ cd <application name>
$ tk init
$ tk env add environments/loki --namespace=loki --server=<Kubernetes API server>
```

## Deploying

Grab the Loki module using `jb`:

```bash
$ go get -u github.com/jsonnet-bundler/jsonnet-bundler/cmd/jb
$ jb install github.com/grafana/loki/production/ksonnet/loki
```

Be sure to replace the username, password and the relevant `htpasswd` contents.
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

Run `tk show environments/loki` to see the manifests that will be deployed to the cluster and
finally run `tk apply environments/loki` to deploy it.
