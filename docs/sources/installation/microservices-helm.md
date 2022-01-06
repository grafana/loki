---
title: Microservices deployment with Helm
weight: 25
---

# Microservices deployment of Grafana Loki with Helm

The Helm installation runs the Grafana Loki cluster in 
[microservices mode](../../fundamentals/architecture/deployment-modes/#microservices-mode)
within a Kubernetes cluster.

## Prerequisites

- Helm. See [Installing Helm](https://helm.sh/docs/intro/install/).
- A running Kubernetes cluster.

## Deploy a Loki cluster

1. Add [Loki's chart repository](https://github.com/grafana/helm-charts) to Helm:

    ```bash
    helm repo add grafana https://grafana.github.io/helm-charts
    ```

1. Update the chart repository:

    ```bash
    helm repo update
    ```

1. Deploy the Loki cluster using one of these commands.

    - Deploy with the default configuration:

        ```bash
        helm upgrade --install loki grafana/loki-distributed
        ```

    - Deploy with the default configuration in a custom Kubernetes cluster namespace:

        ```bash
        helm upgrade --install loki --namespace=loki grafana/loki-distributed
        ```

    - Deploy with added custom configuration:

        ```bash
        helm upgrade --install loki grafana/loki-distributed --set "key1=val1,key2=val2,..."
        ```

## Deploy Grafana to your Kubernetes cluster

Install Grafana on your Kubernetes cluster with Helm:

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
`http://<helm-installation-name>-gateway.<namespace>.svc.cluster.local/` for Loki
(with `<helm-installation-name>` and `<namespace>` replaced by the installation and namespace, respectively, of your deployment).

## Run Loki behind HTTPS Ingress

If Loki and Promtail are deployed on different clusters, you can add an Ingress
in front of Loki. By adding a certificate, you create an HTTPS endpoint. For
extra security you can also enable Basic Authentication on Ingress.

In the Promtail configuration, set the following values to communicate using HTTPS and basic authentication:

```yaml
loki:
  serviceScheme: https
  user: user
  password: pass
```

In the `values.yaml` file you passed to the helm chart, add:

```yaml
gateway:
  ingress:
    enabled: true
```

## Run Promtail with syslog support

In order to receive and process syslog messages in Promtail, the following changes will be necessary:

- Review the [Promtail syslog-receiver configuration documentation](../../clients/promtail/scraping/#syslog-receiver)

- Configure the Promtail Helm chart with the syslog configuration added to the `extraScrapeConfigs` section and associated service definition to listen for syslog messages. For example:

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

## Run Promtail with systemd-journal support

In order to receive and process syslog message into Promtail, the following changes will be necessary:

- Review the [Promtail systemd-journal configuration documentation](../../clients/promtail/scraping/#journal-scraping-linux-only)

- Configure the Promtail Helm chart with the systemd-journal configuration added to the `extraScrapeConfigs` section and volume mounts for the Promtail pods to access the log files. For example:

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
    
    # Mount journal directory into Promtail pods
    extraVolumes:
      - name: journal
        hostPath:
          path: /var/log/journal

    extraVolumeMounts:
      - name: journal
        mountPath: /var/log/journal
        readOnly: true
    ```
