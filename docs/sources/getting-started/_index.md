---
title: Getting started
weight: 300
---
# Getting started with Grafana Loki

> **Note:** You can use [Grafana Cloud](https://grafana.com/products/cloud/features/#cloud-logs) to avoid installing, maintaining, and scaling your own instance of Grafana Loki. The free forever plan includes 50GB of free logs. [Create an account to get started](https://grafana.com/auth/sign-up/create-user?pg=docs-loki&plcmt=in-text).

This blah guides the reader through creating and using a simple Loki instance.
The Loki instance is intended for testing, development, and evaluation.
It would not meet most production requirements.

We use Docker compose to instantiate these services within separate
containers that facilitate communication: 

- One [single scalable deployment](../fundamentals/architecture/deployment-modes/) mode **Loki** instance has one read component and one write component:
    - Loki read
    - Loki write
- **Promtail** scrapes logs and pushes them to Loki.
- **Minio** is the storage back end.
- gateway??
- Grafana

Loki and Grafana each run in their own Docker containers.
This guide has three parts:
1. Instantiate the environment
2. Use Grafana to verify that the environment is running correctly.
3. Run an app that generates log lines.
4. Revise the Promtail configuration to scrape the app's log lines and push them to Loki.
5. Use Grafana to query Loki.
 
-----

1. [Getting Logs Into Loki](get-logs-into-loki/)
1. [Grafana](grafana/)
1. [LogCLI](logcli/)
1. [Labels](labels/)
1. [Troubleshooting](troubleshooting/)

