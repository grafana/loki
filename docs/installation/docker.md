# Installing Loki with Docker or Docker Compose

You can install Loki with Docker or Docker Compose for evaluating, testing, or developing Loki.
For production, we recommend Tanka or Helm.

## Prerequisites

- [Docker](https://docs.docker.com/install)
- [Docker Compose](https://docs.docker.com/compose/install) (optional, only needed for the Docker Compose install method)

## Install with Docker

```bash
$ wget https://raw.githubusercontent.com/grafana/loki/v1.3.0/cmd/loki/loki-local-config.yaml -O loki-config.yaml
$ docker run -v $(pwd):/mnt/config -p 3100:3100 grafana/loki:v1.3.0 -config.file=/mnt/config/loki-config.yaml
$ wget https://raw.githubusercontent.com/grafana/loki/v1.3.0/cmd/promtail/promtail-docker-config.yaml -O promtail-config.yaml
$ docker run -v $(pwd):/mnt/config -v /var/log:/var/log grafana/promtail:latest -config.file=/mnt/config/promtail-config.yaml
```

## Install with Docker Compose

```bash
$ wget https://raw.githubusercontent.com/grafana/loki/v1.3.0/production/docker-compose.yaml -O docker-compose.yaml
$ docker-compose -f docker-compose.yaml up
```
