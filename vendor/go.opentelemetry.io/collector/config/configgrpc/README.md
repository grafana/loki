# gRPC Configuration Settings

gRPC exposes a [variety of settings](https://godoc.org/google.golang.org/grpc).
Several of these settings are available for configuration within individual
receivers or exporters. In general, none of these settings should need to be
adjusted.

## Client Configuration

[Exporters](https://github.com/open-telemetry/opentelemetry-collector/blob/main/exporter/README.md)
leverage client configuration.

Note that client configuration supports TLS configuration, the
configuration parameters are also defined under `tls` like server
configuration. For more information, see [configtls
README](../configtls/README.md).

- [`balancer_name`](https://github.com/grpc/grpc-go/blob/master/examples/features/load_balancing/README.md)
- `compression` Compression type to use among `gzip`, `snappy`, `zstd`, and `none`.
- `endpoint`: Valid value syntax available [here](https://github.com/grpc/grpc/blob/master/doc/naming.md)
- [`tls`](../configtls/README.md)
- `headers`: name/value pairs added to the request
- [`keepalive`](https://godoc.org/google.golang.org/grpc/keepalive#ClientParameters)
  - `permit_without_stream`
  - `time`
  - `timeout`
- [`read_buffer_size`](https://godoc.org/google.golang.org/grpc#ReadBufferSize)
- [`write_buffer_size`](https://godoc.org/google.golang.org/grpc#WriteBufferSize)

Please note that [`per_rpc_auth`](https://pkg.go.dev/google.golang.org/grpc#PerRPCCredentials) which allows the credentials to send for every RPC is now moved to become an [extension](https://github.com/open-telemetry/opentelemetry-collector-contrib/blob/main/extension/bearertokenauthextension). Note that this feature isn't about sending the headers only during the initial connection as an `authorization` header under the `headers` would do: this is sent for every RPC performed during an established connection.

Example:

```yaml
exporters:
  otlp:
    endpoint: otelcol2:55690
    tls:
      ca_file: ca.pem
      cert_file: cert.pem
      key_file: key.pem
    headers:
      test1: "value1"
      "test 2": "value 2"
```

### Compression Comparison

[configgrpc_benchmark_test.go](./configgrpc_benchmark_test.go) contains benchmarks comparing the supported compression algorithms. It performs compression using `gzip`, `zstd`, and `snappy` compression on small, medium, and large sized log, trace, and metric payloads. Each test case outputs the uncompressed payload size, the compressed payload size, and the average nanoseconds spent on compression. 

The following table summarizes the results, including some additional columns computed from the raw data. The benchmarks were performed on an AWS m5.large EC2 instance with an Intel(R) Xeon(R) Platinum 8259CL CPU @ 2.50GHz.

| Request           | Compressor | Raw Bytes | Compressed bytes | Compression ratio | Ns / op | Mb compressed / second | Mb saved / second |
|-------------------|------------|-----------|------------------|-------------------|---------|------------------------|-------------------|
| lg_log_request    | gzip       | 5150      | 262              | 19.66             | 49231   | 104.61                 | 99.29             |
| lg_metric_request | gzip       | 6800      | 201              | 33.83             | 51816   | 131.23                 | 127.35            |
| lg_trace_request  | gzip       | 9200      | 270              | 34.07             | 65174   | 141.16                 | 137.02            |
| md_log_request    | gzip       | 363       | 268              | 1.35              | 37609   | 9.65                   | 2.53              |
| md_metric_request | gzip       | 320       | 145              | 2.21              | 30141   | 10.62                  | 5.81              |
| md_trace_request  | gzip       | 451       | 288              | 1.57              | 38270   | 11.78                  | 4.26              |
| sm_log_request    | gzip       | 166       | 168              | 0.99              | 30511   | 5.44                   | -0.07             |
| sm_metric_request | gzip       | 185       | 142              | 1.30              | 29055   | 6.37                   | 1.48              |
| sm_trace_request  | gzip       | 233       | 205              | 1.14              | 33466   | 6.96                   | 0.84              |
| lg_log_request    | snappy     | 5150      | 475              | 10.84             | 1915    | 2,689.30               | 2,441.25          |
| lg_metric_request | snappy     | 6800      | 466              | 14.59             | 2266    | 3,000.88               | 2,795.23          |
| lg_trace_request  | snappy     | 9200      | 644              | 14.29             | 3281    | 2,804.02               | 2,607.74          |
| md_log_request    | snappy     | 363       | 300              | 1.21              | 770.0   | 471.43                 | 81.82             |
| md_metric_request | snappy     | 320       | 162              | 1.98              | 588.6   | 543.66                 | 268.43            |
| md_trace_request  | snappy     | 451       | 330              | 1.37              | 907.7   | 496.86                 | 133.30            |
| sm_log_request    | snappy     | 166       | 184              | 0.90              | 551.8   | 300.83                 | -32.62            |
| sm_metric_request | snappy     | 185       | 154              | 1.20              | 526.3   | 351.51                 | 58.90             |
| sm_trace_request  | snappy     | 233       | 251              | 0.93              | 682.1   | 341.59                 | -26.39            |
| lg_log_request    | zstd       | 5150      | 223              | 23.09             | 17998   | 286.14                 | 273.75            |
| lg_metric_request | zstd       | 6800      | 144              | 47.22             | 14289   | 475.89                 | 465.81            |
| lg_trace_request  | zstd       | 9200      | 208              | 44.23             | 17160   | 536.13                 | 524.01            |
| md_log_request    | zstd       | 363       | 261              | 1.39              | 11216   | 32.36                  | 9.09              |
| md_metric_request | zstd       | 320       | 145              | 2.21              | 9318    | 34.34                  | 18.78             |
| md_trace_request  | zstd       | 451       | 301              | 1.50              | 12583   | 35.84                  | 11.92             |
| sm_log_request    | zstd       | 166       | 165              | 1.01              | 12482   | 13.30                  | 0.08              |
| sm_metric_request | zstd       | 185       | 139              | 1.33              | 8824    | 20.97                  | 5.21              |
| sm_trace_request  | zstd       | 233       | 203              | 1.15              | 10134   | 22.99                  | 2.96              |

Compression ratios will vary in practice as they are highly dependent on the data's information entropy. Compression rates are dependent on the speed of the CPU, and the size of payloads being compressed: smaller payloads compress at slower rates relative to larger payloads, which are able to amortize fixed computation costs over more bytes.

`gzip` is the only required compression algorithm required for [OTLP servers](https://github.com/open-telemetry/opentelemetry-specification/blob/main/specification/protocol/otlp.md#protocol-details), and is a natural first choice. It is not as fast as `snappy`, but achieves better compression ratios and has reasonable performance. If your collector is CPU bound and your OTLP server supports it, you may benefit from using `snappy` compression. If your collector is CPU bound and has a very fast network link, you may benefit from disabling compression, which is the default.

## Server Configuration

[Receivers](https://github.com/open-telemetry/opentelemetry-collector/blob/main/receiver/README.md)
leverage server configuration.

Note that transport configuration can also be configured. For more information,
see [confignet README](../confignet/README.md).

- [`keepalive`](https://godoc.org/google.golang.org/grpc/keepalive#ServerParameters)
  - [`enforcement_policy`](https://godoc.org/google.golang.org/grpc/keepalive#EnforcementPolicy)
    - `min_time`
    - `permit_without_stream`
  - [`server_parameters`](https://godoc.org/google.golang.org/grpc/keepalive#ServerParameters)
    - `max_connection_age`
    - `max_connection_age_grace`
    - `max_connection_idle`
    - `time`
    - `timeout`
- [`max_concurrent_streams`](https://godoc.org/google.golang.org/grpc#MaxConcurrentStreams)
- [`max_recv_msg_size_mib`](https://godoc.org/google.golang.org/grpc#MaxRecvMsgSize)
- [`read_buffer_size`](https://godoc.org/google.golang.org/grpc#ReadBufferSize)
- [`tls`](../configtls/README.md)
- [`write_buffer_size`](https://godoc.org/google.golang.org/grpc#WriteBufferSize)
