# Deploy Loki to k3d for Local Development

## Pre-requisites 

In order to use the make targets in this directory, make sure you have the following tools installed:
* [kubectl](https://kubernetes.io/docs/tasks/tools/)
* [k3d](https://k3d.io/v4.4.8/)
* [tanka](https://github.com/grafana/tanka)
* [jsonnet](https://jsonnet.org/)
* [jq](https://stedolan.github.io/jq/)
* [helm](https://helm.sh/docs/intro/install/) >= 3.9

**Note**: in case Docker is unable to resolve the local k3d registry hostname, add the following entry to the `/etc/hosts` file:
```
127.0.0.1 k3d-grafana
```

## To Spin Up An Environment

Each environment has it's own make target. To bring up `loki-distributed`, for example, run:

```bash
make loki-distributed
```

## To tear Down An Environment

The `down` make target will tear down all environments.

```bash
make down
```

## To Use Custom Docker Image

1. Define the registry port `export REGISTRY_PORT=7000`
1. Build or pull the image.
2. Tag the image with the registry prepended `docker tag us.gcr.io/kubernetes-dev/enterprise-logs:main-4d37d3ea "k3d-grafana.localhost:${REGISTRY_PORT}/grafana/enterprise-logs:main-4d37d3ea"`
3. Push the image to the local registry `docker push "k3d-grafana.localhost:${REGISTRY_PORT}/grafana/enterprise-logs:main-4d37d3ea"`
4. Define the image in the Helm Chart values
   ```yaml
   enterprise:
   enabled: true
   version: main-4d37d3ea
   image:
     registry: k3d-grafana:7000
     repository: grafana/enterprise-logs
   ```
