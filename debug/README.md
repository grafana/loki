## Debug images

To build debug images run

```shell
make debug
```

You can use the `docker-compose.yaml` in this directory to launch the debug versions of the image in docker


## Debug in kubernetes

Refer to [ksonnet](../production/ksonnet/README.md) to deploy, you can set `log.level` as `debug` to see more log.