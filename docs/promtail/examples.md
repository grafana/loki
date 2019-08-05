# Examples

This document shows some example use-cases for promtail and their configuration.

## Local Config
Using this configuration, all files in `/var/log` and `/srv/log/someone_service` are ingested into Loki.  
The labels `job` and `host` are set using `static_configs`.

When using this configuration with Docker, do not forget to mount the configuration, `/var/log` and `/src/log/someone_service` using [volumes](https://docs.docker.com/storage/volumes/).

```yaml
server:
  http_listen_port: 9080
  grpc_listen_port: 0

positions:
  filename: /tmp/positions.yaml # progress of the individual files

client:
  url: http://ip_or_hostname_where_loki_runs:3100/api/prom/push

scrape_configs:
 - job_name: system
   pipeline_stages:
   - docker: # Docker wraps logs in json. Undo this.
   static_configs: # running locally here, no need for service discovery
   - targets:
      - localhost
     labels:
      job: varlogs
      host: yourhost
      __path__: /var/log/*.log # tail all files under /var/log

 - job_name: someone_service
   pipeline_stages:
   - docker: # Docker wraps logs in json. Undo this.
   static_configs: # running locally here, no need for service discovery
   - targets:
      - localhost
     labels:
      job: someone_service
      host: yourhost
      __path__: /srv/log/someone_service/*.log # tail all files under /srv/log/someone_service

```

## Systemd Journal
This example shows how to ship the `systemd` journal to Loki.

Just like the Docker example, the `scrape_configs` section holds various
jobs for parsing logs. A job with a `journal` key configures it for systemd
journal reading.

`path` is an optional string specifying the path to read journal entries
from. If unspecified, defaults to the system default (`/var/log/journal`).

`labels`: is a map of string values specifying labels that should always
be associated with each log entry being read from the systemd journal.
In our example, each log will have a label of `job=systemd-journal`.

Every field written to the systemd journal is available for processing
in the `relabel_configs` section. Label names are converted to lowercase
and prefixed with `__journal_`. After `relabel_configs` processes all
labels for a job entry, any label starting with `__` is deleted.

Our example renames the `_SYSTEMD_UNIT` label (available as
`__journal__systemd_unit` in promtail) to `unit** so it will be available
in Loki. All other labels from the journal entry are dropped.

When running using Docker, **remember to bind the journal into the container**.

```yaml
server:
  http_listen_port: 9080
  grpc_listen_port: 0

positions:
  filename: /tmp/positions.yaml

clients:
  - url: http://ip_or_hostname_where_loki_runns:3100/api/prom/push

scrape_configs:
  - job_name: journal
    journal:
      path: /var/log/journal
      labels:
        job: systemd-journal
    relabel_configs:
      - source_labels: ['__journal__systemd_unit']
        target_label: 'unit'
```
