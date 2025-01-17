---
title: Install Loki mixins
menuTitle: Install mixins
description:  Describes the Loki mixins, how to configure and install the dashboards, alerts, and recording rules.
weight: 100
---

# Install Loki mixins

Loki is instrumented to expose metrics about itself via the `/metrics` endpoint, designed to be scraped by Prometheus. Each Loki release includes a mixin. The Loki mixin provides a set of Grafana dashboards, Prometheus recording rules and alerts for monitoring Loki.

To set up monitoring using the mixin, you need to:

- Deploy an instance of Prometheus (or a Prometheus-compatible time series database, like [Mimir](https://grafana.com/docs/mimir/latest/)) which can store Loki metrics.
- Deploy an agent, such as Grafana Alloy, or Grafana Agent, to scrape Loki metrics.
- Set up Grafana to visualize Loki metrics, by installing the dashboards.
- Install the recording rules and alerts into Prometheus using `mimirtool`.

This procedure assumes that you have set up Loki using the Helm chart.

{{< admonition type="note" >}}
Be sure to update the commands and configuration to match your own deployment.
{{< /admonition >}}

## Before you begin

To make full use of the Loki mixin, you’ll need the following running in your environment:

- Loki instance - A Loki instance which you want to monitor.
- Grafana - For visualizing logs and metrics ([install on Kubernetes](https://grafana.com/docs/grafana/latest/setup-grafana/installation/kubernetes/#deploy-grafana-oss-on-kubernetes)).
- Prometheus or Mimir - An instance of Prometheus or Mimir which will store metrics from Loki.

To scrape metrics from Loki, you can use Grafana Alloy or the OpenTelemetry Collector. This procedure provides examples only for Grafana Alloy.

If you have installed Loki using a Helm Chart, this documentation assumes that the Loki and Grafana instances are located on the same Kubernetes cluster.

## Configure Alloy to scrape Loki metrics

Loki exposes Prometheus metrics from all of its components to allow meta-monitoring. To retrieve these metrics, you need to configure a suitable scraper. Grafana Alloy can collect metrics and act as a Prometheus scraper. To use this capability, you need to configure Alloy to scrape from all of the components.

{{< admonition type="tip" >}}
If you're running on Kubernetes, you can use the Kubernetes Monitoring Helm chart.
{{< /admonition >}}

To scrape metrics from Loki, follow these steps:

Install Grafana Alloy using the provided instructions for your platform.

- [Standalone](https://grafana.com/docs/alloy/latest/get-started/install/binary/)
- [Kubernetes](https://grafana.com/docs/alloy/latest/get-started/install/kubernetes/)
- [Docker](https://grafana.com/docs/alloy/latest/get-started/install/docker/)

Add a configuration block to scrape metrics from your Loki component instances and forward to a Prometheus or Mimir instance.

- On Kubernetes, you can use the Alloy `discovery.kubernetes` component to discover Loki Pods to scrape metrics from.
- On non-Kubernetes deployments, you may use `prometheus.scrape` and an explicit list of targets to discover Loki instances to scrape.

For an example, see [Collect and forward Prometheus metrics](https://grafana.com/docs/alloy/latest/tasks/collect-prometheus-metrics/).

## Configure Grafana

In your Grafana instance, you'll need to [create a Prometheus data source](https://grafana.com/docs/grafana/latest/datasources/prometheus/configure-prometheus-data-source/) to visualize the metrics scraped from your Loki cluster.

## Install Loki dashboards in Grafana

After Loki metrics are scraped by Grafana Alloy and stored in a Prometheus compatible time-series database, you can monitor Loki’s operation using the Loki mixin.

Each Loki release includes a mixin that includes:

- Relevant dashboards for overseeing the health of Loki as a whole, as well as its individual Loki components
- [Recording rules](https://grafana.com/docs/loki/latest/alert/#recording-rules) that compute metrics that are used in the dashboards
- Alerts that trigger when Loki generates metrics that are outside of normal parameters

To install the mixins in Grafana and Mimir, the general steps are as follows:

1. Download the mixin dashboards from the Loki repository.

1. Import the dashboards in your Grafana instance.

1. Upload `alerts.yaml` and `rules.yaml` files to Prometheus or Mimir with `mimirtool`.

### Download the `loki-mixin` dashboards

1. First, clone the Loki repository from Github:

   ```bash
   git clone https://github.com/grafana/loki
   cd loki
   ```

1. Once you have a local copy of the repository, navigate to the `production/loki-mixin-compiled-ssd` directory.

   ```bash
   cd production/loki-mixin-compiled-ssd
   ```

   OR, if you're deploying Loki in microservices mode:

   ```bash
   cd production/loki-mixin-compiled
   ```

This directory contains a compiled version of the alert and recording rules, as well as the dashboards.

{{< admonition type="note" >}}
If you want to change any of the mixins, make your updates in the `production/loki-mixin` directory.
Use the instructions in the [README](https://github.com/grafana/loki/tree/main/production/loki-mixin) in that directory to regenerate the files.
{{< /admonition >}}

### Import the dashboards to Grafana

The `dashboards` directory includes the monitoring dashboards that can be installed into your Grafana instance.
Refer to [Import a dashboard](https://grafana.com/docs/grafana/latest/dashboards/build-dashboards/import-dashboards/) in the Grafana documentation.

{{< admonition type="tip" >}}
Install all dashboards.
You can only import one dashboard at a time.
Create a new folder in the Dashboards area, for example “Loki Monitoring”, as an easy location to save the imported dashboards.
{{< /admonition >}}

To create a folder:

1. Open your Grafana instance and select **Dashboards**.
1. Click the **New** button.
1. Select **New folder** from the **New** menu.
1. Name your folder, for example, “Loki Monitoring”.
1. Click **Create**.

To import a dashboard:

1. Open your Grafana instance and select **Dashboards**.
1. Click the **New** button.
1. Select **Import** from the **New** menu.
1. On the **Import dashboard** screen, select **Upload dashboard JSON file.**
1. Browse to `production/loki-mixin-compiled-ssd/dashboards` and select the dashboard to import. Or, drag the dashboard file, for example, `loki-operational.json`, onto the **Upload** area of the **Import dashboard** screen.
1. Select a folder in the **Folder** menu where you want to save the imported dashboard. For example, select "Loki Monitoring" created in the earlier steps.
1. Click **Import**.

The imported files are listed in the Loki Monitoring dashboard folder.

To view the dashboards in Grafana:

1. Select **Dashboards** in your Grafana instance.
1. Select **Loki Monitoring**, or the folder where you uploaded the imported dashboards.
1. Select any file in the folder to view the dashboard.

### Add alerts and recording rules to Prometheus or Mimir

The rules and alerts need to be installed into a Prometheus instance, Mimir or a Grafana Enterprise Metrics cluster.

You can find the YAML files for alerts and rules in the following directories in the Loki repo:

For SSD mode:
`production/loki-mixin-compiled-ssd/alerts.yaml`
`production/loki-mixin-compiled-ssd/rules.yaml`

For microservices mode:
`production/loki-mixin-compiled/alerts.yaml`
`production/loki-mixin-compiled/rules.yaml`

You use `mimirtool` to load the mixin alerts and rules definitions into a Prometheus instance, Mimir or a Grafana Enterprise Metrics cluster.

1. Download [mimirtool](https://github.com/grafana/mimir/releases).

1. Using the details of a Prometheus instance or Mimir cluster, run the following command to load the recording rules:

    ```bash
    mimirtool rules load --address=http://prometheus:9090 rules.yaml
    ```

    Or, for example if your Mimir cluster requires an API key, as is the case with Grafana Enterprise Metrics:

    ```bash
    mimirtool rules load --id=<tenant-id> --address=http://<mimir-hostname>:<port> --key="<mimir-api key>" rules.yaml
    ```

1. To load alerts:

    ```bash
    mimirtool alertmanager load --address=http://prometheus:9090 alerts.yaml
    ```

    or

    ```bash
    mimirtool alertmanager load --id=<tenant-id> --address=http://<mimir-hostname>:<port> --key="<mimir-api key>" alerts.yaml
    ```

Refer to the [mimirtool](https://grafana.com/docs/mimir/latest/manage/tools/mimirtool/) documentation for more information.
