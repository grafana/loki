---
title: Sending logs to Loki using Fluent Bit tutorial
menuTitle: Fluent Bit tutorial
description: Sending logs to Loki using Fluent Bit using the official Fluent Bit Loki output plugin.
weight: 250
killercoda:
  title: Sending logs to Loki using Fluent Bit tutorial
  description: Sending logs to Loki using Fluent Bit using the official Fluent Bit Loki output plugin.
  preprocessing:
    substitutions:
      - regexp: loki-fundamentals-fluent-bit-1
        replacement: loki-fundamentals_fluent-bit_1
      - regexp: docker compose
        replacement: docker-compose
  backend:
    imageid: ubuntu
---

<!-- INTERACTIVE page intro.md START -->

# Sending logs to Loki using Fluent Bit tutorial

In this tutorial, you will learn how to send logs to Loki using Fluent Bit. Fluent Bit is a lightweight and fast log processor and forwarder that can collect, process, and deliver logs to various destinations. We will use the official Fluent Bit Loki output plugin to send logs to Loki.


<!-- INTERACTIVE ignore START -->

## Dependencies

Before you begin, ensure you have the following to run the demo:

- Docker
- Docker Compose

{{< admonition type="tip" >}}
Alternatively, you can try out this example in our interactive learning environment: [Sending logs to Loki using Fluent Bit tutorial](https://killercoda.com/grafana-labs/course/loki/fluentbit-loki-tutorial).

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

Each service has been instrumented with the Fluent Bit logging framework to generate logs. If you would like to learn more about how the Carnivorous Greenhouse application was instrumented with Fluent Bit, refer to the [Carnivorous Greenhouse repository](https://github.com/grafana/loki-fundamentals/blob/fluentbit-official/greenhouse/loggingfw.py).

<!-- INTERACTIVE page intro.md END -->

<!-- INTERACTIVE page step1.md START -->

## Step 1: Environment setup

In this step, we will set up our environment by cloning the repository that contains our demo application and spinning up our observability stack using Docker Compose.

1. To get started, clone the repository that contains our demo application:
   
    ```bash
    git clone -b fluentbit-official  https://github.com/grafana/loki-fundamentals.git
    ```

1.  Next we will spin up our observability stack using Docker Compose:

    ```bash
    docker compose -f loki-fundamentals/docker-compose.yml up -d
    ```

    This will spin up the following services:
    ```console
    ✔ Container loki-fundamentals-grafana-1       Started                                                        
    ✔ Container loki-fundamentals-loki-1          Started                        
    ✔ Container loki-fundamentals-fluent-bit-1    Started
    ```
Once we have finished configuring the Fluent Bit agent and sending logs to Loki, we will be able to view the logs in Grafana. To check if Grafana is up and running, navigate to the following URL: [http://localhost:3000](http://localhost:3000)
<!-- INTERACTIVE page step1.md END -->

<!-- INTERACTIVE page step2.md START -->

## Step 2: Configure Fluent Bit to send logs to Loki

To configure Fluent Bit to receive logs from our application, we need to provide a configuration file. This configuration file will define the components and their relationships. We will build the entire observability pipeline within this configuration file.

### Open your code editor and locate the `fluent-bit.conf` file

Fluent Bit requires a configuration file to define the components and their relationships. The configuration file is written using Fluent Bit configuration syntax. We will build the entire observability pipeline within this configuration file. To start, we will open the `fluent-bit.conf` file in the code editor:

{{< docs/ignore >}}
> Note: Killercoda has an inbuilt Code editor which can be accessed via the `Editor` tab.
1. Expand the `loki-fundamentals` directory in the file explorer of the `Editor` tab.
1. Locate the `fluent-bit.conf` file in the top level directory, `loki-fundamentals`.
1. Click on the `fluent-bit.conf` file to open it in the code editor.
{{< /docs/ignore >}}

<!-- INTERACTIVE ignore START -->
1. Open the `loki-fundamentals` directory in a code editor of your choice.
1. Locate the `fluent-bit.conf` file in the `loki-fundamentals` directory (Top level directory).
1. Click on the `fluent-bit.conf` file to open it in the code editor.
<!-- INTERACTIVE ignore END -->

You will copy all of the configuration snippets into the `fluent-bit.conf` file.

### Receiving Fluent Bit protocal logs

The first step is to configure Fluent Bit to receive logs from the Carnivorous Greenhouse application. Since the application is instrumented with Fluent Bit logging framework, it will send logs using the forward protocol (unique to Fluent Bit). We will use the `forward` input plugin to receive logs from the application.

Now add the following configuration to the `fluent-bit.conf` file:
```conf
[INPUT]
    Name              forward
    Listen            0.0.0.0
    Port              24224
```

In this configuration:
- `Name`: The name of the input plugin. In this case, we are using the `forward` input plugin.
- `Listen`: The IP address to listen on. In this case, we are listening on all IP addresses.
- `Port`: The port to listen on. In this case, we are listening on port `24224`.

For more information on the `forward` input plugin, see the [Fluent Bit Forward documentation](https://docs.fluentbit.io/manual/pipeline/inputs/forward).



### Export logs to Loki using the official Loki output plugin

Lastly, we will configure Fluent Bit to export logs to Loki using the official Loki output plugin. The Loki output plugin allows you to send logs or events to a Loki service. It supports data enrichment with Kubernetes labels, custom label keys, and structured metadata.

Add the following configuration to the `fluent-bit.conf` file:
```conf
[OUTPUT]
    name   loki
    match  service.**
    host   loki
    port   3100
    labels agent=fluent-bit
    label_map_path /fluent-bit/etc/conf/logmap.json
```

In this configuration:
- `name`: The name of the output plugin. In this case, we are using the `loki` output plugin.
- `match`: The tag to match. In this case, we are matching all logs with the tag `service.**`.
- `host`: The hostname of the Loki service. In this case, we are using the hostname `loki`.
- `port`: The port of the Loki service. In this case, we are using port `3100`.
- `labels`: Additional labels to add to the logs. In this case, we are adding the label `agent=fluent-bit`.
- `label_map_path`: The path to the label map file. In this case, we are using the file `logmap.json`.

For more information on the `loki` output plugin, see the [Fluent Bit Loki documentation](https://docs.fluentbit.io/manual/pipeline/outputs/loki).

#### `logmap.json` file

The `logmap.json` file is used to map the log fields to the Loki labels. In this tutorial we have pre-filled the `logmap.json` file with the following configuration:
```json
{
"service": "service_name",
"instance_id": "instance_id"
 }
```
This configuration maps the `service` field to the Loki label `service_name` and the `instance_id` field to the Loki label `instance_id`.


### Reload the Fluent Bit configuration

After adding the configuration to the `fluent-bit.conf` file, you will need to reload the Fluent Bit configuration. To reload the configuration, run the following command:

```bash
docker restart loki-fundamentals-fluent-bit-1
```
To verify that the configuration has been loaded successfully, you can check the Fluent Bit logs by running the following command:

```bash
docker logs loki-fundamentals-fluent-bit-1
```

## Stuck? Need help?

If you get stuck or need help creating the configuration, you can copy and replace the entire `config.alloy` using the completed configuration file:

```bash
cp loki-fundamentals/completed/fluent-bit.conf loki-fundamentals/fluent-bit.conf
docker restart loki-fundamentals-fluent-bit-1
```

<!-- INTERACTIVE page step2.md END -->

<!-- INTERACTIVE page step3.md START -->

## Step 3: Start the Carnivorous Greenhouse

In this step, we will start the Carnivorous Greenhouse application. To start the application, run the following command:
<!-- INTERACTIVE ignore START -->
{{< admonition type="note" >}}
This docker-compose file relies on the `loki-fundamentals_loki` Docker network. If you have not started the observability stack, you will need to start it first.
{{< /admonition >}}
<!-- INTERACTIVE ignore END -->

{{< docs/ignore >}}

> Note: This docker-compose file relies on the `loki-fundamentals_loki` docker network. If you have not started the observability stack, you will need to start it first.

{{< /docs/ignore >}}

```bash
docker compose -f loki-fundamentals/greenhouse/docker-compose-micro.yml up -d --build 
```

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

1. Create a user.
1. Log in.
1. Create a few plants to monitor.
1. Enable bug mode to activate the bug service. This will cause services to fail and generate additional logs.

Finally to view the logs in Loki, navigate to the Loki Logs Explore view in Grafana at [http://localhost:3000/a/grafana-lokiexplore-app/explore](http://localhost:3000/a/grafana-lokiexplore-app/explore).


<!-- INTERACTIVE page step3.md END -->

<!-- INTERACTIVE page finish.md START -->

## Summary

In this tutorial, you learned how to send logs to Loki using Fluent Bit. You configured Fluent Bit to receive logs from the Carnivorous Greenhouse application and export logs to Loki using the official Loki output plugin. Where to next?

{{< docs/ignore >}}

### Back to Docs
Head back to where you started from to continue with the [Loki documentation](https://grafana.com/docs/loki/latest/send-data/alloy).

{{< /docs/ignore >}}


## Further reading

For more information on Fluent Bit, refer to the following resources:
- [Fluent Bit documentation](https://docs.fluentbit.io/manual/)
- [Other examples of Fluent Bit configurations](https://grafana.com/docs/loki/latest/send-data/fluentbit/)

## Complete metrics, logs, traces, and profiling example

If you would like to use a demo that includes Mimir, Loki, Tempo, and Grafana, you can use [Introduction to Metrics, Logs, Traces, and Profiling in Grafana](https://github.com/grafana/intro-to-mlt). `Intro-to-mltp` provides a self-contained environment for learning about Mimir, Loki, Tempo, and Grafana.

The project includes detailed explanations of each component and annotated configurations for a single-instance deployment. Data from `intro-to-mltp` can also be pushed to Grafana Cloud.


<!-- INTERACTIVE page finish.md END -->
