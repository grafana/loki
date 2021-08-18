---
title: Docker
---
# Install Loki with Docker or Docker Compose

You can install Loki and Promtail with Docker or Docker Compose if you are evaluating, testing, or developing Loki.
For production, we recommend installing with Tanka or Helm.

## Prerequisites

- [Docker](https://docs.docker.com/install)
- [Docker Compose](https://docs.docker.com/compose/install) (optional, only needed for the Docker Compose install method)

## Install with Docker

**Linux**

Copy and paste the commands below into your command line.

```bash
wget https://raw.githubusercontent.com/grafana/loki/v2.3.0/cmd/loki/loki-local-config.yaml -O loki-config.yaml
docker run -v $(pwd):/mnt/config -p 3100:3100 grafana/loki:2.3.0 -config.file=/mnt/config/loki-config.yaml
wget https://raw.githubusercontent.com/grafana/loki/v2.3.0/clients/cmd/promtail/promtail-docker-config.yaml -O promtail-config.yaml
docker run -v $(pwd):/mnt/config -v /var/log:/var/log grafana/promtail:2.3.0 -config.file=/mnt/config/promtail-config.yaml
```

When finished, `loki-config.yaml` and `promtail-config.yaml` are downloaded in the directory you chose. Docker containers are running Loki and Promtail using those config files.

Navigate to http://localhost:3100/metrics to view the metrics and http://localhost:3100/ready for readiness.

As of v1.6.0, image is configured to run by default as user loki with  UID `10001` and GID `10001`. You can use a different user, specially if you are using bind mounts, by specifying the UID with a `docker run` command and using `--user=UID` with numeric UID suited to your needs.

**Windows**

Copy and paste the commands below into your terminal. Note that you will need to replace the `<placeholders>` in the commands with your local path.

```bash
cd "<local-path>"
wget https://raw.githubusercontent.com/grafana/loki/v2.3.0/cmd/loki/loki-local-config.yaml -O loki-config.yaml
docker run -v <local-path>:/mnt/config -p 3100:3100 grafana/loki:2.3.0 --config.file=/mnt/config/loki-config.yaml
wget https://raw.githubusercontent.com/grafana/loki/v2.3.0/clients/cmd/promtail/promtail-docker-config.yaml -O promtail-config.yaml
docker run -v <local-path>:/mnt/config -v /var/log:/var/log grafana/promtail:2.3.0 --config.file=/mnt/config/promtail-config.yaml
```

When finished, `loki-config.yaml` and `promtail-config.yaml` are downloaded in the directory you chose. Docker containers are running Loki and Promtail using those config files.

Navigate to http://localhost:3100/metrics to view the output.

## Install with Docker Compose

Run the following commands in your command line. They work for Windows or Linux systems.

```bash
wget https://raw.githubusercontent.com/grafana/loki/v2.3.0/production/docker-compose.yaml -O docker-compose.yaml
docker-compose -f docker-compose.yaml up
```
