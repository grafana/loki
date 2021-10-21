---
title: Installation
weight: 200
---
# Installation

> **Note:** You can use [Grafana Cloud](https://grafana.com/products/cloud/features/#cloud-logs) to avoid installing, maintaining, and scaling your own instance of Grafana Loki. The free forever plan includes 50GB of free logs. [Create an account to get started](https://grafana.com/auth/sign-up/create-user?pg=docs-loki&plcmt=in-text).

## Installation methods

Instructions for different methods of installing Loki and Promtail.

- [Install using Tanka (recommended)](tanka/)
- [Install through Helm](helm/)
- [Install through Docker or Docker Compose](docker/)
- [Install and run locally](local/)
- [Install from source](install-from-source/)

## General process

In order to run Loki, you must:

1. Download and install both Loki and Promtail.
1. Download config files for both programs.
1. Start Loki.
1. Update the Promtail config file to get your logs into Loki.
1. Start Promtail.
