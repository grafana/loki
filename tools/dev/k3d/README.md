# Deploy Loki to k3d for Local Development

## Pre-requisites

In order to use the make targets in this directory, make sure you have the following tools installed:

- [kubectl](https://kubernetes.io/docs/tasks/tools/)
- [k3d](https://k3d.io/v4.4.8/)
- [tanka](https://github.com/grafana/tanka)
- [jsonnet](https://jsonnet.org/)
- [jq](https://stedolan.github.io/jq/)
- [helm](https://helm.sh/docs/intro/install/) >= 3.9

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

## Helm

The `helm-cluster` environment is designed for spinning up a cluster with just Grafana and Prometheus Operator that can be `helm installed` into. First spin up the cluster, then run the `make` targets for installing the desired configuration.

### Enterprise Logs

1. `make helm-cluster`
1. `make helm-install-enterprise-logs`

   This step will take a while. The `provisioner` is dependent on the `tokengen` job and for the Admin API to be in a healthy state. Be patient and it will evenutally complete. Once the `provisioner` job has completed, the Loki Canaries will come online, and Grafana will start, as both are waiting on secrets provisioned by the `provisioner`.
