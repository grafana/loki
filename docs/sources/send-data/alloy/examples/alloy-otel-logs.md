---
title: Sending OpenTelemetry logs to Loki using Alloy
menuTitle: Sending OpenTelemetry logs to Loki using Alloy
description: Configuring Grafana Alloy to send OpenTelemetry logs to Loki.
weight: 250
killercoda:
  title: Sending OpenTelemetry logs to Loki using Alloy
  description: Configuring Grafana Alloy to send OpenTelemetry logs to Loki.
  backend:
    imageid: ubuntu
---
<!-- vale Grafana.We = NO -->
<!-- INTERACTIVE page intro.md START -->

# Sending OpenTelemetry logs to Loki using Alloy

Alloy natively supports receiving logs in the OpenTelemetry format. This allows you to send logs from applications instrumented with OpenTelemetry to Alloy, which can then be sent to Loki for storage and visualization in Grafana. In this example, we will make use of 3 Alloy components to achieve this:

- **OpenTelemetry Receiver:** This component will receive logs in the OpenTelemetry format via HTTP and gRPC.
- **OpenTelemetry Processor:** This component will accept telemetry data from other `otelcol.*` components and place them into batches. Batching improves the compression of data and reduces the number of outgoing network requests required to transmit data.
- **OpenTelemetry Exporter:** This component will accept telemetry data from other `otelcol.*` components and write them over the network using the OTLP HTTP protocol. We will use this exporter to send the logs to the Loki native OTLP endpoint.

<!-- INTERACTIVE ignore START -->

## Dependencies

Before you begin, ensure you have the following to run the demo:

- Docker
- Docker Compose

{{< admonition type="tip" >}}
Alternatively, you can try out this example in our interactive learning environment: [Sending OpenTelemetry logs to Loki using Alloy](https://killercoda.com/grafana-labs/course/loki/alloy-otel-logs).

It's a fully configured environment with all the dependencies already installed.

![Interactive](/media/docs/loki/loki-ile.svg)

Provide feedback, report bugs, and raise issues in the [Grafana Killercoda repository](https://github.com/grafana/killercoda).
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

Each service generates logs using the OpenTelemetry SDK and exports to Alloy in the OpenTelemetry format. Alloy then ingests the logs and sends them to Loki. We will configure Alloy to ingest OpenTelemetry logs, send them to Loki, and view the logs in Grafana.

<!-- INTERACTIVE page intro.md END -->

<!-- INTERACTIVE page step1.md START -->

## Step 1: Environment setup

In this step, we will set up our environment by cloning the repository that contains our demo application and spinning up our observability stack using Docker Compose.

1. To get started, clone the repository that contains our demo application:
    <!-- INTERACTIVE exec START -->
    ```bash
    git clone -b microservice-otel  https://github.com/grafana/loki-fundamentals.git
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

    This will spin up the following services:

    ```console
    ✔ Container loki-fundamentals-grafana-1  Started                                                        
    ✔ Container loki-fundamentals-loki-1     Started                        
    ✔ Container loki-fundamentals-alloy-1    Started
    ```

We will be access two UI interfaces:

- Alloy at [http://localhost:12345](http://localhost:12345)
- Grafana at [http://localhost:3000](http://localhost:3000)
<!-- INTERACTIVE page step1.md END -->

<!-- INTERACTIVE page step2.md START -->

## Step 2: Configure Alloy to ingest OpenTelemetry logs

To configure Alloy to ingest OpenTelemetry logs, we need to update the Alloy configuration file. To start, we will update the `config.alloy` file to include the OpenTelemetry logs configuration.

### Open your code editor and locate the `config.alloy` file

Grafana Alloy requires a configuration file to define the components and their relationships. The configuration file is written using Alloy configuration syntax. We will build the entire observability pipeline within this configuration file. To start, we will open the `config.alloy` file in the code editor:

{{< docs/ignore >}}
**Note: Killercoda has an inbuilt Code editor which can be accessed via the `Editor` tab.**

1. Expand the `loki-fundamentals` directory in the file explorer of the `Editor` tab.
1. Locate the `config.alloy` file in the top level directory, `loki-fundamentals'.
1. Click on the `config.alloy` file to open it in the code editor.
{{< /docs/ignore >}}

<!-- INTERACTIVE ignore START -->
1. Open the `loki-fundamentals` directory in a code editor of your choice.
1. Locate the `config.alloy` file in the `loki-fundamentals` directory (Top level directory).
1. Click on the `config.alloy` file to open it in the code editor.
<!-- INTERACTIVE ignore END -->

You will copy all three of the following configuration snippets into the `config.alloy` file.

### Receive OpenTelemetry logs via gRPC and HTTP

First, we will configure the OpenTelemetry receiver. `otelcol.receiver.otlp` accepts logs in the OpenTelemetry format via HTTP and gRPC. We will use this receiver to receive logs from the Carnivorous Greenhouse application.

Now add the following configuration to the `config.alloy` file:

```alloy
 otelcol.receiver.otlp "default" {
   http {}
   grpc {}

   output {
     logs    = [otelcol.processor.batch.default.input]
   }
 }
```

In this configuration:

- `http`: The HTTP configuration for the receiver. This configuration is used to receive logs in the OpenTelemetry format via HTTP.
- `grpc`: The gRPC configuration for the receiver. This configuration is used to receive logs in the OpenTelemetry format via gRPC.
- `output`: The list of processors to forward the logs to. In this case, we are forwarding the logs to the `otelcol.processor.batch.default.input`.

For more information on the `otelcol.receiver.otlp` configuration, see the [OpenTelemetry Receiver OTLP documentation](https://grafana.com/docs/alloy/latest/reference/components/otelcol.receiver.otlp/).

### Create batches of logs using a OpenTelemetry processor

Next, we will configure a OpenTelemetry processor. `otelcol.processor.batch` accepts telemetry data from other `otelcol` components and places them into batches. Batching improves the compression of data and reduces the number of outgoing network requests required to transmit data. This processor supports both size and time based batching.

Now add the following configuration to the `config.alloy` file:

```alloy
otelcol.processor.batch "default" {
    output {
        logs = [otelcol.exporter.otlphttp.default.input]
    }
}
```

In this configuration:

- `output`: The list of receivers to forward the logs to. In this case, we are forwarding the logs to the `otelcol.exporter.otlphttp.default.input`.

For more information on the `otelcol.processor.batch` configuration, see the [OpenTelemetry Processor Batch documentation](https://grafana.com/docs/alloy/latest/reference/components/otelcol.processor.batch/).

### Export logs to Loki using a OpenTelemetry exporter

Lastly, we will configure the OpenTelemetry exporter. `otelcol.exporter.otlphttp` accepts telemetry data from other `otelcol` components and writes them over the network using the OTLP HTTP protocol. We will use this exporter to send the logs to the Loki native OTLP endpoint.

Now add the following configuration to the `config.alloy` file:

```alloy
otelcol.exporter.otlphttp "default" {
  client {
    endpoint = "http://loki:3100/otlp"
  }
}
```

For more information on the `otelcol.exporter.otlphttp` configuration, see the [OpenTelemetry Exporter OTLP HTTP documentation](https://grafana.com/docs/alloy/latest/reference/components/otelcol.exporter.otlphttp/).

### Reload the Alloy configuration

Once added, save the file. Then run the following command to request Alloy to reload the configuration:
<!-- INTERACTIVE exec START -->
```bash
curl -X POST http://localhost:12345/-/reload
```
<!-- INTERACTIVE exec END -->

The new configuration will be loaded. You can verify this by checking the Alloy UI: [http://localhost:12345](http://localhost:12345).

## Stuck? Need help?

If you get stuck or need help creating the configuration, you can copy and replace the entire `config.alloy` using the completed configuration file:

<!-- INTERACTIVE exec START -->
```bash
cp loki-fundamentals/completed/config.alloy loki-fundamentals/config.alloy
curl -X POST http://localhost:12345/-/reload
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

```bash
 ✔ Container greenhouse-db-1                 Started                                                         
 ✔ Container greenhouse-websocket_service-1  Started 
 ✔ Container greenhouse-bug_service-1        Started
 ✔ Container greenhouse-user_service-1       Started
 ✔ Container greenhouse-plant_service-1      Started
 ✔ Container greenhouse-simulation_service-1 Started
 ✔ Container greenhouse-main_app-1           Started
```

Once started, you can access the Carnivorous Greenhouse application at [http://localhost:5005](http://localhost:5005). Generate some logs by interacting with the application in the following ways:

- Create a user
- Log in
- Create a few plants to monitor
- Enable bug mode to activate the bug service. This will cause services to fail and generate additional logs.

Finally to view the logs in Loki, navigate to the Loki Logs Explore view in Grafana at [http://localhost:3000/a/grafana-lokiexplore-app/explore](http://localhost:3000/a/grafana-lokiexplore-app/explore).

<!-- INTERACTIVE page step3.md END -->

<!-- INTERACTIVE page finish.md START -->

## Summary

In this example, we configured Alloy to ingest OpenTelemetry logs and send them to Loki. This was a simple example to demonstrate how to send logs from an application instrumented with OpenTelemetry to Loki using Alloy. Where to go next?

{{< docs/ignore >}}

### Back to docs

Head back to where you started from to continue with the Loki documentation: [Loki documentation](https://grafana.com/docs/loki/latest/send-data/alloy)

{{< /docs/ignore >}}

## Further reading

For more information on Grafana Alloy, refer to the following resources:

- [Grafana Alloy getting started examples](https://grafana.com/docs/alloy/latest/tutorials/)
- [Grafana Alloy component reference](https://grafana.com/docs/alloy/latest/reference/components/)

## Complete metrics, logs, traces, and profiling example

If you would like to use a demo that includes Mimir, Loki, Tempo, and Grafana, you can use [Introduction to Metrics, Logs, Traces, and Profiling in Grafana](https://github.com/grafana/intro-to-mlt). `Intro-to-mltp` provides a self-contained environment for learning about Mimir, Loki, Tempo, and Grafana.

The project includes detailed explanations of each component and annotated configurations for a single-instance deployment. Data from `intro-to-mltp` can also be pushed to Grafana Cloud.

<!-- INTERACTIVE page finish.md END -->
<!-- vale Grafana.We = YES -->