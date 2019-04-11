# promtail examples
#### In this file you can see simple examples of configure promtail

For work with 2 and more sources:
```
server:
  http_listen_port: 9080
  grpc_listen_port: 0

positions:
  filename: /tmp/positions.yaml

client:
  url: http://ip_or_hostname_where_Loki_run:3100/api/prom/push

scrape_configs:
 - job_name: system
   entry_parser: raw
   static_configs:
   - targets:
      - localhost
     labels:
      job: varlogs
      host: yourhost
      __path__: /var/log/*.log

 - job_name: someone_service
   entry_parser: raw
   static_configs:
   - targets:
      - localhost
     labels:
      job: someone_service
      host: yourhost
      __path__: /srv/log/someone_service/*.log

```
#### Description
Scrape_config section of config.yaml contents are various jobs for parsing your logs on current host

`job` and `host` these are tags on which you can filter parsed logs date on Grafana later

`__path__` it is path to directory where stored your logs. (*)

* - If you run promtail and this config.yaml in Docker container, you can use docker volumes for mapping real directories
with log to those folders in the container.
