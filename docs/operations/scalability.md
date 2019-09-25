# Scaling with Loki

See this
[blog post](https://grafana.com/blog/2018/12/12/loki-prometheus-inspired-open-source-logging-for-cloud-natives/)
on a discussion about Loki's scalability.

When scaling Loki, operators should consider running several Loki processes
partitioned by role (ingester, distributor, querier) rather than a single Loki
process. Grafana Labs' [production setup](../../production/ksonnet/loki)
contains `.libsonnet` files that demonstrates configuring separate components
and scaling for resource usage.
