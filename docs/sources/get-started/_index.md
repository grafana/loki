---
title: Get started
weight: 200
description: Overview of the steps for setting up Loki.
aliases:
    - ./getting-started
---

# Get started

Loki is a horizontally-scalable, highly-available, multi-tenant log aggregation system inspired by Prometheus. It is designed to be very cost effective and easy to operate. It does not index the contents of the logs, but rather a set of labels for each log stream.

To collect logs and view your log data generally involves the following steps:

![Loki installation steps](loki-install.png)

1. Install Loki on Kubernetes in simple scalable mode, using the recommended [Helm chart](docs/setup/install/helm/install-scalable/). Supply the Helm chart with your object storage authentication details.
   - [Storage options](https://grafana.com/docs/loki/<LOKI-VERSION>/next/operations/storage/)
   - [Configuration reference](https://grafana.com/docs/loki/<LOKI-VERSION>/next/configure/ )

    For configuration examples for your Object Storage provider, see the [examples](https://grafana.com/docs/loki/<LOKI-VERSION>/next/configure/examples/).

1. Deploy the Grafana Agent to collect logs from your applications.
   
    1. On Kubernetes, deploy the Grafana Agent using the Helm chart. Configure Grafana Agent to scrape logs from your Kubernetes cluster, and add your Loki endpoint details. See the section below for an example Grafana Agent Flow configuration file.
    1. Add labels to your logs following our best practices. Most Loki users start by adding labels which describe where the logs are coming from (region, cluster, environment, etc.) 

    For more information, see the [labels documentation](https://grafana.com/docs/loki/<LOKI-VERSION>/next/get-started/labels/) and [best practices](https://grafana.com/docs/loki/<LOKI-VERSION>/latest/get-started/labels/bp-labels/) 

1. [Deploy Grafana](https://grafana.com/docs/grafana/<GRAFANA-VERSION>/setup-grafana/) or [Grafana Cloud](https://grafana.com/docs/grafana-cloud/quickstart/) and configure a [Loki datasource](https://grafana.com/docs/grafana/<GRAFANA-VERSION>/datasources/loki/configure-loki-data-source/). 

1. Select the [Explore feature](https://grafana.com/docs/grafana/<GRAFANA-VERSION>/explore/) in the Grafana main menu. You can [view logs in Explore](https://grafana.com/docs/grafana/<GRAFANA-VERSION>/explore/logs-integration/).
  a. Pick a time range.
  b. Choose the Loki datasource.
  c. Use [LogQL](https://grafana.com/docs/loki/<LOKI-VERSION>/latest/query/) in the [query editor](https://grafana.com/docs/grafana/<GRAFANA-VERSION>/datasources/loki/query-editor/), use the Builder view to explore your labels, or kick start a query using the “Kick start your query” button. 

Next steps:
Learn about Loki’s query language, [LogQL](https://grafana.com/docs/loki/<LOKI-VERSION>/latest/query/).


## Example Grafana Agent configuration file 

To deploy Grafana Agent to collect Pod logs from your Kubernetes cluster and ship them to Loki, you can use the Grafana Agent Helm chart, and a values.yaml file similar to the following:

1. Install Loki with the [Helm chart](https://grafana.com/docs/loki/<LOKI-VERSION>/latest/setup/install/helm/install-scalable/ ).
1. Deploy the Grafana Agent, using Grafana Agent Helm chart and this [Sample Agent values.yaml file](https://gist.github.com/monodot/42d2d6e672244bf261fe63a3da097030) updating the value for ` forward_to = [loki.write.endpoint.receiver]`.
1. Then install Grafana Agent in your Kubernetes cluster using:

 ```bash
helm upgrade -f values.yaml agent grafana/grafana-agent 
 ```

  This will:
   - Install Grafana Agent to discover Pod logs.
   - Add `container` and `pod` labels to the logs.
   - Push the logs to your Loki cluster using the tenant ID `cloud`.
