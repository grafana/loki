# Installing Loki with Docker/Docker-compose

You can installing Loki with Docker/Docker-compose for try, test or development.
For production, we recommend Tanka or Helm.

## Prerequisites

Basically, you must install [docker](https://docs.docker.com/install).
If you want to install Loki with [docker-compose](https://docs.docker.com/compose/install/), install it too.

## Install with Docker

```bash
$ wget https://raw.githubusercontent.com/grafana/loki/v1.2.0/cmd/loki/loki-local-config.yaml -o loki-config.yaml
$ docker run -v $(pwd):/mnt/config -config.file=/mnt/config/loki-config.yaml -p 3100:3100 grafana/loki:v1.2.0
$ wget https://raw.githubusercontent.com/grafana/loki/v1.2.0/cmd/promtail/promtail-docker-config.yaml -o promtail-config.yaml
$ docker run -v $(pwd):/mnt/config -v /var/log:/var/log -config.file=/mnt/config/promtail-config.yaml grafana/promtail:latest
```

## Install with Docker-compose

```bash
$ wget https://raw.githubusercontent.com/grafana/loki/master/production/docker-compose.yaml -o docker-compose.yaml
$ docker-compose -f docker-compose.yaml up
```
