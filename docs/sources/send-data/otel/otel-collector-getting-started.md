---
title: Getting started with the OpenTelemetry Collector and Loki tutorial
menuTitle: OTel Collector tutorial
description: A Tutorial configuring the OpenTelemetry Collector to send OpenTelemetry logs to Loki
weight: 300
killercoda:
  title: Getting started with the OpenTelemetry Collector and Loki tutorial
  description: A Tutorial configuring the OpenTelemetry Collector to send OpenTelemetry logs to Loki
  preprocessing:
    substitutions:
      - regexp: loki-fundamentals-otel-collector-1
        replacement: loki-fundamentals_otel-collector_1
  backend:
    imageid: ubuntu
---

<!-- vale Grafana.We = NO -->
<!-- INTERACTIVE page intro.md START -->

# Getting started with the OpenTelemetry Collector and Loki tutorial

The OpenTelemetry Collector offers a vendor-agnostic implementation of how to receive, process, and export telemetry data. With the introduction of the OTLP endpoint in Loki, you can now send logs from applications instrumented with OpenTelemetry to Loki using the OpenTelemetry Collector in native OTLP format.
In this example, we will teach you how to configure the OpenTelemetry Collector to receive logs in the OpenTelemetry format and send them to Loki using the OTLP HTTP protocol. This will involve configuring the following components in the OpenTelemetry Collector:

- **OpenTelemetry Receiver:** This component will receive logs in the OpenTelemetry format via HTTP and gRPC.
- **OpenTelemetry Processor:** This component will accept telemetry data from other `otelcol.*` components and place them into batches. Batching improves the compression of data and reduces the number of outgoing network requests required to transmit data.
- **OpenTelemetry Exporter:** This component will accept telemetry data from other `otelcol.*` components and write them over the network using the OTLP HTTP protocol. We will use this exporter to send the logs to the Loki native OTLP endpoint.

<!-- INTERACTIVE ignore START -->

## Dependencies

Before you begin, ensure you have the following to run the demo:

- Docker
- Docker Compose

{{< admonition type="tip" >}}
Alternatively, you can try out this example in our interactive learning environment: [Getting started with the OpenTelemetry Collector and Loki tutorial](https://killercoda.com/grafana-labs/course/loki/otel-collector-getting-started).

It's a fully configured environment with all the dependencies already installed.

![Interactive](/media/docs/loki/loki-ile.svg)

Provide feedback, report bugs, and raise issues for the tutorial in the [Grafana Killercoda repository](https://github.com/grafana/killercoda).
{{< /admonition >}}

<!-- INTERACTIVE ignore END -->

## Scenario

In this scenario, we have a microservices application called the Carnivorous Greenhouse. This application consists of the following services:

- **User Service:** Manages user data and authentication for the application. Such as creating users and logging in.
- **Plant Service:** Manages the creation of new plants and updates other services when a new plant is created.
- **Simulation Service:** Generates sensor data for each plant.
- **Websocket Service:** Manages the websocket connections for the application.
- **Bug Service:** A service that when enabled, randomly causes services to fail and generate additional logs.
- **Main App:** The main application that ties all the services together.
- **Database:** A database that stores user and plant data.

Each service generates logs using the OpenTelemetry SDK and exports to the OpenTelemetry Collector in the OpenTelemetry format (OTLP). The Collector then ingests the logs and sends them to Loki.

<!-- INTERACTIVE page intro.md END -->

<!-- INTERACTIVE page step1.md START -->

## Step 1: Environment setup

In this step, we will set up our environment by cloning the repository that contains our demo application and spinning up our observability stack using Docker Compose.

1. To get started, clone the repository that contains our demo application:
    <!-- INTERACTIVE exec START -->
    ```bash
    git clone -b microservice-otel-collector  https://github.com/grafana/loki-fundamentals.git
    ```
    <!-- INTERACTIVE exec END -->
1. Next we will spin up our observability stack using Docker Compose:

    <!-- INTERACTIVE ignore START -->
    ```bash
    docker compose -f loki-fundamentals/docker-compose.yml up -d
    ```
    <!-- INTERACTIVE ignore END -->

    {{< docs/ignore >}}

    <!-- INTERACTIVE exec START -->
    ```bash
    docker-compose -f loki-fundamentals/docker-compose.yml up -d 
    ```
    <!-- INTERACTIVE exec END -->

    {{< /docs/ignore >}}

    To check the status of services we can run the following command:

    ```bash
    docker ps -a
    ```
    <!-- INTERACTIVE ignore START -->
    {{< admonition type="note" >}}
    The OpenTelemetry Collector container will show as `Stopped` or `Exited (1) About a minute ago`. This is expected as we have provided an empty configuration file. We will update this file in the next step.
    {{< /admonition >}}
    <!-- INTERACTIVE ignore END -->

After we've finished configuring the OpenTelemetry Collector and sending logs to Loki, we will be able to view the logs in Grafana. To check if Grafana is up and running, navigate to the following URL: [http://localhost:3000](http://localhost:3000)
<!-- INTERACTIVE page step1.md END -->

<!-- INTERACTIVE page step2.md START -->

## Step 2: Configuring the OpenTelemetry Collector

To configure the Collector to ingest OpenTelemetry logs from our application, we need to provide a configuration file. This configuration file will define the components and their relationships. We will build the entire observability pipeline within this configuration file.

### Open your code editor and locate the `otel-config.yaml` file

The configuration file is written using **YAML** configuration syntax. To start, we will open the `otel-config.yaml` file in the code editor:

{{< docs/ignore >}}
**Note: Killercoda has an inbuilt Code editor which can be accessed via the `Editor` tab.**

1. Expand the `loki-fundamentals` directory in the file explorer of the `Editor` tab.
2. Locate the `otel-config.yaml` file in the top level directory, `loki-fundamentals`.
3. Click on the `otel-config.yaml` file to open it in the code editor.
{{< /docs/ignore >}}

<!-- INTERACTIVE ignore START -->
1. Open the `loki-fundamentals` directory in a code editor of your choice.
1. Locate the `otel-config.yaml` file in the `loki-fundamentals` directory (Top level directory).
1. Click on the `otel-config.yaml` file to open it in the code editor.
<!-- INTERACTIVE ignore END -->

You will copy all three of the following configuration snippets into the `otel-config.yaml` file.

### Receive OpenTelemetry logs via gRPC and HTTP

First, we will configure the OpenTelemetry receiver. `otlp:` accepts logs in the OpenTelemetry format via HTTP and gRPC. We will use this receiver to receive logs from the Carnivorous Greenhouse application.

Now add the following configuration to the `otel-config.yaml` file:

```yaml
# Receivers
receivers:
  otlp:
    protocols:
      grpc:
        endpoint: 0.0.0.0:4317
      http:
        endpoint: 0.0.0.0:4318
```

In this configuration:

- `receivers`: The list of receivers to receive telemetry data. In this case, we are using the `otlp` receiver.
- `otlp`: The OpenTelemetry receiver that accepts logs in the OpenTelemetry format.
- `protocols`: The list of protocols that the receiver supports. In this case, we are using `grpc` and `http`.
- `grpc`: The gRPC protocol configuration. The receiver will accept logs via gRPC on `4317`.
- `http`: The HTTP protocol configuration. The receiver will accept logs via HTTP on `4318`.
- `endpoint`: The IP address and port number to listen on. In this case, we are listening on all IP addresses on port `4317` for gRPC and port `4318` for HTTP.

For more information on the `otlp` receiver configuration, see the [OpenTelemetry Receiver OTLP documentation](https://github.com/open-telemetry/opentelemetry-collector/blob/main/receiver/otlpreceiver/README.md).

### Create batches of logs using a OpenTelemetry processor

Next add the following configuration to the `otel-config.yaml` file:

```yaml
# Processors
processors:
  batch:
```

In this configuration:

- `processors`: The list of processors to process telemetry data. In this case, we are using the `batch` processor.
- `batch`: The OpenTelemetry processor that accepts telemetry data from other `otelcol` components and places them into batches.

For more information on the `batch` processor configuration, see the [OpenTelemetry Processor Batch documentation](https://github.com/open-telemetry/opentelemetry-collector/blob/main/processor/batchprocessor/README.md).

### Export logs to Loki using a OpenTelemetry exporter

We will use the `otlphttp/logs` exporter to send the logs to the Loki native OTLP endpoint. Add the following configuration to the `otel-config.yaml` file:

```yaml
# Exporters
exporters:
  otlphttp/logs:
    endpoint: "http://loki:3100/otlp"
    tls:
      insecure: true
```

In this configuration:

- `exporters`: The list of exporters to export telemetry data. In this case, we are using the `otlphttp/logs` exporter.
- `otlphttp/logs`: The OpenTelemetry exporter that accepts telemetry data from other `otelcol` components and writes them over the network using the OTLP HTTP protocol.
- `endpoint`: The URL to send the telemetry data to. In this case, we are sending the logs to the Loki native OTLP endpoint at `http://loki:3100/otlp`.
- `tls`: The TLS configuration for the exporter. In this case, we are setting `insecure` to `true` to disable TLS verification.
- `insecure`: Disables TLS verification. This is set to `true` as we are using an insecure connection.
  
For more information on the `otlphttp/logs` exporter configuration, see the [OpenTelemetry Exporter OTLP HTTP documentation](https://github.com/open-telemetry/opentelemetry-collector/blob/main/exporter/otlphttpexporter/README.md)

### Creating the pipeline

Now that we have configured the receiver, processor, and exporter, we need to create a pipeline to connect these components. Add the following configuration to the `otel-config.yaml` file:

```yaml
# Pipelines
service:
  pipelines:
    logs:
      receivers: [otlp]
      processors: [batch]
      exporters: [otlphttp/logs]
```

In this configuration:

- `pipelines`: The list of pipelines to connect the receiver, processor, and exporter. In this case, we are using the `logs` pipeline but there is also pipelines for metrics, traces, and continuous profiling.
- `receivers`: The list of receivers to receive telemetry data. In this case, we are using the `otlp` receiver component we created earlier.
- `processors`: The list of processors to process telemetry data. In this case, we are using the `batch` processor component we created earlier.
- `exporters`: The list of exporters to export telemetry data. In this case, we are using the `otlphttp/logs` component exporter we created earlier.

### Load the configuration

Before you load the configuration into the OpenTelemetry Collector, compare your configuration with the completed configuration below:

```yaml
# Receivers
receivers:
  otlp:
    protocols:
      grpc:
        endpoint: 0.0.0.0:4317
      http:
        endpoint: 0.0.0.0:4318
        
# Processors
processors:
  batch:

# Exporters
exporters:
  otlphttp/logs:
    endpoint: "http://loki:3100/otlp"
    tls:
      insecure: true
      
# Pipelines
service:
  pipelines:
    logs:
      receivers: [otlp]
      processors: [batch]
      exporters: [otlphttp/logs]
```

Next, we need apply the configuration to the OpenTelemetry Collector. To do this, we will restart the OpenTelemetry Collector container:
<!-- INTERACTIVE exec START -->
```bash
docker restart loki-fundamentals-otel-collector-1
```
<!-- INTERACTIVE exec END -->

This will restart the OpenTelemetry Collector container with the new configuration. You can check the logs of the OpenTelemetry Collector container to see if the configuration was loaded successfully:
<!-- INTERACTIVE exec START -->
```bash
docker logs loki-fundamentals-otel-collector-1
```

Within the logs, you should see the following message:

```console
2024-08-02T13:10:25.136Z        info    service@v0.106.1/service.go:225 Everything is ready. Begin running and processing data.
```

## Stuck? Need help?

If you get stuck or need help creating the configuration, you can copy and replace the entire `otel-config.yaml` using the completed configuration file:

<!-- INTERACTIVE exec START -->
```bash
cp loki-fundamentals/completed/otel-config.yaml loki-fundamentals/otel-config.yaml
docker restart loki-fundamentals-otel-collector-1
```
<!-- INTERACTIVE exec END -->

<!-- INTERACTIVE page step2.md END -->

<!-- INTERACTIVE page step3.md START -->

## Step 3: Start the Carnivorous Greenhouse

In this step, we will start the Carnivorous Greenhouse application. To start the application, run the following command:
<!-- INTERACTIVE ignore START -->
{{< admonition type="note" >}}
This docker-compose file relies on the `loki-fundamentals_loki` docker network. If you have not started the observability stack, you will need to start it first.
{{< /admonition >}}
<!-- INTERACTIVE ignore END -->

{{< docs/ignore >}}

**Note: This docker-compose file relies on the `loki-fundamentals_loki` docker network. If you have not started the observability stack, you will need to start it first.**

{{< /docs/ignore >}}

<!-- INTERACTIVE ignore START -->
```bash
docker compose -f loki-fundamentals/greenhouse/docker-compose-micro.yml up -d --build 
```
<!-- INTERACTIVE ignore END -->

{{< docs/ignore >}}

<!-- INTERACTIVE exec START -->
```bash
docker-compose -f loki-fundamentals/greenhouse/docker-compose-micro.yml up -d --build
```
<!-- INTERACTIVE exec END -->

{{< /docs/ignore >}}

This will start the following services:

```console
 ✔ Container greenhouse-db-1                 Started                                                         
 ✔ Container greenhouse-websocket_service-1  Started 
 ✔ Container greenhouse-bug_service-1        Started
 ✔ Container greenhouse-user_service-1       Started
 ✔ Container greenhouse-plant_service-1      Started
 ✔ Container greenhouse-simulation_service-1 Started
 ✔ Container greenhouse-main_app-1           Started
```

Once started, you can access the Carnivorous Greenhouse application at [http://localhost:5005](http://localhost:5005). Generate some logs by interacting with the application in the following ways:

1. Create a user.
1. Log in.
1. Create a few plants to monitor.
1. Enable bug mode to activate the bug service. This will cause services to fail and generate additional logs.

Finally to view the logs in Loki, navigate to the Loki Logs Explore view in Grafana at [http://localhost:3000/a/grafana-lokiexplore-app/explore](http://localhost:3000/a/grafana-lokiexplore-app/explore).

<!-- INTERACTIVE page step3.md END -->

<!-- INTERACTIVE page finish.md START -->

## Summary

In this example, we configured the OpenTelemetry Collector to receive logs from an example application and send them to Loki using the native OTLP endpoint. Make sure to also consult the Loki configuration file `loki-config.yaml` to understand how we have configured Loki to receive logs from the OpenTelemetry Collector.

{{< docs/ignore >}}

### Back to docs

Head back to where you started from to continue with the [Loki documentation](https://grafana.com/docs/loki/latest/send-data/otel).

{{< /docs/ignore >}}

## Further reading

For more information on the OpenTelemetry Collector and the native OTLP endpoint of Loki, refer to the following resources:

- [Loki OTLP endpoint](https://grafana.com/docs/loki/<LOKI_VERSION>/send-data/otel/)
- [How is native OTLP endpoint different from Loki Exporter](https://grafana.com/docs/loki/<LOKI_VERSION>/send-data/otel/native_otlp_vs_loki_exporter)
- [OpenTelemetry Collector Configuration](https://opentelemetry.io/docs/collector/configuration/)

## Complete metrics, logs, traces, and profiling example

If you would like to use a demo that includes Mimir, Loki, Tempo, and Grafana, you can use [Introduction to Metrics, Logs, Traces, and Profiling in Grafana](https://github.com/grafana/intro-to-mlt). `Intro-to-mltp` provides a self-contained environment for learning about Mimir, Loki, Tempo, and Grafana.

The project includes detailed explanations of each component and annotated configurations for a single-instance deployment. Data from `intro-to-mltp` can also be pushed to Grafana Cloud.

<!-- INTERACTIVE page finish.md END -->
<!-- vale Grafana.We = YES -->