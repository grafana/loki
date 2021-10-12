---
title: Docker driver
weight: 40
---
# Docker Driver Client

Grafana Loki officially supports a Docker plugin that will read logs from Docker
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

The driver keeps all logs in memory and will drop log entries if Loki is not reachable and if the quantity of `max_retries` has been exceeded. To avoid the dropping of log entries, setting `max_retries` to zero allows unlimited retries; the drive will continue trying forever until Loki is again reachable. Trying forever may have undesired consequences, because the Docker daemon will wait for the Loki driver to process all logs of a container, until the container is removed. Thus, the Docker daemon might wait forever if the container is stuck.

This issue is avoided by using  [Promtail](../promtail) with this configuration:

```yaml
server:
  disable: true

positions:
  filename: loki-positions.yml

clients:
  - url: http://ip_or_hostname_where_Loki_run:3100/loki/api/v1/push
    # If using basic auth, configures the username and password sent.
    basic_auth:
      # The username to use for basic auth
      username: <string>
      # The password to use for basic auth
      password: <string>

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
