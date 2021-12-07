---
title: Simple scalable cluster
weight: 35
---
# Install and deploy a simple scalable cluster with Docker compose

A local Docker compose installation of Grafana Loki and Promtail is appropriate for an evaluation, testing, or development environment.
Use a Tanka or Helm process for a production environment.

This installation runs Loki in a simple scalable  deployment mode with one read path component and one write path component.

## Prerequisites

- [Docker](https://docs.docker.com/install)
- [Docker Compose](https://docs.docker.com/compose/install)

## Obtain Loki and Promtail configuration files

Download `loki-config.yaml`, `promtail-config.yaml`, and `docker-compose.yaml` to your current directory:

```bash
wget https://raw.githubusercontent.com/grafana/loki/main/production/simple-scalable/promtail-config.yaml -O promtail-config.yaml
wget https://raw.githubusercontent.com/grafana/loki/main/production/simple-scalable/loki-config.yaml -O loki-config.yaml
wget https://raw.githubusercontent.com/grafana/loki/main/production/simple-scalable/docker-compose.yaml -O docker-compose.yaml
```

This will download `loki-config.yaml`, `promtail-config.yaml`, and `docker-compose.yaml` to your current directory.

The `docker-compose.yaml` relies on the [Loki docker driver](https://grafana.com/docs/loki/latest/clients/docker-driver/), 
aliased to `loki-compose`, to send logs to the loki cluster. If this driver is not installed on your system, you can install it by running the following:

```bash
docker plugin install grafana/loki-docker-driver:latest --alias loki-compose --grant-all-permissions
```

If this driver is already installed, but under a different alias, you will have to change `docker-compose.yaml` to use the correct alias.

## Deploy and verify readiness of the Loki cluster

From the directory containing the configuration files, deploy the cluster with docker-compose:

```bash
docker-compose up
```

The running Docker containers use the directory's configuration files.

Navigate to http://localhost:3100/ready to check for cluster readiness.
Navigate to http://localhost:3100/metrics to view the cluster metrics.
Navigate to http://localhost:3000 for the Grafana instance that has Loki configured as a datasource.

By default, the image runs processes as user loki with  UID `10001` and GID `10001`.
You can use a different user, specially if you are using bind mounts, by specifying the UID with a `docker run` command and using `--user=UID` with numeric UID suited to your needs.
