# Loki in Grafana

Grafana ships with built-in support for Loki for versions greater than
[6.0](https://grafana.com/grafana/download/6.0.0). Using
[6.3](https://grafana.com/grafana/download/6.3.0) or later is highly
recommended to take advantage of new LogQL functionality.

1. Log into your Grafana instance. If this is your first time running
   Grafana, the username and password are both defaulted to `admin`.
2. In Grafana, go to `Configuration` > `Data Sources` via the cog icon on the
   left sidebar.
3. Click the big <kbd>+ Add data source</kbd> button.
4. Choose Loki from the list.
5. The http URL field should be the address of your Loki server. For example,
   when running locally or with Docker using port mapping, the address is
   likely `http://localhost:3100`. When running with docker-compose or
   Kubernetes, the address is likely `https://loki:3100`.
6. To see the logs, click <kbd>Explore</kbd> on the sidebar, select the Loki
   datasource in the top-left dropdown, and then choose a log stream using the
   <kbd>Log labels</kbd> button.

Read more about Grafana's Explore feature in the
[Grafana documentation](http://docs.grafana.org/features/explore) and on how to
search and filter for logs with Loki.

> To configure the datasource via provisioning, see [Configuring Grafana via
> Provisioning](http://docs.grafana.org/features/datasources/loki/#configure-the-datasource-with-provisioning)
> in the Grafana documentation and make sure to adjust the URL similarly as
> shown above.
