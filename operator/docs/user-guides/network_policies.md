---
title: "Network Policies"
description: "Configure network policies for LokiStack deployments to enhance security through network segmentation"
lead: ""
date: 2025-01-05T15:00:00+00:00
lastmod: 2025-01-05T15:00:00+00:00
draft: false
images: []
menu:
  docs:
    parent: "user-guides"
weight: 100
toc: true
---

## Overview

Network policies provide network segmentation for your LokiStack deployment by controlling ingress and egress traffic between Loki components and external services. When enabled, the Loki Operator automatically creates Kubernetes NetworkPolicy resources that implement a "default deny" security model with explicit allow rules for required communications.

This guide covers how to configure and understand network policies for your LokiStack deployment.

### Platform-Specific Notes

#### Vanilla Kubernetes

On standard Kubernetes clusters:

- **Monitoring**: Open access for any Prometheus instance
- **DNS**: Support for kube-dns and CoreDNS (port 53)
- **AlertManager**: If AlertManager endpoint is configured in the RulerConfig resource then allow egress to the port specified in the endpoint URL. If no port is specified, defaults to 9093

#### OpenShift

Network policies on OpenShift include additional integrations:

- **Monitoring**: Automatic integration with OpenShift Monitoring stack
- **DNS**: Support for both standard and OpenShift DNS services (port 5353)
- **AlertManager**: Built-in access to cluster monitoring AlertManager

### Configuration

Network policies are configured through the `networkPolicies` field in your LokiStack specification:

```yaml
apiVersion: loki.grafana.com/v1
kind: LokiStack
metadata:
  name: lokistack-sample
  namespace: openshift-logging
spec:
  size: 1x.small
  storage:
    secret:
      name: lokistack-object-storage
      type: s3
  storageClassName: gp3-csi
  tenants:
    mode: openshift-logging
  networkPolicies:
    disabled: false  # Enable network policies
```

#### Configuration Options

| Configuration | Description | Behavior |
|---------------|-------------|----------|
| `networkPolicies: null` (omitted) | **Default** - inherits platform defaults | Enabled on OpenShift 4.20+, disabled elsewhere |
| `networkPolicies.disabled: true`  | **Disabled** - no network policies created | Full network access allowed |
| `networkPolicies.disabled: false` | **Enabled** - network policies enforced | Restricted network access with explicit allow rules |

## Generated Network Policies

When network policies are enabled, the Loki Operator creates several NetworkPolicy resources to secure different aspects of your LokiStack deployment:

| Policy Name | Purpose | Affected Components |
|-------------|---------|-------------------|
| `{name}-default-deny` | Baseline deny-all policy | All LokiStack pods |
| `{name}-loki-allow` | Inter-component communication | All Loki components |
| `{name}-loki-allow-bucket-egress` | Object storage access | ingester, querier, index-gateway, compactor, ruler |
| `{name}-loki-allow-gateway-ingress` | Gateway access to Loki components | distributor, query-frontend, ruler |
| `{name}-gateway-allow` | Gateway external & monitoring access | lokistack-gateway |
| `{name}-ruler-allow-alert-egress` | Ruler egress to AlertManager | ruler |
| `{name}-loki-allow-query-frontend` | Query frontend external access | query-frontend (OpenShift network mode) |

## Flow Matrix

### Ingress (Incoming Traffic)

| Component | From Gateway | From Components | From External | From Monitoring |
|-----------|--------------|-----------------|---------------|-----------------|
| distributor | ✅ | ✅ | ❌ | ✅ |
| ingester | ❌ | ✅ | ❌ | ✅ |
| querier | ❌ | ✅ | ❌ | ✅ |
| query-frontend | ✅ | ✅ | ✅* | ✅ |
| ruler | ✅ | ✅ | ❌ | ✅ |
| compactor | ❌ | ✅ | ❌ | ✅ |
| index-gateway | ❌ | ✅ | ❌ | ✅ |
| gateway | ❌ | ❌ | ✅ | ✅ |

*Only in OpenShift network mode

### Egress (Outgoing Traffic)

| Component | To Components | To Object Storage | To DNS | To AlertManager | To API Server |
|-----------|---------------|-------------------|--------|-----------------|---------------|
| distributor | ✅ | ❌ | ✅ | ❌ | ❌ |
| ingester | ✅ | ✅ | ✅ | ❌ | ❌ |
| querier | ✅ | ✅ | ✅ | ❌ | ❌ |
| query-frontend | ✅ | ❌ | ✅ | ❌ | ❌ |
| ruler | ✅ | ✅ | ✅ | ✅ | ❌ |
| compactor | ✅ | ✅ | ✅ | ❌ | ❌ |
| index-gateway | ✅ | ✅ | ✅ | ❌ | ❌ |
| gateway | ✅ | ❌ | ✅ | ❌ | ✅ |

## Integration with External Systems

For additional integrations (custom dashboards, external alerting), you may need to create supplementary NetworkPolicies. You can select specific components by using the label `app.kubernetes.io/component` you should always also include the labels `app.kubernetes.io/name=lokistack` and `app.kubernetes.io/instance={name}` to avoid collision with other pods deployed in the namespace.

```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: custom-dashboard-access
spec:
  podSelector:
    matchLabels:
      app.kubernetes.io/name: lokistack
      app.kubernetes.io/instance: lokistack-dev
      app.kubernetes.io/component: ruler
  policyTypes:
  - Egress
  egress:
  - to:
    - namespaceSelector:
        matchLabels:
          name: monitoring
    - podSelector:
        matchLabels:
          app: custom-alertmanager
    ports:
    - protocol: TCP
      port: 3100
```

## Conclusion

Network policies provide essential security controls for LokiStack deployments by implementing network segmentation and access controls. While they add a layer of complexity, the security benefits make them highly recommended for production environments.

The Loki Operator's network policies are designed to be secure by default while maintaining compatibility across diverse environments.
