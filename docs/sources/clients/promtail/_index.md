---
title: Promtail
description: Promtail
weight: 10
---
# Promtail

Promtail is an agent which ships the contents of local logs to a private Grafana Loki
instance or [Grafana Cloud](/oss/loki). It is usually
deployed to every machine that has applications needed to be monitored.

It primarily:

- Discovers targets
- Attaches labels to log streams
- Pushes them to the Loki instance.

Currently, Promtail can tail logs from two sources: local log files and the
systemd journal (on AMD64 machines only).

## Log file discovery

Before Promtail can ship any data from log files to Loki, it needs to find out
information about its environment. Specifically, this means discovering
applications emitting log lines to files that need to be monitored.

Promtail borrows the same
[service discovery mechanism from Prometheus](https://prometheus.io/docs/prometheus/latest/configuration/configuration/#scrape_config),
although it currently only supports `static` and `kubernetes` service
discovery. This limitation is due to the fact that Promtail is deployed as a
daemon to every local machine and, as such, does not discover label from other
machines. `kubernetes` service discovery fetches required labels from the
Kubernetes API server while `static` usually covers all other use cases.

Just like Prometheus, `promtail` is configured using a `scrape_configs` stanza.
`relabel_configs` allows for fine-grained control of what to ingest, what to
drop, and the final metadata to attach to the log line. Refer to the docs for
[configuring Promtail]({{<relref "configuration.md">}}) for more details.

### Support for compressed files

Promtail now has native support for ingesting compressed files by a mechanism that
relies on file extensions. If a discovered file has an expected compression file
extension, Promtail will **lazily** decompress the compressed file and push the
parsed data to Loki. Important details are:
* It relies on the `\n` character to separate the data into different log lines.
* The max expected log line is 2MB bytes within the compressed file.
* The data is decompressed in blocks of 4096 bytes. i.e: it first fetches a block of 4096 bytes
  from the compressed file and process it. After processing this block and pushing the data to Loki,
  it fetches the following 4096 bytes, and so on.
* It supports the following extensions:
  - `.gz`: Data will be decompressed with the native Gunzip Golang pkg (`pkg/compress/gzip`)
  - `.z`: Data will be decompressed with the native Zlib Golang pkg (`pkg/compress/zlib`)
  - `.bz2`: Data will be decompressed with the native Bzip2 Golang pkg (`pkg/compress/bzip2`)
  - `.tar.gz`: Data will be decompressed exactly as the `.gz` extension.
      However, because `tar` will add its metadata at the beggining of the
      compressed file, **the first parsed line will contains metadata together with
      your log line**. It is illustrated at
      `./clients/pkg/promtail/targets/file/decompresser_test.go`.
* `.zip` extension isn't supported as of now because it doesn't support some of the interfaces
  Promtail requires. We have plans to add support for it in the near future.
* The decompression is quite CPU intensive and a lot of allocations are expected
  to work, especially depending on the size of the file. You can expect the number
  of garbage collection runs and the CPU usage to skyrocket, but no memory leak is
  expected.
* Positions are supported. That means that, if you interrupt Promtail after
  parsing and pushing (for example) 45% of your compressed file data, you can expect Promtail
  to resume work from the last scraped line and process the rest of the remaining 55%.
* Since decompression and pushing can be very fast, depending on the size
  of your compressed file Loki will rate-limit your ingestion. In that case you
  might configure Promtail's [`limits` stage](/docs/loki/latest/clients/promtail/stages/limit/) to slow the pace or increase
  [ingestion limits on Loki](/docs/loki/latest/configuration/#limits_config).
* Log rotations **aren't supported as of now**, mostly because it requires us modifying Promtail to
  rely on file inodes instead of file names. If you'd like to see support for it, please create a new
  issue on Github asking for it and explaining your use case.
* If you would like to see support for a compression protocol that isn't listed here, please
  create a new issue on Github asking for it and explaining your use case.


## Loki Push API

Promtail can also be configured to receive logs from another Promtail or any Loki client by exposing the [Loki Push API]({{<relref "../../api#push-log-entries-to-loki">}}) with the [loki_push_api]({{<relref "configuration#loki_push_api">}}) scrape config.

There are a few instances where this might be helpful:

- complex network infrastructures where many machines having egress is not desirable.
- using the Docker Logging Driver and wanting to provide a complex pipeline or to extract metrics from logs.
- serverless setups where many ephemeral log sources want to send to Loki, sending to a Promtail instance with `use_incoming_timestamp` == false can avoid out-of-order errors and avoid having to use high cardinality labels.

## Receiving logs From Syslog

When the [Syslog Target]({{<relref "configuration#syslog">}}) is being used, logs
can be written with the syslog protocol to the configured port.

## AWS

If you need to run Promtail on Amazon Web Services EC2 instances, you can use our [detailed tutorial]({{<relref "../aws/ec2/">}}).

## Labeling and parsing

During service discovery, metadata is determined (pod name, filename, etc.) that
may be attached to the log line as a label for easier identification when
querying logs in Loki. Through `relabel_configs`, discovered labels can be
mutated into the desired form.

To allow more sophisticated filtering afterwards, Promtail allows to set labels
not only from service discovery, but also based on the contents of each log
line. The `pipeline_stages` can be used to add or update labels, correct the
timestamp, or re-write log lines entirely. Refer to the documentation for
[pipelines]({{<relref "pipelines">}}) for more details.

## Shipping

Once Promtail has a set of targets (i.e., things to read from, like files) and
all labels are set correctly, it will start tailing (continuously reading) the
logs from targets. Once enough data is read into memory or after a configurable
timeout, it is flushed as a single batch to Loki.

As Promtail reads data from sources (files and systemd journal, if configured),
it will track the last offset it read in a positions file. By default, the
positions file is stored at `/var/log/positions.yaml`. The positions file helps
Promtail continue reading from where it left off in the case of the Promtail
instance restarting.

## API

Promtail features an embedded web server exposing a web console at `/` and the following API endpoints:

### `GET /ready`

This endpoint returns 200 when Promtail is up and running, and there's at least one working target.

### `GET /metrics`

This endpoint returns Promtail metrics for Prometheus. Refer to
[Observing Grafana Loki]({{<relref "../../operations/observability/">}}) for the list
of exported metrics.

### Promtail web server config

The web server exposed by Promtail can be configured in the Promtail `.yaml` config file:

```yaml
server:
  http_listen_address: 127.0.0.1
  http_listen_port: 9080
```
