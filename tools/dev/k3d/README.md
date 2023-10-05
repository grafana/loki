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

The installation step executes a set of jobs required for the enterprise-logs deployment:
1) The `tokengen` job generates an admin-api token, stores it in the object storage and creates a Kubernetes secret.
1) The `provisioner` job depends on the `tokengen` job to create the Kubernetes secret and on the Admin API to be in a healthy state.
The tokengen Kubernetes secret will be used to create the input admin resources via the Admin API.
Afterwards, a new Kubernetes secret for each newly generated token is created.
1) Both the Loki Canaries and Grafana depend on the secrets provisioned by the `provisioner`. 
Therefore, once the `provisioner` job is completed, these components will become online.
