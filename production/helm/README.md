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

## Deploy Loki only

```bash
$ helm upgrade --install loki loki/loki
```

## Deploy Promtail only

```bash
$ helm upgrade --install promtail loki/promtail --set "loki.serviceName=loki"
```

## Deploy Loki and Fluent Bit to your cluster

```bash
$ helm upgrade --install loki loki/loki-stack \
    --set fluent-bit.enabled=true,promtail.enabled=false
```

## Deploy Fluent Bit only

```bash
$ helm upgrade --install fluent-bit loki/fluent-bit \
    --set "loki.serviceName=loki.svc.cluster.local"
```

## Deploy Grafana to your cluster

To install Grafana on your cluster with helm, use the following command:

```bash
$ helm install stable/grafana -n loki-grafana
```

> The chart loki-stack contains a pre-configured Grafana, simply use `--set grafana.enabled=true`

To get the admin password for the Grafana pod, run the following command:

```bash
$  kubectl get secret --namespace <YOUR-NAMESPACE> loki-grafana -o jsonpath="{.data.admin-password}" | base64 --decode ; echo
```

To access the Grafana UI, run the following command:

```bash
$ kubectl port-forward --namespace <YOUR-NAMESPACE> service/loki-grafana 3000:80
```

Navigate to http://localhost:3000 and login with `admin` and the password output above.
Then follow the [instructions for adding the loki datasource](/docs/getting-started/grafana.md), using the URL `http://loki:3100/`.

## Run Loki behind https ingress

If Loki and Promtail are deployed on different clusters you can add an Ingress in front of Loki.
By adding a certificate you create an https endpoint. For extra security enable basic authentication on the Ingress.

In Promtail set the following values to communicate with https and basic auth

```
loki:
  serviceScheme: https
  user: user
  password: pass
```

Sample helm template for ingress:
```
apiVersion: extensions/v1beta1
kind: Ingress
metadata:
annotations:
    kubernetes.io/ingress.class: {{ .Values.ingress.class }}
    ingress.kubernetes.io/auth-type: "basic"
    ingress.kubernetes.io/auth-secret: {{ .Values.ingress.basic.secret }}
name: loki
spec:
rules:
- host: {{ .Values.ingress.host }}
    http:
    paths:
    - backend:
        serviceName: loki
        servicePort: 3100
tls:
- secretName: {{ .Values.ingress.cert }}
    hosts:
    - {{ .Values.ingress.host }}
```

## How to contribute

After adding your new feature to the appropriate chart, you can build and deploy it locally to test:

```bash
$ make helm
$ helm upgrade --install loki ./loki-stack-*.tgz
```

After verifying your changes, you need to bump the chart version following [semantic versioning](https://semver.org) rules.
For example, if you update the loki chart, you need to bump the versions as follows:

- Update version loki/Chart.yaml
- Update version loki-stack/Chart.yaml

You can use the `make helm-debug` to test and print out all chart templates. If you want to install helm (tiller) in your cluster use `make helm-install`, to install the current build in your Kubernetes cluster run `make helm-upgrade`.
