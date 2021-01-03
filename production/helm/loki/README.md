# ⚠️  DEPRECATED - Loki Helm Chart

This chart was moved to <https://github.com/grafana/helm-charts>.

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

## Deploy Loki only

```bash
$ helm upgrade --install loki loki/loki
```

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

## Use Loki Alerting

You can add your own alerting rules with `alerting_groups` in `values.yaml`. This will create a ConfigMap with your rules and additional volumes and mounts for Loki.

This does **not** enable the Loki `ruler` component which does the evaluation of your rules. The `values.yaml` file does contain a simple example. For more details take a look at the official [alerting docs](https://grafana.com/docs/loki/latest/alerting/).