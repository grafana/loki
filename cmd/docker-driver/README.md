# Loki Docker Logging Driver

## Overview

Docker logging driver plugins extends Docker's logging capabilities. You can use Loki Docker logging driver plugin to send
Docker container logs directly to your Loki instance or [Grafana Cloud](https://grafana.com/loki).

> Docker plugins are not yet supported on Windows; see Docker's logging driver plugin [documentation](https://docs.docker.com/engine/extend/)

If you have any questions or issues using the Docker plugin feel free to open an issue in this [repository](https://github.com/grafana/loki/issues).

## Plugin Installation

You need to install the plugin on each Docker host with container from which you want to collect logs.

You can install the plugin from our Docker hub repository by running on the Docker host the following command:

```bash
docker plugin install  grafana/loki-docker-driver:latest --alias loki --grant-all-permissions
```

To check the status of installed plugins, use the `docker plugin ls` command. Plugins that start successfully are listed as enabled in the output:

```bash
docker plugin ls
ID                  NAME         DESCRIPTION           ENABLED
ac720b8fcfdb        loki         Loki Logging Driver   true
```

You can now configure the plugin.

## Plugin Configuration

The Docker daemon on each Docker host has a default logging driver; each container on the Docker host uses the default driver, unless you configure it to use a different logging driver.

### Configure the logging driver for a container

When you start a container, you can configure it to use a different logging driver than the Docker daemon’s default, using the `--log-driver` flag. If the logging driver has configurable options, you can set them using one or more instances of the `--log-opt <NAME>=<VALUE>` flag. Even if the container uses the default logging driver, it can use different configurable options.

The following command configure the container `grafana` to start with the Loki drivers which will send logs to `logs-us-west1.grafana.net` Loki instance, using a batch size of 400 entries and will retry maximum 5 times if it fails.

```bash
docker run --log-driver=loki \
    --log-opt loki-url="https://<user_id>:<password>@logs-us-west1.grafana.net/loki/api/v1/push" \
    --log-opt loki-retries=5 \
    --log-opt loki-batch-size=400 \
    grafana/grafana
```

> **Note**: The Loki logging driver still uses the json-log driver in combination with sending logs to Loki, this is mainly useful to keep the `docker logs` command working.
> You can adjust file size and rotation using the respective log option `max-size` and `max-file`.
> You can deactivate this behavior by setting the log option `no-file` to true.

### Configure the default logging driver

To configure the Docker daemon to default to Loki logging driver, set the value of `log-driver` to `loki` logging driver in the `daemon.json` file, which is located in `/etc/docker/`. The following example explicitly sets the default logging driver to Loki:

```json
{
    "debug" : true,
    "log-driver": "loki"
}
```

The logging driver has configurable options, you can set them in the `daemon.json` file as a JSON array with the key log-opts. The following example sets the Loki push endpoint and batch size of the logging driver:

```json
{
    "debug" : true,
    "log-driver": "loki",
    "log-opts": {
        "loki-url": "https://<user_id>:<password>@logs-us-west1.grafana.net/loki/api/v1/push",
        "loki-batch-size": "400"
    }
}
```

> **Note**: log-opt configuration options in the daemon.json configuration file must be provided as strings. Boolean and numeric values (such as the value for loki-batch-size in the example above) must therefore be enclosed in quotes (").

Restart the Docker daemon and it will be configured with Loki logging driver, all containers from that host will send logs to Loki instance.

### Configure the logging driver for a Swarm service or Compose

You can also configure the logging driver for a [swarm service](https://docs.docker.com/engine/swarm/how-swarm-mode-works/services/) directly in your compose file, this also work for a docker-compose deployment:

```yaml
version: "3.7"
services:
  logger:
    image: grafana/grafana
    logging:
      driver: loki
      options:
        loki-url: "https://<user_id>:<password>@logs-us-west1.grafana.net/loki/api/v1/push"
```

You can then deploy your stack using:

```bash
docker stack deploy my_stack_name --compose-file docker-compose.yaml
```

Once deployed the Grafana service will be sending logs automatically to Loki.

> **Note**: stack name and service name for each swarm service and project name and service name for each compose service are automatically discovered and sent as Loki labels, this way you can filter by them in Grafana.

## Labels

Loki can received a set of labels along with log line. These labels are used to index log entries and query back logs using [LogQL stream selector](../../docs/logql.md#log-stream-selector).

By default the Docker driver will add the `filename` where the log is written, the `host` where the log has been generated as well as the `container_name`. Additionally `swarm_stack` and `swarm_service` are added for Docker Swarm deployments.

You can add more labels by using `loki-external-labels`,`loki-pipeline-stages`,`loki-pipeline-stage-file`,`labels`,`env` and `env-regex` options as described below.

## Pipeline stages

While you can provide `loki-pipeline-stage-file` it can be hard to mount the configuration file to the driver root filesystem.
This is why another option `loki-pipeline-stages` is available allowing your to pass a list of stages inlined.

The example [docker-compose](./docker-compose.yaml) below configures 2 stages, one to extract level values and one to set it as a label:

```yaml
version: "3"
services:
  nginx:
    image: grafana/grafana
    logging:
      driver: loki
      options:
        loki-url: http://host.docker.internal:3100/loki/api/v1/push
        loki-pipeline-stages: |
          - regex:
              expression: '(level|lvl|severity)=(?P<level>\w+)'
          - labels:
              level:
    ports:
      - "3000:3000"
```

> Note the `loki-pipeline-stages: |` allowing to keep the indentation correct.

When using docker run you can also pass the value via a string parameter like such:

```bash
read -d '' stages << EOF
- regex:
     expression: '(level|lvl|severity)=(?P<level>\\\w+)'
- labels:
    level:
EOF

docker run --log-driver=loki \
    --log-opt loki-url="http://host.docker.internal:3100/loki/api/v1/push" \
    --log-opt loki-pipeline-stages="$stages" \
    -p 3000:3000 grafana/grafana
```

This is a bit more difficult as you need to properly escape bash special characters. (note `\\\w+` for `\w+`)

Providing both `loki-pipeline-stage-file` and `loki-pipeline-stages` will cause an error.

## Relabeling

You can use [Prometheus relabeling](https://prometheus.io/docs/prometheus/latest/configuration/configuration/#relabel_config) configuration to modify labels discovered by the driver. The configuration must be passed as a YAML string like the [pipeline stages](#pipeline-stages).

For example the configuration below will rename the label `swarm_stack` and `swarm_service` to respectively `namespace` and `service`.

```yaml
version: "3"
services:
  nginx:
    image: grafana/grafana
    logging:
      driver: loki
      options:
        loki-url: http://host.docker.internal:3100/loki/api/v1/push
        loki-relabel-config: |
          - action: labelmap
            regex: swarm_stack
            replacement: namespace
          - action: labelmap
            regex: swarm_(service)
    ports:
      - "3000:3000"
```

## log-opt options

To specify additional logging driver options, you can use the --log-opt NAME=VALUE flag.

| Option                          | Required? |       Default Value        | Description                                                                                                                                                                                                                                                                   |
|---------------------------------|:---------:|:--------------------------:|-------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `loki-url`                      |    Yes    |                            | Loki HTTP push endpoint.                                                                                                                                                                                                                                                      |
| `loki-external-labels`          |    No     | `container_name={{.Name}}` | Additional label value pair separated by `,` to send with logs. The value is expanded with the [Docker tag template format](https://docs.docker.com/config/containers/logging/log_tags/). (eg: `container_name={{.ID}}.{{.Name}},cluster=prod`)                               |
| `loki-timeout`                  |    No     |           `10s`            | The timeout to use when sending logs to the Loki instance. Valid time units are "ns", "us" (or "µs"), "ms", "s", "m", "h".                                                                                                                                                    |
| `loki-batch-wait`               |    No     |            `1s`            | The amount of time to wait before sending a log batch complete or not. Valid time units are "ns", "us" (or "µs"), "ms", "s", "m", "h".                                                                                                                                        |
| `loki-batch-size`               |    No     |          `102400`          | The maximum size of a log batch to send.                                                                                                                                                                                                                                      |
| `loki-min-backoff`              |    No     |          `100ms`           | The minimum amount of time to wait before retrying a batch. Valid time units are "ns", "us" (or "µs"), "ms", "s", "m", "h".                                                                                                                                                   |
| `loki-max-backoff`              |    No     |           `10s`            | The maximum amount of time to wait before retrying a batch. Valid time units are "ns", "us" (or "µs"), "ms", "s", "m", "h".                                                                                                                                                   |
| `loki-retries`                  |    No     |            `10`            | The maximum amount of retries for a log batch.                                                                                                                                                                                                                                |
| `loki-pipeline-stage-file`      |    No     |                            | The location of a pipeline stage configuration file ([example](./pipeline-example.yaml)). Pipeline stages allows to parse log lines to extract more labels. [see documentation](../../docs/logentry/processing-log-lines.md)                                                  |
| `loki-pipeline-stages`          |    No     |                            | The pipeline stage configuration provided as a string [see](#pipeline-stages) and  [see documentation](../../docs/clients/promtail/pipelines.md)                                                                                                                              |
| `loki-relabel-config`           |    No     |                            | A [Prometheus relabeling configuration](https://prometheus.io/docs/prometheus/latest/configuration/configuration/#relabel_config) allowing you to rename labels [see](#relabeling)                                                                                            |
| `loki-tenant-id`                |    No     |                            | Set the tenant id (http header`X-Scope-OrgID`) when sending logs to Loki. It can be overrides by a pipeline stage.                                                                                                                                                            |
| `loki-tls-ca-file`              |    No     |                            | Set the path to a custom certificate authority.                                                                                                                                                                                                                               |
| `loki-tls-cert-file`            |    No     |                            | Set the path to a client certificate file.                                                                                                                                                                                                                                    |
| `loki-tls-key-file`             |    No     |                            | Set the path to a client key.                                                                                                                                                                                                                                                 |
| `loki-tls-server-name`          |    No     |                            | Name used to validate the server certificate.                                                                                                                                                                                                                                 |
| `loki-tls-insecure-skip-verify` |    No     |          `false`           | Allow to skip tls verification.                                                                                                                                                                                                                                               |
| `loki-proxy-url`                |    No     |                            | Proxy URL use to connect to Loki.                                                                                                                                                                                                                                             |
| `no-file`                       |    No     |          `false`           | This indicates the driver to not create log files on disk, however this means you won't be able to use `docker logs` on the container anymore. You can use this if you don't need to use `docker logs` and you run with limited disk space. (By default files are created)    |
| `keep-file`                     |    No     |          `false`           | This indicates the driver to keep json log files once the container is stopped. By default files are removed, this means you won't be able to use `docker logs` once the container is stopped.                                                                                |
| `max-size`                      |    No     |             -1             | The maximum size of the log before it is rolled. A positive integer plus a modifier representing the unit of measure (k, m, or g). Defaults to -1 (unlimited). This is used by json-log required to keep the `docker log` command working.                                    |
| `max-file`                      |    No     |             1              | The maximum number of log files that can be present. If rolling the logs creates excess files, the oldest file is removed. Only effective when max-size is also set. A positive integer. Defaults to 1.                                                                       |
| `labels`                        |    No     |                            | Comma-separated list of keys of labels, which should be included in message, if these labels are specified for container.                                                                                                                                                     |
| `env`                           |    No     |                            | Comma-separated list of keys of environment variables to be included in message if they specified for a container.                                                                                                                                                            |
| `env-regex`                     |    No     |                            | A regular expression to match logging-related environment variables. Used for advanced log label options. If there is collision between the label and env keys, the value of the env takes precedence. Both options add additional fields to the labels of a logging message. |


## Uninstall the plugin

To cleanly disable and remove the plugin, run:

```bash
docker plugin disable loki
docker plugin rm loki
```

## Upgrade the plugin

To upgrade the plugin to the last version, run:

```bash
docker plugin disable loki
docker plugin upgrade loki grafana/loki-docker-driver:master
docker plugin enable loki
```

## Troubleshooting

Plugin logs can be found as docker daemon log. To enable debug mode refer to the Docker daemon documentation: https://docs.docker.com/config/daemon/

Stdout of a plugin is redirected to Docker logs. Such entries have a plugin= suffix.

To find out the plugin ID of Loki, use the command below and look for Loki plugin entry.

```bash
docker plugin ls
```

Depending on your system, location of Docker daemon logging may vary. Refer to Docker documentation for Docker daemon log location for your specific platform. ([see](https://docs.docker.com/config/daemon/))
