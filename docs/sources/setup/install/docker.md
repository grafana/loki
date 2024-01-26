---
title: Install Loki with Docker or Docker Compose
menuTitle:  Install using Docker
description: Describes how to install Loki using Docker or Docker Compose
aliases: 
 - ../../installation/docker/
weight: 400
---
# Install Loki with Docker or Docker Compose

You can install Loki and Promtail with Docker or Docker Compose if you are evaluating, testing, or developing Loki.
For production, we recommend installing with Tanka or Helm.

The configuration acquired with these installation instructions run Loki as a single binary.

## Prerequisites

- [Docker](https://docs.docker.com/install)
- [Docker Compose](https://docs.docker.com/compose/install) (optional, only needed for the Docker Compose install method)

## Install with Docker

**Linux**

Copy and paste the commands below into your command line.

```bash
wget https://raw.githubusercontent.com/grafana/loki/v2.9.2/cmd/loki/loki-local-config.yaml -O loki-config.yaml
docker run --name loki -d -v $(pwd):/mnt/config -p 3100:3100 grafana/loki:2.9.2 -config.file=/mnt/config/loki-config.yaml
wget https://raw.githubusercontent.com/grafana/loki/v2.9.2/clients/cmd/promtail/promtail-docker-config.yaml -O promtail-config.yaml
docker run --name promtail -d -v $(pwd):/mnt/config -v /var/log:/var/log --link loki grafana/promtail:2.9.2 -config.file=/mnt/config/promtail-config.yaml
```

When finished, `loki-config.yaml` and `promtail-config.yaml` are downloaded in the directory you chose. Docker containers are running Loki and Promtail using those config files.

Navigate to http://localhost:3100/metrics to view the metrics and http://localhost:3100/ready for readiness.

The image is configured to run by default as user loki with  UID `10001` and GID `10001`. You can use a different user, specially if you are using bind mounts, by specifying the UID with a `docker run` command and using `--user=UID` with numeric UID suited to your needs.

**Windows**

Copy and paste the commands below into your terminal. Note that you will need to replace the `<placeholders>` in the commands with your local path.

```bash
cd "<local-path>"
wget https://raw.githubusercontent.com/grafana/loki/v2.9.2/cmd/loki/loki-local-config.yaml -O loki-config.yaml
docker run --name loki -v <local-path>:/mnt/config -p 3100:3100 grafana/loki:2.9.2 --config.file=/mnt/config/loki-config.yaml
wget https://raw.githubusercontent.com/grafana/loki/v2.9.2/clients/cmd/promtail/promtail-docker-config.yaml -O promtail-config.yaml
docker run -v <local-path>:/mnt/config -v /var/log:/var/log --link loki grafana/promtail:2.9.2 --config.file=/mnt/config/promtail-config.yaml
```

When finished, `loki-config.yaml` and `promtail-config.yaml` are downloaded in the directory you chose. Docker containers are running Loki and Promtail using those config files.

Navigate to http://localhost:3100/metrics to view the output.

## Install with Docker Compose

Run the following commands in your command line. They work for Windows or Linux systems.

```bash
wget https://raw.githubusercontent.com/grafana/loki/v2.9.2/production/docker-compose.yaml -O docker-compose.yaml
docker-compose -f docker-compose.yaml up
```
