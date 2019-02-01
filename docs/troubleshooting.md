# Troubleshooting

## "Data source connected, but no labels received. Verify that Loki and Promtail is configured properly."

This error can appear in Grafana when you add Loki as a datasource.
It means that Grafana can connect to Loki, but Loki has not received any logs from promtail.
This can have several reasons:

- Promtail cannot reach Loki, check promtails output.
- Promtail started sending logs before Loki was ready. This can happen in test environments where promtail already read all logs and sent them off. You have two options:
  - Start promtail later, e.g., 60 seconds
  - Make sure new log messages are written after both promtail and Loki have started

## Failed to create target, "ioutil.ReadDir: readdirent: not a directory"

The promtail configuration contains a `__path__` entry to a directory that promtail cannot find.
