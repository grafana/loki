<p align="center"> <img src="logo_and_name.png" alt="Loki Logo"> <br>
  <small>Like Prometheus, but for logs!</small> </p>

Grafana Loki is a set of components, that can be composed into a fully featured
logging stack.

It builds around the idea of treating a single log line as-is. This means that
instead of full-text indexing them, related logs are grouped using the same
labels as in Prometheus. This is much more efficient and scales better.

## Components
- **[Loki](loki/overview.md)**: The main server component is called Loki. It is
  responsible for permanently storing the logs it is being shipped and it
  executes the LogQL
  queries from clients.  
  Loki shares its high-level architecture with Cortex, a highly scalable
  Prometheus backend.
- **[Promtail](promtail/overview.md)**: To ship logs to a central place, an
  agent is required. Promtail
  is deployed to every node that should be monitored and sends the logs to Loki.  
  It also does important task of pre-processing the log lines, including
  attaching labels to them for easier querying.
- *Grafana*: The *Explore* feature of Grafana 6.0+ is the primary place of
  contact between a human and Loki. It is used for discovering and analyzing
  logs.

Alongside these main components, there are some other ones as well:

- **[LogCLI](logcli.md)**: A command line interface to query logs and labels
  from Loki
- **[Canary](canary.md)**: An audit utility to analyze the log-capturing
  performance of Loki. Ingests data into Loki and immediately reads it back to
  check for latency and loss.
- **[Docker
  Driver](https://github.com/grafana/loki/tree/master/cmd/docker-driver)**: A
  Docker [log
  driver](https://docs.docker.com/config/containers/logging/configure/) to ship
  logs captured by Docker directly to Loki, without the need of an agent.
- **[Fluentd
  Plugin](https://github.com/grafana/loki/tree/master/fluentd/fluent-plugin-grafana-loki)**:
  An Fluentd [output plugin](https://docs.fluentd.org/output), to use Fluentd
  for shipping logs into Loki
