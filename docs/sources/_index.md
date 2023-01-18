---
title: Grafana Loki documentation
description: "Technical documentation for Grafana Loki"
aliases:
  - /docs/loki/
---

# Grafana Loki documentation

<p align="center"> <img src="logo_and_name.png" alt="Loki Logo"> <br>
  <small>Like Prometheus, but for logs!</small> </p>

Grafana Loki is a set of components that can be composed into a fully featured
logging stack.

Unlike other logging systems, Loki is built around the idea of only indexing
metadata about your logs: labels (just like Prometheus labels). Log data itself
is then compressed and stored in chunks in object stores such as S3 or GCS, or
even locally on the filesystem. A small index and highly compressed chunks
simplifies the operation and significantly lowers the cost of Loki.
