# fluent-bit output plugin

[Fluent-bit](https://fluentbit.io/) is a Fast and Lightweight Data Forwarder, it can be configured with Loki [output plugin](https://fluentbit.io/documentation/0.12/output/) to ship logs to Loki. You can define which log files you want to collect using the [`Tail`](https://fluentbit.io/documentation/0.12/input/tail.html)  [input plugin](https://fluentbit.io/documentation/0.12/getting_started/input.html). Additionally fluent-bit support multiple `Filter` and `Parser` plugins (`Kubernetes`, `JSON`, etc..) to structure and alter log lines.

This plugin is implemented with the [fluent-bit's go plugin](https://github.com/fluent/fluent-bit-go) interface, it communicates with Loki server using a GRPC connection.

> syslog and systemd input plugin have not been tested yet, feedback appreciated.

## Configuration Options

| Key           | Description                                   | Default                             |
| --------------|-----------------------------------------------|-------------------------------------|
| Url           | Url of loki server API endpoint.               | http://localhost:3100/loki/api/v1/push |
| BatchWait     | Time to wait before send a log batch to Loki, full or not. (unit: sec) | 1 second   |
| BatchSize     | Log batch size to send a log batch to Loki (unit: Bytes).    | 10 KiB (10 * 1024 Bytes) |
| Labels        | labels for API requests.                       | {job="fluent-bit"}                    |
| LogLevel      | LogLevel for plugin logger.                    | "info"                              |
| RemoveKeys    | Specify removing keys.                         | none                                |
| LabelKeys     | Comma separated list of keys to use as stream labels. All other keys will be placed into the log line. LabelKeys is desactivated when using `LabelMapPath` label mapping configuration. | none |
| LineFormat    | Format to use when flattening the record to a log line. Valid values are "json" or "key_value". If set to "json" the log line sent to Loki will be the fluentd record (excluding any keys extracted out as labels) dumped as json. If set to "key_value", the log line will be each item in the record concatenated together (separated by a single space) in the format <key>=<value>. | json |
| DropSingleKey | if set to true and after extracting label_keys a record only has a single key remaining, the log line sent to Loki will just be the value of the record key.| true |
| LabelMapPath | Path to a json file defining how to transform nested records. | none

### Labels

Labels are used to [query logs](../../docs/logql.md) `{container_name="nginx", cluster="us-west1"}`, they are usually metadata about the workload producing the log stream (`instance`, `container_name`, `region`, `cluster`, `level`). Like in Prometheus labels are indexed consequently you should be cautious when choosing them. (high cardinality label values can have performance drastic impact).

You can use `Labels`, `RemoveKeys` , `LabelKeys` and `LabelMapPath` to how the output plugin will perform labels extraction.

### LabelMapPath

When using `Parser` and `Filter` plugin fluent-bit can extract and add data to the current record/log data. While Loki labels are key value pair, record data can be nested structures.
You can pass a json file to defines how to extract [labels](../../docs/overview/README.md#overview-of-loki) from each record. Each json key from the file will be matched with the log record to find label values. Values from the configuration are used as label names.

Considering the record below :

```json
{
  "kubernetes": {
    "container_name": "promtail",
    "pod_name": "promtail-xxx",
    "namespace_name": "prod",
    "labels" : {
        "team": "x-men",
    },
  },
  "HOSTNAME": "docker-desktop",
  "log" : "a log line",
  "time": "20190926T152206Z",
}
```

and a LabelMap file as follow :

```json
{
  "kubernetes": {
    "container_name": "container",
    "pod_name": "pod",
    "namespace_name": "namespace",
    "labels" : {
        "team": "team",
    },
  },
}
```

The labels extracted will be `{team="x-men", container="promtail", pod="promtail-xxx", namespace="prod"}`

If you don't want the `kubernetes` and `HOSTNAME` fields to appear in the log line you can use the `RemoveKeys` configuration. (e.g. `RemoveKeys kubernetes,HOSTNAME`).

### Configuration examples

To configure Loki output plugin add this section to fluent-bit.conf

```properties
[Output]
    Name loki
    Match *
    Url http://localhost:3100/loki/api/v1/push
    BatchWait 1 # (1sec)
    BatchSize 30720 # (30KiB)
    Labels {test="fluent-bit-go", lang="Golang"}
    RemoveKeys key1,key2
    LabelKeys key3,key4
    LineFormat key_value
```

A full example of configuration [file](fluent-bit.conf) is also available in this repository.

## Building

## Prerequisites

* Go 1.11+
* gcc (for cgo)

```bash
make fluent-bit-plugin
```

## Usage

## Local

If you have fluent-bit installed in your `$PATH` you can run the plugin using:

```bash
fluent-bit -e /path/to/built/out_loki.so -c fluent-bit.conf
```

## Docker

You can run a fluent-bit container with Loki output plugin pre-installed using our [docker hub](https://cloud.docker.com/u/grafana/repository/docker/grafana/fluent-bit-plugin-loki) image:

```bash
docker run -v /var/log:/var/log \
    -e LOG_PATH="/var/log/*.log" -e LOKI_URL="http://localhost:3100/loki/api/v1/push" \
    grafana/fluent-bit-plugin-loki:latest
```

## Kubernetes

You can run fluent-bit as a DaemonSet to collect all your Kubernetes workload logs.

To do so you can use our [fluent-bit helm chart](../../production/helm/fluent-bit/README.m):

> Make sure [tiller](https://helm.sh/docs/install/) is installed correctly in your cluster

```bash
helm repo add loki https://grafana.github.io/loki/charts
helm repo update
helm upgrade --install fluent-bit loki/fluent-bit \
    --set loki.serviceName=loki.svc.cluster.local
```

By default it will collect all containers logs and extract labels from Kubernetes API (`container_name`, `namespace`, etc..).

Alternatively you can install the Loki and fluent-bit all together using:

```bash
helm upgrade --install loki-stack loki/loki-stack \
    --set fluent-bit.enabled=true,promtail.enabled=false
```
