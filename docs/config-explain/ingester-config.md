Below is the loki part of a Loki sample config:
```yaml
ingester:
  lifecycler:
    address: 127.0.0.1
    ring:
      kvstore:
        store: consul
        consul:
          host: 127.0.0.1:8500
          prefix: ''
          httpclienttimeout: 20s
          consistentreads: true
      replication_factor: 1
  concurrent_flushes: 20
  flush_check_period: 30s
  flush_op_timeout: 10s
  chunk_retain_period: 1m
  chunk_idle_period: 5m
  chunk_block_size: 524288
```

Explain all except lifecycler part as below:

| config | usage | default |
| --- | --- | --- |
| concurrent_flushes | Number of concurrent goroutines used for flushing | 16 |
| flush_check_period | Period with which to attempt to flush chunks | 30s |
| flush_op_timeout | Timeout for individual flush operations | 10s |
| chunk_retain_period | Period chunks will remain in memory after flushing | 15m |
| chunk_idle_period | Maximum chunk idle time before flushing | 30m |
| chunk_block_size | Size of a chunk used for holding logs | 256*1024 |