---
title: Install dashboards, alerts, and recording rules
menuTitle: Install Mixins
description: Describes the Loki mixins, how to configure and install the dashboards, alerts, and recording rules.
weight: 100
---
# Install dashboards, alerts, and recording rules

Loki is instrumented to expose metrics about itself via the `/metrics` endpoint, designed to be scraped by Prometheus. Each Loki release includes a mixin. The Loki mixin provides a set of Grafana dashboards, Prometheus recording rules and alerts for monitoring Loki.

To set up monitoring using the mixin, you need to:

1. Deploy the Kubernetes Monitoring Helm chart. Follow the instructions in the [Deploy Loki Meta-monitoring](https://grafana.com/docs/loki/latest/operations/meta-monitoring/deploy) documentation.
1. Be actively storing metrics from your Loki cluster in Grafana Cloud or a separate LGTM stack.

This procedure assumes that you have set up Loki using the Helm chart.

{{< admonition type="note" >}}
Be sure to update the commands and configuration to match your own deployment.
{{< /admonition >}}

## Install Loki dashboards in Grafana

After Loki metrics are scraped by the Kubernetes Monitoring Helm chart and stored in a Prometheus compatible time-series database, you can monitor Loki’s operation using the Loki mixin.

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

1. Once you have a local copy of the repository, navigate to the `production/loki-mixin-compiled` directory.

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
1. Browse to `production/loki-mixin-compiled/dashboards` and select the dashboard to import. Or, drag the dashboard file, for example, `loki-operational.json`, onto the **Upload** area of the **Import dashboard** screen.
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

For microservices mode:

* `production/loki-mixin-compiled/alerts.yaml` (Optional)
* `production/loki-mixin-compiled/rules.yaml` (Required)

You use `mimirtool` to load the mixin alerts and rules definitions into a Prometheus instance, Mimir, or a Grafana Enterprise Metrics cluster. The following examples show how to load the mixin alerts and rules into a Grafana Cloud instance.

1. Download [mimirtool](https://github.com/grafana/mimir/releases).

1. Export the authentication credentials for connecting to your Grafana Cloud Mimir instance.

   ```bash
   export MIMIR_ADDRESS=<CLOUD-MIMIR-URL>
   export MIMIR_API_USER=<CLOUD-MIMIR-USER>
   export MIMIR_API_KEY=<CLOUD-MIMIR-API-KEY>
   export MIMIR_TENANT_ID=<CLOUD-MIMIR-USER> # Same as MIMIR_API_USER when using Grafana Cloud
   ```

   The best place to locate these credentials is to:
   1. Sign into [Grafana Cloud](https://grafana.com/auth/sign-in/) and create a new access policy.
       1. In the main menu, select **Security > Access Policies**.
       1. Click **Create access policy**.
       1. Give the policy a **Name** and select the following permissions:
          - Alerts: Write & Read
          - Rules: Write & Read
       1. Click **Create**.
       1. Click **Add Token**. Give the token a name and click **Create**.
   1. Collect `URL` and `user` for Prometheus
       1. Navigate to the Grafana Cloud Portal **Overview** page.
       1. Click the **Details** button for your Alerts instance.
       1. From the **Configuring your Alerting Stacks** section, collect the instance **User** and **URL** for **Metrics Authentication Settings**.

1. Using the same terminal we exported the Mimir environment variables into earlier, run the following command to load the recording rules:

    ```bash
    mimirtool rules load rules.yaml
    ```

1. (Optional) Load the alert rules:

    ```bash
    mimirtool rules load alerts.yaml
    ```

Refer to the [mimirtool](https://grafana.com/docs/mimir/latest/manage/tools/mimirtool/) documentation for more information.

## Next steps

After you have installed the Loki mixin dashboards, alerts, and recording rules, you can now monitor your production Loki cluster using Grafana. Make sure you review the mixins when you upgrade Loki to make sure you are using the latest version of the mixin.

You can now move onto:

* **Send Logs:** Ready to start sending your own logs to Loki, there a several methods you can use. For more information, refer to [send data](https://grafana.com/docs/loki/<LOKI_VERSION>/send-data/).
* **Query Logs:** LogQL is an extensive query language for logs and contains many tools to improve log retrival and generate insights. For more information see the [Query section](https://grafana.com/docs/loki/<LOKI_VERSION>/query/).
* **Alert:** Lastly you can use the ruler component of Loki to create alerts based on log queries. For more information refer to [Alerting](https://grafana.com/docs/loki/<LOKI_VERSION>/alert/).
