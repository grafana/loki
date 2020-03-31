# Docker Driver Client

Loki officially supports a Docker plugin that will read logs from Docker
containers and ship them to Loki. The plugin can be configured to send the logs
to a private Loki instance or [Grafana Cloud](https://grafana.com/oss/loki).

> Docker plugins are not yet supported on Windows; see the
> [Docker docs](https://docs.docker.com/engine/extend) for more information.

Documentation on configuring the Loki Docker Driver can be found on the
[configuration page](./configuration.md).

## Installing

The Docker plugin must be installed on each Docker host that will be running
containers you want to collect logs from.

Run the following command to install the plugin:

```bash
docker plugin install grafana/loki-docker-driver:latest --alias loki --grant-all-permissions
```

To check installed plugins, use the `docker plugin ls` command. Plugins that
have started successfully are listed as enabled:

```
$ docker plugin ls
ID                  NAME         DESCRIPTION           ENABLED
ac720b8fcfdb        loki         Loki Logging Driver   true
```

Once the plugin is installed it can be [configured](./configuration.md).

## Upgrading

The upgrade process involves disabling the existing plugin, upgrading, and then
re-enabling:

```bash
docker plugin disable loki
docker plugin upgrade loki grafana/loki-docker-driver:master
docker plugin enable loki
```

## Uninstalling

To cleanly uninstall the plugin, disable and remove it:

```bash
docker plugin disable loki
docker plugin rm loki
```

## Amazon ECS
The Docker driver is not currently supported on [Amazon ECS](https://aws.amazon.com/ecs/), although you can work around this if you are using EC2 based ECS (as opposed to Fargate based ECS).
The solution suggested in the [LogConfiguration Documentation](https://docs.aws.amazon.com/AmazonECS/latest/APIReference/API_LogConfiguration.html) is to fork the ECS agent and modify it to work with your log driver of choice. 
The other option is to configure the Loki Docker driver as the default Docker logging driver, and then specify no logging configuration within the ECS task.
