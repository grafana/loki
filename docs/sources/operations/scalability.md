---
title: Scalability
---
# Scaling with Loki

See [Loki: Prometheus-inspired, open source logging for cloud natives](https://grafana.com/blog/2018/12/12/loki-prometheus-inspired-open-source-logging-for-cloud-natives/)
for a discussion about Loki's scalability.

When scaling Loki, operators should consider running several Loki processes
partitioned by role (ingester, distributor, querier) rather than a single Loki
process. Grafana Labs' [production setup](https://github.com/grafana/loki/blob/master/production/ksonnet/loki)
contains `.libsonnet` files that demonstrates configuring separate components
and scaling for resource usage.
