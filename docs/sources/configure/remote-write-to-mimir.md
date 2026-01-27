---
title: Remote Write to Grafana Mimir
menuTitle: Remote Write
description: How to configure Loki to remote write metrics to Grafana Mimir
aliases: ["remote-write-mimir"]
weight: 510
---

# Remote Write to Grafana Mimir

This guide explains how to configure Loki to send metrics to Grafana Mimir using the **remote_write** configuration.

## Prerequisites

- A running Loki instance
- Access to a Grafana Mimir endpoint
- API token or credentials for Mimir
- Familiarity with Loki configuration files ([see Loki Configuration](../configure/))

## Loki Configuration

Edit your Loki configuration file (e.g., `loki-local-config.yaml`) and add a `remote_write` section:

```yaml
remote_write:
  - url: https://<MIMIR_ENDPOINT>/api/prom/push
    bearer_token: <YOUR_MIMIR_API_TOKEN>
    tenant_id: <YOUR_TENANT_ID> # Optional: only if using multi-tenancy
