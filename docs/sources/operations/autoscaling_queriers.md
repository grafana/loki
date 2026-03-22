---
title: Manage varying workloads at scale with autoscaling queriers
menuTitle: Autoscaling queriers
description: Describes how to use KEDA to autoscale the quantity of queriers for a microsevices mode Kubernetes deployment.
weight: 
---
# Manage varying workloads at scale with autoscaling queriers

A microservices deployment of a Loki cluster that runs on Kubernetes typically handles a
workload that varies throughout the day.
To make Loki easier to operate and optimize the cost of running Loki at scale,
we have designed a set of resources to help you autoscale your Loki queriers.

## Prerequisites

You need to run Loki in Kubernetes as a set of microservices. You need to use the query-scheduler.

We recommend using [Kubernetes Event-Driven Autoscaling (KEDA)](https://keda.sh/) to configure autoscaling
based on Prometheus metrics. Refer to [Deploying KEDA](https://keda.sh/docs/latest/deploy) to learn more
about setting up KEDA in your Kubernetes cluster.

## Scaling metric

Because queriers pull queries from the query-scheduler queue and process them on the querier workers, you should scale metrics based on:

- The scheduler queue size.
- The queries running in the queriers.

The query-scheduler exposes the `loki_query_scheduler_inflight_requests` metric.
It tracks the sum of queued queries plus the number of queries currently running in the querier workers.
The following query is useful to scale queriers based on the inflight requests.

```promql
sum(
  max_over_time(
    loki_query_scheduler_inflight_requests{namespace="loki-cluster", quantile="<Q>"}[<R>]
  )
)
```

Use the quantile (Q) and the range (R) parameters to fine-tune the metric.
The higher Q is, the more sensitive the metric is to short-lasting spikes.
As R increases, you can reduce the variation over time in the metric.
A higher R-value helps avoid the autoscaler from modifying the number of replicas too frequently.

In our experience, we have found that a Q of 0.75 and an R of 2 minutes work well.
You can adjust these values according to your workload.

## Cluster capacity planning

To scale the Loki queries, you configure the following settings:

- The threshold for scaling up and down
- The scale down stabilization period
- The minimum and the maximum number of queriers

Querier workers process queries from the queue. You can configure each Loki querier to run several workers.
To reserve workforce headroom to address workload spikes, our recommendation is not to use more than 75% of the workers.
For example, if you configure the Loki queriers to run 6 workers, set a threshold of `floor(0.75 * 6) = 4`.

To determine the minimum number of queries that you should run, run at least one querier and determine the average
number of inflight requests the system processes 75% of the time over seven days. The target utilization of the queries is 75%.
So if we use 6 workers per querier, we will use the following query:

```promql
clamp_min(ceil(
    avg(
        avg_over_time(loki_query_scheduler_inflight_requests{namespace="loki-cluster", quantile="0.75"}[7d])
    ) / scalar(floor(vector(6 * 0.75)))
), 1)
```

The maximum number of queriers to run is equal to the number of queriers required to process all inflight
requests 50% of the time during a seven-day timespan.
As for the previous example, if each querier runs 6 workers, divide the inflight requests by 6.
The resulting query becomes:

```promql
ceil(
    max(
        max_over_time(loki_query_scheduler_inflight_requests{namespace="loki-cluster", quantile="0.5"}[7d])
    ) / 6
)
```

To minimize the scenario where Loki scales up shortly after scaling down, set
a stabilization window for scaling down.


### KEDA configuration

This [KEDA ScaledObject](https://keda.sh/docs/latest/concepts/scaling-deployments/) example configures autoscaling
for the querier deployment in the `loki-cluster` namespace.
The example shows the minimum number of replicas set to 10 and the maximum number of replicas set to 50.
Because each querier runs 6 workers, aiming to use 75% of those workers, the threshold is set to 4.
The metric is served at `http://prometheus.default:9090/prometheus`. We configure a stabilization window of 30 minutes.

```yaml
apiVersion: keda.sh/v1alpha1
kind: ScaledObject
metadata:
  name: querier
  namespace: loki-cluster
spec:
  maxReplicaCount: 50
  minReplicaCount: 10
  scaleTargetRef:
    kind: Deployment
    name: querier
  triggers:
  - metadata:
      metricName: querier_autoscaling_metric
      query: sum(max_over_time(loki_query_scheduler_inflight_requests{namespace="loki-cluster", quantile="0.75"}[2m]))
      serverAddress: http://prometheus.default:9090/prometheus
      threshold: "4"
    type: prometheus
  advanced:
    horizontalPodAutoscalerConfig:
      behavior:
        scaleDown:
          stabilizationWindowSeconds: 1800
```

## Prometheus alerting when at capacity

Because the configured maximum might not be sufficient, a Prometheus alert can identify
when the quantity of queriers has been at its configured maximum for an extended time. The following example specifies three hours (`3h`) as the extended time:

```yaml
name: LokiAutoscalerMaxedOut
expr: kube_horizontalpodautoscaler_status_current_replicas{namespace=~"loki-cluster"} == kube_horizontalpodautoscaler_spec_max_replicas{namespace=~"loki-cluster"}
for: 3h
labels:
  severity: warning
annotations:
  description: HPA {{ $labels.namespace }}/{{ $labels.horizontalpodautoscaler }} has been running at max replicas for longer than 3h; this can indicate underprovisioning.
  summary: HPA has been running at max replicas for an extended time
```


