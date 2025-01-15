{
  cpu: (import './cpu.libsonnet').new,
  latency: (import './latency.libsonnet').new,
  memory: (import './memory.libsonnet').new,
  qps: (import './qps.libsonnet').new,
  throughput: (import './throughput.libsonnet').new,
  queueDuration: (import './queue-duration.libsonnet').new,
  queueLength: (import './queue-length.libsonnet').new,
}
