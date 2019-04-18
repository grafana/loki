# Loki Helm Chart

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

```bash
$ helm upgrade --install loki loki/loki-stack
```

## Deploy Loki only

```bash
$ helm upgrade --install loki loki/loki --set "loki.serviceName=my-loki"
```

## Deploy Promtail only

```bash
$ helm upgrade --install promtail loki/promtail
```

## Deploy Grafana to your cluster

To install Grafana on your cluster with helm, use the following command:

```bash
$ helm install stable/grafana -n loki-grafana
```

To get the admin password for the Grafana pod, run the following command:

```bash
$  kubectl get secret --namespace <YOUR-NAMESPACE> loki-grafana -o jsonpath="{.data.admin-password}" | base64 --decode ; echo
```

To access the Grafana UI, run the following command:

```bash
$ kubectl port-forward --namespace <YOUR-NAMESPACE> service/loki-grafana 3000:80
```

Navigate to http://localhost:3000 and login with `admin` and the password output above.
Then follow the [instructions for adding the loki datasource](/docs/usage.md), using the URL `http://loki:3100/`.

## How to contribute

If you want to add any feature to helm chart, you can follow as below:

```bash
$ cd production/helm
$ # do some changes to loki/promtail in corresponding directory
$ helm dependency update loki-stack
$ helm install ./loki-stack --dry-run --debug # to see changes format as expected
$ helm install ./loki-stack # to see changes work as expected
```

After verify changes, need to bump chart version.
For example, if you update loki chart, you need bump version as following:

```bash
$ # update version loki/Chart.yaml
$ # update version loki-stack/Chart.yaml
```
