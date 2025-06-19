---
title: Fluent Bit community plugin
menuTitle:  Fluent Bit Community Plugin
description: Provides instructions for how to install, configure, and use the Fluent Bit Community plugin to send logs to Loki.
aliases: 
- ../clients/fluentbit/
weight:  500
---
# Fluent Bit community plugin

{{< admonition type="warning" >}}

We recommend using the official [Fluent Bit Loki plugin](https://grafana.com/docs/loki/<LOKI_VERSION>/send-data/fluentbit/fluent-bit-plugin/). The official plugin is more feature-rich and has better support for features such as structured metadata. The community plugin is still available for use, but it's no longer actively maintained.

{{< /admonition >}}

The Fluent Bit community plugin by Grafana Labs (`grafana-loki`) provided an alternative way to send logs to Loki. Although very similar to the [official plugin](https://grafana.com/docs/loki/<LOKI_VERSION>/send-data/fluentbit/fluent-bit-plugin/) there are some differences in the configuration options. This page provides instructions for how to install, configure, and use the Fluent Bit community plugin to send logs to Loki. Although the plugin is no longer actively maintained, this documentation is still available for reference.

{{< youtube id="s43IBSVyTpQ" >}}

## Usage

### Docker

You can run a Fluent Bit container with Loki output plugin pre-installed using our [Docker Hub](https://hub.docker.com/r/grafana/fluent-bit-plugin-loki) image:

```bash
docker run -v /var/log:/var/log \
    -e LOG_PATH="/var/log/*.log" -e LOKI_URL="http://localhost:3100/loki/api/v1/push" \
    grafana/fluent-bit-plugin-loki:latest
```

Or, an alternative is to run the fluent-bit container using [Docker Hub](https://hub.docker.com/r/fluent/fluent-bit) image:

### Docker container logs

To ship logs from Docker containers to Grafana Cloud using Fluent Bit, you can use the Fluent Bit Docker image and configure it to forward logs directly to Grafana Loki. Below is a step-by-step guide on setting up Fluent Bit for this purpose.

#### Prerequisites

- Docker is installed on your machine.
- Running instance of Loki OSS.

#### Configuration

1. Create a Fluent Bit configuration file named `fluent-bit.conf` with the following content, which defines the input from Docker container logs and sets up the output to send logs to your Grafana Cloud Loki instance:

   ```ini
   [SERVICE]
       Flush        1
       Log_Level    info

   [INPUT]
       Name     tail
       Path     /var/lib/docker/containers/*/*.log
       Parser   docker
       Tag      docker.*

   [OUTPUT]
       Name         grafana-loki
       Match        *
       Url          http://localhost:3100/loki/api/v1/push
       Labels       {job="fluentbit"}

### Kubernetes

You can run Fluent Bit as a [daemonset](https://kubernetes.io/docs/concepts/workloads/controllers/daemonset/) to collect all your Kubernetes workload logs.

To do so you can use the [Fluent Bit Helm chart](https://github.com/fluent/helm-charts) with the following `values.yaml` changing the value of `FLUENT_LOKI_URL`:

```yaml
image:
  # Here we use the Docker image which has the plugin installed
  repository: grafana/fluent-bit-plugin-loki
  tag: main-e2ed1c0

args:
  - "-e"
  - "/fluent-bit/bin/out_grafana_loki.so"
  - --workdir=/fluent-bit/etc
  - --config=/fluent-bit/etc/conf/fluent-bit.conf

env:
  # Note that for security reasons you should fetch the credentials through a Kubernetes Secret https://kubernetes.io/docs/concepts/configuration/secret/ . You may use the envFrom for this.
  - name: FLUENT_LOKI_URL
    value: https://user:pass@your-loki.endpoint/loki/api/v1/push

config:
  inputs: |
    [INPUT]
        Name tail
        Tag kube.*
        Path /var/log/containers/*.log
        # Be aware that local clusters like docker-desktop or kind use the docker log format and not the cri (https://docs.fluentbit.io/manual/installation/kubernetes#container-runtime-interface-cri-parser)
        multiline.parser docker, cri
        Mem_Buf_Limit 5MB
        Skip_Long_Lines On

  outputs: |
    [Output]
        Name grafana-loki
        Match kube.*
        Url ${FLUENT_LOKI_URL}
        Labels {job="fluent-bit"}
        LabelKeys level,app # this sets the values for actual Loki streams and the other labels are converted to structured_metadata https://grafana.com/docs/loki/<LOKI_VERSION>/get-started/labels/structured-metadata/
        BatchWait 1
        BatchSize 1001024
        LineFormat json
        LogLevel info
        AutoKubernetesLabels true
```

```bash
helm repo add fluent https://fluent.github.io/helm-charts
helm repo update
helm install fluent-bit fluent/fluent-bit -f values.yaml
```

By default it will collect all containers logs and extract labels from Kubernetes API (`container_name`, `namespace`, etc.).

If you also want to host your Loki instance inside the cluster install the [official Loki Helm chart](https://grafana.com/docs/loki/<LOKI_VERSION>/setup/install/helm/).

### AWS Elastic Container Service (ECS)

You can use the fluent-bit Loki Docker image as a Firelens log router in AWS ECS.
For more information about this see our [AWS documentation](https://grafana.com/docs/loki/<LOKI_VERSION>/send-data/promtail/cloud/ecs/).

### Local

First, you need to follow the [instructions](https://github.com/grafana/loki/blob/main/clients/cmd/fluent-bit/README.md) in order to build the plugin dynamic library.

Assuming you have Fluent Bit installed in your `$PATH` you can run the plugin using:

```bash
fluent-bit -e /path/to/built/out_grafana_loki.so -c fluent-bit.conf
```

You can also adapt your plugins.conf, removing the need to change the command line options:

```conf
[PLUGINS]
    Path /path/to/built/out_grafana_loki.so
```

## Configuration options

| Key                  | Description                                                                                                                                                                                                                                                                                                                                                                             | Default                                |
|----------------------|-----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|----------------------------------------|
| Url                  | Url of Loki server API endpoint.                                                                                                                                                                                                                                                                                                                                                        | http://localhost:3100/loki/api/v1/push |
| TenantID             | The tenant ID used by default to push logs to Loki. If omitted or empty it assumes Loki is running in single-tenant mode and no `X-Scope-OrgID` header is sent.                                                                                                                                                                                                                         | ""                                     |
| BatchWait            | Time to wait before send a log batch to Loki, full or not.                                                                                                                                                                                                                                                                                                                              | 1s                                     |
| BatchSize            | Log batch size to send a log batch to Loki (unit: Bytes).                                                                                                                                                                                                                                                                                                                               | 10 KiB (10 * 1024 Bytes)               |
| Timeout              | Maximum time to wait for Loki server to respond to a request.                                                                                                                                                                                                                                                                                                                           | 10s                                    |
| MinBackoff           | Initial backoff time between retries.                                                                                                                                                                                                                                                                                                                                                   | 500ms                                  |
| MaxBackoff           | Maximum backoff time between retries.                                                                                                                                                                                                                                                                                                                                                   | 5m                                     |
| MaxRetries           | Maximum number of retries when sending batches. Setting it to `0` will retry indefinitely.                                                                                                                                                                                                                                                                                                                                        | 10                                     |
| Labels               | Labels for API requests.                                                                                                                                                                                                                                                                                                                                                                | {job="fluent-bit"}                     |
| LogLevel             | LogLevel for plugin logger.                                                                                                                                                                                                                                                                                                                                                             | `info`                                 |
| RemoveKeys           | Specify removing keys.                                                                                                                                                                                                                                                                                                                                                                  | none                                   |
| AutoKubernetesLabels | If set to `true`, it will add all Kubernetes labels to Loki labels.                                                                                                                                                                                                                                                                                                                        | false                                  |
| LabelKeys            | Comma separated list of keys to use as stream labels. All other keys will be placed into the log line. LabelKeys is deactivated when using `LabelMapPath` label mapping configuration.                                                                                                                                                                                                  | none                                   |
| LineFormat           | Format to use when flattening the record to a log line. Valid values are `json` or `key_value`. If set to `json` the log line sent to Loki will be the fluentd record (excluding any keys extracted out as labels) dumped as json. If set to `key_value`, the log line will be each item in the record concatenated together (separated by a single space) in the format &lt;key&gt;=&lt;value&gt;. | json                                   |
| DropSingleKey        | If set to true and after extracting label_keys a record only has a single key remaining, the log line sent to Loki will just be the value of the record key.                                                                                                                                                                                                                            | true                                   |
| LabelMapPath         | Path to a json file defining how to transform nested records.                                                                                                                                                                                                                                                                                                                           | none                                   |
| Buffer               | Enable buffering mechanism.                                                                                                                                                                                                                                                                                                                                                              | false                                  |
| BufferType           | Specify the buffering mechanism to use (currently only `dque` is implemented).                                                                                                                                                                                                                                                                                                            | dque                                   |
| DqueDir              | Path to the directory for queued logs.                                                                                                                                                                                                                                                                                                                                                   | /tmp/flb-storage/loki                  |
| DqueSegmentSize      | Segment size in terms of number of records per segment.                                                                                                                                                                                                                                                                                                                                  | 500                                    |
| DqueSync             | Whether to fsync each queue change. Specify no fsync with `normal`, and fsync with `full`.                                                                                                                                                                                                                                                                                                                                                      | `normal`                                  |
| DqueName             | Queue name, must be unique per output.                                                                                                                                                                                                                                                                                                                                                     | dque                                   |

### Labels

Labels, for example `{container_name="nginx", cluster="us-west1"}`, are used to [query logs](https://grafana.com/docs/loki/<LOKI_VERSION>/query/).  Labels are usually metadata about the workload producing the log stream (`instance`, `container_name`, `region`, `cluster`, `level`). In Loki labels are indexed, so you should be cautious when choosing them. High cardinality label values can have drastic impact on query performance.

You can use the config parameters `Labels`, `RemoveKeys` , `LabelKeys` and `LabelMapPath` to instruct the output plugin how to perform labels extraction from your log entries or to add static labels to all log entries.

### AutoKubernetesLabels

If set to `true`, `AutoKubernetesLabels` will add all Kubernetes labels to Loki labels automatically and ignore parameters `LabelKeys`, `LabelMapPath`.

### LabelMapPath

When using the `Parser` and `Filter` plugins Fluent Bit can extract and add data to the current record/log data. While Loki labels are key value pairs, record data can be nested structures.
You can pass a JSON file that defines how to extract labels from each record. Each JSON key from the file will be matched with the log record to find label values. Values from the configuration are used as label names.

Considering the record below :

```json
{
  "kubernetes": {
    "container_name": "promtail",
    "pod_name": "promtail-xxx",
    "namespace_name": "prod",
    "labels" : {
        "team": "x-men"
    }
  },
  "HOSTNAME": "docker-desktop",
  "log" : "a log line",
  "time": "20190926T152206Z"
}
```

and a LabelMap file as follows :

```json
{
  "kubernetes": {
    "container_name": "container",
    "pod_name": "pod",
    "namespace_name": "namespace",
    "labels" : {
        "team": "team"
    }
  }
}
```

The labels extracted will be `{team="x-men", container="promtail", pod="promtail-xxx", namespace="prod"}`.

If you don't want the `kubernetes` and `HOSTNAME` fields to appear in the log line you can use the `RemoveKeys` configuration field. For example, `RemoveKeys kubernetes,HOSTNAME`.

### Buffering

Buffering refers to the ability to store the records somewhere, and while they are processed and delivered, still be able to continue storing more records. The Loki output plugin can be blocked by the Loki client because of its design:

- If the BatchSize is over the limit, the output plugin pauses receiving new records until the pending batch is successfully sent to the server
- If the Loki server is unreachable (retry 429s, 500s and connection-level errors), the output plugin blocks new records until the Loki server is available again, and the pending batch is successfully sent to the server or as long as the maximum number of attempts has been reached within configured back-off mechanism

The blocking state with some of the input plugins is not acceptable, because it can have an undesirable side effect on the part that generates the logs. Fluent Bit implements a buffering mechanism that is based on parallel processing. Therefore, it cannot send logs in order. There are two ways of handling the out-of-order logs: 

- Configure Loki to [accept out-of-order writes](https://grafana.com/docs/loki/<LOKI_VERSION>/configure/#accept-out-of-order-writes).

- Configure the Loki output plugin to use the buffering mechanism based on [`dque`](https://github.com/joncrlsn/dque), which is compatible with the Loki server strict time ordering:

    ```properties
    [Output]
        Name grafana-loki
        Match *
        Url http://localhost:3100/loki/api/v1/push
        Buffer true
        DqueSegmentSize 8096
        DqueDir /tmp/flb-storage/buffer
        DqueName loki.0
    ```

### Configuration examples

To configure the Loki output plugin add this section to your luent-bit.conf file.

```properties
[Output]
    Name grafana-loki
    Match *
    Url http://localhost:3100/loki/api/v1/push
    BatchWait 1s
    BatchSize 30720
    # (30KiB)
    Labels {test="fluent-bit-go", lang="Golang"}
    RemoveKeys key1,key2
    LabelKeys key3,key4
    LineFormat key_value
```

```properties
[Output]
    Name grafana-loki
    Match *
    Url http://localhost:3100/loki/api/v1/push
    BatchWait 1s
    BatchSize 30720 # (30KiB)
    AutoKubernetesLabels true
    RemoveKeys key1,key2
```

A full [example configuration file](https://github.com/grafana/loki/blob/main/clients/cmd/fluent-bit/fluent-bit.conf) is also available in the Loki repository.

### Running multiple plugin instances

You can run multiple plugin instances in the same fluent-bit process, for example if you want to push to different Loki servers or route logs into different Loki tenant IDs. To do so, add additional `[Output]` sections.
