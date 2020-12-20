# HTTPS Package for Prometheus

The `https` directory contains a Go package and a sample configuration file for
running `node_exporter` with HTTPS instead of HTTP. We currently support TLS 1.3
and TLS 1.2.

To run a server with TLS, use the flag `--web.config`.

e.g. `./node_exporter --web.config="web-config.yml"`
If the config is kept within the https directory.

The config file should be written in YAML format, and is reloaded on each connection to check for new certificates and/or authentication policy.

## Sample Config

```
tls_config:
  # Certificate and key files for server to use to authenticate to client
  cert_file: <filename>
  key_file: <filename>

  # Server policy for client authentication. Maps to ClientAuth Policies
  # For more detail on clientAuth options: [ClientAuthType](https://golang.org/pkg/crypto/tls/#ClientAuthType)
  [ client_auth_type: <string> | default = "NoClientCert" ]

  # CA certificate for client certificate authentication to the server
  [ client_ca_file: <filename> ]
```
