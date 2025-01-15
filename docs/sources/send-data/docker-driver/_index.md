---
title: Docker driver client
menuTItle:  Docker driver
description: Provides instructions for how to install, upgrade, and remove the Docker driver client, used to send logs to Loki.
aliases: 
- ../clients/docker-driver/
weight:  400
---
# Docker driver client

Grafana Loki officially supports a Docker plugin that will read logs from Docker
containers and ship them to Loki. The plugin can be configured to send the logs
to a private Loki instance or [Grafana Cloud](/oss/loki).

{{< admonition type="note" >}}
Docker plugins are not supported on Windows; see the [Docker Engine managed plugin system](https://docs.docker.com/engine/extend) documentation for more information.
{{< /admonition >}}

Documentation on configuring the Loki Docker Driver can be found on the
If you have any questions or issues using the Docker plugin, open an issue in
the [Loki repository](https://github.com/grafana/loki/issues).

## Install the Docker driver client

The Docker plugin must be installed on each Docker host that will be running containers you want to collect logs from.

Run the following command to install the plugin, updating the release version, or changing the architecture (`arm64` and `amd64` are currently supported), if needed:

```bash
docker plugin install grafana/loki-docker-driver:3.3.2-arm64 --alias loki --grant-all-permissions
```

{{< admonition type="note" >}}
Add `-arm64` to the image tag for ARM64 hosts.
{{< /admonition >}}

To check installed plugins, use the `docker plugin ls` command.
Plugins that have started successfully are listed as enabled:

```bash
docker plugin ls
```

You should see output similar to the following:

```bash
ID                  NAME         DESCRIPTION           ENABLED
ac720b8fcfdb        loki         Loki Logging Driver   true
```

Once you have successfully installed the plugin you can [configure](https://grafana.com/docs/loki/<LOKI_VERSION>/send-data/docker-driver/configuration/) it.

## Upgrade the Docker driver client

The upgrade process involves disabling the existing plugin, upgrading (chaning version and architecture as needed), then
re-enabling and restarting Docker:

```bash
docker plugin disable loki --force
docker plugin upgrade loki grafana/loki-docker-driver:3.3.2-arm64 --grant-all-permissions
docker plugin enable loki
systemctl restart docker
```

{{< admonition type="note" >}}
Update the version number to the appropriate version.
{{< /admonition >}}

## Uninstall the Docker driver client

To cleanly uninstall the plugin, disable and remove it:

```bash
docker plugin disable loki --force
docker plugin rm loki
```

## Known Issue: Deadlocked Docker Daemon

The driver keeps all logs in memory and will drop log entries if Loki is not reachable and if the quantity of `max_retries` has been exceeded. To avoid the dropping of log entries, setting `max_retries` to zero allows unlimited retries; the driver will continue trying forever until Loki is again reachable. Trying forever may have undesired consequences, because the Docker daemon will wait for the Loki driver to process all logs of a container, until the container is removed. Thus, the Docker daemon might wait forever if the container is stuck.

The wait time can be lowered by setting `loki-retries=2`, `loki-max-backoff=800ms`, `loki-timeout=1s` and `keep-file=true`. This way the daemon will be locked only for a short time and the logs will be persisted locally when the Loki client is unable to re-connect.

To avoid this issue, use the Promtail [Docker target](https://grafana.com/docs/loki/<LOKI_VERSION>/send-data/promtail/configuration/#docker) or [Docker service discovery](https://grafana.com/docs/loki/<LOKI_VERSION>/send-data/promtail/configuration/#docker_sd_configs).
