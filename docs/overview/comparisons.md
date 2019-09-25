# Loki compared to other log systems

## Loki / Promtail / Grafana vs EFK

The EFK (Elasticsearch, Fluentd, Kibana) stack is used to ingest, visualize, and
query for logs from various sources.

Data in Elasticsearch is stored on-disk as unstructured JSON objects. Both the
keys for each object and the contents of each key are indexed. Data can then be
queried using a JSON object to define a query (called the Query DSL) or through
the Lucene query language.

In comparison, Loki in single-binary mode can store data on-disk, but in
horizontally-scalable mode data is stored in a cloud storage system such as S3,
GCS, or Cassandra. Logs are stored in plaintext form tagged with a set of label
names and values, where only the label pairs are indexed. This tradeoff makes it
cheaper to operate than a full index and allows developers to aggressively log
from their applications. Logs in Loki are queried using [LogQL](../logql.md).
However, because of this design tradeoff, LogQL queries that filter based on
content (i.e., text within the log lines) require loading all chunks within the
search window that match the labels defined in the query.

Fluentd is usually used to collect and forward logs to Elasticsearch. Fluentd is
called a data collector which can ingest logs from many sources, process it, and
forward it to one or more targets.

In comparison, Promtail's use case is specifically tailored to Loki. Its main mode
of operation is to discover log files stored on disk and forward them associated
with a set of labels to Loki. Promtail can do service discovery for Kubernetes
pods running on the same node as Promtail, act as a container sidecar or a
Docker logging driver, read logs from specified folders, and tail the systemd
journal.

The way Loki represents logs by a set of label pairs is similar to how
[Prometheus](https://prometheus.io) represents metrics. When deployed in an
environment alongside Prometheus, logs from Promtail usually have the same
labels as your applications metrics thanks to using the same service
discovery mechanisms. Having logs and metrics with the same levels enables users
to seamlessly context switch between metrics and logs, helping with root cause
analysis.

Kibana is used to visualize and search Elasticsearch data and is very powerful
for doing analytics on that data. Kibana provides many visualization tools to do
data analysis, such as location maps, machine learning for anomaly detection,
and graphs to discover relationships in data. Alerts can be configured to notify
users when an unexpected condition occurs.

In comparison, Grafana is tailored specifically towards time series data from
sources like Prometheus and Loki. Dashboards can be set up to visualize metrics
(log support coming soon) and an explore view can be used to make ad-hoc queries
against your data. Like Kibana, Grafana supports alerting based on your metrics.
