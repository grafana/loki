---
title: Migrate to three scalable targets
menuTitle: Migrate to three targets
description: Migration guide for moving from two scalable to three scalable targets
aliases:
  - /docs/installation/helm/migrate-from-distributed
weight: 700
keywords:
  - migrate
  - ssd
  - scalable
  - simple
---

# Migrate to three scalable targets

This guide will walk you through migrating from the old, two target, scalable configuration to the new, three target, scalable configuration. This new configuration introduces a `backend` component, and reduces the `read` component to running just a `Querier` and `QueryFrontend`, allowing it to be run as a kubernetes `Deployment` rather than a `StatefulSet`.

**Before you begin:**

We recommend having a Grafana instance available to monitor both the existing and new clusters, to make sure there is no data loss during the migration process. The `loki` chart ships with self-monitoring features, including dashboards. These are useful for monitoring the health of the cluster during migration.

**To Migrate from a "read & write" to a "backend, read & write" deployment**

1. Make sure your deployment is using a new enough version of loki

This feature landed as an option in the helm chart while still in the `main` branch of Loki. As a result, depending on when you run this migration, you may neeed to manually override the Loki or GEL image being used to one that has the third, `backend` target available. For loki, add the following to your `values.yaml`.

```yaml
loki:
  image:
    repository: "grafana/loki"
    tag: "main-f5fbfab-amd64"
```

For GEL, you'll need to add:

```yaml
enterprise:
  image:
    repository: "grafana/enterprise-logs"
    tag: "main-96f32b9f"
```

1. Set the `legacyReadTarget` flag to false

Set the value `read.legacyReadTarget` to false. In your `values.yaml`, add:

```yaml
read:
  legacyReadTarget: false
```

1. Upgrade the helm installation

Run `helm upgrade` on your installation with your updated `values.yaml` file.
