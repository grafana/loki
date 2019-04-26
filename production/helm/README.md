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

### Deploy with custom config

```bash
$ helm upgrade --install loki loki/loki --set "key1=val1,key2=val2,..."
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

## Run Loki behind https ingress

If Loki and Promtail are deployed on different clusters you can add an Ingress in front of Loki.
By adding a certificate you create an https endpoint. For extra security enable basic authentication on the Ingress.

In promtail set the following values to communicate with https and basic auth

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
