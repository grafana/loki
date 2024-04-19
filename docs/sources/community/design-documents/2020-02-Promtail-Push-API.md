---
title: Promtail Push API
description: Promtail Push API
aliases: 
- ../../design-documents/2020-02-promtail-push-api/
weight: 20
---
# Promtail Push API

- Author: Robert Fratto (@rfratto)
- Date: Feb 4 2020
- Status: DRAFT

Despite being an optional piece of software, Promtail provides half the power
of Loki's story: log transformations, service discovery, metrics from logs,
and context switching between your existing metrics and logs. Today, Promtail
can only be operated to consume logs from very specific sources: files, journal,
or syslog. If users wanted to write custom tooling to ship logs, the tooling
has to bypass Promtail and push directly to Loki. This can lead users to
reimplement functionality Promtail already provides, including its error retries
and batching code.

This document proposes a Push API for Promtail. The preferred implementation is
by copying the existing Loki Push API and implementing it for Promtail. By
being compatible with the Loki Push API, the Promtail Push API can allow batches
of logs to be processed at once for optimize performance. Matching the
Promtail API also allows users to transparently switch their push URLs from
their existing tooling. Finally, a series of alternative solutions are detailed.

## Configuration

Promtail will have a new target called HTTPTarget, configurable in the
`scrape_config` array with the following schema:

```yaml
# Defines an HTTP target, which exposes an endpoint against the Promtail
# HTTP server to accept log traffic.
http:
  # Defines the base URL for the push path, adding a prefix to the
  # exposed endpoint. The final endpoint path is
  # <base_url>loki/api/v1/push. If omitted, defaults to /.
  #
  # Multiple http targets with the same base_url must not exist.
  base_url: /

  # Map of labels to add to every log line passed through to the target.
  labels: {}
```

### Considerations

Users will be able to define multiple `http` scrape configs, but the base URL
value must be different for each instance. This allows to cleanly separate
pipelines through different push endpoints.

Users must also be aware about problems with running Promtail with an HTTP
target behind a load balancer: if payloads are load balanced between multiple
Promtail instances, ordering of logs in Loki will be disrupted leading to
rejected pushes. Users are recommended to do one of the following:

1. Have a dedicated Promtail instance for receiving pushes. This also applies to
   using the syslog target.
1. Have a separate Kubernetes service that always resolves to the same Promtail pod,
   bypassing the load balancing issue.

## Implementation

As discussed in this document, this feature will be implemented by copying the
existing [Loki Push API](/docs/loki/<LOKI_VERSION>/api/#post-lokiapiv1push)
and exposing it via Promtail.

## Considered Alternatives

Using the existing API was chosen for its simplicity and capabilities of being
used for interesting configurations (e.g., chaining Promtails together). These
other options were considered but rejected as not the best solution for the
problem being solved.

Note that Option 3 has value and may be implemented separately from this
feature.

### Option 1: JSON / Protobuf Payload

A new JSON and Protobuf payload format can be designed instead of the existing
Loki push payload. Both formats would have to be exposed to support clients that
either can't or won't use protobuf marshalling.

The primary benefit of this approach is to allow us to tweak the payload schema
independently of Loki's existing schema, but otherwise may not be very useful
and is essentially just code duplication.

### Option 2: gRPC Service

The
[logproto.Pusher](https://github.com/grafana/loki/blob/f7ee1c753c76ef63338d53cfba782188a165144d/pkg/logproto/logproto.proto#L8-L10)
service could be exposed through Promtail. This would enable clients stubs to be
generated for languages that have gRPC support, and, for HTTP1 support, a
[gRPC gateway](https://github.com/grpc-ecosystem/grpc-gateway) would be embedded
in Promtail itself.

This implementation option is similar to the original proposed solution, but
uses the gRPC gateway to handle HTTP/1 traffic instead of the HTTP1 shim that
Loki uses. There are some concerns with this approach:

1. The gRPC Gateway reverse proxy will need to play nice with the existing HTTP
   mux used in Promtail.
1. We couldn't control the HTTP and Protobuf formats separately as Loki can.
1. Log lines will be double-encoded thanks to the reverse proxy.
1. A small overhead of using a reverse proxy in-process will be introduced.
1. This breaks our normal pattern of writing our own shim functions; may add
   some cognitive overhead of having to deal with the gRPC gateway as an outlier
   in the code.

### Option 3: Plaintext Payload

Prometheus' [Push Gateway API](https://github.com/prometheus/pushgateway#command-line)
is cleverly designed and we should consider implementing our API in the same
format: users would push to `http://promtail-url/push/label1/value1?timestamp=now`
with a plaintext POST body. For example:

```
curl -X POST http://promtail.default/push/foo/bar/fizz/buzz -d “hello, world!”
```

This approach may be slightly faster when compared to a non-plaintext payload as
no unmarshaling needs to be performed. This URL path and timestamp still needs
to be parsed, but this will generally be faster than the reflection requirements
imposed by JSON.

However, note that this API limits Promtail to accepting one line at a time and
may cause performance issues when trying to handle large volumes of traffic. As
an alternative, this API could also be implemented by external tooling and be
built on top of any of the other implementation options.

An [example implementation](https://github.com/grafana/loki/pull/1270) was
created and has received positive support for its simplicity and ease of
integration.

