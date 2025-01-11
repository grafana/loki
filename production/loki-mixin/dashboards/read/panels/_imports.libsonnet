{
  qps: (import './qps.libsonnet').new,
  latency: (import './latency.libsonnet').new,
  latencyDistribution: (import './latency-distribution.libsonnet').new,
  perPodLatency: (import './per-pod-latency.libsonnet').new,
  routeLatency: (import './route-latency.libsonnet').new,
}
