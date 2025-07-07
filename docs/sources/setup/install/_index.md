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

- [Install using Helm (recommended)](helm/)
- [Install using Tanka](tanka/)
- [Install using Docker or Docker Compose](docker/)
- [Install and run locally](local/)
- [Install from source](install-from-source/)

Alloy:
- [Install Alloy](https://grafana.com/docs/alloy/latest/set-up/install/)
- [Ingest Logs with Alloy](../../send-data/alloy/)

## General process

In order to run Loki, you must:

1. Download and install both Loki and Alloy.
1. Download config files for both programs.
1. Start Loki.
1. Update the Alloy config file to get your logs into Loki.
1. Start Alloy.
