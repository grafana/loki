---
title: Grafana Loki
description: Grafana Loki is a set of open source components that can be composed into a fully featured logging stack.
aliases:
  - /docs/loki/
weight: 100
hero:
  title: Grafana Loki
  level: 1
  image: /media/docs/loki/logo-grafana-loki.png
  width: 110
  height: 110
  description: Grafana Loki is a set of open source components that can be composed into a fully featured logging stack. A small index and highly compressed chunks simplifies the operation and significantly lowers the cost of Loki.
cards:
  title_class: pt-0 lh-1
  items:
    - title: Learn about Loki
      href: /docs/loki/latest/get-started/
      description: Learn about the Loki architecture and components, the various deployment modes, and best practices for labels.
    - title: Set up Loki
      href: /docs/loki/latest/setup/
      description: View instructions for how to configure and install Loki, migrate from previous deployments, and upgrade your Loki environment.
    - title: Configure Loki
      href: /docs/loki/latest/configure/
      description: View the Loki configuration reference and configuration examples.
    - title: Send logs to Loki
      href: /docs/loki/latest/send-data/
      description: Select one or more clients to use to send your logs to Loki.
    - title: Manage Loki
      href: /docs/loki/latest/operations/
      description: Learn how to manage tenants, log ingestion, storage, queries, and more.
    - title: Query with LogQL
      href: /docs/loki/latest/query/
      description: Inspired by PromQL, LogQL is Grafana Loki’s query language. LogQL uses labels and operators for filtering.
---

{{< docs/hero-simple key="hero" >}}

---

## Overview

Unlike other logging systems, Loki is built around the idea of only indexing metadata about your logs' labels (just like Prometheus labels).
Log data itself is then compressed and stored in chunks in object stores such as Amazon Simple Storage Service (S3) or Google Cloud Storage (GCS), or even locally on the filesystem.  

## Explore

{{< card-grid key="cards" type="simple" >}}
