---
title: Visualize log data
menuTitle: Visualize
description: Describes the different ways that you can use Grafana to visualize your log data.
aliases:
   - ../getting-started/grafana/
   - ../operations/grafana/
weight: 725
keywords:
   - visualize
   - grafana
   - dashboards
---

# Visualize log data

Grafana Loki does not have its own user interface. Most users [install Grafana](https://grafana.com/docs/grafana/latest/setup-grafana/installation/) in order to visualize their log data. Grafana versions after 6.3 have built-in support for Grafana Loki and [LogQL](https://grafana.com/docs/loki/<LOKI_VERSION>/query/).

There are several different options for how to visualize your log data in Grafana:

- [Logs Drilldown](https://grafana.com/docs/grafana-cloud/visualizations/simplified-exploration/logs/) lets you explore logs from your Loki data source without writing LogQL queries.
- [Grafana Explore](https://grafana.com/docs/grafana/latest/explore/logs-integration/) helps you build and iterate on queries written in LogQL. Once you have a query that finds the data you're looking for, you can consider using your query in a Grafana dashboard.
- [Loki Mixins](https://grafana.com/docs/loki/latest/operations/observability/#mixins) include a pre-built set of dashboards, recording rules, and alerts for monitoring Loki.
- [Grafana Dashboards](https://grafana.com/docs/grafana/latest/dashboards/) let you query, transform, visualize, and understand your log data. You can create your own custom dashboards, or import and modify public dashboards shared by the community.

## Logs Drilldown

Grafana Logs Drilldown lets you automatically visualize and explore logs. Logs Drilldown makes assumptions about what data you might want to see to help you quickly get started viewing your logs without having to learn LogQL and write queries.

- If you are a Grafana Cloud user, you can access Logs Drilldown from the Grafana Cloud main navigation menu: select **Explore** > **Logs**.

- If you are an open source or Grafana Enterprise Logs user, you can install the [Logs Drilldown plugin](https://grafana.com/docs/grafana-cloud/visualizations/simplified-exploration/logs/access/). For more information, refer to the [Logs Drilldown documentation](https://grafana.com/docs/grafana-cloud/visualizations/simplified-exploration/logs/).

{{< admonition type="note" >}}
Starting with Grafana v11.3, the plugin for the Logs Drilldown app is installed in Grafana by default.
{{< /admonition >}}

## Grafana Explore

[Grafana Explore](https://grafana.com/docs/grafana/latest/explore/) helps you build and iterate on a LogQL query outside of the dashboard user interface. If you just want to explore your data and do not want to create a dashboard, then Explore makes this much easier.

1. Log into your Grafana instance. If this is your first time running Grafana, the username and password are both defaulted to `admin`.
1. In the Grafana main menu, select **Connections** > **Data source**.
1. Click the **+ Add new data source** button.
1. Search for, or choose Loki from the list.
1. On the **Settings** tab, the **URL** field should be the address of your Loki server.
For example, when running locally or with Docker using port mapping, the address is likely `http://localhost:3100`.
When running with docker-compose or Kubernetes, the address is likely `http://loki:3100`.
When running Grafana (with Docker) and trying to connect to a locally built Loki instance, the address (for the URL field) is:
   On Mac: `docker.for.mac.localhost`
   On Windows: `docker.for.win.localhost`
1. If your Loki server has [multi-tenancy](https://grafana.com/docs/loki/latest/operations/multi-tenancy/) enabled, then you must provide your tenant ID in the `X-Scope-OrgID` header. Click the **+ Add header** button under **HTTP headers**, enter `X-Scope-OrgID` in the **Header** field, and your tenant ID in the **Value** field. Multi-tenancy is enabled by default when running Loki with Helm on Kubernetes.
1. To view your logs, click **Explore** in the main menu.
1. Select the Loki datasource in the top-left menu.
1. You can click **Kick start your query** to select from a list of common queries, or use the **Label filters** to start choosing labels that you want to query. For more information about the Loki query language, refer to the [LogQL section](https://grafana.com/docs/loki/<LOKI_VERSION>/query/).

If you would like to see an example of this live, you can try [Grafana Play's Explore feature](https://play.grafana.org/explore?schemaVersion=1&panes=%7B%22v1d%22:%7B%22datasource%22:%22ac4000ca-1959-45f5-aa45-2bd0898f7026%22,%22queries%22:%5B%7B%22refId%22:%22A%22,%22expr%22:%22%7Bagent%3D%5C%22promtail%5C%22%7D%20%7C%3D%20%60%60%22,%22queryType%22:%22range%22,%22datasource%22:%7B%22type%22:%22loki%22,%22uid%22:%22ac4000ca-1959-45f5-aa45-2bd0898f7026%22%7D,%22editorMode%22:%22builder%22%7D%5D,%22range%22:%7B%22from%22:%22now-1h%22,%22to%22:%22now%22%7D%7D%7D&orgId=1).

Learn more about the Grafana Explore feature in the [Grafana documentation](https://grafana.com/docs/grafana/latest/explore/logs-integration/).

## Loki mixins

The Loki mixin provides a set of Grafana dashboards, Prometheus recording rules and alerts for monitoring Loki itself. For instructions on how to install the Loki mixins, refer to the [installation topic](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/meta-monitoring/mixins/).

## Using Grafana dashboards

{{< docs/play title="Loki Example Grafana Dashboard" url="https://play.grafana.org/d/T512JVH7z/" >}}

Because Loki can be used as a built-in data source, you can use LogQL queries based on that data source to build complex visualizations that persist on Grafana dashboards.

To configure Loki as a data source via provisioning, refer to the documentation for [Loki data source](https://grafana.com/docs/grafana/latest/datasources/loki/#configure-the-datasource-with-provisioning).

Read more about how to build Grafana Dashboards in [build your first dashboard](https://grafana.com/docs/grafana/latest/getting-started/build-first-dashboard/).
