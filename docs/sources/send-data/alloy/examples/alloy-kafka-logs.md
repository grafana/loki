---
title: Sending Logs to Loki via Kafka using Alloy
menuTitle: Sending Logs to Loki via Kafka using Alloy
description: Configuring Grafana Alloy to receive logs via Kafka and send them to Loki.
weight: 250
killercoda:
  comment: |
    This file is used to generate the interactive tutorial for sending logs to Loki via Kafka using Alloy. Please do not change url's with placeholders from the code snippets. This tutorial is assumes they remain static.
  title: Sending Logs to Loki via Kafka using Alloy
  description: Configuring Grafana Alloy to receive logs via Kafka and send them to Loki.
  backend:
    imageid: ubuntu
---
<!-- vale Grafana.We = NO -->
<!-- INTERACTIVE page intro.md START -->

# Sending Logs to Loki via Kafka using Alloy

Alloy natively supports receiving logs via Kafka. In this example, we will configure Alloy to receive logs via Kafka using two different methods:

- [loki.source.kafka](https://grafana.com/docs/alloy/latest/reference/components/loki.source.kafka): reads messages from Kafka using a consumer group and forwards them to other `loki.*` components.
- [otelcol.receiver.kafka](https://grafana.com/docs/alloy/latest/reference/components/otelcol.receiver.kafka/): accepts telemetry data from a Kafka broker and forwards it to other `otelcol.*` components.

<!-- INTERACTIVE ignore START -->

## Dependencies

Before you begin, ensure you have the following to run the demo:

- Docker
- Docker Compose

{{< admonition type="tip" >}}
Alternatively, you can try out this example in our interactive learning environment: [Sending Logs to Loki via Kafka using Alloy](https://killercoda.com/grafana-labs/course/loki/alloy-kafka-logs).

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

Each service generates logs that are sent to Alloy via Kafka. In this example, they are sent on two different topics:

- `loki`: This sends a structured log formatted message (json).
- `otlp`: This sends a serialized OpenTelemetry log message.

You would not typically do this within your own application, but for the purposes of this example we wanted to show how Alloy can handle different types of log messages over Kafka.

<!-- INTERACTIVE page intro.md END -->

<!-- INTERACTIVE page step1.md START -->

## Step 1: Environment setup

In this step, we will set up our environment by cloning the repository that contains our demo application and spinning up our observability stack using Docker Compose.

1. To get started, clone the repository that contains our demo application:
    <!-- INTERACTIVE exec START -->
    ```bash
    git clone -b microservice-kafka  https://github.com/grafana/loki-fundamentals.git
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
    ✔ Container loki-fundamentals-grafana-1      Started                                                        
    ✔ Container loki-fundamentals-loki-1         Started                        
    ✔ Container loki-fundamentals-alloy-1        Started
    ✔ Container loki-fundamentals-zookeeper-1    Started
    ✔ Container loki-fundamentals-kafka-1        Started
    ```

We will be access two UI interfaces:

- Alloy at [http://localhost:12345](http://localhost:12345)
- Grafana at [http://localhost:3000](http://localhost:3000)
<!-- INTERACTIVE page step1.md END -->

<!-- INTERACTIVE page step2.md START -->

## Step 2: Configure Alloy to ingest raw Kafka logs

In this first step, we will configure Alloy to ingest raw Kafka logs. To do this, we will update the `config.alloy` file to include the Kafka logs configuration.

### Open your code editor and locate the `config.alloy` file

Grafana Alloy requires a configuration file to define the components and their relationships. The configuration file is written using Alloy configuration syntax. We will build the entire observability pipeline within this configuration file. To start, we will open the `config.alloy` file in the code editor:

{{< docs/ignore >}}
**Note: Killercoda has an inbuilt Code editor which can be accessed via the `Editor` tab.**

1. Expand the `loki-fundamentals` directory in the file explorer of the `Editor` tab.
1. Locate the `config.alloy` file in the `loki-fundamentals` directory (Top level directory).
1. Click on the `config.alloy` file to open it in the code editor.
{{< /docs/ignore >}}

<!-- INTERACTIVE ignore START -->
1. Open the `loki-fundamentals` directory in a code editor of your choice.
1. Locate the `config.alloy` file in the top level directory, `loki-fundamentals'.
1. Click on the `config.alloy` file to open it in the code editor.
<!-- INTERACTIVE ignore END -->

You will copy all three of the following configuration snippets into the `config.alloy` file.  

### Source logs from Kafka

First, we will configure the Loki Kafka source. `loki.source.kafka` reads messages from Kafka using a consumer group and forwards them to other `loki.*` components.

The component starts a new Kafka consumer group for the given arguments and fans out incoming entries to the list of receivers in `forward_to`.

Add the following configuration to the `config.alloy` file:

```alloy
loki.source.kafka "raw" {
  brokers                = ["kafka:9092"]
  topics                 = ["loki"]
  forward_to             = [loki.write.http.receiver]
  relabel_rules          = loki.relabel.kafka.rules
  version                = "2.0.0"
  labels                = {service_name = "raw_kafka"}
}
```

In this configuration:

- `brokers`: The Kafka brokers to connect to.
- `topics`: The Kafka topics to consume. In this case, we are consuming the `loki` topic.
- `forward_to`: The list of receivers to forward the logs to. In this case, we are forwarding the logs to the `loki.write.http.receiver`.
- `relabel_rules`: The relabel rules to apply to the incoming logs. This can be used to generate labels from the temporary internal labels that are added by the Kafka source.
- `version`: The Kafka protocol version to use.
- `labels`: The labels to add to the incoming logs. In this case, we are adding a `service_name` label with the value `raw_kafka`. This will be used to identify the logs from the raw Kafka source in the Log Explorer App in Grafana.

For more information on the `loki.source.kafka` configuration, see the [Loki Kafka Source documentation](https://grafana.com/docs/alloy/latest/reference/components/loki.source.kafka/).

### Create a dynamic relabel based on Kafka topic

Next, we will configure the Loki relabel rules. The `loki.relabel` component rewrites the label set of each log entry passed to its receiver by applying one or more relabeling rules and forwards the results to the list of receivers in the component’s arguments. In our case we are directly calling the rule from the `loki.source.kafka` component.

Now add the following configuration to the `config.alloy` file:

```alloy
loki.relabel "kafka" {
  forward_to      = [loki.write.http.receiver]
  rule {
    source_labels = ["__meta_kafka_topic"]
    target_label  = "topic"
  }
}
```

In this configuration:

- `forward_to`: The list of receivers to forward the logs to. In this case, we are forwarding the logs to the `loki.write.http.receiver`. Though in this case, we are directly calling the rule from the `loki.source.kafka` component. So `forward_to` is being used as a placeholder as it is required by the `loki.relabel` component.
- `rule`: The relabeling rule to apply to the incoming logs. In this case, we are renaming the `__meta_kafka_topic` label to `topic`.

For more information on the `loki.relabel` configuration, see the [Loki Relabel documentation](https://grafana.com/docs/alloy/latest/reference/components/loki.relabel/).

### Write logs to Loki

Lastly, we will configure the Loki write component. `loki.write` receives log entries from other loki components and sends them over the network using the Loki logproto format.

And finally, add the following configuration to the `config.alloy` file:

```alloy
loki.write "http" {
  endpoint {
    url = "http://loki:3100/loki/api/v1/push"
  }
}
```

In this configuration:

- `endpoint`: The endpoint to send the logs to. In this case, we are sending the logs to the Loki HTTP endpoint.

For more information on the `loki.write` configuration, see the [Loki Write documentation](https://grafana.com/docs/alloy/latest/reference/components/loki.write/).

### Reload the Alloy configuration to check the changes

Once added, save the file. Then run the following command to request Alloy to reload the configuration:

<!-- INTERACTIVE exec START -->
```bash
curl -X POST http://localhost:12345/-/reload
```
<!-- INTERACTIVE exec END -->

The new configuration will be loaded.  You can verify this by checking the Alloy UI: [http://localhost:12345](http://localhost:12345).

## Stuck? Need help?

If you get stuck or need help creating the configuration, you can copy and replace the entire `config.alloy` using the completed configuration file:

<!-- INTERACTIVE exec START -->
```bash
cp loki-fundamentals/completed/config-raw.alloy loki-fundamentals/config.alloy
curl -X POST http://localhost:12345/-/reload
```
<!-- INTERACTIVE exec END -->

<!-- INTERACTIVE page step2.md END -->

<!-- INTERACTIVE page step3.md START -->

## Step 3: Configure Alloy to ingest OpenTelemetry logs via Kafka

Next we will configure Alloy to also ingest OpenTelemetry logs via Kafka, we need to update the Alloy configuration file once again. We will add the new components to the `config.alloy` file along with the existing components.

### Open your code editor and locate the `config.alloy` file

Like before, we generate our next pipeline configuration within the same `config.alloy` file. You will add the following configuration snippets to the file **in addition** to the existing configuration. Essentially, we are configuring two pipelines within the same Alloy configuration file.

### Source OpenTelemetry logs from Kafka

First, we will configure the OpenTelemetry Kafka receiver. `otelcol.receiver.kafka` accepts telemetry data from a Kafka broker and forwards it to other `otelcol.*` components.

Now add the following configuration to the `config.alloy` file:

```alloy
otelcol.receiver.kafka "default" {
  brokers          = ["kafka:9092"]
  protocol_version = "2.0.0"
  topic           = "otlp"
  encoding        = "otlp_proto"

  output {
    logs    = [otelcol.processor.batch.default.input]
  }
}
```

In this configuration:

- `brokers`: The Kafka brokers to connect to.
- `protocol_version`: The Kafka protocol version to use.
- `topic`: The Kafka topic to consume. In this case, we are consuming the `otlp` topic.
- `encoding`: The encoding of the incoming logs. Which decodes messages as OTLP protobuf.
- `output`: The list of receivers to forward the logs to. In this case, we are forwarding the logs to the `otelcol.processor.batch.default.input`.

For more information on the `otelcol.receiver.kafka` configuration, see the [OpenTelemetry Receiver Kafka documentation](https://grafana.com/docs/alloy/latest/reference/components/otelcol.receiver.kafka/).

### Batch OpenTelemetry logs before sending

Next, we will configure a OpenTelemetry processor. `otelcol.processor.batch` accepts telemetry data from other otelcol components and places them into batches. Batching improves the compression of data and reduces the number of outgoing network requests required to transmit data. This processor supports both size and time based batching.

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

### Write OpenTelemetry logs to Loki

Lastly, we will configure the OpenTelemetry exporter. `otelcol.exporter.otlphttp` accepts telemetry data from other otelcol components and writes them over the network using the OTLP HTTP protocol. We will use this exporter to send the logs to the Loki native OTLP endpoint.

Finally, add the following configuration to the `config.alloy` file:

```alloy
otelcol.exporter.otlphttp "default" {
  client {
    endpoint = "http://loki:3100/otlp"
  }
}
```

In this configuration:

- `client`: The client configuration for the exporter. In this case, we are sending the logs to the Loki OTLP endpoint.

For more information on the `otelcol.exporter.otlphttp` configuration, see the [OpenTelemetry Exporter OTLP HTTP documentation](https://grafana.com/docs/alloy/latest/reference/components/otelcol.exporter.otlphttp/).

### Reload the Alloy configuration to check the changes

Once added, save the file. Then run the following command to request Alloy to reload the configuration:
<!-- INTERACTIVE exec START -->
```bash
curl -X POST http://localhost:12345/-/reload
```
<!-- INTERACTIVE exec END -->

The new configuration will be loaded. You can verify this by checking the Alloy UI: [http://localhost:12345](http://localhost:12345).

## Stuck? Need help (Full Configuration)?

If you get stuck or need help creating the configuration, you can copy and replace the entire `config.alloy`. This differs from the previous `Stuck? Need help` section as we are replacing the entire configuration file with the completed configuration file. Rather than just adding the first Loki Raw Pipeline configuration.

<!-- INTERACTIVE exec START -->
```bash
cp loki-fundamentals/completed/config.alloy loki-fundamentals/config.alloy
curl -X POST http://localhost:12345/-/reload
```
<!-- INTERACTIVE exec END -->

<!-- INTERACTIVE page step3.md END -->

<!-- INTERACTIVE page step4.md START -->

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

- Create a user
- Log in
- Create a few plants to monitor
- Enable bug mode to activate the bug service. This will cause services to fail and generate additional logs.

Finally to view the logs in Loki, navigate to the Loki Logs Explore view in Grafana at [http://localhost:3000/a/grafana-lokiexplore-app/explore](http://localhost:3000/a/grafana-lokiexplore-app/explore).

<!-- INTERACTIVE page step4.md END -->

<!-- INTERACTIVE page finish.md START -->

## Summary

In this example, we configured Alloy to ingest logs via Kafka. We configured Alloy to ingest logs in two different formats: raw logs and OpenTelemetry logs. Where to go next?

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