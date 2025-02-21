---
title: Quickstart to run Loki locally
menuTitle: Loki quickstart
weight: 200
description: How to deploy Loki locally using Docker Compose.
killercoda:
  comment: |
    The killercoda front matter and the HTML comments that start '<!-- INTERACTIVE ' are used by a transformation tool that converts this Markdown source into a Killercoda tutorial.

    You can find the tutorial in https://github.com/grafana/killercoda/tree/staging/loki/loki-quickstart.

    Changes to this source file affect the Killercoda tutorial.

    For more information about the transformation tool, refer to https://github.com/grafana/killercoda/blob/staging/docs/transformer.md.
  title: Loki Quickstart Demo
  description: This sandbox provides an online enviroment for testing the Loki quickstart demo.
  details:
    intro:
      foreground: setup.sh
  backend:
    imageid: ubuntu
---

# Quickstart to run Loki locally

Grafana Loki by default runs in [monolithic](https://grafana.com/docs/loki/<LOKI_VERSION>/get-started/deployment-modes/#monolithic-mode) mode, which means all components run in a single binary. In this quickstart guide you will deploy the basic Loki stack for collecting logs:

* **Loki**: A log aggregation system to store the collected logs. For more information on what Loki is, see [Loki overview.](https://grafana.com/docs/loki/<LOKI_VERSION>/get-started/overview/).
* **Alloy**: Grafana Alloy is an open source telemtry collector for metrics, logs, traces and continuous profiles. In this quickstart guide Grafana Alloy has been configured to tail logs from all docker containers and forward them to Loki.
* **Grafana**: Grafana is an open-source platform for monitoring and observability. Grafana will be used to query, vizualize and alert on the logs stored in Loki.

<!-- INTERACTIVE ignore START -->
## Before you begin

Before you start, you need to have the following installed on your local system:
- Install [Docker](https://docs.docker.com/install)
- Install [Docker Compose](https://docs.docker.com/compose/install)

{{< admonition type="tip" >}}
Alternatively, you can try out this example in our interactive learning environment: [Loki Quickstart Sandbox](https://killercoda.com/grafana-labs/course/loki/loki-quickstart).

It's a fully configured environment with all the dependencies already installed.

![Interactive](/media/docs/loki/loki-ile.svg)

Provide feedback, report bugs, and raise issues in the [Grafana Killercoda repository](https://github.com/grafana/killercoda).
{{< /admonition >}}
<!-- INTERACTIVE ignore END -->

## Install the Loki stack

{{< admonition type="note" >}}
This quickstart assumes you are running Linux or MacOS. Windows users can follow the same steps using [WSL](https://learn.microsoft.com/en-us/windows/wsl/install).
{{< /admonition >}}

**To install Loki locally, follow these steps:**

1. Clone the Loki fundamentals repository and checkout the getting-started branch:

     ```bash
     git checkout https://github.com/grafana/loki-fundamentals.git -b getting-started
     ```

1. Change to the `loki-fundamentals` directory:

     ```bash
     cd loki-fundamentals
     ```

1. With `loki-fundamentals` as the current working directory deploy Loki, Alloy and Grafana using Docker Compose:

     ```bash
     docker compose up -d
     ```
      At the end of the command, you should see something similar to the following:

      ```console
       ✔ Container loki-fundamentals-grafana-1  Started  0.3s 
       ✔ Container loki-fundamentals-loki-1     Started  0.3s 
       ✔ Container loki-fundamentals-alloy-1    Started  0.4s
      ```

With the Loki stack running, you can now verify component is up and running:

* **Alloy**: Open a browser and navigate to [http://localhost:12345/graph](http://localhost:12345/graph). You should see the Alloy UI.
* **Grafana**: Open a browser and navigate to [http://localhost:3000](http://localhost:3000). You should see the Grafana home page.
* **Loki**: Open a browser and navigate to [http://localhost:3100/metrics](http://localhost:3100/metrics). You should see the Loki metrics page.

Since Grafana Alloy is configured to tail logs from all docker containers, Loki should already be receiving logs. The best place to verify this is using the Grafana Drilldown Logs feature. To do navigate to [http://localhost:3000/a/grafana-lokiexplore-app](http://localhost:3000/a/grafana-lokiexplore-app). You should see the Grafana Logs Drilldown page.

{{< figure max-width="100%" src="/media/docs/loki/get-started-drill-down.png" caption="Getting started sample application" alt="Grafana Drilldown" >}}

If you have only the getting started demo deployed in your docker environment, you should see three containers and their logs; `loki-fundamentals-alloy-1`, `loki-fundamentals-grafana-1` and `loki-fundamentals-loki-1`. Click **Show Logs** within the `loki-fundamentals-loki-1` container to drill down into the logs for that container.

{{< figure max-width="100%" src="/media/docs/loki/get-started-drill-down-container.png" caption="Getting started sample application" alt="Grafana Drilldown Service View" >}}

We will not cover the rest of the Grafana Drilldown Log features in this quickstart guide. For more information on how to use the Grafana Drilldown Logs feature, see [Drilldown Logs](https://grafana.com/docs/grafana/latest/explore/simplified-exploration/logs/get-started/).

## Collect Logs from a sample application

Currently, the Loki stack is collecting logs about itself. To provide a more realistic example, you can deploy a sample application that generates logs. The sample application is called **The Carnivourous Greenhouse**, a microservices application that allows users to login and simulate a greenhouse with carnivorous plants to monitor. The application consists of seven services:
- **User Service:** Manages user data and authentication for the application. Such as creating users and logging in.
- **Plant Service:** Manages the creation of new plants and updates other services when a new plant is created.
- **Simulation Service:** Generates sensor data for each plant.
- **Websocket Service:** Manages the websocket connections for the application.
- **Bug Service:** A service that when enabled, randomly causes services to fail and generate additional logs.
- **Main App:** The main application that ties all the services together.
- **Database:** A database that stores user and plant data.

The architecture of the application is shown below:

{{< figure max-width="100%" src="/media/docs/loki/get-started-architecture.png" caption="Getting started sample application" alt="Getting started sample application" >}}

To deploy the sample application, follow these steps:

1. With `loki-fundamentals` as the current working directory, deploy the sample application using Docker Compose:

     ```bash
     docker compose -f greenhouse/docker-compose-micro.yml up -d --build  
     ```
     {{< admonition type="note" >}}
     This may take a few minutes to complete since the images for the sample application need to be built. Go grab a coffee and come back.
     {{< /admonition >}}

     Once the command completes, you should see something similar to the following:

     ```console
       ✔ bug_service                                Built     0.0s 
       ✔ main_app                                   Built     0.0s 
       ✔ plant_service                              Built     0.0s 
       ✔ simulation_service                         Built     0.0s 
       ✔ user_service                               Built     0.0s 
       ✔ websocket_service                          Built     0.0s 
       ✔ Container greenhouse-websocket_service-1   Started   0.7s 
       ✔ Container greenhouse-db-1                  Started   0.7s 
       ✔ Container greenhouse-user_service-1        Started   0.8s 
       ✔ Container greenhouse-bug_service-1         Started   0.8s 
       ✔ Container greenhouse-plant_service-1       Started   0.8s 
       ✔ Container greenhouse-simulation_service-1  Started   0.7s 
       ✔ Container greenhouse-main_app-1            Started   0.7s
     ```

1. To verify the sample application is running, open a browser and navigate to [http://localhost:5005](http://localhost:5005). You should see the login page for the Carnivorous Greenhouse application.

   {{< figure max-width="100%" src="/media/docs/loki/get-started-login.png" caption="Getting started sample application" alt="Getting started sample application" >}}

   Now that the sample application is running, run some actions in the application to generate logs. Here is a list of actions:
   - **Create a user:** Click **Sign Up** and create a new user. Add a username and password and click **Sign Up**.
   - **Login:** Use the username and password you created to login. Add the username and password and click **Login**.
   - **Create a plant:** Once logged in, give your plant a name, select a plant type and click **Add Plant**. Do this a few times if you like.

  Your greenhouse should look something like this:

  {{< figure max-width="100%" src="/media/docs/loki/get-started-greenhouse.png" caption="Greenhouse Dashboard" alt="Greenhouse Dashboard" >}} 

  Now that you have generated some logs, you can view them in Grafana. To do this, navigate to [http://localhost:3000/a/grafana-lokiexplore-app](http://localhost:3000/a/grafana-lokiexplore-app). You should see the Grafana Logs Drilldown page.

## Querying Logs

At this point, you have viewed logs using the Grafana Drilldown Logs feature. In many cases this will provide you with all the information you need. However, we can also manually query Loki to ask more advanced questions about the logs. This can be done via the **Grafana Explore**.

1. Open a browser and navigate to [http://localhost:3000](http://localhost:3000) to open Grafana.

1. From the Grafana main menu, click the **Explore** icon (1) to open the Explore tab.

   To learn more about Explore, refer to the [Explore](https://grafana.com/docs/grafana/latest/explore/) documentation.

1. From the menu in the dashboard header, select the Loki data source (2).

   This displays the Loki query editor.

   In the query editor you use the Loki query language, [LogQL](https://grafana.com/docs/loki/<LOKI_VERSION>/query/), to query your logs.
   To learn more about the query editor, refer to the [query editor documentation](https://grafana.com/docs/grafana/latest/datasources/loki/query-editor/).

1. The Loki query editor has two modes (3):

   - [Builder mode](https://grafana.com/docs/grafana/latest/datasources/loki/query-editor/#builder-mode), which provides a visual query designer.
   - [Code mode](https://grafana.com/docs/grafana/latest/datasources/loki/query-editor/#code-mode), which provides a feature-rich editor for writing LogQL queries.

   Next we’ll walk through a few simple queries using both the builder and code views.

1. Click **Code** (3) to work in Code mode in the query editor.

   Here are some sample queries to get you started using LogQL. After copying any of these queries into the query editor, click **Run Query** (4) to execute the query.

   1. View all the log lines which have the container label `evaluate-loki-flog-1`:
      <!-- INTERACTIVE copy START -->
      ```bash
      {container="greenhouse-main_app-1"}
      ```
      <!-- INTERACTIVE copy END -->
      In Loki, this is a log stream.

      Loki uses [labels](https://grafana.com/docs/loki/<LOKI_VERSION>/get-started/labels/) as metadata to describe log streams.

      Loki queries always start with a label selector.
      In the previous query, the label selector is `{container="greenhouse-main_app-1"}`.

      <!-- INTERACTIVE copy END -->
   2. Find all the log lines in the `{container="greenhouse-main_app-1"}` stream that contain the string `POST`:
      <!-- INTERACTIVE copy START -->
      ```bash
      {container="greenhouse-main_app-1"} |= `POST`
      ```
      <!-- INTERACTIVE copy END -->

### Extracting Attributes from Logs

Loki by design does not force log lines into a specific schema format. Whether you are using JSON, key-value pairs, or plain text, Logfmt, or any other format, Loki ingests these logs lines as a stream of characters. The sample application we are using stores logs in [Logfmt](https://brandur.org/logfmt) format:
<!-- INTERACTIVE copy START -->
```bash
ts=2025-02-21 16:09:42,176 level=INFO line=97 msg="192.168.65.1 - - [21/Feb/2025 16:09:42] "GET /static/style.css HTTP/1.1" 304 -"
```
<!-- INTERACTIVE copy END -->
When querying Loki, you can pipe the result of the label selector through a formatter. This extracts attributes from the log line for further processing. For example lets pipe `{container="greenhouse-main_app-1"}` through the `logfmt` formatter to extract the `level` and `line` attributes:
<!-- INTERACTIVE copy START -->
```bash
{container="greenhouse-main_app-1"} | logfmt
```
<!-- INTERACTIVE copy END -->
When you now expand a log line in the query result, you will see the extracted attributes.

**Before we move on** to the next section, let's generate some error logs. To do this, enable the bug service in the sample application. This is done by setting the `Toggle Error Mode` to `On` in the Carnivorous Greenhouse application. This will cause the bug service to randomly cause services to fail.

### Advanced and Metrics Queries

Now that are sample application is failing, we can query Loki to find the error logs. Lets start by parsing the logs to extract the `level` attribute and then filter for logs with a `level` of `ERROR`:
<!-- INTERACTIVE copy START -->
```bash
{container="greenhouse-plant_service-1"} | logfmt | level="ERROR"
```
<!-- INTERACTIVE copy END -->
This query will return all the logs from the `greenhouse-plant_service-1` container that have a `level` attribute of `ERROR`. You can further refine this query by filtering for a specific code line:
<!-- INTERACTIVE copy START -->
```bash
{container="greenhouse-plant_service-1"} | logfmt | level="ERROR", line="58"
```
<!-- INTERACTIVE copy END -->
This query will return all the logs from the `greenhouse-plant_service-1` container that have a `level` attribute of `ERROR` and a `line` attribute of `58`.

LogQL also supports metrics queries. Metrics are useful for abstracting the raw log data into a more manageable form. For example, you can use metrics to count the number of logs per second that have a specific attribute:
<!-- INTERACTIVE copy START -->
```bash
sum(rate({container="greenhouse-plant_service-1"} | logfmt | level=`ERROR` [$__auto]))
```
<!-- INTERACTIVE copy END -->

Another example is to get the top 10 services producing the highest rate of errors:
<!-- INTERACTIVE copy START -->
```bash
topk(10,sum(rate({level="error"} | logfmt [5m])) by (service_name))
```
{{< admonition type="note" >}}
`service_name` is a label created by Loki when no service name is provided in the log line. It will use the container name as the service name. A list of all labels can be found in [Labels](https://grafana.com/docs/loki/latest/get-started/labels/#default-labels-for-all-users).
{{< /admonition >}}

## Alerting on Logs
