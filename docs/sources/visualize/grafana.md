---
title: Visualize log data
menuTitle: Visualize
description: Visualize your log data with Grafana

aliases:
   - ../getting-started/grafana/
   - ../operations/grafana/
   - ../visualize/grafana/
weight: 825
keywords:
   - visualize
   - grafana
   - dashboards
---
# Visualize log data

[Grafana 6.0](/grafana/download/6.0.0) and more recent
versions have built-in support for Grafana Loki.
Use [Grafana 6.3](/grafana/download/6.3.0) or a more
recent version to take advantage of [LogQL]({{< relref "../query/_index.md" >}}) functionality.

1. Log into your Grafana instance. If this is your first time running
   Grafana, the username and password are both defaulted to `admin`.
1. In Grafana, go to `Configuration` > `Data Sources` via the cog icon on the
   left sidebar.
1. Click the big <kbd>+ Add data source</kbd> button.
1. Choose Loki from the list.
1. The http URL field should be the address of your Loki server. For example,
   when running locally or with Docker using port mapping, the address is
   likely `http://localhost:3100`. When running with docker-compose or
   Kubernetes, the address is likely `http://loki:3100`.
1. To see the logs, click <kbd>Explore</kbd> on the sidebar, select the Loki
   datasource in the top-left dropdown, and then choose a log stream using the
   <kbd>Log labels</kbd> button.
1. Learn more about querying by reading about Loki's query language [LogQL]({{< relref "../query/_index.md" >}}).

Read more about Grafana's Explore feature in the
[Grafana documentation](http://docs.grafana.org/features/explore) and on how to
search and filter for logs with Loki.

To configure Loki as a datasource via provisioning, see [Configuring Grafana via
Provisioning](http://docs.grafana.org/features/datasources/loki/#configure-the-datasource-with-provisioning).
Set the URL in the provisioning.
