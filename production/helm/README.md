# Deploy Loki to Kubernetes with Helm

## Prerequisites

Make sure you have the helm configure on your cluster:

```bash
$ helm init
```

Clone `grafana/loki` repository and navigate to `production helm` directory:

```bash
$ git clone https://github.com/grafana/loki.git
$ cd loki/production/helm
```

## Deploy Loki and Promtail to your cluster

```bash
$ helm install . -n loki --namespace <YOUR-NAMESPACE>
```

## Deploy Grafana to your cluster

To install Grafana on your cluster with helm, use the following command:

```bash
$ helm install stable/grafana -n loki-grafana -f grafana.yaml --namespace <YOUR-NAMESPACE>
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
