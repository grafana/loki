---
title: Visualize log data
menuTitle: Visualize
description: Visualize your log data with Grafana
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

Modern Grafana versions after 6.3 have built-in support for Grafana Loki and [LogQL]({{< relref "../query/_index.md" >}}).

## Using Explore

1. Log into your Grafana instance. If this is your first time running
   Grafana, the username and password are both defaulted to `admin`.
1. In Grafana, go to `Connections` > `Data Sources` via the cog icon on the
   left sidebar.
1. Click the big <kbd>+ Add a new data source</kbd> button.
1. Search for, or choose Loki from the list.
1. The http URL field should be the address of your Loki server. For example,
   when running locally or with Docker using port mapping, the address is
   likely `http://localhost:3100`. When running with docker-compose or
   Kubernetes, the address is likely `http://loki:3100`.\
   When running Grafana (with Docker) and trying to connect to a locally built Loki instance, the address (for the URL field) is:\
   On Mac: `docker.for.mac.localhost` \
   On Windows: `docker.for.win.localhost`
1. To see the logs, click <kbd>Explore</kbd> on the sidebar, select the Loki
   data source in the top-left dropdown, and then choose a log stream using the
   <kbd>Log labels</kbd> button.
1. Learn more about querying by reading about Loki's query language [LogQL]({{< relref "../query/_index.md" >}}).

If you would like to see an example of this live, you can try [Grafana Play's Explore feature](https://play.grafana.org/explore?schemaVersion=1&panes=%7B%22v1d%22:%7B%22datasource%22:%22ac4000ca-1959-45f5-aa45-2bd0898f7026%22,%22queries%22:%5B%7B%22refId%22:%22A%22,%22expr%22:%22%7Bagent%3D%5C%22promtail%5C%22%7D%20%7C%3D%20%60%60%22,%22queryType%22:%22range%22,%22datasource%22:%7B%22type%22:%22loki%22,%22uid%22:%22ac4000ca-1959-45f5-aa45-2bd0898f7026%22%7D,%22editorMode%22:%22builder%22%7D%5D,%22range%22:%7B%22from%22:%22now-1h%22,%22to%22:%22now%22%7D%7D%7D&orgId=1)

Read more about Grafana's Explore feature in the
[Grafana documentation](http://docs.grafana.org/features/explore) and on how to
search and filter for logs with Loki.

## Using Grafana Dashboards

Because Loki can be used as a built-in data source above, we can use LogQL queries based on that datasource
to build complex visualizations that persist on Grafana dashboards.

{{< docs/play title="Loki Example Grafana Dashboard" url="https://play.grafana.org/d/T512JVH7z/" >}}

Read more about how to build Grafana Dashboards in [build your first dashbboard](https://grafana.com/docs/grafana/latest/getting-started/build-first-dashboard/)

To configure Loki as a data source via provisioning, see [Configuring Grafana via
Provisioning](http://docs.grafana.org/features/datasources/loki/#configure-the-datasource-with-provisioning).
Set the URL in the provisioning.
