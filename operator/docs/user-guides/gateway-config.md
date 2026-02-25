---
title: "Gateway Configuration"
description: "Configure LokiStack Gateway"
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

This document describes how to configure the LokiStack Gateway component.

## Overview

The LokiStack Gateway is a reverse-proxy component that provides secure access to Loki for multi-tenant authentication and authorization. By default, the operator creates external access resources to make the gateway accessible from outside the cluster:

- **OpenShift**: Creates Route objects
- **Kubernetes**: Creates Ingress objects

You can disable external access creation.

## External Access Configuration

### Disable External Access

To disable automatic creation of external access resources:

```yaml
apiVersion: loki.grafana.com/v1
kind: LokiStack
metadata:
  name: lokistack-dev
spec:
  ...
  tenants:
    mode: static  # or openshift-logging, openshift-network, dynamic
    disableIngress: true  # Disable external access resource creation
```

**Result:**

- **OpenShift**: No Route object created (existing Routes are automatically removed)
- **Kubernetes**: No Ingress object created (existing Ingress resources are automatically removed)
- Gateway remains accessible via internal Service
- Gateway can still be exposed to external access by creating a custom resource.

### Explicitly Enable External Access

You can explicitly enable external access (same as default behavior):

```yaml
apiVersion: loki.grafana.com/v1
kind: LokiStack
metadata:
  name: lokistack-dev
spec:
  ...
  tenants:
    mode: static  # or openshift-logging, openshift-network, dynamic
    disableIngress: false  # Explicitly enable external access (default)
```

## Custom TLS Configuration

If the `httpEncryption` is enabled in the project config, users can provide a custom TLS certificate for the Gateway. If running on OpenShift this certificate will be used instead of the auto-generated certificates.

### Basic Custom Certificate Setup

```yaml
apiVersion: loki.grafana.com/v1
kind: LokiStack
metadata:
  name: lokistack-dev
spec:
  tenants:
    mode: static
    gateway:
      tls:
        certificate:
          secretName: my-tls-cert-secret
          key: tls.crt
        privateKey:
          secretName: my-tls-cert-secret
          key: tls.key
```

If necessary users can also pass a custom CA through the `ca` field:

```yaml
spec:
  tenants:
    mode: static
    gateway:
      tls:
        ca:
          configMapName: my-ca-bundle
          key: ca.crt
```

The CA Certificate is necessary for:

1. **Internal Healthcheck Verification**: The Gateway performs periodic self-checks by making HTTP requests to itself. When `httpEncryption` is enabled the gateway is running with TLS enabled, the healthcheck client needs to be able to verify the gateway TLS certificate.

2. **Metrics Scraping**: When `serviceMonitorTlsEndpoints` is enabled the CA is used in the Prometheus ServiceMonitor to allow secure scraping of Gateway metrics over HTTPS.

**Important:** When custom TLS is configured, OpenShift Routes automatically change to **passthrough** termination mode.

## Resource Cleanup Behavior

When you change the external access configuration from enabled to disabled:

1. **Automatic Cleanup**: The operator automatically removes existing external access resources
2. **Safe Deletion**: Only resources owned by the LokiStack are deleted (prevents accidental deletion of user-created resources)

## Related Documentation

- [API Reference](../operator/api.md) - Complete API documentation
- [How to Connect Grafana](howto_connect_grafana.md) - Connect Grafana to LokiStack
- [Forwarding Logs to LokiStack](forwarding_logs_to_gateway.md) - How to send logs to the gateway
- [Forwarding Logs without the Gateway](forwarding_logs_without_gateway.md) - Bypass gateway authentication
