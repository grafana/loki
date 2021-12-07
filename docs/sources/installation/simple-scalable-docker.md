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
