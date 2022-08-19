---
title: Autoscaling Loki queriers
menuTitle: Autoscaling queriers
description: Kubernetes deployments of a microservices mode Loki cluster can use KEDA to autoscale the quantity of queriers.
weight: 30
---

# Autoscaling Loki queriers

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

The query-scheduler exposes the `cortex_query_scheduler_inflight_requests` metric.
It tracks the sum of queued queries plus the number of queries currently running in the querier workers.
The following query is useful to scale queriers based on the inflight requests.

```promql
sum(
  max_over_time(
    cortex_query_scheduler_inflight_requests{namespace="loki-cluster", quantile="<Q>"}[<R>]
  )
)
```

We can use the quantile (Q) and the range (R) parameters to fine-tune the metric.
The higher Q is, the more sensitive the metric is to short-lasting spikes.
As R increases, we reduce the variation over time in the metric.
A higher value for R helps avoid the autoscaler from modifying the number of replicas too frequently.

In our experience, we have found that a Q of 0.75 and an R of 2 minutes work well.
Our recommendation is to adjust these values according to your workload.

## Cluster capacity planning

When configuring KEDA to scale your Loki queriers, we need to configure:

- The threshold for scaling up and down
- The scale down stabilization period
- The minimum and the maximum number of queriers

Querier workers process queries from the queue. Each of our queriers can be configured to run several workers.
Our recommendation is to aim for using 75% of those workers, to leave some workforce headroom for workload spikes.
Therefore, if we configure our queriers to run 6 workers, we will set a threshold of `floor(0.75 * 6) = 4`.

Our recommendation for the minimum number of queriers is to run at least one querier, and to look at the average
number of inflight requests 75% of the time in the last seven days, targeting a 75% utilization of the queriers.
So if we use 6 workers per querier, we will use the following query:

```promql
clamp_min(ceil(
    avg(
        avg_over_time(cortex_query_scheduler_inflight_requests{namespace="loki-cluster", quantile="0.75"}[7d])
    ) / scalar(floor(vector(6 * 0.75)))
), 1)
```

Our recommendation for the maximum number of queriers is running the number of queriers that would be required
to process all the inflight requests you had 50% of the time during the last seven days.
As for the previous example, if each querier runs 6 workers, we divide the inflight requests by 6.
The resulting query becomes:

```promql
ceil(
    max(
        max_over_time(cortex_query_scheduler_inflight_requests{namespace="loki-cluster", quantile="0.5"}[7d])
    ) / 6
)
```

Finally, to minimize the scenario where we scale up shortly after scaling down, we recommend setting
a stabilization window for scaling down.


### KEDA configuration

This [KEDA ScaledObject](https://keda.sh/docs/latest/concepts/scaling-deployments/) example configures autoscaling
for the querier deployment in the `loki-cluster` namespace.
We set the minimum number of replicas to 10 and the maximum number of replicas to 50.
Since each querier runs 6 workers, aiming to use 75% of those workers, we set the threshold to 4.
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
      query: sum(max_over_time(cortex_query_scheduler_inflight_requests{namespace="loki-cluster", quantile="0.75"}[2m]))
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

Because the configured maximum may not be enough, a Prometheus alert can identify
when the quantity of queriers has been at its configured maximum for an extended time. This example specifies 3 hours as the extended time:

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


