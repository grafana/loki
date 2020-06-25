# Loki-Stack Helm Chart

## Prerequisites

Make sure you have Helm [installed](https://helm.sh/docs/using_helm/#installing-helm) and
[deployed](https://helm.sh/docs/using_helm/#installing-tiller) to your cluster. Then add
Loki's chart repository to Helm:

```bash
$ helm repo add loki https://grafana.github.io/loki/charts
```

You can update the chart repository by running:

```bash
$ helm repo update
```

## Deploy Loki and Promtail to your cluster

### Deploy with default config

```bash
$ helm upgrade --install loki loki/loki-stack
```

### Deploy in a custom namespace

```bash
$ helm upgrade --install loki --namespace=loki-stack loki/loki-stack
```

### Deploy with custom config

```bash
$ helm upgrade --install loki loki/loki-stack --set "key1=val1,key2=val2,..."
```

## Deploy Loki and Fluent Bit to your cluster

```bash
$ helm upgrade --install loki loki/loki-stack \
    --set fluent-bit.enabled=true,promtail.enabled=false
```

## Deploy Grafana to your cluster

The chart loki-stack contains a pre-configured Grafana, simply use `--set grafana.enabled=true`

To get the admin password for the Grafana pod, run the following command:

```bash
$ kubectl get secret --namespace <YOUR-NAMESPACE> loki-grafana -o jsonpath="{.data.admin-password}" | base64 --decode ; echo
```

To access the Grafana UI, run the following command:

```bash
$ kubectl port-forward --namespace <YOUR-NAMESPACE> service/loki-grafana 3000:80
```

Navigate to http://localhost:3000 and login with `admin` and the password output above.
Then follow the [instructions for adding the loki datasource](/docs/getting-started/grafana.md), using the URL `http://loki:3100/`.

