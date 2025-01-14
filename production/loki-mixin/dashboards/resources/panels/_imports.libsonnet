{
  cpu: (import './cpu.libsonnet').new,
  diskReads: (import './disk-reads.libsonnet').new,
  diskWrites: (import './disk-writes.libsonnet').new,
  memoryWorkingSet: (import './memory-working-set.libsonnet').new,
  memoryGoHeap: (import './memory-go-heap.libsonnet').new,
  memoryStreams: (import './memory-streams.libsonnet').new,
}
