---
title: Ingesting OpenTelemetry logs to Loki using Alloy
menuTitle: Ingesting OpenTelemetry logs using Alloy
description: Configuring Grafana Alloy to ingest OpenTelemetry logs to Loki.
weight: 250
killercoda:
  title: Ingesting OpenTelemetry logs to Loki using Alloy
  description: Configuring Grafana Alloy to ingest OpenTelemetry logs to Loki.
  details:
    finish:
      text: finish.md
  backend:
    imageid: ubuntu
---

<!-- Killercoda intro.md START -->

# Ingesting OpenTelemetry logs to Loki using Alloy

Alloy natively supports ingesting OpenTelemetry logs. In this example, we will configure Alloy to ingest OpenTelemetry logs to Loki.

## Dependencies

Before you begin, ensure you have the following to run the demo:

- Docker
- Docker Compose

<!-- Killercoda ignore START -->

{{< admonition type="note" >}}
Alternatively, you can try out this example in our online sandbox. Which is a fully configured environment with all the dependencies pre-installed. You can access the sandbox [here](https://killercoda.com/grafana-labs/course/loki/alloy-otel-logs).

{{< /admonition >}}

<!-- Killercoda ignore END -->

## Scenario

In this scenario, we have a microservices application called the Carnivourse Greenhouse. This application consists of the following services:

- foo

<!-- Killercoda intro.md END -->

<!-- Killercoda step1.md START -->

## Step 1: Environment setup

In this step, we will set up our environment by cloning the repository that contains our demo application and spinning up our observability stack using Docker Compose.

1.  To get started, clone the repository that contains our demo application:
    <!-- Killercoda exec START -->
    ```bash
    git clone -b microservice-otel  https://github.com/grafana/loki-fundamentals.git
    ```
    <!-- Killercoda exec END -->
1.  Next we will spin up our observability stack using Docker Compose:

    <!-- Killercoda ignore START -->
    ```bash
    docker compose -f loki-fundamentals/docker-compose.yml up -d
    ```
    <!-- Killercoda ignore END -->

    <!-- Killercoda include START -->

    <!--  ```bash -->
    <!-- docker-compose -f loki-fundamentals/docker-compose.yml up -d -->
    <!--  ```{{exec}} -->
    <!-- Killercoda include END -->



    This will spin up the following services:
    ```bash
    ✔ Container loki-fundamentals-grafana-1  Started                                                        
    ✔ Container loki-fundamentals-loki-1     Started                        
    ✔ Container loki-fundamentals-alloy-1    Started
    ```

We will be access two UI interfaces:

- Grafana at [http://localhost:3000](http://localhost:3000)
- Alloy at [http://localhost:12345](http://localhost:12345)

<!-- Killercoda step1.md END -->

<!-- Killercoda step2.md START -->

## Step 2: Configure Alloy to ingest OpenTelemetry logs

To configure Alloy to ingest OpenTelemetry logs, we need to update the Alloy configuration file. To start, we will update the `config.alloy` file to include the OpenTelemetry logs configuration.

### OpenTelelmetry Logs Receiver

First, we will configure the OpenTelemetry logs receiver. This receiver will accept logs via HTTP and gRPC.

1. Open the `config.alloy` file in the `loki-fundamentals` directory and copy the following configuration:
   <!-- Killercoda copy START -->

   ```alloy
     otelcol.receiver.otlp "default" {
       http {}
       grpc {}

       output {
         logs    = [otelcol.processor.batch.default.input]
       }
     }
   ```

   <!-- Killercoda copy END -->


### OpenTelemetry Logs Processor

Next, we will configure the OpenTelemetry logs processor. This processor will batch the logs before sending them to the logs exporter.

1.  Open the `config.alloy` file in the `loki-fundamentals` directory and copy the following configuration:
    <!-- Killercoda copy START -->
    ```alloy
        otelcol.processor.batch "default" {
        output {
        logs = [otelcol.exporter.otlphttp.default.input]
        }
          }
    ```
    <!-- Killercoda copy END -->

### OpenTelemetry Logs Exporter

Lastly, we will configure the OpenTelemetry logs exporter. This exporter will send the logs to Loki.

1.  Open the `config.alloy` file in the `loki-fundamentals` directory and copy the following configuration:
    <!-- Killercoda copy START -->
    ```alloy
    otelcol.exporter.otlphttp "default" {
      client {
        endpoint = "http://loki:3100/otlp"
      }
    }
    ```
    <!-- Killercoda copy END -->

Once added, save the file. Then run the following command to request Alloy to reload the configuration:

<!-- Killercoda exec START -->
```bash
curl -X POST http://localhost:12345/-/reload
```
<!-- Killercoda exec END -->

## Stuck? Need help?

If you get stuck or need help creating the configuration, you can copy and replace the entire `config.alloy` using the completed configuration file:

<!-- Killercoda exec START -->
```bash
cp loki-fundamentals/completed/config.alloy loki-fundamentals/config.alloy
```
<!-- Killercoda exec END -->

<!-- Killercoda step2.md END -->

<!-- Killercoda step3.md START -->

## Step 3: Start the Carnivorous Greenhouse

In this step, we will start the Carnivorous Greenhouse application. To start the application, run the following command:
<!-- Killercoda ignore START -->
{{< admonition type="note" >}}
This docker-compose file relies on the `loki-fundamentals_loki` docker network. If you have not started the observability stack, you will need to start it first.
{{< /admonition >}}
<!-- Killercoda ignore END -->

<!-- Killercoda include START -->
<!-- **Note: This docker-compose file relies on the `loki-fundamentals_loki` docker network. If you have not started the observability stack, you will need to start it first. ** -->
<!-- Killercoda include END -->

<!-- Killercoda exec START -->
```bash
 docker compose -f loki-fundamentals/greenhouse/docker-compose-micro.yml up -d --build 
```
<!-- Killercoda exec END -->

This will start the following services:
```


```



