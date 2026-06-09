---
title: "Tenancy Modes"
description: "Configure multi-tenancy for LokiStack Gateway"
lead: ""
date: 2026-01-29T00:00:00+00:00
lastmod: 2026-01-29T00:00:00+00:00
draft: false
images: []
menu:
  docs:
    parent: "user-guides"
weight: 110
toc: true
---

This document describes the available tenancy modes for the LokiStack Gateway, their purposes, and how to configure each one.

## Overview

LokiStack Gateway is a component specific to LokiStack that handles authentication, authorization, and multi-tenant access to Loki. It ensures that different teams or applications can securely store and query their logs in isolation. Note that you can also run LokiStack without the Gateway; this is done through the `FeatureGates` configuration. The operator supports five tenancy modes:

| Mode | Authentication | Authorization | Use Case |
| ------ | --------------- | --------------- | ---------- |
| `static` | OIDC/mTLS | In-process OPA | Log store for clusters where the authorization scheme is static |
| `dynamic` | OIDC/mTLS | External OPA | Log store for clusters with dynamic/external authorization policies |
| `passthrough` | mTLS | None | Delegate authentication/authorization to an external component that forwards the `X-Scope-OrgID` header |
| `openshift-logging` | OpenShift OAuth | External OPA with OpenShift RBAC | Log Store for the OpenShift Logging Product |
| `openshift-network` | OpenShift OAuth | External OPA with OpenShift RBAC | Network traces store for the OpenShift Network Observability Product |

## Static Mode

**Mode:** `static`

Use this mode when you want to define tenants, roles, and role bindings statically in the LokiStack configuration. The authorization is handled by an in-process OpenPolicyAgent (OPA) Rego authorizer.

Features:

- OIDC or mTLS authentication
- Static role and role binding definitions
- In-process OPA authorization

### Example Configuration

```yaml
apiVersion: loki.grafana.com/v1
kind: LokiStack
metadata:
  name: lokistack-dev
  namespace: loki
spec:
  size: 1x.pico
  storage:
    schemas:
    - version: v13
      effectiveDate: 2023-10-15
    secret:
      name: loki-storage
      type: s3
  storageClassName: standard
  tenants:
    mode: static
    authentication:
    - tenantName: team-a
      tenantId: team-a
      oidc:
        issuerURL: https://auth.example.com
        redirectURL: https://loki.example.com/oidc/team-a/callback
        groupClaim: groups
        usernameClaim: email
        secret:
          name: team-a-oidc-secret
    authorization:
      roles:
      - name: read-write
        resources:
        - logs
        tenants:
        - team-a
        permissions:
        - read
        - write
      roleBindings:
      - name: team-a-binding
        roles:
        - read-write
        subjects:
        - kind: group
          name: team-a-developers
```

## Dynamic Mode

**Mode:** `dynamic`

Use this mode when you want to delegate authorization decisions to an external OPA-compatible endpoint. This allows for dynamic authorization policies without redeploying the gateway.

Features:

- OIDC or mTLS authentication
- External OPA endpoint for authorization

### Example Configuration

```yaml
apiVersion: loki.grafana.com/v1
kind: LokiStack
metadata:
  name: lokistack-dev
  namespace: loki
spec:
  size: 1x.pico
  storage:
    schemas:
    - version: v13
      effectiveDate: 2023-10-15
    secret:
      name: loki-storage
      type: s3
  storageClassName: standard
  tenants:
    mode: dynamic
    authentication:
    - tenantName: team-a
      tenantId: team-a
      oidc:
        issuerURL: https://auth.example.com
        redirectURL: https://loki.example.com/oidc/team-a/callback
        groupClaim: groups
        usernameClaim: email
        secret:
          name: team-a-oidc-secret
    authorization:
      opa:
        url: http://opa.loki.svc:8181/v1/data/loki/authz
```

## Passthrough Mode

**Mode:** `passthrough`

Use this mode when you want to delegate the authentication and authorization to an external component that will then forward the `X-Scope-OrgID` header for tenant identification. This mode can also be used with a `defaultTenant` if there is no concern for authorization.

Features:

- Delegates authentication and authorization to an external component
- mTLS-based client authentication
- Tenant ID from `X-Scope-OrgID` header
- Optional default tenant for requests without the header

### Example Configuration

```yaml
apiVersion: loki.grafana.com/v1
kind: LokiStack
metadata:
  name: lokistack-dev
  namespace: openshift-logging
spec:
  size: 1x.demo
  storage:
    schemas:
    - version: v13
      effectiveDate: 2023-10-15
    secret:
      name: loki-storage
      type: s3
  storageClassName: gp3-csi
  tenants:
    mode: passthrough
    passthrough:
      ca:
        secretName: client-ca-secret
        key: ca.crt
```

## OpenShift Logging Mode

**Mode:** `openshift-logging`

Use this mode when running the operator in the context of OpenShift Logging as the main store for logs. It provides pre-configured authentication and authorization using OpenShift's built-in OAuth server and RBAC.

Features:

- Seamless integration with OpenShift
- RBAC-based authorization using ClusterRoles and ClusterRoleBindings
- Pre-defined tenants: `application`, `infrastructure`, and `audit`
- Admin groups support for cluster-wide log access

### Example Configuration

```yaml
apiVersion: loki.grafana.com/v1
kind: LokiStack
metadata:
  name: lokistack-dev
  namespace: openshift-logging
spec:
  size: 1x.pico
  storage:
    schemas:
    - version: v13
      effectiveDate: 2023-10-15
    secret:
      name: loki-storage
      type: s3
  storageClassName: gp3-csi
  tenants:
    mode: openshift-logging
    openshift:
      adminGroups:
        - system:cluster-admins
        - cluster-admin
        - dedicated-admin
```

## OpenShift Network Mode

**Mode:** `openshift-network`

Use this mode when running the operator in the context of OpenShift Network Observability as the main store for network traces. It provides pre-configured authentication and authorization using OpenShift's built-in OAuth server and RBAC.

Features:

- Seamless integration with OpenShift
- RBAC-based authorization using ClusterRoles and ClusterRoleBindings
- Single tenant: `network`
- Admin groups support for cluster-wide log access

### Example Configuration

```yaml
apiVersion: loki.grafana.com/v1
kind: LokiStack
metadata:
  name: lokistack-network
  namespace: netobserv
spec:
  size: 1x.small
  storage:
    schemas:
    - version: v13
      effectiveDate: 2023-10-15
    secret:
      name: loki-storage
      type: s3
  storageClassName: gp3-csi
  tenants:
    mode: openshift-network
```

## Related Documentation

- [API Reference](../operator/api.md) - Complete API documentation
- [Gateway Configuration](gateway-config.md) - External access and ingress settings
- [Forwarding Logs to LokiStack](forwarding_logs_to_gateway.md) - Log forwarding examples
