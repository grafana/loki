---
title: Scalability
weight: 30
---
# Scaling with Loki

See [Loki: Prometheus-inspired, open source logging for cloud natives](https://grafana.com/blog/2018/12/12/loki-prometheus-inspired-open-source-logging-for-cloud-natives/)
for a discussion about Loki's scalability.

When scaling Loki, operators should consider running several Loki processes
partitioned by role (ingester, distributor, querier) rather than a single Loki
process. Grafana Labs' [production setup](https://github.com/grafana/loki/blob/master/production/ksonnet/loki)
contains `.libsonnet` files that demonstrates configuring separate components
and scaling for resource usage.

## Separate Query Scheduler

The Query frontend has an in-memory queue that can be moved out into a separate process similar to the [Cortex Query Scheduler](https://cortexmetrics.io/docs/operations/scaling-query-frontend/#query-scheduler). This allows running multiple query frontends.

In order to run with the Query Scheduler, the frontend needs to be passed the scheduler's address via `-frontend.scheduler-address` and the querier processes needs to be started with `-querier.scheduler-address` set to the same address. Both options can also be defined via the [configuration file](../configuration).

It is not valid to start the querier with both a configured frontend and a scheduler address. 

The query scheduler process itself can be started via the `-target=query-scheduler` option of the Loki Docker image. For instance, `docker run grafana/loki:latest -config.file=/cortex/config/cortex.yaml -target=query-scheduler -server.http-listen-port=8009 -server.grpc-listen-port=9009` starts the query scheduler listening on ports `8009` and `9009`.
