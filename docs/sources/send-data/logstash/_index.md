---
title: Logstash plugin
menuTitle:
description: Instructions to install, configure, and use the Logstash plugin to send logs to Loki.
aliases:
- ../send-data/a/logstash/
weight:  800
---
# Logstash plugin

Grafana Loki has a [Logstash](https://www.elastic.co/logstash) output plugin called
`logstash-output-loki` that enables shipping logs to a Loki
instance or [Grafana Cloud](/products/cloud/).

{{< admonition type="warning" >}}
Grafana Labs does not recommend using the Logstash plugin for new deployments. Even as a mechanism for quickly testing Loki with your existing Beats/Logstash infrastructure we highly discourage the use of this plugin.

Our experience over the years has found numerous significant challenges using Logstash and this plugin:

  * It is very difficult to configure labels correctly.  Conceptually Elasticsearch is a very different database from Loki and users almost always end up sending too many high cardinality labels to Loki, which makes getting started with Loki unnecessarily complicated and confusing vs. using other clients.
  * Logstash and the upstream Beats components implement backoff and flow control which we've found hard to observe, leading to ingestion delays into Loki which are extremely difficult to address.
  * We at Grafana Labs have no expertise at configuring Logstash or understanding of its configuration language, so we cannot provide support for it.
  * It's very hard to troubleshoot and debug. Our experience has shown that in nearly every case where it was assumed this would be the fast path to getting logs to Loki, that was not the case and it ended up taking far longer than anticipated.
  
Please strongly consider using any alternative mechanism to sending logs to Loki. We recommend using [Grafana Alloy](/docs/loki/latest/send-data/alloy/).  This is the tool we build and where we can offer the best experience and most support.

{{< /admonition >}}

## Installation

### Local

If you need to install the Logstash output plugin manually you can do simply so by using the command below:

```bash
$ bin/logstash-plugin install logstash-output-loki
```

This will download the latest gem for the output plugin and install it in logstash.

### Docker

We also provide a docker image on [docker hub](https://hub.docker.com/r/grafana/logstash-output-loki). The image contains logstash and the Loki output plugin
already pre-installed.

For example if you want to run logstash in docker with the `loki.conf` as pipeline configuration you can use the command bellow :

```bash
docker run -v `pwd`/loki-test.conf:/home/logstash/ --rm grafana/logstash-output-loki:1.0.1 -f loki-test.conf
```

### Kubernetes

We also provide default helm values for scraping logs with Filebeat and forward them to Loki with logstash in our `loki-stack` umbrella chart.
You can switch from Promtail to logstash by using the following command:

```bash
helm upgrade --install loki loki/loki-stack \
    --set filebeat.enabled=true,logstash.enabled=true,promtail.enabled=false \
    --set loki.fullnameOverride=loki,logstash.fullnameOverride=logstash-loki
```

This will automatically scrape all pods logs in the cluster and send them to Loki with Kubernetes metadata attached as labels.
You can use the [`values.yaml`](https://github.com/grafana/helm-charts/blob/main/charts/loki-stack/values.yaml) file as a starting point for your own configuration.

## Usage and Configuration

To configure Logstash to forward logs to Loki, simply add the `loki` output to your [Logstash configuration file](https://www.elastic.co/guide/en/logstash/current/configuration-file-structure.html) as documented below :

```conf
output {
  loki {
    [url => "" | default = none | required=true]

    [tenant_id => string | default = nil | required=false]

    [message_field => string | default = "message" | required=false]

    [include_fields => array | default = [] | required=false]

    [metadata_fields => array | default = [] | required=false]

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

    [insecure_skip_verify => boolean | default = false | required=false]
  }
}
```

By default Loki will create entry from event fields it receives.
A logstash event as shown below.

```conf
{
  "@timestamp" => 2017-04-26T19:33:39.257Z,
  "src"        => "localhost",
  "@version"   => "1",
  "host"       => "localhost.localdomain",
  "pid"        => "1",
  "message"    => "Apr 26 12:20:02 localhost systemd[1]: Starting system activity accounting tool...",
  "type"       => "stdin",
  "prog"       => "systemd",
}
```

Contains a `message` and `@timestamp` fields, which are respectively used to form the Loki entry log line and timestamp.

> You can use a different property for the log line by using the configuration property [`message_field`](#message_field). If you also need to change the timestamp value use the Logstash `date` filter to change the `@timestamp` field.

All other fields (except nested fields) will form the label set (key value pairs) attached to the log line. [This means you're responsible for mutating and dropping high cardinality labels](/blog/2020/04/21/how-labels-in-loki-can-make-log-queries-faster-and-easier/) such as client IPs.
You can usually do so by using a [`mutate`](https://www.elastic.co/guide/en/logstash/current/plugins-filters-mutate.html) filter.

For example the configuration below :

```conf
input {
  ...
}

filter {
  mutate {
    add_field => {
      "cluster" => "us-central1"
      "job" => "logstash"
    }
    replace => { "type" => "stream"}
    remove_field => ["src"]
  }
}
output {
  loki {
    url => "http://myloki.domain:3100/loki/api/v1/push"
  }
}
```

Will add `cluster` and `job` static labels, remove `src` fields and replace `type` to be named `stream`.

If you want to include nested fields or metadata fields (starting with `@`) you need to rename them.

For example when using Filebeat with the [`add_kubernetes_metadata`](https://www.elastic.co/guide/en/beats/filebeat/current/add-kubernetes-metadata.html) processor, it will attach Kubernetes metadata to your events like below:

```json
{
  "kubernetes" : {
    "labels" : {
      "app" : "MY-APP",
      "pod-template-hash" : "959f54cd",
      "serving" : "true",
      "version" : "1.0",
      "visualize" : "true"
    },
    "pod" : {
      "uid" : "e20173cb-3c5f-11ea-836e-02c1ee65b375",
      "name" : "MY-APP-959f54cd-lhd5p"
    },
    "node" : {
      "name" : "ip-xxx-xx-xx-xxx.ec2.internal"
    },
    "container" : {
      "name" : "istio"
    },
    "namespace" : "production",
    "replicaset" : {
      "name" : "MY-APP-959f54cd"
    }
  },
  "message": "Failed to parse configuration",
  "@timestamp": "2017-04-26T19:33:39.257Z",
}
```

The filter below show you how to extract those Kubernetes fields into labels (`container_name`,`namespace`,`pod` and `host`):

```conf
filter {
  if [kubernetes] {
    mutate {
      add_field => {
        "container_name" => "%{[kubernetes][container][name]}"
        "namespace" => "%{[kubernetes][namespace]}"
        "pod" => "%{[kubernetes][pod][name]}"
      }
      replace => { "host" => "%{[kubernetes][node][name]}"}
    }
  }
  mutate {
    remove_field => ["tags"]
  }
}
```

### Version Notes

Important notes regarding versions:

- Version 1.1.0 and greater of this plugin you can also specify a list of labels to allow list via the `include_fields` configuration.
- Version 1.2.0 and greater of this plugin you can also specify structured metadata via the `metadata_fields` configuration.

### Configuration Properties

#### url

The url of the Loki server to send logs to.
When sending data the push path need to also be provided e.g. `http://localhost:3100/loki/api/v1/push`.

If you want to send to [GrafanaCloud](/products/cloud/) you would use `https://logs-prod-us-central1.grafana.net/loki/api/v1/push`.

#### username / password

Specify a username and password if the Loki server requires basic authentication.
If using the [GrafanaLab's hosted Loki](/products/cloud/), the username needs to be set to your instance/user id and the password should be a Grafana.com api key.

#### message_field

Message field to use for log lines. You can use logstash key accessor language to grab nested property, for example : `[log][message]`.

#### include_fields

An array of fields which will be mapped to labels and sent to Loki, when this list is configured **only** these fields will be sent, all other fields will be ignored.

#### metadata_fields

An array of fields which will be mapped to [structured metadata]({{< relref "../../get-started/labels/structured-metadata.md" >}}) and sent to Loki for each log line

#### batch_wait

Interval in seconds to wait before pushing a batch of records to Loki. This means even if the [batch size](#batch_size) is not reached after `batch_wait` a partial batch will be sent, this is to ensure freshness of the data.

#### batch_size

Maximum batch size to accrue before pushing to Loki. Defaults to 102400 bytes

#### Backoff config

##### min_delay => 1(1s)

Initial backoff time between retries

##### max_delay => 300(5m)

Maximum backoff time between retries

##### retries => 10

Maximum number of retries to do. Setting it to `0` will retry indefinitely.

#### tenant_id

Loki is a multi-tenant log storage platform and all requests sent must include a tenant.  For some installations the tenant will be set automatically by an authenticating proxy.  Otherwise you can define a tenant to be passed through.  The tenant can be any string value.

#### client certificate verification

Specify a pair of client certificate and private key with `cert` and `key` if a reverse proxy with client certificate verification is configured in front of Loki. `ca_cert` can also be specified if the server uses custom certificate authority.

#### insecure_skip_verify

A flag to disable server certificate verification. By default it is set to `false`.

### Full configuration example

```conf
input {
  beats {
    port => 5044
  }
}

filter {
  if [kubernetes] {
    mutate {
      add_field => {
        "container_name" => "%{[kubernetes][container][name]}"
        "namespace" => "%{[kubernetes][namespace]}"
        "pod" => "%{[kubernetes][pod][name]}"
      }
      replace => { "host" => "%{[kubernetes][node][name]}"}
    }
  }
  mutate {
    remove_field => ["tags"]  # Note: with include_fields defined below this wouldn't be necessary
  }
}

output {
  loki {
    url => "https://logs-prod-us-central1.grafana.net/loki/api/v1/push"
    username => "3241"
    password => "REDACTED"
    batch_size => 112640 #112.64 kilobytes
    retries => 5
    min_delay => 3
    max_delay => 500
    message_field => "message"
    include_fields => ["container_name","namespace","pod","host"]
    metadata_fields => ["pod"]
  }
  # stdout { codec => rubydebug }
}
```
