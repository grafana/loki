---
title: Recive OpenTelemetry logs via Kafka using Alloy and Loki
menuTitle: Recive OpenTelemetry logs via Kafka using Alloy and Loki
description: Configuring Grafana Alloy to recive OpenTelemetry logs via Kafka and send them to Loki.
weight: 250
killercoda:
  title: Recive OpenTelemetry logs via Kafka using Alloy and Loki
  description: Configuring Grafana Alloy to recive OpenTelemetry logs via Kafka and send them to Loki.
  backend:
    imageid: ubuntu
---

<!-- INTERACTIVE page intro.md START -->

#  Recive OpenTelemetry logs via Kafka using Alloy and Loki

Alloy natively supports ingesting OpenTelemetry logs via Kafka. There maybe several scenarios where you may want to ingest logs via Kafka. For instance you may already use Kafka to aggregate logs from several otel collectors. Or your application may already be writing logs to Kafka and you want to ingest them into Loki. In this example, we will make use of 3 Alloy components to achieve this:

## Dependencies

Before you begin, ensure you have the following to run the demo:

- Docker
- Docker Compose

<!-- INTERACTIVE ignore START -->
{{< admonition type="note" >}}
Alternatively, you can try out this example in our online sandbox. Which is a fully configured environment with all the dependencies pre-installed. You can access the sandbox [here](https://killercoda.com/grafana-labs/course/loki/alloy-otel-logs).
{{< /admonition >}}
<!-- INTERACTIVE ignore END -->

## Scenario

In this scenario, we have a microservices application called the Carnivourse Greenhouse. This application consists of the following services:

- **User Service:** Mangages user data and authentication for the application. Such as creating users and logging in.
- **plant Service:** Manges the creation of new plants and updates other services when a new plant is created.
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
1.  Next we will spin up our observability stack using Docker Compose:

    <!-- INTERACTIVE ignore START -->
    ```bash
    docker compose -f loki-fundamentals/docker-compose.yml up -d
    ```
    <!-- INTERACTIVE ignore END -->

    <!-- INTERACTIVE include START -->

    <!--  ```bash -->
    <!-- docker-compose -f loki-fundamentals/docker-compose.yml up -d -->
    <!--  ```{{exec}} -->
    <!-- INTERACTIVE include END -->

    This will spin up the following services:
    ```bash
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

<!-- INTERACTIVE include START -->
<!-- **Note: Killercoda has an inbuilt Code editor which can be accessed via the `Editor` tab.** -->
<!-- INTERACTIVE include END -->

### OpenTelelmetry Logs Receiver

First, we will configure the OpenTelemetry logs receiver. This receiver will accept logs via HTTP and gRPC.

Open the `config.alloy` file in the `loki-fundamentals` directory and copy the following configuration:
<!-- INTERACTIVE copy START -->

```alloy
 otelcol.receiver.otlp "default" {
   http {}
   grpc {}

   output {
     logs    = [otelcol.processor.batch.default.input]
   }
 }
```

<!-- INTERACTIVE copy END -->


### OpenTelemetry Logs Processor

Next, we will configure the OpenTelemetry logs processor. This processor will batch the logs before sending them to the logs exporter.

Open the `config.alloy` file in the `loki-fundamentals` directory and copy the following configuration:
<!-- INTERACTIVE copy START -->
```alloy
otelcol.processor.batch "default" {
    output {
        logs = [otelcol.exporter.otlphttp.default.input]
    }
}
```
<!-- INTERACTIVE copy END -->

### OpenTelemetry Logs Exporter

Lastly, we will configure the OpenTelemetry logs exporter. This exporter will send the logs to Loki.

Open the `config.alloy` file in the `loki-fundamentals` directory and copy the following configuration:
<!-- INTERACTIVE copy START -->
```alloy
otelcol.exporter.otlphttp "default" {
  client {
    endpoint = "http://loki:3100/otlp"
  }
}
```
<!-- INTERACTIVE copy END -->

### Reload the Alloy configuration

Once added, save the file. Then run the following command to request Alloy to reload the configuration:
<!-- INTERACTIVE exec START -->
```bash
curl -X POST http://localhost:12345/-/reload
```
<!-- INTERACTIVE exec END -->

The new configuration will be loaded this can be verified by checking the Alloy UI: [http://localhost:12345](http://localhost:12345).

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

<!-- INTERACTIVE include START -->
<!-- **Note: This docker-compose file relies on the `loki-fundamentals_loki` docker network. If you have not started the observability stack, you will need to start it first.** -->
<!-- INTERACTIVE include END -->

<!-- INTERACTIVE ignore START -->
```bash
docker compose -f lloki-fundamentals/greenhouse/docker-compose-micro.yml up -d --build 
```
<!-- INTERACTIVE ignore END -->

<!-- INTERACTIVE include START -->

<!--  ```bash -->
<!-- docker-compose -f loki-fundamentals/greenhouse/docker-compose-micro.yml up -d --build  -->
<!--  ```{{exec}} -->
<!-- INTERACTIVE include END -->

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