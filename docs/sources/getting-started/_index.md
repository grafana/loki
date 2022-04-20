---
title: Getting started
weight: 300
description: "This guide assists the reader to create and use a simple Loki cluster for testing and evaluation purposes."
aliases:
    - /docs/loki/latest/getting-started/get-logs-into-loki/
---

# Getting started with Grafana Loki

> **Note:** You can use [Grafana Cloud](https://grafana.com/products/cloud/features/#cloud-logs) to avoid installing, maintaining, and scaling your own instance of Grafana Loki. The free forever plan includes 50GB of free logs. [Create an account to get started](https://grafana.com/auth/sign-up/create-user?pg=docs-loki&plcmt=in-text).

This guide assists the reader to create and use a simple Loki cluster.
The cluster is intended for testing, development, and evaluation;
it will not meet most production requirements.

The test environment runs an app (flog) that generates log lines.
Promtail is the environment's agent (or client) that captures the log lines and pushes them to the Loki cluster through a gateway.
Grafana provides a way to enter and visualize queries against the logs stored in Loki.
 
![Simple scalable deployment test environment](simple-scalable-test-environment.png)

The test environment uses Docker compose to instantiate these services, each in its own container: 

- One [single scalable deployment](../fundamentals/architecture/deployment-modes/) mode **Loki** instance has:
    - One Loki read component
    - One Loki write component
- **Minio** is the storage back end.
- The **gateway** receives requests and redirects them to the appropriate container based on the request's URL.
- **Grafana** provides visualization of the log lines captured within Loki.

## Instantiate the environment

## Prerequisites

- [Docker](https://docs.docker.com/install)
- [Docker Compose](https://docs.docker.com/compose/install)

## Obtain configuration files

Download `loki-config.yaml`, `promtail-config.yaml`, and `docker-compose.yaml` to your current directory:

```bash
wget https://raw.githubusercontent.com/grafana/loki/main/production/simple-scalable/promtail-config.yaml -O promtail-config.yaml
wget https://raw.githubusercontent.com/grafana/loki/main/production/simple-scalable/loki-config.yaml -O loki-config.yaml
wget https://raw.githubusercontent.com/grafana/loki/main/production/simple-scalable/docker-compose.yaml -O docker-compose.yaml
```

This will download `loki-config.yaml`, `promtail-config.yaml`, and `docker-compose.yaml` to your current directory.

The `docker-compose.yaml` relies on the [Loki docker driver](https://grafana.com/docs/loki/latest/clients/docker-driver/), 
aliased to `loki-compose`, to send logs to the loki cluster. If this driver is not installed on your system, you can install it by running the following:

```bash
docker plugin install grafana/loki-docker-driver:latest --alias loki-compose --grant-all-permissions
```

If this driver is already installed, but under a different alias, you will have to change `docker-compose.yaml` to use the correct alias.

## Deploy and verify readiness of the Loki cluster

From the directory containing the configuration files, deploy the cluster with docker-compose:

```bash
docker-compose up
```

The running Docker containers use the directory's configuration files.

Navigate to http://localhost:3100/ready to check for cluster readiness.
Navigate to http://localhost:3100/metrics to view the cluster metrics.
Navigate to http://localhost:3000 for the Grafana instance that has Loki configured as a datasource.

By default, the image runs processes as user loki with  UID `10001` and GID `10001`.
You can use a different user, specially if you are using bind mounts, by specifying the UID with a `docker run` command and using `--user=UID` with numeric UID suited to your needs.
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

Click on the Grafana instance's [Explore](https://grafana.com/docs/grafana/latest/explore/) icon to bring up the log browser.

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
