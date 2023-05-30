# Empty Helm Cluster

This is a cluster designed for testing out helm charts. It is meant for running `helm install` against. It provides the required dependencies of the helm chart and nothing else (ie. minio is installed by the helm chart). This differs from the other envs that, while also using the helm chart, use `tanka` to deploy those charts.

## Using

From the `k3d` directory, run the following commands

```bash
make helm-cluster
make helm-install-enterprise-logs
```
