# ⚠️  DEPRECATED - Promtail Helm Chart

This chart was moved to <https://github.com/grafana/helm-charts>.

## Deploy Promtail only

```bash
$ helm upgrade --install promtail loki/promtail --set "loki.serviceName=loki"
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
```

