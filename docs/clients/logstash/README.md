# Logstash

Loki has a [Logstash](https://www.elastic.co/logstash) output plugin called
`logstash-output-grafana-loki` that enables shipping logs to a private Loki
instance or [Grafana Cloud](https://grafana.com/oss/loki).

## Installation

```bash
$ bin/logstash-plugin install logstash-output-grafana-loki
```

## Usage

In your Logstash configuration, use `output loki`. The configuration values would look like this.

```
output {
  loki {
    [url => "" | default = none | required=true]

    [include_labels => array | default = [] | required=true]

    [extra_labels => hash | default = {} |  required=false]

    [tenant_id => string | default = nil | required=false]

    [message_field => string | default = "message" | required=false]

    [batch_wait => number | default = 1(s) | required=false]

    [batch_size => number | default = 102400(bytes) | required=false]

    [min_delay => number | default = 1(s) | required=false]

    [max_delay => number | default = 300(s) | required=false]

    [retries => number | default = 10 | required=false]

    [username => string | default = nil | required=false]

    [password => secret | default = nil | required=false]

    [cert => path | default = nil | required=false]

    [key => path | default = nil| required=false]

    [ca_cert => path | default = nil | required=false]

  }
}
```

## Docker Image

There is a Docker image `grafana/logstash-output-grafana-loki:master` which contains [default configuration files](https://github.com/grafana/loki/tree/master/logstash/conf)

## Configuration

### url

The url of the Loki server to send logs to.
When sending data the push path need to also be provided e.g. `http://localhost:3100/loki/api/v1/push`.

### tenant_id

Loki is a multi-tenant log storage platform and all requests sent must include a tenant.  For some installations the tenant will be set automatically by an authenticating proxy.  Otherwise you can define a tenant to be passed through.  The tenant can be any string value.

### include_labels

Array specifiying the extracted labels. This is a mandatory field. A minimum of 1 label should be provided.
All nested labels are extracted by prefixing the parent key i.e if there is a nested label like `{log => {file => {path => "/path/to/file.log"}}}`, extratced label would be `{log_file_path => /path/to/file.log}`

### extra_labels

All logs send to Loki will be added with these extra labels

### message_field

Message field to use for log lines.

### batch_wait

Interval in seconds to wait before pushing a batch of records to loki

### batch_size

Maximum batch size to accrue before pushing to loki. Defaults to 102400 bytes

### Backoff config

#### min_delay

Initial backoff time between retries

#### max_delay => 300(5m)

Maximum backoff time between retries

#### retries => 10

Maximum number of retries to do

### username / password

Specify a username and password if the Loki server requires basic authentication.
If using the GrafanaLab's hosted Loki, the username needs to be set to your instanceId and the password should be a Grafana.com api key.

### client certificate verification

Specify a pair of client certificate and private key with `cert` and `key` if a reverse proxy with client certificate verification is configured in front of Loki. `ca_cert` can also be specified if the server uses custom certificate authority.
