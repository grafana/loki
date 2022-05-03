# loki-mixin

loki-mixin is a jsonnet library containing a set of loki monitoring dashboards, alerts and rules used on our production setup. 

## Dashboards

To test the dashboards against a local grafana & loki setup perform the following steps.

### Pre-requisites

* Grizzly is a tool for managing jsonnet dashboards in Grafana: https://github.com/grafana/grizzly.
To install it follow the instructions at: https://grafana.github.io/grizzly/installation/.

* Make sure you have the latest dependencies in the `vendor` directory by running the following command in `production/loki-mixin`:

```shell
jb install
```

* On your Grafana instance create an API key with role 'Admin' under Configuration > API keys. 
Copy this key for the next step.

### Testing dashboards

To test the dashboard in your local grafana instance, in directory `production/loki-mixin` run the command:

```shell
GRAFANA_URL=http://localhost:3000 GRAFANA_TOKEN=<API_KEY> grr watch ./ dashboards.libsonnet
```

`grr watch` will detect changes when you save files and try to add/update dashboards:

```shell
INFO[0000] Watching for changes
INFO[0005] Changes detected. Applyingdashboards.libsonnet
INFO[0007] Applying 10 resources
Dashboard.reads-resources added
Dashboard.writes-resources added
Dashboard.reads updated
Dashboard.writes updated
...
```

**Disclaimer:** Since these dashboards are used on our own production setup, these contain very specific configurations to our cloud environment which may be incorrect for other setups.
