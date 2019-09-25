# Loki Clients

Loki supports the following official clients for sending logs:

1. [Promtail](./promtail/README.md)
2. [Docker Driver](./docker-driver/README.md)
3. [Fluentd](./fluentd.md)

## Picking a Client

While all clients can be used simultaneously to cover multiple use cases, which
client is initially picked to send logs depends on your use case.

Promtail is the client of choice when you're running Kubernetes, as you can
configure it to automatically scrape logs from pods running on the same node
that Promtail runs on. Promtail and Prometheus running together in Kubernetes
enables powerful debugging: if Prometheus and Promtail use the same labels,
users can use tools like Grafana to switch between metrics and logs based on the
label set.

Promtail is also the client of choice on bare-metal: since it can be configured
to tail logs from all files given a host path, it is the easiest way to send
logs to Loki from plain-text files (e.g., things that log to `/var/log/*.log`).

When using Docker and not Kubernetes, the Docker Logging driver should be used,
as it automatically adds labels appropriate to the running container.

The Fluentd plugin is ideal when you already have Fluentd deployed and you don't
need the service discovery capabilities of Promtail.
