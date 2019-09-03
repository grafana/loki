# Overview
Promtail is an agent which ships the content of local log files to Loki. It is
usually deployed to every machine that has applications needed to be monitored.

It primarily **discovers** targets, attaches **labels** to log streams and
**pushes** them to the Loki instance.

### Discovery
Before Promtail is able to ship anything to Loki, it needs to find about its
environment. This specifically means discovering applications emitting log lines
that need to be monitored.

Promtail borrows the [service discovery mechanism from
Prometheus](https://prometheus.io/docs/prometheus/latest/configuration/configuration/#scrape_config),
although it currently only supports `static` and `kubernetes` service discovery.
This is due to the fact that `promtail` is deployed as a daemon to every local
machine and does not need to discover labels from other systems. `kubernetes`
service discovery fetches required labels from the api-server, `static` usually
covers the other use cases.

Just like Prometheus, `promtail` is configured using a `scrape_configs` stanza.
`relabel_configs` allows fine-grained control of what to ingest, what to drop
and the final metadata attached to the log line. Refer to the
[configuration](configuration.md) for more details.

### Labeling and Parsing
During service discovery, metadata is determined (pod name, filename, etc.) that
may be attached to the log line as a label for easier identification afterwards.
Using `relabel_configs`, those discovered labels can be mutated into the form
they should have for querying.

To allow more sophisticated filtering afterwards, Promtail allows to set labels
not only from service discovery, but also based on the contents of the log
lines. The so-called `pipeline_stages` can be used to add or update labels,
correct the timestamp or rewrite the log line entirely. Refer to the [logentry
processing documentation](../logentry/processing-log-lines.md) for more details.

### Shipping
Once Promtail is certain about what to ingest and all labels are set correctly,
it starts *tailing* (continuously reading) the log files from the applications.
Once enough data is read into memory, it is flushed in as a batch to Loki.
