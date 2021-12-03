---
title: Simple Scalable through Docker Compose
weight: 30
---
# Install Grafana Loki in Simple, Scalable mode with Docker Compose

You can install Grafana Loki and Promtail with Docker Compose if you are evaluating, testing, or developing Loki.
For production, we recommend installing with Tanka or Helm.

The configuration acquired with these installation instructions run Loki in Simple Scalable mode (ie. 1 read and 1 write target)

## Prerequisites

- [Docker](https://docs.docker.com/install)
- [Docker Compose](https://docs.docker.com/compose/install) (optional, only needed for the Docker Compose install method)

## Install with Docker Compose

Copy and paste the commands below into your command line.

```bash
wget https://raw.githubusercontent.com/grafana/loki/main/production/simple-scalable/promtail-config.yaml -O promtail-config.yaml
wget https://raw.githubusercontent.com/grafana/loki/main/production/simple-scalable/loki-config.yaml -O loki-config.yaml
wget https://raw.githubusercontent.com/grafana/loki/main/production/simple-scalable/docker-compose.yaml -O docker-compose.yaml
docker-compose up
```

This will download `loki-config.yaml`, `promtail-config.yaml`, and `docker-compose.yaml` to your current directory.
From that same directory run `docker-compose up` to bring up the cluster.
Docker containers are running Loki and Promtail using those config files.

Navigate to http://localhost:3100/metrics to view the metrics and http://localhost:3100/ready for readiness.
Navigate to http://localhost:3000 for the grafana instance that has Loki configured as a datasource.

The image is configured to run by default as user loki with  UID `10001` and GID `10001`. You can use a different user, specially if you are using bind mounts, by specifying the UID with a `docker run` command and using `--user=UID` with numeric UID suited to your needs.
