---
title: Docker driver client configuration
menuTitle:  Configure Docker driver
description: Configuring the Docker driver client to send logs to Loki.
aliases: 
- ../../clients/docker-driver/configuration/
weight:  410
---
# Docker driver client configuration

The Docker daemon on each machine has a default logging driver and
each container will use the default driver unless configured otherwise.

## Installation

Before configuring the plugin, [install or upgrade the Grafana Loki Docker Driver Client]({{< relref "../docker-driver" >}})

## Change the logging driver for a container

The `docker run` command can be configured to use a different logging driver
than the Docker daemon's default with the `--log-driver` flag. Any options that
the logging driver supports can be set using the `--log-opt <NAME>=<VALUE>` flag.
`--log-opt` can be passed multiple times for each option to be set.

The following command will start Grafana in a container and send logs to Grafana
Cloud, using a batch size of 400 entries and no more than 5 retries if a send
fails.

```bash
docker run --log-driver=loki \
    --log-opt loki-url="https://<user_id>:<password>@logs-us-west1.grafana.net/loki/api/v1/push" \
    --log-opt loki-retries=5 \
    --log-opt loki-batch-size=400 \
    grafana/grafana
```
{{% admonition type="note" %}}
The Loki logging driver still uses the json-log driver in combination with sending logs to Loki, this is mainly useful to keep the `docker logs` command working. 
You can adjust file size and rotation using the respective log option `max-size` and `max-file`. Keep in mind that default values for these options are not taken from json-log configuration.
You can deactivate this behavior by setting the log option `no-file` to true. 
{{% /admonition %}}

## Change the default logging driver

If you want the Loki logging driver to be the default for all containers,
change Docker's `daemon.json` file (located in `/etc/docker` on Linux) and set
the value of `log-driver` to `loki`:

```json
{
  "debug": true,
  "log-driver": "loki"
}
```

Options for the logging driver can also be configured with `log-opts` in the
`daemon.json`:

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
{{% admonition type="note" %}}
log-opt configuration options in daemon.json must be provided as
> strings. Boolean and numeric values (such as the value for loki-batch-size in
> the example above) must therefore be enclosed in quotes (`"`).
{{% /admonition %}}

After changing `daemon.json`, restart the Docker daemon for the changes to take
effect. All **newly created** containers from that host will then send logs to Loki via the driver.

## Configure the logging driver for a Swarm service or Compose

You can also configure the logging driver for a [swarm service](https://docs.docker.com/engine/swarm/how-swarm-mode-works/services/)
directly in your compose file. This also applies for `docker-compose`:

```yaml
version: "3.7"
services:
  logger:
    image: grafana/grafana
    logging:
      driver: loki
      options:
        loki-url: "https://<user_id>:<password>@logs-prod-us-central1.grafana.net/loki/api/v1/push"
```

You can then deploy your stack using:

```bash
docker stack deploy my_stack_name --compose-file docker-compose.yaml
```

Or with `docker-compose`:

```bash
docker-compose -f docker-compose.yaml up
```

Once deployed, the Grafana service will send its logs to Loki.

{{% admonition type="note" %}}
Stack name and service name for each swarm service and project name and service name for each compose service are automatically discovered and sent as Loki labels, this way you can filter by them in Grafana.
{{% /admonition %}}

## Labels

Loki can receive a set of labels along with log line. These labels are used to index log entries and query back logs using [LogQL stream selector]({{< relref "../../query/log_queries#log-stream-selector" >}}).

By default, the Docker driver will add the following labels to each log line:

- `filename`: where the log is written to on disk
- `host`: the hostname where the log has been generated
- `swarm_stack`, `swarm_service`: added when deploying from Docker Swarm.
- `compose_project`, `compose_service`: added when deploying with Docker Compose.

Custom labels can be added using the `loki-external-labels`, `loki-pipeline-stages`,
`loki-pipeline-stage-file`, `labels`, `env`, and `env-regex` options. See the
next section for all supported options.

`loki-external-labels` have the default value of `container_name={{.Name}}`. If you have custom value for `loki-external-labels` then that will replace the default value, meaning you won't have `container_name` label unless you explcity add it (e.g: `loki-external-labels: "job=docker,container_name={{.Name}}"`.

## Pipeline stages

While you can provide `loki-pipeline-stage-file` it can be hard to mount the configuration file to the driver root filesystem.
This is why another option `loki-pipeline-stages` is available allowing you to pass a list of stages inlined. Pipeline stages are run at last on every lines.

The example [docker-compose](https://github.com/grafana/loki/blob/main/clients/cmd/docker-driver/docker-compose.yaml) below configures 2 stages, one to extract level values and one to set it as a label:

```yaml
version: "3"
services:
  grafana:
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

{{% admonition type="note" %}}
Note the `loki-pipeline-stages: |` letting you keep the indentation correct.
{{% /admonition %}}

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

Relabeling phase will happen only once per container and it is applied on the container metadata when it starts. So you can for example rename the labels that are only available during the starting of the container, not the labels available on log lines. Use [pipeline stages](#pipeline-stages) instead.

For example the configuration below will rename the label `swarm_stack` and `swarm_service` to respectively `namespace` and `service`.

```yaml
version: "3"
services:
  grafana:
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

## Supported log-opt options

To specify additional logging driver options, you can use the --log-opt NAME=VALUE flag.

| Option                          | Required? |       Default Value        | Description                                                                                                                                                                                                                                                                             |
|---------------------------------|:---------:|:--------------------------:|-----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `loki-url`                      |    Yes    |                            | Loki HTTP push endpoint.                                                                                                                                                                                                                                                                |
| `loki-external-labels`          |    No     | `container_name={{.Name}}` | Additional label value pair separated by `,` to send with logs. The value is expanded with the [Docker tag template format](https://docs.docker.com/config/containers/logging/log_tags/). (eg: `container_name={{.ID}}.{{.Name}},cluster=prod`)                                         |
| `loki-timeout`                  |    No     |           `10s`            | The timeout to use when sending logs to the Loki instance. Valid time units are "ns", "us" (or "µs"), "ms", "s", "m", "h".                                                                                                                                                              |
| `loki-batch-wait`               |    No     |            `1s`            | The amount of time to wait before sending a log batch complete or not. Valid time units are "ns", "us" (or "µs"), "ms", "s", "m", "h".                                                                                                                                                  |
| `loki-batch-size`               |    No     |         `1048576`          | The maximum size of a log batch to send.                                                                                                                                                                                                                                                |
| `loki-min-backoff`              |    No     |          `500ms`           | The minimum amount of time to wait before retrying a batch. Valid time units are "ns", "us" (or "µs"), "ms", "s", "m", "h".                                                                                                                                                             |
| `loki-max-backoff`              |    No     |            `5m`            | The maximum amount of time to wait before retrying a batch. Valid time units are "ns", "us" (or "µs"), "ms", "s", "m", "h".                                                                                                                                                             |
| `loki-retries`                  |    No     |            `10`            | The maximum amount of retries for a log batch. Setting it to `0` will retry indefinitely.                                                                                                                                                                                               |
| `loki-pipeline-stage-file`      |    No     |                            | The location of a pipeline stage configuration file ([example](https://github.com/grafana/loki/blob/main/clients/cmd/docker-driver/pipeline-example.yaml)). Pipeline stages allows to parse log lines to extract more labels, [see associated documentation]({{< relref "../../send-data/promtail/stages" >}}). |
| `loki-pipeline-stages`          |    No     |                            | The pipeline stage configuration provided as a string [see pipeline stages](#pipeline-stages) and [associated documentation]({{< relref "../../send-data/promtail/stages" >}}).                                                                                                              |
| `loki-relabel-config`           |    No     |                            | A [Prometheus relabeling configuration](https://prometheus.io/docs/prometheus/latest/configuration/configuration/#relabel_config) allowing you to rename labels [see relabeling](#relabeling).                                                                                          |
| `loki-tenant-id`                |    No     |                            | Set the tenant id (http header`X-Scope-OrgID`) when sending logs to Loki. It can be overridden by a pipeline stage.                                                                                                                                                                     |
| `loki-tls-ca-file`              |    No     |                            | Set the path to a custom certificate authority.                                                                                                                                                                                                                                         |
| `loki-tls-cert-file`            |    No     |                            | Set the path to a client certificate file.                                                                                                                                                                                                                                              |
| `loki-tls-key-file`             |    No     |                            | Set the path to a client key.                                                                                                                                                                                                                                                           |
| `loki-tls-server-name`          |    No     |                            | Name used to validate the server certificate.                                                                                                                                                                                                                                           |
| `loki-tls-insecure-skip-verify` |    No     |          `false`           | Allow to skip tls verification.                                                                                                                                                                                                                                                         |
| `loki-proxy-url`                |    No     |                            | Proxy URL use to connect to Loki.                                                                                                                                                                                                                                                       |
| `no-file`                       |    No     |          `false`           | This indicates the driver to not create log files on disk, however this means you won't be able to use `docker logs` on the container anymore. You can use this if you don't need to use `docker logs` and you run with limited disk space. (By default files are created)              |
| `keep-file`                     |    No     |          `false`           | This indicates the driver to keep json log files once the container is stopped. By default files are removed, this means you won't be able to use `docker logs` once the container is stopped.                                                                                          |
| `max-size`                      |    No     |             -1             | The maximum size of the log before it is rolled. A positive integer plus a modifier representing the unit of measure (k, m, or g). Defaults to -1 (unlimited). This is used by json-log required to keep the `docker log` command working.                                              |
| `max-file`                      |    No     |             1              | The maximum number of log files that can be present. If rolling the logs creates excess files, the oldest file is removed. Only effective when max-size is also set. A positive integer. Defaults to 1.                                                                                 |
| `labels`                        |    No     |                            | Comma-separated list of keys of labels, which should be included in message, if these labels are specified for container.                                                                                                                                                               |
| `env`                           |    No     |                            | Comma-separated list of keys of environment variables to be included in message if they specified for a container.                                                                                                                                                                      |
| `env-regex`                     |    No     |                            | A regular expression to match logging-related environment variables. Used for advanced log label options. If there is collision between the label and env keys, the value of the env takes precedence. Both options add additional fields to the labels of a logging message.           |

## Troubleshooting

Plugin logs can be found as docker daemon log. To enable debug mode refer to the
[Docker daemon documentation](https://docs.docker.com/config/daemon/).

The standard output (`stdout`) of a plugin is redirected to Docker logs. Such
entries are prefixed with `plugin=`.

To find out the plugin ID of the Loki logging driver, use `docker plugin ls` and
look for the `loki` entry.

Depending on your system, location of Docker daemon logging may vary. Refer to
[Docker documentation for Docker daemon](https://docs.docker.com/config/daemon/)
log location for your specific platform.
