# promtail examples
#### In this file you can see simple examples of configure promtail

* This example of config promtail based on original docker [config](https://github.com/grafana/loki/blob/master/cmd/promtail/promtail-docker-config.yaml)
and show how work with 2 and more sources:

Filename for example: my-docker-config.yaml
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
##### Description
Scrape_config section of config.yaml contents are various jobs for parsing your logs on current host

`job` and `host` these are tags on which you can filter parsed logs date on Grafana later

`__path__` it is path to directory where stored your logs.

If you run promtail and this config.yaml in Docker container, don't forget use docker volumes for mapping real directories
with log to those folders in the container. 

* See next example of Dockerfile, who use our modified promtail config (my-docker-config.yaml)
1) Create folder, for example `promtail`, then new folder build and in this filder conf and place there `my-docker-config.yaml`.
2) Create new Dockerfile in root folder `promtail`, with contents
```
FROM grafana/promtail:latest
COPY build/conf /etc/promtail
```
3) Create your Docker image based on original Promtail image and tag it, for example `mypromtail-image`
3) After that you can run Docker container by this command:
`docker run -d --name promtail --network loki_network -p 9080:9080 -v /var/log:/var/log -v /srv/log/someone_service:/srv/log/someone_service mypromtail-image -config.file=/etc/promtail/my-docker-config.yaml`
