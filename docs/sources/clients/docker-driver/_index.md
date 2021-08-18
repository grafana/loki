---
title: Docker driver
---
# Docker Driver Client

Loki officially supports a Docker plugin that will read logs from Docker
containers and ship them to Loki. The plugin can be configured to send the logs
to a private Loki instance or [Grafana Cloud](https://grafana.com/oss/loki).

> Docker plugins are not yet supported on Windows; see the
> [Docker Engine managed plugin system](https://docs.docker.com/engine/extend) documentation for more information.

Documentation on configuring the Loki Docker Driver can be found on the
[configuration page](./configuration).

If you have any questions or issues using the Docker plugin feel free to open an issue in this [repository](https://github.com/grafana/loki/issues).

## Installing

The Docker plugin must be installed on each Docker host that will be running
containers you want to collect logs from.

Run the following command to install the plugin:

```bash
docker plugin install grafana/loki-docker-driver:latest --alias loki --grant-all-permissions
```

To check installed plugins, use the `docker plugin ls` command. Plugins that
have started successfully are listed as enabled:

```bash
$ docker plugin ls
ID                  NAME         DESCRIPTION           ENABLED
ac720b8fcfdb        loki         Loki Logging Driver   true
```

Once the plugin is installed it can be [configured](./configuration).

## Upgrading

The upgrade process involves disabling the existing plugin, upgrading, then
re-enabling and restarting Docker:

```bash
docker plugin disable loki --force
docker plugin upgrade loki grafana/loki-docker-driver:latest --grant-all-permissions
docker plugin enable loki
systemctl restart docker
```

## Uninstalling

To cleanly uninstall the plugin, disable and remove it:

```bash
docker plugin disable loki --force
docker plugin rm loki
```

# Know Issues

The driver keeps all logs in memory and will drop log entries in case Loki is not reachable and the `max_retries` are exceeded. This can be avoided by setting `max_retries` to zero and thus trying forever until Loki is reachable again. However, this setting might have undesired consequences. The Docker daemon will wait for the Loki driver to process all logs of a container until it's removed. So it might wait forever and the container is stuck.

In order to avoid this situation it's recommended to use [Promtail](../promtail) instead with the following configuration.

```yaml
server:
  disable: true

positions:
  filename: loki-positions.yml

clients:
  - url: ${LOKI_ENDPOINT}
    basic_auth:
      username: ${LOKI_USER}
      password: ${LOKI_PASSOWORD}

scrape_configs:
  - job_name: system 
    pipeline_stages:
      - docker: {}
    static_configs:
      - labels:
          job: docker
          __path__: /var/lib/docker/containers/*/*-json.log

```

This will enable Promtail to tail *all* Docker container logs and publish them to Loki.
