---
title: Getting started
weight: 300
aliases:
    - /docs/loki/latest/getting-started/get-logs-into-loki/
---

# Getting started with Grafana Loki

> **Note:** You can use [Grafana Cloud](https://grafana.com/products/cloud/features/#cloud-logs) to avoid installing, maintaining, and scaling your own instance of Grafana Loki. The free forever plan includes 50GB of free logs. [Create an account to get started](https://grafana.com/auth/sign-up/create-user?pg=docs-loki&plcmt=in-text).

This guide assists the reader through creating and using a simple Loki cluster.
The cluster is intended for testing, development, and evaluation;
it would not meet most production requirements.

This guide uses Docker compose to instantiate these services, each in its own container: 

- One [single scalable deployment](../fundamentals/architecture/deployment-modes/) mode **Loki** instance has one read component and one write component:
    - Loki read
    - Loki write
- **Promtail** scrapes logs and pushes them to Loki.
- **Minio** is the storage back end.
- The **gateway** receives external requests and redirects them to the appropriate container based on the request's URL.
- **Grafana** provides visualization of the log lines captured within Loki.

## Instantiate the environment

Follow the instructions at [Install and deploy a simple scalable cluster with Docker compose](../installation/simple-scalable-docker) to instantiate Grafana and the Loki cluster.

The environment uses Promtail to capture log lines and push them to Loki through
the gateway service.
Which logs Promtail scrapes are defined in its configuration;
`promtail-config.yaml` specifies the configuration for this example environment.
Within the file, the `scrape_configs` block defines which logs are captured.

## Interact with the environment

This guide uses Grafana to query and observe the log lines captured in the Loki cluster.

Use Grafana by navigating a browswer to http://localhost:3000.
The Grafana instance has Loki configured as a [datasource](https://grafana.com/docs/grafana/latest/datasources/loki/).

## Get logs into Grafana Loki

After you [install and run Grafana Loki](../../installation/local/), you probably want to get logs from other applications into it.

To get application logs into Loki, you need to edit the [Promtail]({{< relref "../clients/promtail" >}}) configuration file.

Detailed information about configuring Promtail is available in [Promtail configuration](../../clients/promtail/configuration/).

The following instructions should help you get started.

1. If you haven't already, download a Promtail configuration file. Keep track of where it is, because you will need to cite it when you run the binary.

    ```
    wget https://raw.githubusercontent.com/grafana/loki/main/clients/cmd/promtail/promtail-local-config.yaml
    ```

1. Open the configuration file in the text editor of your choice. It should look similar to this:

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

    The seven lines under `scrape_configs` are what send the logs that Promtail generates to Loki, which then outputs them in the command line and http://localhost:3100/metrics.

    Copy the seven lines under `scrape_configs`, and then paste them under the original job. You can instead edit the original seven lines.

    Below is an example that sends logs from a default Grafana installation to Loki. We updated the following fields:
    - job_name - This differentiates the logs collected from other log groups.
    - targets - Optional for `static_configs`. However, is often defined because in older versions of Promtail it was not optional. This was an artifact from directly using the Prometheus service discovery code, which required this entry.
    - labels - Static label to apply to every log line scraped by this definition. Good examples include the environment name, job name, or app name.
    - __path__ - The path to where the logs that Loki is to consume are stored.

    ```
    - job_name: grafana
      static_configs:
      - targets:
          - grafana
        labels:
          job: grafana
          __path__: "C:/Program Files/GrafanaLabs/grafana/data/log/grafana.log"
    ```

1. Enter the following command to run Promtail. Examples below assume you have placed the configuration file in the same directory as the binary.

    **Windows**

    ```
    .\promtail-windows-amd64.exe --config.file=promtail-local-config.yaml
    ```

    **Linux**

    ```
    ./promtail-linux-amd64 -config.file=promtail-local-config.yaml
    ```

You should now see your application logs. If you are using Grafana, you might need to refresh your instance in order to see the logs.
