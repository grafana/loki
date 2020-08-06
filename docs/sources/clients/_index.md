---
title: Clients
weight: 600
---
# Loki clients

Loki supports the following official clients for sending logs:

- [Promtail](promtail/)
- [Docker Driver](docker-driver/)
- [Fluentd](fluentd/)
- [Fluent Bit](fluentbit/)
- [Logstash](logstash/)
- [Lambda Promtail](lambda-promtail/)

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

This is a workflow combining the promtail push-api [scrape config](./promtail/configuration#loki_push_api_config) and the [lambda-promtail](../../tools/lambda-promtail/) AWS Lambda function which pipes logs from Cloudwatch to Loki.

This is a good choice if you're looking to try out Loki in a low-footprint way or if you wish to monitor AWS lambda logs in Loki.

# Unofficial clients

Please note that the Loki API is not stable yet, so breaking changes might occur
when using or writing a third-party client.

- [promtail-client](https://github.com/afiskon/promtail-client) (Go)
- [push-to-loki.py](https://github.com/sleleko/devops-kb/blob/master/python/push-to-loki.py) (Python 3)
- [Serilog-Sinks-Loki](https://github.com/JosephWoodward/Serilog-Sinks-Loki) (C#)
