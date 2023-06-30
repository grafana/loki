---
title: Docker driver
description: Docker driver client
weight: 40
---
# Docker driver

Grafana Loki officially supports a Docker plugin that will read logs from Docker
containers and ship them to Loki. The plugin can be configured to send the logs
to a private Loki instance or [Grafana Cloud](/oss/loki).

> Docker plugins are not yet supported on Windows; see the
> [Docker Engine managed plugin system](https://docs.docker.com/engine/extend) documentation for more information.

Documentation on configuring the Loki Docker Driver can be found on the
[configuration page]({{< relref "./configuration" >}}).

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

Once the plugin is installed it can be [configured]({{< relref "./configuration" >}}).

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

## Known Issues

The driver keeps all logs in memory and will drop log entries if Loki is not reachable and if the quantity of `max_retries` has been exceeded. To avoid the dropping of log entries, setting `max_retries` to zero allows unlimited retries; the driver will continue trying forever until Loki is again reachable. Trying forever may have undesired consequences, because the Docker daemon will wait for the Loki driver to process all logs of a container, until the container is removed. Thus, the Docker daemon might wait forever if the container is stuck.

Use Promtail's [Docker target]({{< relref "../promtail/configuration#docker" >}}) or [Docker service discovery]({{< relref "../promtail/configuration#docker_sd_config" >}}) to avoid this issue.
