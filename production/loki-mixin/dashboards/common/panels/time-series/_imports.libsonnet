{
  cpu: (import './cpu.libsonnet').new,
  latency: (import './latency.libsonnet').new,
  memory: (import './memory.libsonnet').new,
  qps: (import './qps.libsonnet').new,
  throughput: (import './throughput.libsonnet').new,
}
