---
title: Install Loki
menuTitle:  Install
description: Overview of methods for installing Loki.
aliases: 
 -  ../installation/
weight: 200
---

# Install Loki

There are several methods of installing Loki:

- [Install using Helm (recommended)](https://grafana.com/docs/loki/<LOKI_VERSION>/setup/install/helm/)
- [Install using Tanka](https://grafana.com/docs/loki/<LOKI_VERSION>/setup/install/tanka/)
- [Install using Docker or Docker Compose](https://grafana.com/docs/loki/<LOKI_VERSION>/setup/install/docker/)
- [Install and run locally](https://grafana.com/docs/loki/<LOKI_VERSION>/setup/install/local/)
- [Install from source](https://grafana.com/docs/loki/<LOKI_VERSION>/setup/install/install-from-source/)

Alloy:

- [Install Alloy](https://grafana.com/docs/alloy/latest/set-up/install/)
- [Ingest Logs with Alloy](https://grafana.com/docs/loki/<LOKI_VERSION>/send-data/alloy/)

## General process

In order to run Loki, you must:

1. Download and install both Loki and Alloy.
1. Download config files for both programs.
    {{< admonition type="note" >}}
    Grafana Loki does not come with any included authentication layer. You must run an authenticating reverse proxy in front of your services to prevent unauthorized access to Loki (for example, nginx). Refer to [Manage authentication](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/authentication/) for a list of open-source reverse proxies you can use.
    {{< /admonition >}}
1. Start Loki.
1. Update the Alloy config file to get your logs into Loki.
1. Start Alloy.
