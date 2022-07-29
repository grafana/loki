---
title: Autoscaling in Kubernetes
weight: 30
---
# Autoscaling Grafana Loki in Kubernetes

<!--- TODO: Link to blogpost -->

A Loki cluster will typically handle a workload that varies throughout the day.
To make Loki easier to operate and optimize the cost of running Loki at scale,
we have designed a set of resources to help you autoscale your Loki cluster,
so you don't need to scale up and down resources manually as your workload changes.

## Prerequisites

We use [KEDA (*Kubernetes-based Event Driven Autoscaler*)](https://keda.sh/) to configure autoscaling based
on Prometheus metrics.  Please refer to their documentation for more information about how to install
and operate KEDA on your Kubernetes cluster.

## Querier Autoscaling

Queriers pull queries from the query scheduler queue and process them on the querier workers. Therefore, it makes sense to scale based on:

- The scheduler queue size.
- The queries running in the queriers.

### Scaling metric

The Query Scheduler exposes the `cortex_query_scheduler_inflight_requests` metric.
It tracks the number of queued queries plus the number of queries currently running in the querier workers.
The following query is useful to scale queriers based on the inflight requests.

```
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

In our experience, we have found that a Q of 0.75 and R of 2 minutes work well. But you will need to adjust it to your workload.

### KEDA ScaledObject

As stated previously, we recommend using KEDA to scale your Loki queriers. We need to configure a few things:

- The threshold for scaling up and down.
- Scale down stabilization period.
- The minimum and the maximum number of queriers.

Querier workers process queries from the queue, each of our queriers can be configured to run several workers.
We should leave some workforce headroom for spikes, so we recommend aiming to use 75% of those workers. Therefore,
if we configure our queriers to run 6 workers, we should use a threshold of `floor(0.75 * 6) = 4`.

For the minimum number of replicas, we recommend running at least one queriers and look at the average number
of inflight requests 75% of the time in the last seven days, targeting a 75% utilization of our queriers.
So if we use 6 workers per querier, we should use the following query:

```
clamp_min(ceil(
    avg(
        avg_over_time(cortex_query_scheduler_inflight_requests{namespace="loki-cluster", quantile="0.75"}[7d])
    ) / scalar(floor(vector(6 * 0.75)))
), 1)
```

For the maximum number of replicas, we recommend running the number of queriers that would be required
to process all the inflight requests you had 50% of the time during the last seven days.
As for the previous example, if each querier runs 6 workers, we divide the inflight requests by 6.
The resulting query looks as follows.

```
ceil(
    max(
        max_over_time(cortex_query_scheduler_inflight_requests{namespace="loki-cluster", quantile="0.5"}[7d])
    ) / 6
)
```

Finally, with regard to the scale-down stabilization period, we recommend using a period of at least 30 minutes
to minimize the scenario where we scale up shortly after scaling down.

The following KEDA ScaledObject configures autoscaling for the querier deployment in the `loki-cluster` namespace.
We set the minimum number of replicas to 10 and the maximum number of replicas to 50.
Since each querier runs 6 workers, aiming to use 75% of those workers, we set the threshold to 4.
The metric is served at `http://prometheus.default:9090/prometheus`.

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

### Alerting

To know if we may need to set a higher maximum number of querier replicas, we can configure the following alert that fires
when the autoscaler runs at the maximum configured number of replicas for an extended time (e.g., 3 hours).

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


