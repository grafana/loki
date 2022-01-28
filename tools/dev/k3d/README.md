# Deploy Loki to k3d for Local Development

## Pre-requisites 

In order to use the make targets in this directory, make sure you have the following tools installed:
* [kubectl](https://kubernetes.io/docs/tasks/tools/)
* [k3d](https://k3d.io/v4.4.8/)
* [tanka](https://github.com/grafana/tanka)
* [jsonnet](https://jsonnet.org/)
* [jq](https://stedolan.github.io/jq/)

**Note**: in case docker is unable to resolve the local k3d registry hostname, add the following entry to the `/etc/hosts` file:
```
127.0.0.1 k3d-grafana
```

## Spinning Up An Environment

Each environment has it's own make target. To bring up `loki-distributed`, for example, run:

```bash
make loki-distributed
```

## Tearing Down An Environment

The `down` make target will tear down all environments.

```bash
make down
```
