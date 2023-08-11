---
title: Send log data to Loki
menuTitle: Send data
description: List of clients that can be used to send log data to Loki. 
aliases: 
- ./clients/
weight: 500
---

# Send log data to Loki

There are a number of different clients available to send log data to Loki.
While all clients can be used simultaneously to cover multiple use cases, which client is initially picked to send logs depends on your use case.

## Grafana Clients

The following clients are developed and supported by Grafana Labs for sending logs to Loki:

- [Grafana Agent](/docs/agent/latest/) - The Grafana Agent is the recommended client for the Grafana stack. It can  collect telemetry data for metrics, logs, traces, and continuous profiles and is fully compatible with the Prometheus, OpenTelemetry, and Grafana open source ecosystems.
- [Promtail]({{< relref "./promtail" >}}) - Promtail is the client of choice when you're running Kubernetes, as you can configure it to automatically scrape logs from pods running on the same node that Promtail runs on. Promtail and Prometheus running together in Kubernetes enables powerful debugging: if Prometheus and Promtail use the same labels, users can use tools like Grafana to switch between metrics and logs based on the label set.  
Promtail is also the client of choice on bare-metal since it can be configured to tail logs from all files given a host path. It is the easiest way to send logs to Loki from plain-text files (for example, things that log to `/var/log/*.log`).
Lastly, Promtail works well if you want to extract metrics from logs such as counting the occurrences of a particular message.
- [xk6-loki extension](https://github.com/grafana/xk6-loki) - The k6-loki extension lets you perform [load testing on Loki]({{< relref "./k6" >}}).

## Third-party clients

The following clients have been developed by the Loki community or other third-parties and can be used to send log data to Loki.  

{{% admonition type="note" %}}
Grafana Labs cannot provide support for third-party clients. Once an issue has been determined to be with the client and not Loki, it is the responsibility of the customer to work with the associated vendor or project for bug fixes to these clients.
{{% /admonition %}}

The following are popular third-party Loki clients:

- [Docker Driver]({{< relref "./docker-driver" >}}) - When using Docker and not Kubernetes, the Docker logging driver for Loki should
be used as it automatically adds labels appropriate to the running container.
- [Fluent Bit]({{< relref "./fluentbit" >}}) - The Fluent Bit plugin is ideal when you already have Fluentd deployed
and you already have configured `Parser` and `Filter` plugins.
- [Fluentd]({{< relref "./fluentd" >}}) - The Fluentd plugin is ideal when you already have Fluentd deployed
and you already have configured `Parser` and `Filter` plugins. Fluentd also works well for extracting metrics from logs when using itsPrometheus plugin.
- [Lambda Promtail]({{< relref "./lambda-promtail" >}}) - This is a workflow combining the Promtail push-api [scrape config]({{< relref "./promtail/configuration#loki_push_api" >}}) and the [lambda-promtail]({{< relref "./lambda-promtail" >}}) AWS Lambda function which pipes logs from Cloudwatch to Loki. This is a good choice if you're looking to try out Loki in a low-footprint way or if you wish to monitor AWS lambda logs in Loki
- [Logstash]({{< relref "./logstash" >}}) - If you are already using logstash and/or beats, this will be the easiest way to start.
By adding our output plugin you can quickly try Loki without doing big configuration changes.

These third-party clients also enable sending logs to Loki:

- [Cribl Loki Destination](https://docs.cribl.io/stream/destinations-loki)
- [ilogtail](https://github.com/alibaba/ilogtail) (Go)
- [Log4j2 appender for Loki](https://github.com/tkowalcz/tjahzi) (Java)
- [loki-logback-appender](https://github.com/loki4j/loki-logback-appender) (Java)
- [LokiLogger.jl](https://github.com/JuliaLogging/LokiLogger.jl) (Julia)
- [mjaron-tinyloki-java](https://github.com/mjfryc/mjaron-tinyloki-java) (Java)
- [NLog-Targets-Loki](https://github.com/corentinaltepe/nlog.loki) (C#)
- [promtail-client](https://github.com/afiskon/promtail-client) (Go)
- [push-to-loki.py](https://github.com/sleleko/devops-kb/blob/master/python/push-to-loki.py) (Python 3)
- [python-logging-loki](https://pypi.org/project/python-logging-loki/) (Python 3)
- [Serilog-Sinks-Loki](https://github.com/JosephWoodward/Serilog-Sinks-Loki) (C#)
- [Vector Loki Sink](https://vector.dev/docs/reference/configuration/sinks/loki/)
- [winston-loki](https://github.com/JaniAnttonen/winston-loki) (JS)
