---
title: Clients
description: Grafana Loki clients
weight: 600
---
# Clients

Grafana Loki works with the following clients for sending logs:

- [Promtail]({{< relref "./promtail" >}})
- [Docker driver]({{< relref "../send-data/docker-driver" >}}).
- [Fluentd]({{< relref "../send-data/fluentd" >}})
- [Fluent Bit]({{< relref "../send-data/fluentbit" >}})
- [Logstash]({{< relref "../send-data/logstash" >}})
- [Lambda Promtail]({{< relref "../send-data/lambda-promtail" >}})

There are also a number of third-party clients, see [Unofficial clients](#unofficial-clients).

The [xk6-loki extension](https://github.com/grafana/xk6-loki) permits [load testing Loki]({{< relref "../send-data/k6" >}}).

## Picking a client

While all clients can be used simultaneously to cover multiple use cases, which
client is initially picked to send logs depends on your use case.

### Promtail

Promtail is the client of choice when you're running Kubernetes, as you can
configure it to automatically scrape logs from pods running on the same node
that Promtail runs on. Promtail and Prometheus running together in Kubernetes
enables powerful debugging: if Prometheus and Promtail use the same labels,
users can use tools like Grafana to switch between metrics and logs based on the
label set.

Promtail is also the client of choice on bare-metal since it can be configured
to tail logs from all files given a host path. It is the easiest way to send
logs to Loki from plain-text files (e.g., things that log to `/var/log/*.log`).

Lastly, Promtail works well if you want to extract metrics from logs such as
counting the occurrences of a particular message.

### Docker Logging Driver

When using Docker and not Kubernetes, the Docker logging driver for Loki should
be used as it automatically adds labels appropriate to the running container.

### Fluentd and Fluent Bit

The Fluentd and Fluent Bit plugins are ideal when you already have Fluentd deployed
and you already have configured `Parser` and `Filter` plugins.

Fluentd also works well for extracting metrics from logs when using its
Prometheus plugin.

### Logstash

If you are already using logstash and/or beats, this will be the easiest way to start.
By adding our output plugin you can quickly try Loki without doing big configuration changes.

### Lambda Promtail

This is a workflow combining the Promtail push-api [scrape config]({{< relref "./promtail/configuration#loki_push_api" >}}) and the [lambda-promtail]({{< relref "../send-data/lambda-promtail" >}}) AWS Lambda function which pipes logs from Cloudwatch to Loki.

This is a good choice if you're looking to try out Loki in a low-footprint way or if you wish to monitor AWS lambda logs in Loki.

## Unofficial clients

Note that the Loki API is not stable yet, so breaking changes might occur
when using or writing a third-party client.

- [promtail-client](https://github.com/afiskon/promtail-client) (Go)
- [push-to-loki.py](https://github.com/sleleko/devops-kb/blob/master/python/push-to-loki.py) (Python 3)
- [python-logging-loki](https://pypi.org/project/python-logging-loki/) (Python 3)
- [Serilog-Sinks-Loki](https://github.com/JosephWoodward/Serilog-Sinks-Loki) (C#)
- [NLog-Targets-Loki](https://github.com/corentinaltepe/nlog.loki) (C#)
- [loki-logback-appender](https://github.com/loki4j/loki-logback-appender) (Java)
- [Log4j2 appender for Loki](https://github.com/tkowalcz/tjahzi) (Java)
- [mjaron-tinyloki-java](https://github.com/mjfryc/mjaron-tinyloki-java) (Java)
- [LokiLogger.jl](https://github.com/JuliaLogging/LokiLogger.jl) (Julia)
- [winston-loki](https://github.com/JaniAnttonen/winston-loki) (JS)
- [ilogtail](https://github.com/alibaba/ilogtail) (Go)
- [Vector Loki Sink](https://vector.dev/docs/reference/configuration/sinks/loki/)
- [Cribl Loki Destination](https://docs.cribl.io/stream/destinations-loki)
