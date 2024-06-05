---
title: Configure monitoring and alerting of Loki using Grafana Cloud
menuTitle: Monitor Loki with Grafana Cloud
description: Configuring monitoring and alerts for Loki using Grafana Cloud.
aliases:
  - ../../../../installation/helm/monitor-and-alert/with-grafana-cloud
weight: 200
keywords:
  - monitoring
  - alert
  - alerting
  - grafana cloud
---

# Configure monitoring and alerting of Loki using Grafana Cloud

This topic will walk you through using Grafana Cloud to monitor a Loki installation that is installed with the Helm chart. This approach leverages many of the chart's _self monitoring_ features, but instead of sending logs back to Loki itself, it sends them to a Grafana Cloud Logs instance. This approach also does not require the installation of the Prometheus Operator and instead sends metrics to a Grafana Cloud Metrics instance. Using Grafana Cloud to monitor Loki has the added benefit of being able to troubleshoot problems with Loki when the Helm installed Loki is down, as the logs will still be available in the Grafana Cloud Logs instance.

**Before you begin:**

- Helm 3 or above. See [Installing Helm](https://helm.sh/docs/intro/install/).
- A Grafana Cloud account and stack (including Cloud Grafana, Cloud Metrics, and Cloud Logs).
- A running Loki deployment installed in that Kubernetes cluster via the Helm chart.


** Grafana Cloud Connection Credentials:**

The meta-monitoring stack sends metrics, logs and traces to Grafana Cloud. To do this, connection Credentials need to be collected from Grafana Cloud. To do this, follow the steps below:

1. Create a new Cloud Access Policy in Grafana Cloud. This policy should have the following permissions:
   - Logs: Write
   - Metrics: Write
   - Traces: Write
  To do this sign into [Grafana Cloud](https://grafana.com/auth/sign-in/) and select  `Access Policies` located under `security` in the left-hand menu. Click on `Create access policy`; name it and select the permissions as described above. Then click `Create`.

1. Once the policy is created, click on the policy and then click on `Add token`. Name the token, select an expiration date and click `Create`. Copy the token to a secure location as it will not be displayed again.

1. Finaly collect the `Username / Instance ID` and `URL` for the following components in the Grafana Cloud stack:
   - **Logs (Loki):** Select `Send Logs`, copy down: `User` and `URL`. From the *Using Grafana with Logs* section.
   - **Metrics (Prometheus):** Select `Send Metrics`, copy down: `User` and `URL`. From the *Using a self-hosted Grafana instance with Grafana Cloud Metrics* section.
   - **Traces (OTLP):** Select `Configure`, copy down: `User` and `URL`. From the *Using Grafana with Traces* section.




