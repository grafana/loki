---
title: Quickstart to run Loki locally
menuTitle: Loki quickstart
weight: 200
description: How to create and use a simple local Loki cluster for testing and evaluation purposes.
---

# Quickstart to run Loki locally

If you want to experiment with Loki, you can run Loki locally using the Docker Compose file that ships with Loki. It runs Loki in a [monolithic deployment](https://grafana.com/docs/loki/<LOKI_VERSION>/get-started/deployment-modes/#monolithic-mode) mode and includes a sample application to generate logs.

The Docker Compose configuration instantiates the following components, each in its own container:

- **flog** a sample application which generates log lines.  [flog](https://github.com/mingrammer/flog) is a log generator for common log formats.
- **Grafana Alloy** which scrapes the log lines from flog, and pushes them to Loki through the gateway.
- **Gateway** (NGINX) which receives requests and redirects them to the appropriate container based on the request's URL.
- One Loki **read** component (Query Frontend, Querier).
- One Loki **write** component (Distributor, Ingester).
- One Loki **backend** component (Index Gateway, Compactor, Ruler, Bloom Compactor (Experimental), Bloom Gateway (Experimental)).
- **Minio** an S3-compatible object store which Loki uses to store its index and chunks.
- **Grafana** which provides visualization of the log lines captured within Loki.

{{< figure max-width="75%" src="/media/docs/loki/get-started-flog-v3.png" caption="Getting started sample application" alt="Getting started sample application">}}

## Interactive Learning Environment

{{< admonition type="note" >}}
The Interactive Learning Environment is currently in trial. Please provide feedback, report bugs, and raise issues in the [Grafana Killercoda Repository](https://github.com/grafana/killercoda).
{{< /admonition >}}

Try out this demo within our interactive learning environment: [Loki Quickstart Sandbox](https://killercoda.com/grafana-labs/course/loki/loki-quickstart) 

- A free Killercoda account is required to verify you are not a bot.
- Tutorial instructions are located on the left-hand side of the screen. Click to move on to the next section.
- All commands run inside the interactive terminal. Grafana can also be accessed via the URL links provided within the sandbox.  

## Installing Loki and collecting sample logs

Prerequisites

- [Docker](https://docs.docker.com/install)
- [Docker Compose](https://docs.docker.com/compose/install)

{{% admonition type="note" %}}
This quickstart assumes you are running Linux.
{{% /admonition %}}

**To install Loki locally, follow these steps:**

1. Create a directory called `evaluate-loki` for the demo environment. Make `evaluate-loki` your current working directory:

    ```bash
    mkdir evaluate-loki
    cd evaluate-loki
    ```

1. Download `loki-config.yaml`, `alloy-local-config.yaml`, and `docker-compose.yaml`:

    ```bash
    wget https://raw.githubusercontent.com/grafana/loki/main/examples/getting-started/loki-config.yaml -O loki-config.yaml
    wget https://raw.githubusercontent.com/grafana/loki/main/examples/getting-started/alloy-local-config.yaml -O alloy-local-config.yaml
    wget https://raw.githubusercontent.com/grafana/loki/main/examples/getting-started/docker-compose.yaml -O docker-compose.yaml
    ```

1. Deploy the sample Docker image.

    With `evaluate-loki` as the current working directory, start the demo environment using `docker compose`:

    ```bash
    docker compose up -d
    ```

    You should see something similar to the following:

    ```bash
    ✔ Network evaluate-loki_loki          Created      0.1s 
    ✔ Container evaluate-loki-minio-1     Started      0.6s 
    ✔ Container evaluate-loki-flog-1      Started      0.6s 
    ✔ Container evaluate-loki-backend-1   Started      0.8s 
    ✔ Container evaluate-loki-write-1     Started      0.8s 
    ✔ Container evaluate-loki-read-1      Started      0.8s 
    ✔ Container evaluate-loki-gateway-1   Started      1.1s 
    ✔ Container evaluate-loki-grafana-1   Started      1.4s 
    ✔ Container evaluate-loki-alloy-1     Started      1.4s
    ```

1. (Optional) Verify that the Loki cluster is up and running.
    - The read component returns `ready` when you point a web browser at [http://localhost:3101/ready](http://localhost:3101/ready). The message `Query Frontend not ready: not ready: number of schedulers this worker is connected to is 0` will show prior to the read component being ready.
    - The write component returns `ready` when you point a web browser at [http://localhost:3102/ready](http://localhost:3102/ready). The message `Ingester not ready: waiting for 15s after being ready` will show prior to the write component being ready.
  
1. (Optional) Verify that Grafana Alloy is running.
    - Grafana Alloy's UI can be accessed at [http://localhost:12345](http://localhost:12345).  

## Viewing your logs in Grafana

Once you have collected logs, you will want to view them.  You can view your logs using the command line interface, [LogCLI](/docs/loki/<LOKI_VERSION>/query/logcli/), but the easiest way to view your logs is with Grafana.

1. Use Grafana to query the Loki data source.  

    The test environment includes [Grafana](https://grafana.com/docs/grafana/latest/), which you can use to query and observe the sample logs generated by the flog application.  You can access the Grafana cluster by navigating to [http://localhost:3000](http://localhost:3000).  The Grafana instance provided with this demo has a Loki [datasource](https://grafana.com/docs/grafana/latest/datasources/loki/) already configured.

   {{< figure src="/media/docs/loki/grafana-query-builder-v2.png" caption="Grafana Explore" alt="Grafana Explore">}}

1. From the Grafana main menu, click the **Explore** icon (1) to launch the Explore tab. To learn more about Explore, refer the [Explore](https://grafana.com/docs/grafana/latest/explore/) documentation.

1. From the menu in the dashboard header, select the Loki data source (2).  This displays the Loki query editor. In the query editor you use the Loki query language, [LogQL](https://grafana.com/docs/loki/<LOKI_VERSION>/query/), to query your logs.
    To learn more about the query editor, refer to the [query editor documentation](https://grafana.com/docs/grafana/latest/datasources/loki/query-editor/).

1. The Loki query editor has two modes (3):

   - [Builder mode](https://grafana.com/docs/grafana/latest/datasources/loki/query-editor/#builder-mode), which provides a visual query designer.
   - [Code mode](https://grafana.com/docs/grafana/latest/datasources/loki/query-editor/#code-mode), which provides a feature-rich editor for writing LogQL queries.

   Next we’ll walk through a few simple queries using both the builder and code views.

1. Click **Code** (3) to work in Code mode in the query editor.

    Here are some basic sample queries to get you started using LogQL.  Note that these queries assume that you followed the instructions to create a directory called `evaluate-loki`. If you installed in a different directory, you’ll need to modify these queries to match your installation directory.  After copying any of these queries into the query editor, click **Run Query** (4) to execute the query.

    1. View all the log lines which have the container label "flog":

        ```bash
        {container="evaluate-loki-flog-1"}
        ```

        In Loki, this is called a log stream. Loki uses [labels](https://grafana.com/docs/loki/<LOKI_VERSION>/get-started/labels/) as metadata to describe log streams.  Loki queries always start with a label selector.  In the query above, the label selector is `container`.

    1. To view all the log lines which have the container label "grafana":

        ```bash
        {container="evaluate-loki-grafana-1"}
        ```

    1. Find all the log lines in the container=flog stream that contain the string "status":

        ```bash
        {container="evaluate-loki-flog-1"} |= `status`
        ```

    1. Find all the log lines in the container=flog stream where the JSON field "status" is "404":

        ```bash
        {container="evaluate-loki-flog-1"} | json | status=`404`
        ```

    1. Calculate the number of logs per second where the JSON field "status" is "404":

        ```bash
        sum by(container) (rate({container="evaluate-loki-flog-1"} | json | status=`404` [$__auto]))        
        ```

    The final query above is a metric query which returns a time series. This will trigger Grafana to draw a graph of the results.  You can change the type of graph for a different view of the data.  Click **Bars** to view a bar graph of the data.

1. Click the **Builder** tab (3) to return to Builder mode in the query editor.
    1. In Builder view, click **Kick start your query**(5).
    1. Expand the **Log query starters** section.
    1. Select the first choice, **Parse log lines with logfmt parser**, by clicking **Use this query**.
    1. On the Explore tab, click **Label browser**, in the dialog select a container and click **Show logs**.

For a thorough introduction to LogQL, refer to the [LogQL reference](https://grafana.com/docs/loki/<LOKI_VERSION>/query/).

## Sample queries (code view)

Here are some more sample queries that you can run using the Flog sample data.

To see all the log lines that flog has generated, enter the LogQL query:

```bash
{container="evaluate-loki-flog-1"}|= ``
```

The flog app generates log lines for simulated HTTP requests.

To see all `GET` log lines, enter the LogQL query:

```bash
{container="evaluate-loki-flog-1"} |= "GET"
```

To see all `POST` methods, enter the LogQL query:

```bash
{container="evaluate-loki-flog-1"} |= "POST"
```

To see every log line with a 401 status (unauthorized error), enter the LogQL query:

```bash
{container="evaluate-loki-flog-1"} | json | status="401"
```

To see every log line that does not contain the value 401:

```bash
{container="evaluate-loki-flog-1"} != "401"
```

For more examples, refer to the [query documentation](https://grafana.com/docs/loki/<LOKI_VERSION>/query/query_examples/).

## Complete metrics, logs, traces, and profiling example

If you would like to use a demo that includes Mimir, Loki, Tempo, and Grafana, you can use [Introduction to Metrics, Logs, Traces, and Profiling in Grafana](https://github.com/grafana/intro-to-mlt). `Intro-to-mltp` provides a self-contained environment for learning about Mimir, Loki, Tempo, and Grafana.

The project includes detailed explanations of each component and annotated configurations for a single-instance deployment. Data from `intro-to-mltp` can also be pushed to Grafana Cloud.
