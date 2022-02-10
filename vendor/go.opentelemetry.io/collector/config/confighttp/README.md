# HTTP Configuration Settings

HTTP exposes a [variety of settings](https://golang.org/pkg/net/http/).
Several of these settings are available for configuration within individual
receivers or exporters.

## Client Configuration

[Exporters](https://github.com/open-telemetry/opentelemetry-collector/blob/main/exporter/README.md)
leverage client configuration.

Note that client configuration supports TLS configuration, the
configuration parameters are also defined under `tls` like server
configuration. For more information, see [configtls
README](../configtls/README.md).

- `endpoint`: address:port
- [`tls`](../configtls/README.md)
- `headers`: name/value pairs added to the HTTP request headers
- [`read_buffer_size`](https://golang.org/pkg/net/http/#Transport)
- [`timeout`](https://golang.org/pkg/net/http/#Client)
- [`write_buffer_size`](https://golang.org/pkg/net/http/#Transport)
- `compression`: Compression type to use among `gzip`, `zstd`, `snappy`, `zlib`, and `deflate`.
  - look at the documentation for the server-side of the communication.
  - `none` will be treated as uncompressed, and any other inputs will cause an error.
- [`max_idle_conns`](https://golang.org/pkg/net/http/#Transport)
- [`max_idle_conns_per_host`](https://golang.org/pkg/net/http/#Transport)
- [`max_conns_per_host`](https://golang.org/pkg/net/http/#Transport)
- [`idle_conn_timeout`](https://golang.org/pkg/net/http/#Transport)

Example:

```yaml
exporter:
  otlp:
    endpoint: otelcol2:55690
    tls:
      ca_file: ca.pem
      cert_file: cert.pem
      key_file: key.pem
    headers:
      test1: "value1"
      "test 2": "value 2"
    compression: zstd
```

## Server Configuration

[Receivers](https://github.com/open-telemetry/opentelemetry-collector/blob/main/receiver/README.md)
leverage server configuration.

- [`cors`](https://github.com/rs/cors#parameters): Configure [CORS][cors],
allowing the receiver to accept traces from web browsers, even if the receiver
is hosted at a different [origin][origin]. If left blank or set to `null`, CORS
will not be enabled.
  - `allowed_origins`: A list of [origins][origin] allowed to send requests to
  the receiver. An origin may contain a wildcard (`*`) to replace 0 or more
  characters (e.g., `https://*.example.com`). To allow any origin, set to
  `["*"]`. If no origins are listed, CORS will not be enabled.
  - `allowed_headers`: Allow CORS requests to include headers outside the
  [default safelist][cors-headers]. By default, safelist headers and
  `X-Requested-With` will be allowed. To allow any request header, set to
  `["*"]`.
  - `max_age`: Sets the value of the [`Access-Control-Max-Age`][cors-cache]
  header, allowing clients to cache the response to CORS preflight requests. If
  not set, browsers use a default of 5 seconds.
- `endpoint`: Valid value syntax available [here](https://github.com/grpc/grpc/blob/master/doc/naming.md)
- [`tls`](../configtls/README.md)

[cors]: https://developer.mozilla.org/en-US/docs/Web/HTTP/CORS
[cors-headers]: https://developer.mozilla.org/en-US/docs/Glossary/CORS-safelisted_request_header
[cors-cache]: https://developer.mozilla.org/en-US/docs/Web/HTTP/Headers/Access-Control-Max-Age
[origin]: https://developer.mozilla.org/en-US/docs/Glossary/Origin

Example:

```yaml
receivers:
  otlp:
    protocols:
      http:
        cors:
          allowed_origins:
            - https://foo.bar.com
            - https://*.test.com
          allowed_headers:
            - Example-Header
          max_age: 7200
        endpoint: 0.0.0.0:55690
```
