# Installing Loki with Helm

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

## Deploy Loki to your cluster

### Deploy with default config

```bash
$ helm upgrade --install loki loki/loki-stack
```

### Deploy in a custom namespace

```bash
$ helm upgrade --install loki --namespace=loki loki/loki
```

### Deploy with custom config

```bash
$ helm upgrade --install loki loki/loki --set "key1=val1,key2=val2,..."
```

### Deploy Loki Stack (Loki, Promtail, Grafana, Prometheus)

```bash
$ helm upgrade --install loki loki/loki-stack  --set grafana.enabled=true,prometheus.enabled=true,prometheus.alertmanager.persistentVolume.enabled=false,prometheus.server.persistentVolume.enabled=false
```

### Deploy Loki Stack (Loki, Fluent Bit, Grafana, Prometheus)

```bash
$ helm upgrade --install loki loki/loki-stack \
    --set fluent-bit.enabled=true,promtail.enabled=false,grafana.enabled=true,prometheus.enabled=true,prometheus.alertmanager.persistentVolume.enabled=false,prometheus.server.persistentVolume.enabled=false
```

## Deploy Grafana to your cluster

To install Grafana on your cluster with Helm, use the following command:

```bash
$ helm install stable/grafana -n loki-grafana
```

To get the admin password for the Grafana pod, run the following command:

```bash
$ kubectl get secret --namespace <YOUR-NAMESPACE> loki-grafana -o jsonpath="{.data.admin-password}" | base64 --decode ; echo
```

To access the Grafana UI, run the following command:

```bash
$ kubectl port-forward --namespace <YOUR-NAMESPACE> service/loki-grafana 3000:80
```

Navigate to `http://localhost:3000` and login with `admin` and the password
output above. Then follow the [instructions for adding the Loki Data Source](../getting-started/grafana.md), using the URL
`http://loki:3100/` for Loki.

## Run Loki behind HTTPS ingress

If Loki and Promtail are deployed on different clusters you can add an Ingress
in front of Loki. By adding a certificate you create an HTTPS endpoint. For
extra security you can also enable Basic Authentication on the Ingress.

In Promtail, set the following values to communicate using HTTPS and basic
authentication:

```yaml
loki:
  serviceScheme: https
  user: user
  password: pass
```

Sample Helm template for Ingress:

```yaml
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
