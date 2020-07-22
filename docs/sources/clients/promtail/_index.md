---
title: Promtail
---
# Promtail

Promtail is an agent which ships the contents of local logs to a private Loki
instance or [Grafana Cloud](https://grafana.com/oss/loki). It is usually
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
discovery. This limitation is due to the fact that `promtail` is deployed as a
daemon to every local machine and, as such, does not discover label from other
machines. `kubernetes` service discovery fetches required labels from the
Kubernetes API server while `static` usually covers all other use cases.

Just like Prometheus, `promtail` is configured using a `scrape_configs` stanza.
`relabel_configs` allows for fine-grained control of what to ingest, what to
drop, and the final metadata to attach to the log line. Refer to the docs for
[configuring Promtail](configuration/) for more details.

## Loki Push API

Promtail can also be configured to receive logs from another Promtail or any Loki client by exposing the [Loki Push API](../../api#post-lokiapiv1push) with the [loki_push_api](configuration#loki_push_api_config) scrape config.

There are a few instances where this might be helpful:

* complex network infrastructures where many machines having egress is not desirable.
* using the Docker Logging Driver and wanting to provide a complex pipeline or to extract metrics from logs.
* serverless setups where many ephemeral log sources want to send to Loki, sending to a Promtail instance with `use_incoming_timestamp` == false can avoid out of order errors and avoid having to use high cardinality labels.

## Receiving logs From Syslog

When the [Syslog Target](configuration#syslog_config) is being used, logs
can be written with the syslog protocol to the configured port.

## AWS

If you need to run Promtail on Amazon Web Services EC2 instances, you can use our [detailed tutorial](../aws/ec2/).

## Labeling and parsing

During service discovery, metadata is determined (pod name, filename, etc.) that
may be attached to the log line as a label for easier identification when
querying logs in Loki. Through `relabel_configs`, discovered labels can be
mutated into the desired form.

To allow more sophisticated filtering afterwards, Promtail allows to set labels
not only from service discovery, but also based on the contents of each log
line. The `pipeline_stages` can be used to add or update labels, correct the
timestamp, or re-write log lines entirely. Refer to the documentation for
[pipelines](pipelines/) for more details.

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

This endpoint returns Promtail metrics for Prometheus. See
"[Operations > Observability](../../operations/observability/)" to get a list
of exported metrics.

### Promtail web server config

The web server exposed by Promtail can be configured in the Promtail `.yaml` config file:

```yaml
server:
  http_listen_address: 127.0.0.1
  http_listen_port: 9080
```
