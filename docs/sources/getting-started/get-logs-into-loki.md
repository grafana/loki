---
title: Get logs into Loki
---
# Get logs into Loki

After you [install and run Loki](../../installation/local/), you probably want to get logs from other applications into it.

To get application logs into Loki, you need to edit the [Promtail]({{< relref "../clients/promtail" >}}) config file.

Detailed information about configuring Promtail is available in [Promtail configuration](../../clients/promtail/configuration/).

The following instructions should help you get started.

1. If you haven't already, download a Promtail configuration file. Keep track of where it is, because you will need to cite it when you run the binary.

```
wget https://github.com/grafana/loki/blob/master/cmd/promtail/promtail-local-config.yaml
```

2. Open the config file in the text editor of your choice. It should look similar to this:

```
server:
  http_listen_port: 9080
  grpc_listen_port: 0

positions:
  filename: /tmp/positions.yaml

clients:
  - url: http://loki:3100/loki/api/v1/push

scrape_configs:
- job_name: system
  static_configs:
  - targets:
      - localhost
    labels:
      job: varlogs
      __path__: /var/log/*log
```

   The seven lines under `scrape_configs` are what send the logs that Loki generates to Loki, which then outputs them in the command line and http://localhost:3100/metrics.

3. Copy the seven lines under `scrape_configs`, and then paste them under the original job (you can also just edit the original seven lines).

   Below is an example that sends logs from a default Grafana installation to Loki. We updated the following fields:
   - job_name - This differentiates the logs collected from other log groups.
   - targets - Optional for static_configs, however is often defined because in older versions of Promtail it was not optional. This was an artifact from directly using the Prometheus service discovery code which required this entry.
   - labels - Static label to apply to every log line scraped by this definition. Good examples would be environment name, job name, or app name.
   - __path__ - The path to where the logs are stored that I want Loki to consume.

```
- job_name: grafana
  static_configs:
  - targets:
      - grafana
    labels:
      job: grafana
      __path__: "C:/Program Files/GrafanaLabs/grafana/data/log/grafana.log"
```

4. Enter the following command to run Promtail. Examples below assume you have put the config file in the same directory as the binary.

**Windows**

```
`.\promtail-windows-amd64.exe --config.file=promtail-local-config.yaml`
```

**Linux**

```
./promtail-linux-amd64 -config.file=promtail-local-config.yaml
```

You should now see your application logs. If you are using Grafana, you might need to refresh your instance in order to see the logs.
