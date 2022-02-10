# OTLP Receiver

Receives data via gRPC or HTTP using [OTLP](
https://github.com/open-telemetry/opentelemetry-specification/blob/main/specification/protocol/otlp.md)
format.

Supported pipeline types: traces, metrics, logs

:warning: OTLP logs format is currently marked as "Beta" and may change in
incompatible ways.

## Getting Started

All that is required to enable the OTLP receiver is to include it in the
receiver definitions. A protocol can be disabled by simply not specifying it in
the list of protocols.

```yaml
receivers:
  otlp:
    protocols:
      grpc:
      http:
```

The following settings are configurable:

- `endpoint` (default = 0.0.0.0:4317 for grpc protocol, 0.0.0.0:4318 http protocol):
  host:port to which the receiver is going to receive data. The valid syntax is
  described at https://github.com/grpc/grpc/blob/master/doc/naming.md.

## Advanced Configuration

Several helper files are leveraged to provide additional capabilities automatically:

- [gRPC settings](https://github.com/open-telemetry/opentelemetry-collector/blob/main/config/configgrpc/README.md) including CORS
- [TLS and mTLS settings](https://github.com/open-telemetry/opentelemetry-collector/blob/main/config/configtls/README.md)
- [Queuing, retry and timeout settings](https://github.com/open-telemetry/opentelemetry-collector/blob/main/exporter/exporterhelper/README.md)

## Writing with HTTP/JSON

The OTLP receiver can receive trace export calls via HTTP/JSON in addition to
gRPC. The HTTP/JSON address is the same as gRPC as the protocol is recognized
and processed accordingly. Note the format needs to be [protobuf JSON
serialization](https://developers.google.com/protocol-buffers/docs/proto3#json).

To write traces with HTTP/JSON, `POST` to `[address]/v1/traces` for traces,
to `[address]/v1/metrics` for metrics, to `[address]/v1/logs` for logs. The default
port is `4318`.

### CORS (Cross-origin resource sharing)

The HTTP/JSON endpoint can also optionally configure [CORS][cors] under `cors:`.
Specify what origins (or wildcard patterns) to allow requests from as
`allowed_origins`. To allow additional request headers outside of the [default
safelist][cors-headers], set `allowed_headers`. Browsers can be instructed to
[cache][cors-max-age] responses to preflight requests by setting `max_age`.

[cors]: https://developer.mozilla.org/en-US/docs/Web/HTTP/CORS
[cors-headers]: https://developer.mozilla.org/en-US/docs/Glossary/CORS-safelisted_request_header
[cors-max-age]: https://developer.mozilla.org/en-US/docs/Web/HTTP/Headers/Access-Control-Max-Age

```yaml
receivers:
  otlp:
    protocols:
      http:
        endpoint: "localhost:4318"
        cors:
          allowed_origins:
            - http://test.com
            # Origins can have wildcards with *, use * by itself to match any origin.
            - https://*.example.com
          allowed_headers:
            - Example-Header
          max_age: 7200
```
