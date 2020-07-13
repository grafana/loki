# Configuring the Docker Driver

The Docker daemon on each machine has a default logging driver and
each container will use the default driver unless configured otherwise.

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

> **Note**: The Loki logging driver still uses the json-log driver in
> combination with sending logs to Loki. This is mainly useful to keep the
> `docker logs` command working. You can adjust file size and rotation
> using the respective log option `max-size` and `max-file`.

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

> **Note**: log-opt configuration options in daemon.json must be provided as
> strings. Boolean and numeric values (such as the value for loki-batch-size in
> the example above) must therefore be enclosed in quotes (`"`).

After changing `daemon.json`, restart the Docker daemon for the changes to take
effect. All containers from that host will then send logs to Loki.

## Configure the logging driver for a Swarm service or Compose

You can also configure the logging driver for a [swarm
service](https://docs.docker.com/engine/swarm/how-swarm-mode-works/services/)
directly in your compose file. This also applies for `docker-compose`:

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

Or with `docker-compose`:

```bash
docker-compose -f docker-compose.yaml up
```

Once deployed, the Grafana service will send its logs to Loki.

> **Note**: stack name and service name for each swarm service and project name
> and service name for each compose service are automatically discovered and
> sent as Loki labels, this way you can filter by them in Grafana.

## Labels

By default, the Docker driver will add the following labels to each log line:

- `filename`: where the log is written to on disk
- `host`: the hostname where the log has been generated
- `container_name`: the name of the container generating logs
- `swarm_stack`, `swarm_service`: added when deploying from Docker Swarm.

Custom labels can be added using the `loki-external-labels`,
`loki-pipeline-stage-file`, `labels`, `env`, and `env-regex` options. See the
next section for all supported options.

## Supported log-opt options

The following are all supported options that the Loki logging driver supports:

| Option                          | Required? | Default Value              | Description
| ------------------------------- | :-------: | :------------------------: | -------------------------------------- |
| `loki-url`                      | Yes       |                            | Loki HTTP push endpoint.
| `loki-external-labels`          | No        | `container_name={{.Name}}` | Additional label value pairs separated by `,` to send with logs. The value is expanded with the [Docker tag template format](https://docs.docker.com/engine/admin/logging/log_tags/). (e.g.,: `container_name={{.ID}}.{{.Name}},cluster=prod`)
| `loki-timeout`                  | No        | `10s`                      | The timeout to use when sending logs to the Loki instance. Valid time units are "ns", "us" (or "µs"), "ms", "s", "m", "h".
| `loki-batch-wait`               | No        | `1s`                       | The amount of time to wait before sending a log batch complete or not. Valid time units are "ns", "us" (or "µs"), "ms", "s", "m", "h".
| `loki-batch-size`               | No        | `102400`                   | The maximum size of a log batch to send.
| `loki-min-backoff`              | No        | `100ms`                    | The minimum amount of time to wait before retrying a batch. Valid time units are "ns", "us" (or "µs"), "ms", "s", "m", "h".
| `loki-max-backoff`              | No        | `10s`                      | The maximum amount of time to wait before retrying a batch. Valid time units are "ns", "us" (or "µs"), "ms", "s", "m", "h".
| `loki-retries`                  | No        | `10`                       | The maximum amount of retries for a log batch.
| `loki-pipeline-stage-file`      | No        |                            | The location of a pipeline stage configuration file. Pipeline stages allows to parse log lines to extract more labels. [See the Promtail documentation for more info.](../promtail/pipelines.md)
| `loki-tls-ca-file`              | No        |                            | Set the path to a custom certificate authority.
| `loki-tls-cert-file`            | No        |                            | Set the path to a client certificate file.
| `loki-tls-key-file`             | No        |                            | Set the path to a client key.
| `loki-tls-server-name`          | No        |                            | Name used to validate the server certificate.
| `loki-tls-insecure-skip-verify` | No        | `false`                    | Allow to skip tls verification.
| `loki-proxy-url`                | No        |                            | Proxy URL use to connect to Loki.
| `max-size`                      | No        |       -1                   | The maximum size of the log before it is rolled. A positive integer plus a modifier representing the unit of measure (k, m, or g). Defaults to -1 (unlimited). This is used by json-log required to keep the `docker log` command working.
| `max-file`                      | No        |       1                    | The maximum number of log files that can be present. If rolling the logs creates excess files, the oldest file is removed. Only effective when max-size is also set. A positive integer. Defaults to 1.
| `labels`                        | No        |                            | Comma-separated list of keys of labels, which should be included in message, if these labels are specified for container.
| `env`                           | No        |                            | Comma-separated list of keys of environment variables to be included in message if they specified for a container.
| `env-regex`                     | No        |                            | A regular expression to match logging-related environment variables. Used for advanced log label options. If there is collision between the label and env keys, the value of the env takes precedence. Both options add additional fields to the labels of a logging message.

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
