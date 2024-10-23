---
title: Fluent Bit
menuTitle:  Fluent Bit
description: Provides instructions for how to install, configure, and use the Fluent Bit client to send logs to Loki.
aliases: 
- ../clients/fluentbit/
weight:  500
---
# Fluent Bit

[Fluent Bit](https://fluentbit.io/) is a fast, lightweight logs and metrics agent. It is a CNCF graduated sub-project under the umbrella of Fluentd. Fluent Bit is licensed under the terms of the Apache License v2.0.

When using Fluent Bit to ship logs to Loki, you can define which log files you want to collect using the [`Tail`](https://docs.fluentbit.io/manual/pipeline/inputs/tail) or [`Stdin`](https://docs.fluentbit.io/manual/pipeline/inputs/standard-input) data pipeline input. Additionally, Fluent Bit supports multiple `Filter` and `Parser` plugins (`Kubernetes`, `JSON`, etc.) to structure and alter log lines.

There are two Fluent Bit plugins for Loki: 

1. The integrated `loki` [plugin](https://grafana.com/docs/loki/<LOKI_VERSION>/send-data/fluentbit/fluent-bit-plugin/), which is officially maintained by the Fluent Bit project.
2. The `grafana-loki` [plugin](https://grafana.com/docs/loki/<LOKI_VERSION>/send-data/fluentbit/community-plugin/), an alternative community plugin by Grafana Labs.

We recommend using the `loki` plugin as this provides the most complete feature set and is actively maintained by the Fluent Bit project.

## Tutorial

To get started with the `loki` plugin, follow the [Sending logs to Loki using Fluent Bit tutorial](https://grafana.com/docs/loki/<LOKI_VERSION>/send-data/fluentbit/fluent-bit-loki-tutorial/). 
