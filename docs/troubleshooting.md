# Troubleshooting

## "Data source connected, but no labels received. Verify that Loki and Promtail is configured properly."

This error can appear in Grafana when you add Loki as a datasource.
It means that Grafana can connect to Loki, but Loki has not received any logs from promtail.
This can have several reasons:

- Promtail cannot reach Loki, check promtail's output.
- Promtail started sending logs before Loki was ready. This can happen in test environments where promtail already read all logs and sent them off. Here is what you can do:
  - Generally start promtail after Loki, e.g., 60 seconds later.
  - Restarting promtail will not necessarily resend log messages that have been read. To force sending all messages again, delete the positions file (default location `/tmp/positions.yaml`) or make sure new log messages are written after both promtail and Loki have started.

## Failed to create target, "ioutil.ReadDir: readdirent: not a directory"

The promtail configuration contains a `__path__` entry to a directory that promtail cannot find.
