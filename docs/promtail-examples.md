# Promtail Config Examples

## Pipeline Examples

[Pipeline Docs](./logentry/processing-log-lines.md) contains detailed documentation of the pipeline stages

## Simple Docker Config

This example of config promtail based on original docker [config](https://github.com/grafana/loki/blob/master/cmd/promtail/promtail-docker-config.yaml)
and show how work with 2 and more sources:

Filename for example: my-docker-config.yaml
```yaml
server:
  http_listen_port: 9080
  grpc_listen_port: 0

positions:
  filename: /tmp/positions.yaml

client:
  url: http://ip_or_hostname_where_Loki_run:3100/api/prom/push

scrape_configs:
 - job_name: system
   pipeline_stages:
   - docker:
   static_configs:
   - targets:
      - localhost
     labels:
      job: varlogs
      host: yourhost
      __path__: /var/log/*.log

 - job_name: someone_service
   pipeline_stages:
   - docker:
   static_configs:
   - targets:
      - localhost
     labels:
      job: someone_service
      host: yourhost
      __path__: /srv/log/someone_service/*.log

```

#### Description

Scrape_config section of config.yaml contents contains various jobs for parsing your logs

`job` and `host` are examples of static labels added to all logs, labels are indexed by Loki and are used to help search logs.

`__path__` it is path to directory where stored your logs.

If you run promtail and this config.yaml in Docker container, don't forget use docker volumes for mapping real directories
with log to those folders in the container.

#### Example Use
1) Create folder, for example `promtail`, then new sub directory `build/conf` and place there `my-docker-config.yaml`.
2) Create new Dockerfile in root folder `promtail`, with contents
```dockerfile
FROM grafana/promtail:latest
COPY build/conf /etc/promtail
```
3) Create your Docker image based on original Promtail image and tag it, for example `mypromtail-image`
3) After that you can run Docker container by this command:
`docker run -d --name promtail --network loki_network -p 9080:9080 -v /var/log:/var/log -v /srv/log/someone_service:/srv/log/someone_service mypromtail-image -config.file=/etc/promtail/my-docker-config.yaml`

## Simple Systemd Journal Config

This example demonstrates how to configure promtail to listen to systemd journal
entries and write them to Loki:

Filename for example: my-systemd-journal-config.yaml

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

### Description

Just like the Docker example, the `scrape_configs` sections holds various
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
`__journal__systemd_unit` in promtail) to `unit` so it will be available
in Loki. All other labels from the journal entry are dropped.

### Example Use

`promtail` must have access to the journal path (`/var/log/journal`)
where journal entries are stored for journal support to work correctly.

If running with Docker, that means to bind that path:

```bash
docker run -d --name promtail --network loki_network -p 9080:9080 \
  -v /var/log/journal:/var/log/journal \
  mypromtail-image -config.file=/etc/promtail/my-systemd-journal-config.yaml
```