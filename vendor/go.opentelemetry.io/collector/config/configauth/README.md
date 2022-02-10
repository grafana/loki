# Authentication configuration

This module defines necessary interfaces to implement server and client type authenticators:

- Server type authenticators perform authentication for incoming HTTP/gRPC requests and are typically used in receivers.
- Client type authenticators perform client-side authentication for outgoing HTTP/gRPC requests and are typically used in exporters.

The currently known authenticators are:

- Server Authenticators
  - [oidc](https://github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/extension/oidcauthextension)

- Client Authenticators
  - [oauth2](https://github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/extension/oauth2clientauthextension)
  - [BearerToken](https://github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/extension/bearertokenauthextension)

Examples:
```yaml
extensions:
  oidc:
    # see the blog post on securing the otelcol for information
    # on how to setup an OIDC server and how to generate the TLS certs
    # required for this example
    # https://medium.com/opentelemetry/securing-your-opentelemetry-collector-1a4f9fa5bd6f
    issuer_url: http://localhost:8080/auth/realms/opentelemetry
    audience: account

  oauth2client:
    client_id: someclientid
    client_secret: someclientsecret
    token_url: https://example.com/oauth2/default/v1/token
    scopes: ["api.metrics"]
    # tls settings for the token client
    tls:
      insecure: true
      ca_file: /var/lib/mycert.pem
      cert_file: certfile
      key_file: keyfile
    # timeout for the token client
    timeout: 2s

receivers:
  otlp/with_auth:
    protocols:
      grpc:
        endpoint: localhost:4318
        tls:
          cert_file: /tmp/certs/cert.pem
          key_file: /tmp/certs/cert-key.pem
        auth:
          ## oidc is the extension name to use as the authenticator for this receiver
          authenticator: oidc

  otlphttp/withauth:
    endpoint: http://localhost:9000
    auth:
      authenticator: oauth2client

```

## Creating an authenticator

New authenticators can be added by creating a new extension that also implements the appropriate interface (`configauth.ServerAuthenticator` or `configauth.ClientAuthenticator`).

Generic authenticators that may be used by a good number of users might be accepted as part of the contrib distribution. If you have an interest in contributing an authenticator, open an issue with your proposal. For other cases, you'll need to include your custom authenticator as part of your custom OpenTelemetry Collector, perhaps being built using the [OpenTelemetry Collector Builder](https://github.com/open-telemetry/opentelemetry-collector/tree/main/cmd/builder).
