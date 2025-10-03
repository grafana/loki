---
title: "Gateway Configuration"
description: "Configure LokiStack Gateway external access and networking"
lead: ""
date: 2024-01-15T00:00:00+00:00
lastmod: 2024-01-15T00:00:00+00:00
draft: false
images: []
menu:
  docs:
    parent: "user-guides"
weight: 120
toc: true
---

This document describes how to configure the LokiStack Gateway component, including external access control.

## Overview

The LokiStack Gateway is a reverse-proxy component that provides secure access to Loki for multi-tenant authentication and authorization. By default, the operator creates external access resources to make the gateway accessible from outside the cluster:

- **OpenShift**: Creates Route objects
- **Kubernetes**: Creates Ingress objects

You can disable external access creation to implement custom networking solutions.

## External Access Configuration

### Default Behavior (External Access Enabled)

By default, external access resources are created automatically:

```yaml
apiVersion: loki.grafana.com/v1
kind: LokiStack
metadata:
  name: lokistack-dev
spec:
  size: 1x.small
  storage:
    schemas:
    - version: v13
      effectiveDate: "2022-06-01"
    secret:
      name: test
    type: s3
  storageClassName: gp3-csi
```

**Result:**
- **OpenShift**: Route object created for external access
- **Kubernetes**: Ingress object created for external access

### Disable External Access

To disable automatic creation of external access resources:

```yaml
apiVersion: loki.grafana.com/v1
kind: LokiStack
metadata:
  name: lokistack-dev
spec:
  ...
  template:
    gateway:
      externalAccess:
        disabled: true  # Disable external access resource creation
```

**Result:**
- **OpenShift**: No Route object created (existing Routes are automatically removed)
- **Kubernetes**: No Ingress object created (existing Ingress resources are automatically removed)
- Gateway remains accessible via internal Service

### Explicitly Enable External Access

You can explicitly enable external access (same as default behavior):

```yaml
apiVersion: loki.grafana.com/v1
kind: LokiStack
metadata:
  name: lokistack-dev
spec:
  ...
  template:
    gateway:
      externalAccess:
        disabled: false  # Explicitly enable external access
```

## Resource Cleanup Behavior

When you change the external access configuration from enabled to disabled:

1. **Automatic Cleanup**: The operator automatically removes existing external access resources
2. **Immediate Effect**: Changes take effect during the next reconciliation cycle
3. **No Manual Cleanup**: You don't need to manually delete Routes or Ingress resources
4. **Safe Deletion**: Only resources owned by the LokiStack are deleted (prevents accidental deletion of user-created resources)

## Related Documentation

- [Forwarding Logs to LokiStack](forwarding_logs_to_gateway.md) - How to send logs to the gateway
- [Forwarding Logs without the Gateway](forwarding_logs_without_gateway.md) - Bypass gateway authentication
- [How to Connect Grafana](howto_connect_grafana.md) - Connect Grafana to LokiStack
- [API Reference](../operator/api.md) - Complete API documentation
