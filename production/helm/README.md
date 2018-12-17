# Deploy Loki to Kubernetes with Helm

## Prerequisites

Make sure you have the helm configure on your cluster:

```
$ helm init
```

Clone `grafana/loki` repository and navigate to `production helm` directory:

```
$ git clone https://github.com/grafana/loki.git
$ cd loki/production/helm
```

## Deploying Loki and Promtail to your cluster.

```
$ helm install . -n loki --namespace <YOUR-NAMESPACE>
```
