## Debug images

To build debug images run

```shell
make debug
```

You can use the `docker-compose.yaml` in this directory to launch the debug versions of the image in docker


## Promtail in kubernetes

If you want to debug Promtail in kubernetes, I have done so with the ksonnet setup:

```shell
ks init promtail
cd promtail
ks env add promtail
jb init
jb install github.com/grafana/loki/production/ksonnet/promtail
vi environments/promtail/main.jsonnet
```

Replace the contents with:

```jsonnet
local promtail = import 'promtail/promtail.libsonnet';


promtail + {
  _images+:: {
    promtail: 'grafana/promtail-debug:latest',
  },
  _config+:: {
    namespace: 'default',

    promtail_config+: {
      external_labels+: {
        cluster: 'some_cluster_name',
      },
      scheme: 'https',
      hostname: 'hostname',
      username: 'username',
      password: 'password',
    },
  },
}
```

change the `some_cluster_name` to anything meaningful to help find your logs in Loki

also update the `hostname`, `username`, and `password` for your Loki instance.

## Loki in kubernetes

Haven't tried this yet, it works from docker-compose so it should run in kubernetes just fine also.