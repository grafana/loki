---
title: Upgrade the Helm chart to 7.0
menuTitle: Upgrade the Helm chart to 7.0
description: Upgrade the Helm chart from 6.x to 7.0.
weight: 800
keywords:
  - upgrade
---

## Upgrading to v7.x

v7.x of this chart removed default podAffinity which is a breaking change from v6x.

### Changes

#### BREAKING: `podAntiAffinity` defaults were removed
Previously, almost all components had a podAntiAffinity, which has now been removed to avoid incorrect mergers with certain values.
In detail following components are effected. 
- table-manager
- ruler
- pattern-ingester
- bloom-compactor
- bloom-gateway
- compactor
- index-gateway
- query-scheduler
- query-frontend
- querier
- distributor
- ingester
- backend
- read
- write
- single-binary
- gateway 

To migrate from v6.x to v7.x, simply add the following values for the corresponding component.
(only applies if you have more than one replica)
```
  affinity:
    podAntiAffinity:
      requiredDuringSchedulingIgnoredDuringExecution:
        - labelSelector:
            matchLabels:
              app.kubernetes.io/component: table-manager
          topologyKey: kubernetes.io/hostname
```
