---
title: Helm
---
# Install Loki with Helm

## Prerequisites

Make sure you have Helm [installed](https://helm.sh/docs/using_helm/#installing-helm).

Add [Loki's chart repository](https://github.com/grafana/helm-charts) to Helm:

> **PLEASE NOTE** On 2020/12/11 Loki's Helm charts were moved from their initial location within the
Loki repo and hosted at https://grafana.github.io/loki/charts to their new location at https://github.com/grafana/helm-charts which are hosted at https://grafana.github.io/helm-charts

```bash
helm repo add grafana https://grafana.github.io/helm-charts
```

To update the chart repository, run:

```bash
helm repo update
```

## Deploy Loki to your cluster

### Deploy with default config

```bash
helm upgrade --install loki grafana/loki-stack
```

### Deploy in a custom namespace

```bash
helm upgrade --install loki --namespace=loki grafana/loki
```

### Deploy with custom config

```bash
helm upgrade --install loki grafana/loki --set "key1=val1,key2=val2,..."
```

### Deploy Loki Stack (Loki, Promtail, Grafana, Prometheus)

```bash
helm upgrade --install loki grafana/loki-stack  --set grafana.enabled=true,prometheus.enabled=true,prometheus.alertmanager.persistentVolume.enabled=false,prometheus.server.persistentVolume.enabled=false
```

### Deploy Loki Stack (Loki, Promtail, Grafana, Prometheus) with persistent volume claim

```bash
helm upgrade --install loki grafana/loki-stack  --set grafana.enabled=true,prometheus.enabled=true,prometheus.alertmanager.persistentVolume.enabled=false,prometheus.server.persistentVolume.enabled=false,loki.persistence.enabled=true,loki.persistence.storageClassName=standard,loki.persistence.size=5Gi
```

### Deploy Loki Stack (Loki, Fluent Bit, Grafana, Prometheus)

```bash
helm upgrade --install loki grafana/loki-stack \
  --set fluent-bit.enabled=true,promtail.enabled=false,grafana.enabled=true,prometheus.enabled=true,prometheus.alertmanager.persistentVolume.enabled=false,prometheus.server.persistentVolume.enabled=false
```

## Deploy Grafana to your cluster

To install Grafana on your cluster with Helm, use the following command:

```bash
helm install loki-grafana grafana/grafana
```

To get the admin password for the Grafana pod, run the following command:

```bash
kubectl get secret --namespace <YOUR-NAMESPACE> loki-grafana -o jsonpath="{.data.admin-password}" | base64 --decode ; echo
```

To access the Grafana UI, run the following command:

```bash
kubectl port-forward --namespace <YOUR-NAMESPACE> service/loki-grafana 3000:80
```

Navigate to `http://localhost:3000` and login with `admin` and the password
output above. Then follow the [instructions for adding the Loki Data Source](../../getting-started/grafana/), using the URL
`http://loki:3100/` for Loki.

## Run Loki behind HTTPS ingress

If Loki and Promtail are deployed on different clusters you can add an Ingress
in front of Loki. By adding a certificate you create an HTTPS endpoint. For
extra security you can also enable Basic Authentication on the Ingress.

In Promtail, set the following values to communicate using HTTPS and basic authentication:

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

## Run promtail with syslog support

In order to receive and process syslog message into promtail, the following changes will be necessary:

* Review the [promtail syslog-receiver configuration documentation](/docs/clients/promtail/scraping.md#syslog-receiver)

* Configure the promtail helm chart with the syslog configuration added to the `extraScrapeConfigs` section and associated service definition to listen for syslog messages. For example:

```yaml
extraScrapeConfigs:
  - job_name: syslog
    syslog:
      listen_address: 0.0.0.0:1514
      labels:
        job: "syslog"
  relabel_configs:
    - source_labels: ['__syslog_message_hostname']
      target_label: 'host'
syslogService:
  enabled: true
  type: LoadBalancer
  port: 1514
```

## Run promtail with systemd-journal support

In order to receive and process syslog message into promtail, the following changes will be necessary:

* Review the [promtail systemd-journal configuration documentation](/docs/clients/promtail/scraping.md#journal-scraping-linux-only)

* Configure the promtail helm chart with the systemd-journal configuration added to the `extraScrapeConfigs` section and volume mounts for the promtail pods to access the log files. For example:

```yaml
# Add additional scrape config
extraScrapeConfigs:
  - job_name: journal
    journal:
      path: /var/log/journal
      max_age: 12h
      labels:
        job: systemd-journal
    relabel_configs:
      - source_labels: ['__journal__systemd_unit']
        target_label: 'unit'
      - source_labels: ['__journal__hostname']
        target_label: 'hostname'

# Mount journal directory into promtail pods
extraVolumes:
  - name: journal
    hostPath:
      path: /var/log/journal

extraVolumeMounts:
  - name: journal
    mountPath: /var/log/journal
    readOnly: true
